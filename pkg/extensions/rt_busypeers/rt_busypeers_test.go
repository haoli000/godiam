// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_busypeers

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

func newExt(t *testing.T, cfg map[string]interface{}) *rtBusypeers {
	t.Helper()
	ext := &rtBusypeers{}
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
	ext := &rtBusypeers{}
	if ext.Name() != "rt_busypeers" {
		t.Errorf("expected name rt_busypeers, got %s", ext.Name())
	}
}

func TestInit_Defaults(t *testing.T) {
	ext := newExt(t, nil)
	if ext.maxRetries != 3 {
		t.Errorf("expected default maxRetries=3, got %d", ext.maxRetries)
	}
	if ext.relayTimeout != 30*time.Second {
		t.Errorf("expected default relayTimeout=30s, got %v", ext.relayTimeout)
	}
}

func TestInit_CustomConfig(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"retry_max_peers": 5,
		"relay_timeout":   "10s",
	})
	if ext.maxRetries != 5 {
		t.Errorf("expected maxRetries=5, got %d", ext.maxRetries)
	}
	if ext.relayTimeout != 10*time.Second {
		t.Errorf("expected relayTimeout=10s, got %v", ext.relayTimeout)
	}
}

func TestInit_CustomConfig_Float64(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"retry_max_peers": float64(7),
	})
	if ext.maxRetries != 7 {
		t.Errorf("expected maxRetries=7, got %d", ext.maxRetries)
	}
}

func TestHandleRouting_RequestPassThrough(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
	}

	result, err := ext.handleRouting(nil, msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result))
	}
	if result[0].Score != 100 {
		t.Error("request should pass through unchanged")
	}
}

func TestHandleRouting_NonBusyAnswerPassThrough(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	ans := message.NewAnswer(req)
	ans.SetResultCode(types.ResultSuccess)

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
	}

	result, err := ext.handleRouting(nil, ans, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result))
	}
	if ext.busyReceived.Load() != 0 {
		t.Error("non-busy answer should not increment counter")
	}
}

func TestHandleRouting_BusyNoRetryRecord(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.EndToEndID = 0x1234
	ans := message.NewAnswer(req)
	ans.EndToEndID = 0x1234
	ans.SetResultCode(types.ResultTooBusy)

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
	}

	result, err := ext.handleRouting(nil, ans, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No retry record exists, so answer passes through
	if len(result) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result))
	}
	if ext.busyReceived.Load() != 1 {
		t.Errorf("expected busyReceived=1, got %d", ext.busyReceived.Load())
	}
}

func TestTrackRequest(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.EndToEndID = 0xAAAA

	ext.TrackRequest(nil, msg)

	ext.mu.Lock()
	rec, exists := ext.retries[0xAAAA]
	ext.mu.Unlock()

	if !exists {
		t.Fatal("expected retry record to exist")
	}
	if rec.origMsg != msg {
		t.Error("expected retry record to reference original message")
	}
	if rec.excluded == nil {
		t.Error("expected excluded map to be initialized")
	}
}

func TestHandleRouting_MaxRetriesExceeded(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"retry_max_peers": 1,
	})
	defer func() { _ = ext.Stop() }()

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.EndToEndID = 0xBBBB

	// Track the request and add an excluded peer
	ext.TrackRequest(nil, req)
	ext.mu.Lock()
	ext.retries[0xBBBB].excluded["peer1"] = struct{}{}
	ext.mu.Unlock()

	// Send a TOO_BUSY answer
	ans := message.NewAnswer(req)
	ans.EndToEndID = 0xBBBB
	ans.SetResultCode(types.ResultTooBusy)

	result, err := ext.handleRouting(nil, ans, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Max retries reached, pass through
	_ = result
	if ext.retryFailed.Load() != 1 {
		t.Errorf("expected retryFailed=1, got %d", ext.retryFailed.Load())
	}

	// Verify record was cleaned up
	ext.mu.Lock()
	_, exists := ext.retries[0xBBBB]
	ext.mu.Unlock()
	if exists {
		t.Error("expected retry record to be removed after max retries")
	}
}

func TestHandleRouting_TimeoutExceeded(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"relay_timeout": "1ms",
	})
	defer func() { _ = ext.Stop() }()

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.EndToEndID = 0xCCCC

	ext.TrackRequest(nil, req)

	// Backdate the tracked time to simulate timeout
	ext.mu.Lock()
	ext.retries[0xCCCC].createdAt = time.Now().Add(-10 * time.Millisecond)
	ext.mu.Unlock()

	ans := message.NewAnswer(req)
	ans.EndToEndID = 0xCCCC
	ans.SetResultCode(types.ResultTooBusy)

	_, err := ext.handleRouting(nil, ans, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ext.retryFailed.Load() != 1 {
		t.Errorf("expected retryFailed=1, got %d", ext.retryFailed.Load())
	}
}

func TestHandleRouting_AnswerWithoutResultCode(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	ans := message.NewAnswer(req)
	// No result code set

	result, err := ext.handleRouting(nil, ans, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
	if ext.busyReceived.Load() != 0 {
		t.Error("answer without result code should not be treated as TOO_BUSY")
	}
}

func TestStop_ClearsRetries(t *testing.T) {
	ext := newExt(t, nil)

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.EndToEndID = 0xDDDD
	ext.TrackRequest(nil, msg)

	_ = ext.Stop()

	ext.mu.Lock()
	count := len(ext.retries)
	ext.mu.Unlock()
	if count != 0 {
		t.Errorf("expected retries map cleared after Stop, got %d entries", count)
	}
}

func TestMetrics(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	metrics := ext.Metrics()
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(metrics))
	}

	m := make(map[string]extension.Metric)
	for _, metric := range metrics {
		m[metric.Name] = metric
	}

	expected := []struct {
		name  string
		mtype extension.MetricType
	}{
		{"busy_received_total", extension.MetricCounter},
		{"retries_done_total", extension.MetricCounter},
		{"retry_failed_total", extension.MetricCounter},
	}

	for _, e := range expected {
		metric, ok := m[e.name]
		if !ok {
			t.Errorf("missing metric %s", e.name)
			continue
		}
		if metric.Type != e.mtype {
			t.Errorf("metric %s: expected type %d, got %d", e.name, e.mtype, metric.Type)
		}
		if metric.Value != 0 {
			t.Errorf("metric %s: expected initial value 0, got %f", e.name, metric.Value)
		}
	}
}

func TestMetrics_AfterBusy(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	// Simulate a TOO_BUSY without a retry record
	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.EndToEndID = 0xEEEE
	ans := message.NewAnswer(req)
	ans.EndToEndID = 0xEEEE
	ans.SetResultCode(types.ResultTooBusy)
	_, _ = ext.handleRouting(nil, ans, nil)

	metrics := ext.Metrics()
	m := make(map[string]extension.Metric)
	for _, metric := range metrics {
		m[metric.Name] = metric
	}
	if m["busy_received_total"].Value != 1 {
		t.Errorf("expected busy_received_total=1, got %f", m["busy_received_total"].Value)
	}
}
