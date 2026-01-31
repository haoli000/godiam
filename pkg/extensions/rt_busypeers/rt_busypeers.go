// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_busypeers provides a routing extension that automatically retries
// requests when receiving DIAMETER_TOO_BUSY (3004) responses.
// This corresponds to rt_busypeers from the original freeDiameter.
package rt_busypeers

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

// retryRecord tracks retry state per EndToEndID.
type retryRecord struct {
	excluded  map[types.DiamID]struct{} // peers already tried
	origPeer  *peer.Peer                // peer that sent the original request
	origMsg   *message.Message          // original request (before relay modifications)
	createdAt time.Time
}

type rtBusypeers struct {
	router       *routing.Router
	maxRetries   int
	relayTimeout time.Duration

	mu      sync.Mutex
	retries map[types.EndToEndID]*retryRecord

	busyReceived atomic.Uint64
	retriesDone  atomic.Uint64
	retryFailed  atomic.Uint64
}

// Name returns the extension name.
func (e *rtBusypeers) Name() string { return "rt_busypeers" }

// Init initializes the extension.
func (e *rtBusypeers) Init(ctx extension.InitContext, config map[string]interface{}) error {
	e.router = ctx.GetRouter()
	e.retries = make(map[types.EndToEndID]*retryRecord)
	e.maxRetries = 3
	e.relayTimeout = 30 * time.Second

	if v, ok := config["retry_max_peers"]; ok {
		switch n := v.(type) {
		case int:
			e.maxRetries = n
		case float64:
			e.maxRetries = int(n)
		}
	}
	if v, ok := config["relay_timeout"]; ok {
		if s, ok := v.(string); ok {
			if d, err := time.ParseDuration(s); err == nil {
				e.relayTimeout = d
			}
		}
	}

	log.Printf("Initializing extension: rt_busypeers (max_retries=%d, timeout=%v)",
		e.maxRetries, e.relayTimeout)

	// Priority 20: runs early to intercept TOO_BUSY answers before other
	// handlers process them.
	ctx.GetRouter().RegisterInHandler("rt_busypeers", 20, e.handleRouting)
	return nil
}

// handleRouting intercepts DIAMETER_TOO_BUSY answers and re-routes the request.
func (e *rtBusypeers) handleRouting(p *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	// Only process answers.
	if msg.IsRequest() {
		return candidates, nil
	}

	rc, ok := msg.GetResultCode()
	if !ok || rc != types.ResultTooBusy {
		return candidates, nil
	}

	e.busyReceived.Add(1)

	// Check if we have retry state for this E2E ID.
	e.mu.Lock()
	rec, exists := e.retries[msg.EndToEndID]
	if !exists {
		// First TOO_BUSY for this request — nothing we can retry here since we
		// don't have the original request stored. This extension works best when
		// the relay path stores the original request for re-routing.
		// For now, log and pass through.
		e.mu.Unlock()
		log.Printf("rt_busypeers: TOO_BUSY received for e2e=0x%08x but no retry record (first hop)", msg.EndToEndID)
		return candidates, nil
	}

	// Check if we've exhausted retries.
	if len(rec.excluded) >= e.maxRetries {
		delete(e.retries, msg.EndToEndID)
		e.mu.Unlock()
		e.retryFailed.Add(1)
		log.Printf("rt_busypeers: max retries reached for e2e=0x%08x, passing TOO_BUSY through", msg.EndToEndID)
		return candidates, nil
	}

	// Check timeout
	if time.Since(rec.createdAt) > e.relayTimeout {
		delete(e.retries, msg.EndToEndID)
		e.mu.Unlock()
		e.retryFailed.Add(1)
		log.Printf("rt_busypeers: timeout for e2e=0x%08x, passing TOO_BUSY through", msg.EndToEndID)
		return candidates, nil
	}

	// Exclude the peer that returned TOO_BUSY.
	if p != nil {
		rec.excluded[p.DiameterID()] = struct{}{}
	}
	e.mu.Unlock()

	// Attempt to re-route.
	nextHop, err := e.router.RouteOutWithExclusion(rec.origMsg, rec.excluded)
	if err != nil {
		e.mu.Lock()
		delete(e.retries, msg.EndToEndID)
		e.mu.Unlock()
		e.retryFailed.Add(1)
		log.Printf("rt_busypeers: no alternate peer for e2e=0x%08x: %v", msg.EndToEndID, err)
		return candidates, nil
	}

	// Look up the peer and send.
	peerObj, ok := e.router.LookupPeer(nextHop)
	if !ok {
		e.retryFailed.Add(1)
		return candidates, nil
	}

	if sender, ok := peerObj.(interface{ Send(*message.Message) error }); ok {
		if err := sender.Send(rec.origMsg); err != nil {
			e.retryFailed.Add(1)
			log.Printf("rt_busypeers: retry send failed for e2e=0x%08x: %v", msg.EndToEndID, err)
			return candidates, nil
		}
		e.retriesDone.Add(1)
		log.Printf("rt_busypeers: retried e2e=0x%08x to peer %s", msg.EndToEndID, nextHop)
		// Consume the TOO_BUSY answer — don't pass it upstream.
		return nil, routing.ErrConsumed
	}

	return candidates, nil
}

// TrackRequest should be called by the relay path to enable retry.
// It stores the original request so it can be re-sent if TOO_BUSY is received.
func (e *rtBusypeers) TrackRequest(origPeer *peer.Peer, msg *message.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retries[msg.EndToEndID] = &retryRecord{
		excluded:  make(map[types.DiamID]struct{}),
		origPeer:  origPeer,
		origMsg:   msg,
		createdAt: time.Now(),
	}
}

// Stop implements the Stoppable interface.
func (e *rtBusypeers) Stop() error {
	e.mu.Lock()
	e.retries = make(map[types.EndToEndID]*retryRecord)
	e.mu.Unlock()
	return nil
}

// Metrics implements extension.MetricsProvider.
func (e *rtBusypeers) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "busy_received_total",
			Help:  "Total DIAMETER_TOO_BUSY answers received.",
			Type:  extension.MetricCounter,
			Value: float64(e.busyReceived.Load()),
		},
		{
			Name:  "retries_done_total",
			Help:  "Total successful retries to alternate peers.",
			Type:  extension.MetricCounter,
			Value: float64(e.retriesDone.Load()),
		},
		{
			Name:  "retry_failed_total",
			Help:  "Total retry failures (no alternate peer or max retries reached).",
			Type:  extension.MetricCounter,
			Value: float64(e.retryFailed.Load()),
		},
	}
}

func init() {
	extension.Register(&rtBusypeers{})
}
