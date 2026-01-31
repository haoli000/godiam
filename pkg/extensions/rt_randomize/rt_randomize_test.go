// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_randomize

import (
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type mockInitContext struct {
	router *routing.Router
}

func (m *mockInitContext) GetDictionary() *dictionary.Dictionary { return nil }
func (m *mockInitContext) GetRouter() *routing.Router            { return m.router }
func (m *mockInitContext) GetConfig() *config.Config             { return nil }
func (m *mockInitContext) GetPeers() []*peer.Peer                { return nil }
func (m *mockInitContext) GetStartTime() time.Time               { return time.Now() }
func (m *mockInitContext) GetExtensionManager() *extension.Manager {
	return nil
}

func TestName(t *testing.T) {
	ext := &rtRandomize{}
	if ext.Name() != "rt_randomize" {
		t.Errorf("expected name rt_randomize, got %s", ext.Name())
	}
}

func TestInit(t *testing.T) {
	ext := &rtRandomize{}
	ctx := &mockInitContext{router: routing.NewRouter()}
	err := ext.Init(ctx, nil)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestHandleOutgoing_SingleCandidate(t *testing.T) {
	ext := &rtRandomize{}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
	}

	result, err := ext.handleOutgoing(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result))
	}
	if result[0].Score != 100 {
		t.Errorf("expected score 100 unchanged, got %d", result[0].Score)
	}
	if ext.randomized.Load() != 0 {
		t.Error("should not randomize with single candidate")
	}
}

func TestHandleOutgoing_NoCandidates(t *testing.T) {
	ext := &rtRandomize{}
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)

	result, err := ext.handleOutgoing(msg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result")
	}
}

func TestHandleOutgoing_NoTiesSkipped(t *testing.T) {
	ext := &rtRandomize{}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 200},
		{PeerIdentity: "peer2", Score: 100},
	}

	result, err := ext.handleOutgoing(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No ties, scores should be unchanged
	if result[0].Score != 200 {
		t.Errorf("expected peer1 score 200, got %d", result[0].Score)
	}
	if result[1].Score != 100 {
		t.Errorf("expected peer2 score 100, got %d", result[1].Score)
	}
	if ext.randomized.Load() != 0 {
		t.Error("should not randomize when no ties exist")
	}
}

func TestHandleOutgoing_TiesRandomized(t *testing.T) {
	ext := &rtRandomize{}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)

	// Run enough times to verify randomization actually produces variation
	scoreDistribution := make(map[types.DiamID]int) // counts how often each peer gets 101
	const iterations = 100
	for i := 0; i < iterations; i++ {
		candidates := []routing.Route{
			{PeerIdentity: "peer1", Score: 100},
			{PeerIdentity: "peer2", Score: 100},
			{PeerIdentity: "peer3", Score: 50},
		}

		result, err := ext.handleOutgoing(msg, candidates)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// peer3 should never be modified (not in top tier)
		if result[2].Score != 50 {
			t.Errorf("expected peer3 score 50 unchanged, got %d", result[2].Score)
		}

		for _, r := range result[:2] {
			if r.Score < 100 || r.Score > 101 {
				t.Errorf("expected score 100 or 101 for %s, got %d", r.PeerIdentity, r.Score)
			}
			if r.Score == 101 {
				scoreDistribution[r.PeerIdentity]++
			}
		}
	}

	// Over 100 iterations with p=0.5, expect each peer to get score 101
	// at least 10 times (binomial CDF makes <10 astronomically unlikely)
	if scoreDistribution["peer1"] < 10 {
		t.Errorf("peer1 got score 101 only %d/100 times — randomization not working", scoreDistribution["peer1"])
	}
	if scoreDistribution["peer2"] < 10 {
		t.Errorf("peer2 got score 101 only %d/100 times — randomization not working", scoreDistribution["peer2"])
	}
	if ext.randomized.Load() != uint64(iterations) {
		t.Errorf("expected randomized counter=%d, got %d", iterations, ext.randomized.Load())
	}
}

func TestHandleOutgoing_AllTied(t *testing.T) {
	ext := &rtRandomize{}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
		{PeerIdentity: "peer2", Score: 100},
		{PeerIdentity: "peer3", Score: 100},
	}

	result, err := ext.handleOutgoing(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range result {
		if r.Score < 100 || r.Score > 101 {
			t.Errorf("expected score 100 or 101 for %s, got %d", r.PeerIdentity, r.Score)
		}
	}
}

func TestStop(t *testing.T) {
	ext := &rtRandomize{}
	if err := ext.Stop(); err != nil {
		t.Errorf("unexpected Stop error: %v", err)
	}
}

func TestMetrics(t *testing.T) {
	ext := &rtRandomize{}

	metrics := ext.Metrics()
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Name != "randomized_total" {
		t.Errorf("expected metric name randomized_total, got %s", metrics[0].Name)
	}
	if metrics[0].Type != extension.MetricCounter {
		t.Error("expected Counter type")
	}
	if metrics[0].Value != 0 {
		t.Errorf("expected initial value 0, got %f", metrics[0].Value)
	}
}
