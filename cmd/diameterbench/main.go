// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package main provides the diameterbench command for benchmarking Diameter servers.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

var (
	serverHost  = flag.String("host", "127.0.0.1", "Server hostname or IP")
	serverPort  = flag.Int("port", 3868, "Server port")
	serverID    = flag.String("id", "server.example.com", "Server Diameter Identity")
	realm       = flag.String("realm", "example.com", "Diameter Realm")
	connections = flag.Int("conn", 10, "Number of concurrent connections")
	duration    = flag.Duration("duration", 10*time.Second, "Test duration")
	rate        = flag.Int("rate", 1000, "Target requests per second (total)")
	useTLS      = flag.Bool("tls", false, "Use TLS for connections")
	network     = flag.String("network", "tcp", "Transport protocol: tcp or sctp")

	connectInterval = flag.Duration("connect-interval", 10*time.Millisecond, "Delay between starting each worker")
	connectRetries  = flag.Int("connect-retries", 3, "Max retry attempts per worker")
	connectTimeout  = flag.Duration("connect-timeout", 10*time.Second, "Per-attempt connect timeout")
)

type metrics struct {
	requests  uint64
	answers   uint64
	latencies []time.Duration
	mu        sync.Mutex
}

func main() {
	flag.Parse()

	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()
	_ = dict.LoadCreditControl()

	m := &metrics{
		latencies: make([]time.Duration, 0, *rate*int(duration.Seconds())),
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	start := make(chan struct{})

	log.Printf("Starting benchmark: %d conns, %d RPS, %v duration, transport=%s", *connections, *rate, *duration, *network)

	// Phase 1: Connect all workers (staggered to avoid thundering herd)
	var ready atomic.Int32
	for i := 0; i < *connections; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runWorker(id, dict, m, start, stop, &ready)
		}(i)
		if i < *connections-1 && *connectInterval > 0 {
			time.Sleep(*connectInterval)
		}
	}

	// Wait for all workers to connect (auto-computed deadline)
	maxAttemptTime := time.Duration(*connectRetries+1) * *connectTimeout
	staggerTime := time.Duration(*connections) * *connectInterval
	connectDeadline := time.After(maxAttemptTime + staggerTime + 5*time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
waitLoop:
	for {
		select {
		case <-tick.C:
			n := ready.Load()
			if int(n) >= *connections {
				break waitLoop
			}
		case <-connectDeadline:
			log.Printf("Connect phase done: %d/%d workers connected", ready.Load(), *connections)
			break waitLoop
		}
	}

	connected := ready.Load()
	if connected == 0 {
		tick.Stop()
		log.Fatal("No workers connected, aborting")
	}
	log.Printf("All %d workers connected, starting traffic", connected)

	// Phase 2: Start traffic
	close(start)

	timer := time.NewTimer(*duration)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-timer.C:
		log.Println("Duration reached")
	case <-sigChan:
		log.Println("Interrupted")
	}

	close(stop)
	wg.Wait()

	reportResults(m)
}

// connectWithRetry attempts to establish a Diameter peer connection with retry logic.
// On failure (peer reaches StateZombie), it creates a new peer and retries.
func connectWithRetry(id int, dict *dictionary.Dictionary, stop <-chan struct{}) (*peer.Peer, error) {
	for attempt := 0; attempt <= *connectRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 500 * time.Millisecond
			log.Printf("Worker %d retry %d/%d (backoff %v)", id, attempt, *connectRetries, backoff)
			select {
			case <-time.After(backoff):
			case <-stop:
				return nil, fmt.Errorf("stopped")
			}
		}

		clientCfg := peer.Config{
			DiameterIdentity: types.DiamID(*serverID),
			Realm:            types.DiamID(*realm),
			Addresses:        []string{*serverHost},
			Port:             *serverPort,
			Network:          *network,
			ConnectTimeout:   *connectTimeout,
			WatchdogInterval: 0, // Disable for bench
			UseTLS:           *useTLS,
		}
		if *useTLS {
			clientCfg.TLSConfig = &peer.TLSConfig{
				SkipVerify: true,
			}
		}

		p := peer.New(clientCfg, dict)
		p.SetLocalOverride(peer.LocalConfig{
			DiameterIdentity: types.DiamID(fmt.Sprintf("bench-client-%d.example.com", id)),
			Realm:            types.DiamID(*realm),
			HostIPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
			AuthAppIDs:       []types.ApplicationID{types.AppIDCreditControl},
		})

		connected := make(chan struct{}, 1)
		failed := make(chan struct{}, 1)

		p.SetOnStateChange(func(peerConn *peer.Peer, old, newState peer.State) {
			log.Printf("Worker %d state change: %s -> %s", id, old, newState)
			if peerConn.IsOpen() {
				select {
				case connected <- struct{}{}:
				default:
				}
			}
			if newState == peer.StateZombie {
				select {
				case failed <- struct{}{}:
				default:
				}
			}
		})

		if err := p.Start(); err != nil {
			log.Printf("Worker %d failed to start: %v", id, err)
			continue
		}

		select {
		case <-connected:
			return p, nil
		case <-failed:
			p.Stop()
			continue
		case <-stop:
			p.Stop()
			return nil, fmt.Errorf("stopped")
		}
	}
	return nil, fmt.Errorf("all %d retries exhausted", *connectRetries)
}

func runWorker(id int, dict *dictionary.Dictionary, m *metrics, start <-chan struct{}, stop <-chan struct{}, ready *atomic.Int32) {
	p, err := connectWithRetry(id, dict, stop)
	if err != nil {
		log.Printf("Worker %d timed out connecting", id)
		return
	}
	defer p.Stop()

	ready.Add(1)

	// Wait for all workers to be connected before sending traffic
	select {
	case <-start:
	case <-stop:
		return
	}

	// Response tracker
	pending := make(map[types.HopByHopID]time.Time)
	var pendMu sync.Mutex

	p.SetOnMessage(func(_ *peer.Peer, msg *message.Message) {
		if !msg.IsRequest() {
			pendMu.Lock()
			start, ok := pending[msg.HopByHopID]
			if ok {
				delete(pending, msg.HopByHopID)
				latency := time.Since(start)
				m.mu.Lock()
				m.latencies = append(m.latencies, latency)
				m.mu.Unlock()
				atomic.AddUint64(&m.answers, 1)
			}
			pendMu.Unlock()
		}
	})

	// Rate limiter (naive)
	interval := time.Duration(float64(time.Second) / (float64(*rate) / float64(*connections)))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
			msg.HopByHopID = p.NextHopByHopID()
			msg.EndToEndID = types.EndToEndID(msg.HopByHopID)

			// Minimal AVPs
			msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, types.DiamID(fmt.Sprintf("bench-%d-%d", id, msg.HopByHopID))))
			msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, p.LocalConfig().DiameterIdentity))
			msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, p.LocalConfig().Realm))
			msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, types.DiamID(*realm)))
			msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppIDCreditControl)))
			msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeCCRequestType, types.AVPFlagMandatory, 1))
			msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeCCRequestNumber, types.AVPFlagMandatory, 0))

			pendMu.Lock()
			pending[msg.HopByHopID] = time.Now()
			pendMu.Unlock()

			if err := p.Send(msg); err != nil {
				log.Printf("Worker %d send failed: %v", id, err)
				return
			}
			atomic.AddUint64(&m.requests, 1)
		}
	}
}

func reportResults(m *metrics) {
	fmt.Println("\n--- Benchmark Results ---")
	fmt.Printf("Total Requests Sent: %d\n", m.requests)
	fmt.Printf("Total Answers Recv: %d\n", m.answers)

	if len(m.latencies) == 0 {
		fmt.Println("No latencies recorded")
		return
	}

	sort.Slice(m.latencies, func(i, j int) bool {
		return m.latencies[i] < m.latencies[j]
	})

	total := time.Duration(0)
	for _, l := range m.latencies {
		total += l
	}

	fmt.Printf("Throughput: %.2f req/sec\n", float64(m.answers)/duration.Seconds())
	fmt.Printf("Avg Latency: %v\n", total/time.Duration(len(m.latencies)))
	fmt.Printf("Min Latency: %v\n", m.latencies[0])
	fmt.Printf("Max Latency: %v\n", m.latencies[len(m.latencies)-1])
	fmt.Printf("P50 Latency: %v\n", m.latencies[len(m.latencies)/2])
	fmt.Printf("P95 Latency: %v\n", m.latencies[int(float64(len(m.latencies))*0.95)])
	fmt.Printf("P99 Latency: %v\n", m.latencies[int(float64(len(m.latencies))*0.99)])
}
