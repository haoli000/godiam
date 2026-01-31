// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package fifo_stats

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

func newExt(t *testing.T) *fifoStats {
	t.Helper()
	ext := &fifoStats{}
	ctx := &mockInitContext{router: routing.NewRouter()}
	if err := ext.Init(ctx, nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return ext
}

func TestName(t *testing.T) {
	ext := &fifoStats{}
	if ext.Name() != "fifo_stats" {
		t.Errorf("expected name fifo_stats, got %s", ext.Name())
	}
}

func TestInit(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	if ext.peerData == nil {
		t.Error("expected peerData map to be initialized")
	}
	if ext.inflight == nil {
		t.Error("expected inflight map to be initialized")
	}
}

func TestHandleIncoming_RequestTracked(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	p := peer.New(peer.Config{DiameterIdentity: "peer1"}, nil)
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.EndToEndID = 0x1234

	_, err := ext.handleIncoming(p, msg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ext.messagesIn.Load() != 1 {
		t.Errorf("expected messagesIn=1, got %d", ext.messagesIn.Load())
	}

	ext.inflightMu.Lock()
	_, exists := ext.inflight[0x1234]
	ext.inflightMu.Unlock()
	if !exists {
		t.Error("expected request to be tracked in inflight map")
	}

	ps := ext.getOrCreatePeerStats("peer1")
	if ps.currentDepth.Load() != 1 {
		t.Errorf("expected peer queue depth=1, got %d", ps.currentDepth.Load())
	}
	if ps.highWater.Load() != 1 {
		t.Errorf("expected high water=1, got %d", ps.highWater.Load())
	}
}

func TestHandleIncoming_AnswerDequeues(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	p := peer.New(peer.Config{DiameterIdentity: "peer1"}, nil)

	// Enqueue a request
	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.EndToEndID = 0xAAAA
	_, _ = ext.handleIncoming(p, req, nil)

	// Backdate the inflight entry to ensure measurable latency
	ext.inflightMu.Lock()
	ext.inflight[0xAAAA] = time.Now().Add(-10 * time.Millisecond)
	ext.inflightMu.Unlock()

	// Dequeue with answer
	ans := message.NewAnswer(req)
	ans.EndToEndID = 0xAAAA
	ans.SetResultCode(types.ResultSuccess)
	_, err := ext.handleIncoming(p, ans, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ext.messagesOut.Load() != 1 {
		t.Errorf("expected messagesOut=1, got %d", ext.messagesOut.Load())
	}
	if ext.matchedAnswers.Load() != 1 {
		t.Errorf("expected matchedAnswers=1, got %d", ext.matchedAnswers.Load())
	}

	ps := ext.getOrCreatePeerStats("peer1")
	if ps.currentDepth.Load() != 0 {
		t.Errorf("expected depth back to 0, got %d", ps.currentDepth.Load())
	}

	if ext.totalQueueUs.Load() <= 0 {
		t.Error("expected total queue time > 0")
	}
	if ext.maxQueueUs.Load() <= 0 {
		t.Error("expected max queue time > 0")
	}

	// Verify inflight cleaned up
	ext.inflightMu.Lock()
	_, exists := ext.inflight[0xAAAA]
	ext.inflightMu.Unlock()
	if exists {
		t.Error("expected inflight entry removed after answer")
	}
}

func TestHandleIncoming_NilPeer(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.EndToEndID = 0xBBBB

	_, err := ext.handleIncoming(nil, msg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ext.messagesIn.Load() != 1 {
		t.Errorf("expected messagesIn=1, got %d", ext.messagesIn.Load())
	}
	// No peer stats should be created for nil peer
	ext.mu.RLock()
	count := len(ext.peerData)
	ext.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected no peer data for nil peer, got %d", count)
	}
}

func TestHandleIncoming_HighWaterMark(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	p := peer.New(peer.Config{DiameterIdentity: "peer1"}, nil)

	// Send 3 requests (simulating queue buildup)
	for i := types.EndToEndID(1); i <= 3; i++ {
		msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
		msg.EndToEndID = i
		_, _ = ext.handleIncoming(p, msg, nil)
	}

	ps := ext.getOrCreatePeerStats("peer1")
	if ps.highWater.Load() != 3 {
		t.Errorf("expected high water=3, got %d", ps.highWater.Load())
	}
	if ps.currentDepth.Load() != 3 {
		t.Errorf("expected current depth=3, got %d", ps.currentDepth.Load())
	}

	// Dequeue 2
	for i := types.EndToEndID(1); i <= 2; i++ {
		req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
		ans := message.NewAnswer(req)
		ans.EndToEndID = i
		_, _ = ext.handleIncoming(p, ans, nil)
	}

	if ps.currentDepth.Load() != 1 {
		t.Errorf("expected depth=1 after dequeuing, got %d", ps.currentDepth.Load())
	}
	// High water should still be 3
	if ps.highWater.Load() != 3 {
		t.Errorf("expected high water unchanged at 3, got %d", ps.highWater.Load())
	}
}

func TestHandleIncoming_UnmatchedAnswer(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	ans := message.NewAnswer(req)
	ans.EndToEndID = 0x9999

	_, _ = ext.handleIncoming(nil, ans, nil)

	if ext.matchedAnswers.Load() != 0 {
		t.Error("should not match untracked answer")
	}
}

func TestHandleIncoming_CandidatesPassThrough(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
		{PeerIdentity: "peer2", Score: 50},
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	result, err := ext.handleIncoming(nil, msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result))
	}
	if result[0].Score != 100 || result[1].Score != 50 {
		t.Error("candidates should pass through unchanged")
	}
}

func TestPruneStale(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	now := time.Now()
	ext.inflightMu.Lock()
	ext.inflight[0x0001] = now.Add(-10 * time.Minute) // stale
	ext.inflight[0x0002] = now.Add(-1 * time.Minute)  // fresh
	ext.inflightMu.Unlock()

	ext.pruneStale(now)

	ext.inflightMu.Lock()
	_, stale := ext.inflight[0x0001]
	_, fresh := ext.inflight[0x0002]
	ext.inflightMu.Unlock()

	if stale {
		t.Error("expected stale entry pruned")
	}
	if !fresh {
		t.Error("expected fresh entry to remain")
	}
}

func TestStop_Clears(t *testing.T) {
	ext := newExt(t)

	p := peer.New(peer.Config{DiameterIdentity: "peer1"}, nil)
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.EndToEndID = 0x1111
	_, _ = ext.handleIncoming(p, msg, nil)

	_ = ext.Stop()

	ext.inflightMu.Lock()
	inflightCount := len(ext.inflight)
	ext.inflightMu.Unlock()

	ext.mu.RLock()
	peerCount := len(ext.peerData)
	ext.mu.RUnlock()

	if inflightCount != 0 {
		t.Errorf("expected inflight cleared, got %d", inflightCount)
	}
	if peerCount != 0 {
		t.Errorf("expected peerData cleared, got %d", peerCount)
	}
}

func TestMetrics_Empty(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	metrics := ext.Metrics()
	if len(metrics) < 4 {
		t.Fatalf("expected at least 4 metrics, got %d", len(metrics))
	}

	m := make(map[string]extension.Metric)
	for _, metric := range metrics {
		m[metric.Name] = metric
	}

	if m["messages_enqueued_total"].Value != 0 {
		t.Error("expected 0 enqueued initially")
	}
	if m["messages_dequeued_total"].Value != 0 {
		t.Error("expected 0 dequeued initially")
	}
	if m["avg_time_in_queue_us"].Value != 0 {
		t.Error("expected 0 avg time initially")
	}
	if m["max_time_in_queue_us"].Value != 0 {
		t.Error("expected 0 max time initially")
	}
}

func TestMetrics_WithData(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	p := peer.New(peer.Config{DiameterIdentity: "hss1"}, nil)

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.EndToEndID = 0x5555
	_, _ = ext.handleIncoming(p, req, nil)

	// Backdate the inflight entry to ensure measurable latency
	ext.inflightMu.Lock()
	ext.inflight[0x5555] = time.Now().Add(-10 * time.Millisecond)
	ext.inflightMu.Unlock()

	ans := message.NewAnswer(req)
	ans.EndToEndID = 0x5555
	ans.SetResultCode(types.ResultSuccess)
	_, _ = ext.handleIncoming(p, ans, nil)

	metrics := ext.Metrics()
	m := make(map[string]extension.Metric)
	for _, metric := range metrics {
		key := metric.Name
		if metric.Labels != nil {
			key += "_" + metric.Labels["peer"]
		}
		m[key] = metric
	}

	if m["messages_enqueued_total"].Value != 1 {
		t.Errorf("expected 1 enqueued, got %f", m["messages_enqueued_total"].Value)
	}
	if m["messages_dequeued_total"].Value != 1 {
		t.Errorf("expected 1 dequeued, got %f", m["messages_dequeued_total"].Value)
	}
	if m["avg_time_in_queue_us"].Value <= 0 {
		t.Error("expected avg > 0")
	}
	if m["max_time_in_queue_us"].Value <= 0 {
		t.Error("expected max > 0")
	}

	// Check per-peer metrics
	peerDepth, ok := m["peer_queue_depth_hss1"]
	if !ok {
		t.Error("missing peer_queue_depth for hss1")
	} else if peerDepth.Value != 0 {
		t.Errorf("expected depth=0 (request answered), got %f", peerDepth.Value)
	}

	peerHW, ok := m["peer_queue_high_water_hss1"]
	if !ok {
		t.Error("missing peer_queue_high_water for hss1")
	} else if peerHW.Value != 1 {
		t.Errorf("expected high water=1, got %f", peerHW.Value)
	}
}

func TestMultiplePeers(t *testing.T) {
	ext := newExt(t)
	defer func() { _ = ext.Stop() }()

	p1 := peer.New(peer.Config{DiameterIdentity: "peer1"}, nil)
	p2 := peer.New(peer.Config{DiameterIdentity: "peer2"}, nil)

	// 2 requests to peer1, 1 to peer2
	for i := types.EndToEndID(1); i <= 2; i++ {
		msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
		msg.EndToEndID = i
		_, _ = ext.handleIncoming(p1, msg, nil)
	}
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.EndToEndID = 0x10
	_, _ = ext.handleIncoming(p2, msg, nil)

	ps1 := ext.getOrCreatePeerStats("peer1")
	ps2 := ext.getOrCreatePeerStats("peer2")

	if ps1.currentDepth.Load() != 2 {
		t.Errorf("expected peer1 depth=2, got %d", ps1.currentDepth.Load())
	}
	if ps2.currentDepth.Load() != 1 {
		t.Errorf("expected peer2 depth=1, got %d", ps2.currentDepth.Load())
	}
}
