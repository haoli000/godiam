// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_randomize provides a routing extension that adds random jitter
// to candidate scores for better load distribution among equal-weight peers.
// This corresponds to rt_randomize from the original freeDiameter.
package rt_randomize

import (
	"log"
	"math/rand"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
)

type rtRandomize struct {
	randomized atomic.Uint64
}

// Name returns the extension name.
func (e *rtRandomize) Name() string { return "rt_randomize" }

// Init initializes the extension.
func (e *rtRandomize) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: rt_randomize")

	// Priority 70: runs after load balancing and other score adjustments,
	// just before final selection. Adds small random jitter to break ties.
	ctx.GetRouter().RegisterOutHandler("rt_randomize", 70, e.handleOutgoing)
	return nil
}

// handleOutgoing adds a small random score component to each candidate.
func (e *rtRandomize) handleOutgoing(msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if len(candidates) <= 1 {
		return candidates, nil
	}

	// Check if there are at least two candidates with the same top score.
	maxScore := candidates[0].Score
	for _, c := range candidates[1:] {
		if c.Score > maxScore {
			maxScore = c.Score
		}
	}
	tieCount := 0
	for _, c := range candidates {
		if c.Score == maxScore {
			tieCount++
		}
	}
	if tieCount <= 1 {
		return candidates, nil
	}

	// Add random jitter (0 or 1) to candidates in the top tier to shuffle selection.
	for i := range candidates {
		if candidates[i].Score == maxScore {
			candidates[i].Score += rand.Intn(2) //nolint:gosec // G404: crypto randomness not needed
		}
	}
	e.randomized.Add(1)

	return candidates, nil
}

// Stop implements the Stoppable interface.
func (e *rtRandomize) Stop() error { return nil }

// Metrics implements extension.MetricsProvider.
func (e *rtRandomize) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "randomized_total",
			Help:  "Total routing decisions with random tie-breaking applied.",
			Type:  extension.MetricCounter,
			Value: float64(e.randomized.Load()),
		},
	}
}

func init() {
	extension.Register(&rtRandomize{})
}
