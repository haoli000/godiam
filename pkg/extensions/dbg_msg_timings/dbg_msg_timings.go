// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package dbg_msg_timings provides a debugging extension that tracks
// request-to-answer latency for Diameter messages.
// This corresponds to dbg_msg_timings from the original freeDiameter.
package dbg_msg_timings

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

type dbgMsgTimings struct {
	logThreshold time.Duration

	// Track outstanding requests by EndToEndID
	mu       sync.Mutex
	inflight map[types.EndToEndID]time.Time

	// Metrics
	requestsSeen   atomic.Uint64
	answersMatched atomic.Uint64
	totalLatencyUs atomic.Int64 // sum of latencies in microseconds for average
	maxLatencyUs   atomic.Int64
}

// Name returns the extension name.
func (e *dbgMsgTimings) Name() string { return "dbg_msg_timings" }

// Init initializes the extension.
func (e *dbgMsgTimings) Init(ctx extension.InitContext, config map[string]interface{}) error {
	e.inflight = make(map[types.EndToEndID]time.Time)
	e.logThreshold = 100 * time.Millisecond

	if v, ok := config["log_threshold"]; ok {
		if s, ok := v.(string); ok {
			if d, err := time.ParseDuration(s); err == nil {
				e.logThreshold = d
			}
		}
	}

	log.Printf("Initializing extension: dbg_msg_timings (threshold=%v)", e.logThreshold)

	// Priority 5: run very early to capture timestamps before any processing.
	ctx.GetRouter().RegisterInHandler("dbg_msg_timings", 5, e.handleRouting)
	return nil
}

// handleRouting records timestamps for requests and calculates latency for answers.
func (e *dbgMsgTimings) handleRouting(_ *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	now := time.Now()

	if msg.IsRequest() {
		// Record when we first saw this request.
		e.mu.Lock()
		e.inflight[msg.EndToEndID] = now
		e.mu.Unlock()
		e.requestsSeen.Add(1)

		// Prune stale entries periodically (every 1000 requests).
		if e.requestsSeen.Load()%1000 == 0 {
			e.pruneStale(now)
		}
	} else {
		// This is an answer; look up the matching request.
		e.mu.Lock()
		reqTime, ok := e.inflight[msg.EndToEndID]
		if ok {
			delete(e.inflight, msg.EndToEndID)
		}
		e.mu.Unlock()

		if ok {
			latency := now.Sub(reqTime)
			latencyUs := latency.Microseconds()
			e.answersMatched.Add(1)
			e.totalLatencyUs.Add(latencyUs)

			// Update max using CAS loop.
			for {
				cur := e.maxLatencyUs.Load()
				if latencyUs <= cur {
					break
				}
				if e.maxLatencyUs.CompareAndSwap(cur, latencyUs) {
					break
				}
			}

			if latency >= e.logThreshold {
				rc, _ := msg.GetResultCode()
				log.Printf("dbg_msg_timings: cmd=%d e2e=0x%08x latency=%v result=%d",
					msg.CommandCode, msg.EndToEndID, latency, rc)
			}
		}
	}

	return candidates, nil
}

// pruneStale removes request entries older than 5 minutes to avoid memory leaks.
func (e *dbgMsgTimings) pruneStale(now time.Time) {
	cutoff := now.Add(-5 * time.Minute)
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, t := range e.inflight {
		if t.Before(cutoff) {
			delete(e.inflight, id)
		}
	}
}

// Stop implements the Stoppable interface.
func (e *dbgMsgTimings) Stop() error {
	e.mu.Lock()
	e.inflight = make(map[types.EndToEndID]time.Time)
	e.mu.Unlock()
	return nil
}

// Metrics implements extension.MetricsProvider.
func (e *dbgMsgTimings) Metrics() []extension.Metric {
	matched := e.answersMatched.Load()
	var avgUs float64
	if matched > 0 {
		avgUs = float64(e.totalLatencyUs.Load()) / float64(matched)
	}

	return []extension.Metric{
		{
			Name:  "requests_tracked_total",
			Help:  "Total requests seen and tracked for timing.",
			Type:  extension.MetricCounter,
			Value: float64(e.requestsSeen.Load()),
		},
		{
			Name:  "answers_matched_total",
			Help:  "Total answers matched with their corresponding requests.",
			Type:  extension.MetricCounter,
			Value: float64(matched),
		},
		{
			Name:  "avg_latency_us",
			Help:  "Average request-to-answer latency in microseconds.",
			Type:  extension.MetricGauge,
			Value: avgUs,
		},
		{
			Name:  "max_latency_us",
			Help:  "Maximum observed request-to-answer latency in microseconds.",
			Type:  extension.MetricGauge,
			Value: float64(e.maxLatencyUs.Load()),
		},
	}
}

func init() {
	extension.Register(&dbgMsgTimings{})
}
