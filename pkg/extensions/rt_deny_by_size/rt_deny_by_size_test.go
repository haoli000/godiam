// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_deny_by_size

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

func newExt(t *testing.T, cfg map[string]interface{}) *rtDenyBySize {
	t.Helper()
	ext := &rtDenyBySize{}
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
	ext := &rtDenyBySize{}
	if ext.Name() != "rt_deny_by_size" {
		t.Errorf("expected name rt_deny_by_size, got %s", ext.Name())
	}
}

func TestInit_DefaultMaxSize(t *testing.T) {
	ext := newExt(t, nil)
	if ext.maxSize != defaultMaxSize {
		t.Errorf("expected default max size %d, got %d", defaultMaxSize, ext.maxSize)
	}
}

func TestInit_CustomMaxSize_Int(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"maximum_size": 1024,
	})
	if ext.maxSize != 1024 {
		t.Errorf("expected max size 1024, got %d", ext.maxSize)
	}
}

func TestInit_CustomMaxSize_Float64(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"maximum_size": float64(2048),
	})
	if ext.maxSize != 2048 {
		t.Errorf("expected max size 2048, got %d", ext.maxSize)
	}
}

func TestHandleRouting_SmallMessage(t *testing.T) {
	ext := newExt(t, nil)

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "test-session"))

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
	if ext.rejected.Load() != 0 {
		t.Error("expected no rejections for small message")
	}
}

func TestHandleRouting_OversizedMessage(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"maximum_size": 50, // Very small limit
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	// Add enough data to exceed 50 bytes
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "this-is-a-very-long-session-id-that-should-exceed-the-limit"))

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
	}

	_, err := ext.handleRouting(nil, msg, candidates)
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
	if ext.rejected.Load() != 1 {
		t.Errorf("expected 1 rejection, got %d", ext.rejected.Load())
	}
}

func TestHandleRouting_AnswerIgnored(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"maximum_size": 1, // Tiny limit — would reject any request
	})

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	ans := message.NewAnswer(req)
	ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "test"))

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
	}

	result, err := ext.handleRouting(nil, ans, candidates)
	if err != nil {
		t.Fatalf("expected no error for answer, got: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result))
	}
	if ext.rejected.Load() != 0 {
		t.Error("expected no rejection for answer")
	}
}

func TestHandleRouting_ExactlyAtLimit(t *testing.T) {
	ext := &rtDenyBySize{maxSize: 10000}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)

	data, err := msg.Encode()
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	ext.maxSize = len(data) // Set limit to exact message size

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
	}

	result, err := ext.handleRouting(nil, msg, candidates)
	if err != nil {
		t.Fatalf("expected no error at exact limit, got: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result))
	}
}

func TestReconfigure(t *testing.T) {
	ext := newExt(t, nil)

	if ext.maxSize != defaultMaxSize {
		t.Fatalf("precondition: expected default max size")
	}

	err := ext.Reconfigure(map[string]interface{}{
		"maximum_size": 8192,
	})
	if err != nil {
		t.Fatalf("Reconfigure failed: %v", err)
	}
	if ext.maxSize != 8192 {
		t.Errorf("expected max size 8192 after reconfigure, got %d", ext.maxSize)
	}
}

func TestReconfigure_Float64(t *testing.T) {
	ext := newExt(t, nil)
	err := ext.Reconfigure(map[string]interface{}{
		"maximum_size": float64(4096),
	})
	if err != nil {
		t.Fatalf("Reconfigure failed: %v", err)
	}
	if ext.maxSize != 4096 {
		t.Errorf("expected max size 4096, got %d", ext.maxSize)
	}
}

func TestStop(t *testing.T) {
	ext := &rtDenyBySize{}
	if err := ext.Stop(); err != nil {
		t.Errorf("unexpected Stop error: %v", err)
	}
}

func TestMetrics(t *testing.T) {
	ext := newExt(t, nil)

	metrics := ext.Metrics()
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Name != "rejected_total" {
		t.Errorf("expected metric name rejected_total, got %s", metrics[0].Name)
	}
	if metrics[0].Type != extension.MetricCounter {
		t.Error("expected Counter type")
	}
	if metrics[0].Value != 0 {
		t.Errorf("expected initial value 0, got %f", metrics[0].Value)
	}
}

func TestMetrics_AfterRejections(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"maximum_size": 30,
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "a-long-session-id-to-exceed-limit"))

	// Verify both calls actually reject
	_, err1 := ext.handleRouting(nil, msg, nil)
	_, err2 := ext.handleRouting(nil, msg, nil)
	if err1 == nil || err2 == nil {
		t.Fatal("expected both calls to return rejection error")
	}

	metrics := ext.Metrics()
	if metrics[0].Value != 2 {
		t.Errorf("expected rejected_total=2, got %f", metrics[0].Value)
	}
}
