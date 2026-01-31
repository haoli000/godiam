// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_load_balance provides a routing extension that selects the least-loaded
// peer among those with the highest routing score.
// This corresponds to rt_load_balance from the original freeDiameter.
package rt_load_balance

import (
	"log"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type rtLoadBalance struct {
	peerEnumerate func() []*peer.Peer
	rebalanced    atomic.Uint64
}

// Name returns the extension name.
func (e *rtLoadBalance) Name() string { return "rt_load_balance" }

// Init initializes the extension.
func (e *rtLoadBalance) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: rt_load_balance")

	router := ctx.GetRouter()
	e.peerEnumerate = func() []*peer.Peer { return ctx.GetPeers() }

	// Priority 60: runs after default scoring (buildCandidates) and rewrite
	// extensions, but before final selection.
	router.RegisterOutHandler("rt_load_balance", 60, e.handleOutgoing)
	return nil
}

// handleOutgoing adjusts scores so the least-loaded peer in the top tier wins.
func (e *rtLoadBalance) handleOutgoing(msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if len(candidates) <= 1 {
		return candidates, nil
	}

	// Build a pending-count lookup from live peers.
	pending := make(map[types.DiamID]int)
	for _, p := range e.peerEnumerate() {
		id := p.DiameterID()
		if id == "" {
			id = p.Config().DiameterIdentity
		}
		pending[id] = p.PendingCount()
	}

	// Find the highest score.
	maxScore := candidates[0].Score
	for _, c := range candidates[1:] {
		if c.Score > maxScore {
			maxScore = c.Score
		}
	}

	// Among candidates with the highest score, give a bonus to the one with
	// the fewest pending requests so it sorts first.
	// The bonus is small (+1) — just enough to break the tie.
	minPending := -1
	for _, c := range candidates {
		if c.Score == maxScore {
			pc := pending[c.PeerIdentity]
			if minPending < 0 || pc < minPending {
				minPending = pc
			}
		}
	}

	modified := false
	for i := range candidates {
		if candidates[i].Score == maxScore && pending[candidates[i].PeerIdentity] == minPending {
			candidates[i].Score++
			candidates[i].Reason = "rt_load_balance: least-loaded peer"
			modified = true
			break // boost only one peer
		}
	}
	if modified {
		e.rebalanced.Add(1)
	}

	return candidates, nil
}

// Stop implements the Stoppable interface.
func (e *rtLoadBalance) Stop() error { return nil }

// Metrics implements extension.MetricsProvider.
func (e *rtLoadBalance) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "rebalanced_total",
			Help:  "Total routing decisions adjusted by load balancing.",
			Type:  extension.MetricCounter,
			Value: float64(e.rebalanced.Load()),
		},
	}
}

func init() {
	extension.Register(&rtLoadBalance{})
}
