// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_load_balance

import (
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type mockInitContext struct {
	router *routing.Router
	peers  []*peer.Peer
}

func (m *mockInitContext) GetDictionary() *dictionary.Dictionary { return nil }
func (m *mockInitContext) GetRouter() *routing.Router            { return m.router }
func (m *mockInitContext) GetConfig() *config.Config             { return nil }
func (m *mockInitContext) GetPeers() []*peer.Peer                { return m.peers }
func (m *mockInitContext) GetStartTime() time.Time               { return time.Now() }
func (m *mockInitContext) GetExtensionManager() *extension.Manager {
	return nil
}

func TestName(t *testing.T) {
	ext := &rtLoadBalance{}
	if ext.Name() != "rt_load_balance" {
		t.Errorf("expected name rt_load_balance, got %s", ext.Name())
	}
}

func TestHandleOutgoing_SingleCandidate(t *testing.T) {
	ext := &rtLoadBalance{
		peerEnumerate: func() []*peer.Peer { return nil },
	}

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
	// Single candidate should not be modified
	if result[0].Score != 100 {
		t.Errorf("expected score 100, got %d", result[0].Score)
	}
}

func TestHandleOutgoing_EmptyCandidates(t *testing.T) {
	ext := &rtLoadBalance{
		peerEnumerate: func() []*peer.Peer { return nil },
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)

	result, err := ext.handleOutgoing(msg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for nil candidates")
	}
}

func TestHandleOutgoing_BoostsLeastLoaded(t *testing.T) {
	// Create mock peers with different pending counts
	p1 := peer.New(peer.Config{DiameterIdentity: "peer1", Addresses: []string{"peer1.example.com"}}, nil)
	p2 := peer.New(peer.Config{DiameterIdentity: "peer2", Addresses: []string{"peer2.example.com"}}, nil)

	// Give peer1 more pending requests so peer2 is "least loaded"
	for i := 0; i < 5; i++ {
		msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
		msg.HopByHopID = types.HopByHopID(i + 1)
		msg.EndToEndID = types.EndToEndID(i + 1)
		p1.TrackPending(msg)
	}
	// peer2 has 1 pending
	msg1 := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg1.HopByHopID = 100
	msg1.EndToEndID = 100
	p2.TrackPending(msg1)

	ext := &rtLoadBalance{
		peerEnumerate: func() []*peer.Peer { return []*peer.Peer{p1, p2} },
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
		{PeerIdentity: "peer2", Score: 100},
	}

	result, err := ext.handleOutgoing(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// peer2 has fewer pending (1 vs 5), so it should be boosted
	for _, r := range result {
		if r.PeerIdentity == "peer2" && r.Score != 101 {
			t.Errorf("expected peer2 (least loaded) score 101, got %d", r.Score)
		}
		if r.PeerIdentity == "peer1" && r.Score != 100 {
			t.Errorf("expected peer1 (more loaded) score 100, got %d", r.Score)
		}
	}
	if ext.rebalanced.Load() != 1 {
		t.Errorf("expected rebalanced counter=1, got %d", ext.rebalanced.Load())
	}
}

func TestHandleOutgoing_DifferentScores(t *testing.T) {
	ext := &rtLoadBalance{
		peerEnumerate: func() []*peer.Peer { return nil },
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
		{PeerIdentity: "peer2", Score: 50},
	}

	result, err := ext.handleOutgoing(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// peer1 is the only one in the top tier, should get +1
	if result[0].Score != 101 {
		t.Errorf("expected peer1 score 101, got %d", result[0].Score)
	}
	if result[1].Score != 50 {
		t.Errorf("expected peer2 score 50 unchanged, got %d", result[1].Score)
	}
}

func TestHandleOutgoing_NoPeersFound(t *testing.T) {
	// peerEnumerate returns no peers — pending map will be empty
	ext := &rtLoadBalance{
		peerEnumerate: func() []*peer.Peer { return nil },
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
		{PeerIdentity: "peer2", Score: 100},
	}

	result, err := ext.handleOutgoing(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result))
	}
	// Both have 0 pending (default); one should be boosted
	boosted := 0
	for _, r := range result {
		if r.Score == 101 {
			boosted++
		}
	}
	if boosted != 1 {
		t.Errorf("expected 1 boosted candidate, got %d", boosted)
	}
}

func TestStop(t *testing.T) {
	ext := &rtLoadBalance{}
	if err := ext.Stop(); err != nil {
		t.Errorf("unexpected Stop error: %v", err)
	}
}

func TestMetrics(t *testing.T) {
	ext := &rtLoadBalance{
		peerEnumerate: func() []*peer.Peer { return nil },
	}

	metrics := ext.Metrics()
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Name != "rebalanced_total" {
		t.Errorf("expected metric name rebalanced_total, got %s", metrics[0].Name)
	}
	if metrics[0].Type != extension.MetricCounter {
		t.Errorf("expected Counter type")
	}
	if metrics[0].Value != 0 {
		t.Errorf("expected initial value 0, got %f", metrics[0].Value)
	}
}

func TestInit(t *testing.T) {
	ext := &rtLoadBalance{}
	ctx := &mockInitContext{
		router: routing.NewRouter(),
		peers:  nil,
	}
	err := ext.Init(ctx, nil)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if ext.peerEnumerate == nil {
		t.Error("expected peerEnumerate to be set")
	}
}

func (m *mockInitContext) GetEdgeRegistry() *edge.Registry {
	return edge.NewRegistry(m.GetConfig())
}
