// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package fifo_stats provides a debugging extension that tracks queue depth
// statistics for Diameter message processing, including current length,
// high-water marks, and average time-in-queue.
// This corresponds to libfdcore/fifo_stats.c from the original freeDiameter.
package fifo_stats

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// peerQueueStats tracks per-peer queue depth observations.
type peerQueueStats struct {
	currentDepth atomic.Int64
	highWater    atomic.Int64
	totalEnqueue atomic.Uint64
	totalDequeue atomic.Uint64
}

// updateHighWater atomically updates the high-water mark if depth is higher.
func (s *peerQueueStats) updateHighWater(depth int64) {
	for {
		cur := s.highWater.Load()
		if depth <= cur {
			return
		}
		if s.highWater.CompareAndSwap(cur, depth) {
			return
		}
	}
}

type fifoStats struct {
	router        *routing.Router
	peerEnumerate func() []*peer.Peer

	// Per-peer queue stats keyed by DiameterIdentity
	mu       sync.RWMutex
	peerData map[types.DiamID]*peerQueueStats

	// Time-in-queue tracking (request ingress → answer egress)
	inflightMu sync.Mutex
	inflight   map[types.EndToEndID]time.Time

	// Global counters
	messagesIn     atomic.Uint64
	messagesOut    atomic.Uint64
	totalQueueUs   atomic.Int64 // sum of queue times in microseconds
	matchedAnswers atomic.Uint64
	maxQueueUs     atomic.Int64
}

// Name returns the extension name.
func (e *fifoStats) Name() string { return "fifo_stats" }

// Init initializes the extension.
func (e *fifoStats) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	e.router = ctx.GetRouter()
	e.peerEnumerate = func() []*peer.Peer { return ctx.GetPeers() }
	e.peerData = make(map[types.DiamID]*peerQueueStats)
	e.inflight = make(map[types.EndToEndID]time.Time)

	log.Printf("Initializing extension: fifo_stats")

	// Priority 4: run before dbg_msg_timings (5) to capture queue entry timestamps.
	ctx.GetRouter().RegisterInHandler("fifo_stats", 4, e.handleIncoming)
	return nil
}

func (e *fifoStats) getOrCreatePeerStats(id types.DiamID) *peerQueueStats {
	e.mu.RLock()
	ps, ok := e.peerData[id]
	e.mu.RUnlock()
	if ok {
		return ps
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	// Double-check after write lock
	if ps, ok = e.peerData[id]; ok {
		return ps
	}
	ps = &peerQueueStats{}
	e.peerData[id] = ps
	return ps
}

// peerIdentity returns the best available identity for a peer: the negotiated
// DiameterID if available, otherwise the configured DiameterIdentity.
func peerIdentity(p *peer.Peer) types.DiamID {
	if id := p.DiameterID(); id != "" {
		return id
	}
	return p.Config().DiameterIdentity
}

// handleIncoming tracks message ingress/egress and per-peer queue depth.
func (e *fifoStats) handleIncoming(p *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	now := time.Now()

	if msg.IsRequest() {
		e.messagesIn.Add(1)

		// Track per-peer queue depth
		if p != nil {
			ps := e.getOrCreatePeerStats(peerIdentity(p))
			depth := ps.currentDepth.Add(1)
			ps.totalEnqueue.Add(1)
			ps.updateHighWater(depth)
		}

		// Record ingress time for time-in-queue calculation
		e.inflightMu.Lock()
		e.inflight[msg.EndToEndID] = now
		e.inflightMu.Unlock()

		// Periodic pruning of stale entries
		if e.messagesIn.Load()%1000 == 0 {
			e.pruneStale(now)
		}
	} else {
		e.messagesOut.Add(1)

		// Decrease per-peer queue depth
		if p != nil {
			ps := e.getOrCreatePeerStats(peerIdentity(p))
			ps.currentDepth.Add(-1)
			ps.totalDequeue.Add(1)
		}

		// Calculate time-in-queue
		e.inflightMu.Lock()
		ingressTime, ok := e.inflight[msg.EndToEndID]
		if ok {
			delete(e.inflight, msg.EndToEndID)
		}
		e.inflightMu.Unlock()

		if ok {
			queueTime := now.Sub(ingressTime)
			queueUs := queueTime.Microseconds()
			e.matchedAnswers.Add(1)
			e.totalQueueUs.Add(queueUs)

			// Update max using CAS loop
			for {
				cur := e.maxQueueUs.Load()
				if queueUs <= cur {
					break
				}
				if e.maxQueueUs.CompareAndSwap(cur, queueUs) {
					break
				}
			}
		}
	}

	return candidates, nil
}

// pruneStale removes request entries older than 5 minutes.
func (e *fifoStats) pruneStale(now time.Time) {
	cutoff := now.Add(-5 * time.Minute)
	e.inflightMu.Lock()
	defer e.inflightMu.Unlock()
	for id, t := range e.inflight {
		if t.Before(cutoff) {
			delete(e.inflight, id)
		}
	}
}

// Stop implements the Stoppable interface.
func (e *fifoStats) Stop() error {
	e.inflightMu.Lock()
	e.inflight = make(map[types.EndToEndID]time.Time)
	e.inflightMu.Unlock()

	e.mu.Lock()
	e.peerData = make(map[types.DiamID]*peerQueueStats)
	e.mu.Unlock()
	return nil
}

// Metrics implements extension.MetricsProvider.
func (e *fifoStats) Metrics() []extension.Metric {
	matched := e.matchedAnswers.Load()
	var avgQueueUs float64
	if matched > 0 {
		avgQueueUs = float64(e.totalQueueUs.Load()) / float64(matched)
	}

	metrics := []extension.Metric{
		{
			Name:  "messages_enqueued_total",
			Help:  "Total requests entering the queue.",
			Type:  extension.MetricCounter,
			Value: float64(e.messagesIn.Load()),
		},
		{
			Name:  "messages_dequeued_total",
			Help:  "Total answers leaving the queue.",
			Type:  extension.MetricCounter,
			Value: float64(e.messagesOut.Load()),
		},
		{
			Name:  "avg_time_in_queue_us",
			Help:  "Average time-in-queue in microseconds.",
			Type:  extension.MetricGauge,
			Value: avgQueueUs,
		},
		{
			Name:  "max_time_in_queue_us",
			Help:  "Maximum time-in-queue in microseconds.",
			Type:  extension.MetricGauge,
			Value: float64(e.maxQueueUs.Load()),
		},
	}

	// Add per-peer metrics
	e.mu.RLock()
	defer e.mu.RUnlock()
	for id, ps := range e.peerData {
		labels := map[string]string{"peer": string(id)}
		metrics = append(metrics,
			extension.Metric{
				Name:   "peer_queue_depth",
				Help:   "Current queue depth for peer.",
				Type:   extension.MetricGauge,
				Value:  float64(ps.currentDepth.Load()),
				Labels: labels,
			},
			extension.Metric{
				Name:   "peer_queue_high_water",
				Help:   "High-water mark for peer queue depth.",
				Type:   extension.MetricGauge,
				Value:  float64(ps.highWater.Load()),
				Labels: labels,
			},
		)
	}

	return metrics
}

func init() {
	extension.Register(&fifoStats{})
}
