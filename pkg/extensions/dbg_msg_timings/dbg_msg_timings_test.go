// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dbg_msg_timings

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
}

func (m *mockInitContext) GetDictionary() *dictionary.Dictionary { return nil }
func (m *mockInitContext) GetRouter() *routing.Router            { return m.router }
func (m *mockInitContext) GetConfig() *config.Config             { return nil }
func (m *mockInitContext) GetPeers() []*peer.Peer                { return nil }
func (m *mockInitContext) GetStartTime() time.Time               { return time.Now() }
func (m *mockInitContext) GetExtensionManager() *extension.Manager {
	return nil
}

func newExt(t *testing.T, cfg map[string]interface{}) *dbgMsgTimings {
	t.Helper()
	ext := &dbgMsgTimings{}
	ctx := &mockInitContext{router: routing.NewRouter()}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	if err := ext.Init(ctx, cfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return ext
}

func TestName(t *testing.T) {
	ext := &dbgMsgTimings{}
	if ext.Name() != "dbg_msg_timings" {
		t.Errorf("expected name dbg_msg_timings, got %s", ext.Name())
	}
}

func TestInit_DefaultThreshold(t *testing.T) {
	ext := newExt(t, nil)
	if ext.logThreshold != 100*time.Millisecond {
		t.Errorf("expected default threshold 100ms, got %v", ext.logThreshold)
	}
}

func TestInit_CustomThreshold(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"log_threshold": "500ms",
	})
	if ext.logThreshold != 500*time.Millisecond {
		t.Errorf("expected threshold 500ms, got %v", ext.logThreshold)
	}
}

func TestInit_InvalidThreshold(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"log_threshold": "not-a-duration",
	})
	// Should fall back to default
	if ext.logThreshold != 100*time.Millisecond {
		t.Errorf("expected default threshold on invalid input, got %v", ext.logThreshold)
	}
}

func TestHandleRouting_RequestTracked(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.EndToEndID = 0x1234

	result, err := ext.handleRouting(nil, msg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil candidates to be returned as nil")
	}
	if ext.requestsSeen.Load() != 1 {
		t.Errorf("expected 1 request seen, got %d", ext.requestsSeen.Load())
	}

	ext.mu.Lock()
	_, exists := ext.inflight[0x1234]
	ext.mu.Unlock()
	if !exists {
		t.Error("expected EndToEndID 0x1234 to be tracked in inflight map")
	}
}

func TestHandleRouting_AnswerMatchesRequest(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	// Send a request
	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.EndToEndID = 0xABCD
	_, _ = ext.handleRouting(nil, req, nil)

	// Backdate the inflight entry to ensure measurable latency
	ext.mu.Lock()
	ext.inflight[0xABCD] = time.Now().Add(-10 * time.Millisecond)
	ext.mu.Unlock()

	// Send matching answer
	ans := message.NewAnswer(req)
	ans.EndToEndID = 0xABCD
	ans.SetResultCode(types.ResultSuccess)
	_, err := ext.handleRouting(nil, ans, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ext.answersMatched.Load() != 1 {
		t.Errorf("expected 1 answer matched, got %d", ext.answersMatched.Load())
	}
	if ext.maxLatencyUs.Load() <= 0 {
		t.Error("expected max latency > 0")
	}
	if ext.totalLatencyUs.Load() <= 0 {
		t.Error("expected total latency > 0")
	}

	// Verify inflight map is cleaned up
	ext.mu.Lock()
	_, exists := ext.inflight[0xABCD]
	ext.mu.Unlock()
	if exists {
		t.Error("expected EndToEndID to be removed from inflight after match")
	}
}

func TestHandleRouting_UnmatchedAnswer(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	// Answer without a matching request
	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	ans := message.NewAnswer(req)
	ans.EndToEndID = 0x9999
	_, _ = ext.handleRouting(nil, ans, nil)

	if ext.answersMatched.Load() != 0 {
		t.Errorf("expected 0 answers matched for unmatched answer, got %d", ext.answersMatched.Load())
	}
}

func TestHandleRouting_CandidatesPassThrough(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
		{PeerIdentity: "peer2", Score: 50},
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	result, err := ext.handleRouting(nil, msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result))
	}
	if result[0].Score != 100 || result[1].Score != 50 {
		t.Error("candidates should be returned unchanged")
	}
}

func TestPruneStale(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	now := time.Now()

	ext.mu.Lock()
	ext.inflight[0x0001] = now.Add(-10 * time.Minute) // stale
	ext.inflight[0x0002] = now.Add(-1 * time.Minute)  // fresh
	ext.mu.Unlock()

	ext.pruneStale(now)

	ext.mu.Lock()
	_, staleExists := ext.inflight[0x0001]
	_, freshExists := ext.inflight[0x0002]
	ext.mu.Unlock()

	if staleExists {
		t.Error("expected stale entry to be pruned")
	}
	if !freshExists {
		t.Error("expected fresh entry to remain")
	}
}

func TestStop_ClearsInflight(t *testing.T) {
	ext := newExt(t, nil)

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.EndToEndID = 0x1111
	_, _ = ext.handleRouting(nil, msg, nil)

	_ = ext.Stop()

	ext.mu.Lock()
	count := len(ext.inflight)
	ext.mu.Unlock()
	if count != 0 {
		t.Errorf("expected inflight map cleared after Stop, got %d entries", count)
	}
}

func TestMetrics(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	// Process a request/answer pair
	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.EndToEndID = 0x5555
	_, _ = ext.handleRouting(nil, req, nil)

	// Backdate the inflight entry to ensure measurable latency
	ext.mu.Lock()
	ext.inflight[0x5555] = time.Now().Add(-10 * time.Millisecond)
	ext.mu.Unlock()

	ans := message.NewAnswer(req)
	ans.EndToEndID = 0x5555
	ans.SetResultCode(types.ResultSuccess)
	_, _ = ext.handleRouting(nil, ans, nil)

	metrics := ext.Metrics()
	if len(metrics) != 4 {
		t.Fatalf("expected 4 metrics, got %d", len(metrics))
	}

	m := make(map[string]extension.Metric)
	for _, metric := range metrics {
		m[metric.Name] = metric
	}

	if m["requests_tracked_total"].Value != 1 {
		t.Errorf("expected requests_tracked_total=1, got %f", m["requests_tracked_total"].Value)
	}
	if m["answers_matched_total"].Value != 1 {
		t.Errorf("expected answers_matched_total=1, got %f", m["answers_matched_total"].Value)
	}
	if m["avg_latency_us"].Value <= 0 {
		t.Error("expected avg_latency_us > 0")
	}
	if m["max_latency_us"].Value <= 0 {
		t.Error("expected max_latency_us > 0")
	}
}

func TestMetrics_NoAnswers(t *testing.T) {
	ext := newExt(t, nil)

	metrics := ext.Metrics()
	m := make(map[string]extension.Metric)
	for _, metric := range metrics {
		m[metric.Name] = metric
	}
	if m["avg_latency_us"].Value != 0 {
		t.Errorf("expected avg_latency_us=0 when no answers, got %f", m["avg_latency_us"].Value)
	}
}

func (m *mockInitContext) GetEdgeRegistry() *edge.Registry {
	return edge.NewRegistry(m.GetConfig())
}
