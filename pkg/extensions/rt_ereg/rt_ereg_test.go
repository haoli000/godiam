// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_ereg

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
	dict   *dictionary.Dictionary
}

func (m *mockInitContext) GetDictionary() *dictionary.Dictionary { return m.dict }
func (m *mockInitContext) GetRouter() *routing.Router            { return m.router }
func (m *mockInitContext) GetConfig() *config.Config             { return nil }
func (m *mockInitContext) GetPeers() []*peer.Peer                { return nil }
func (m *mockInitContext) GetStartTime() time.Time               { return time.Now() }
func (m *mockInitContext) GetExtensionManager() *extension.Manager {
	return nil
}

func newTestContext() *mockInitContext {
	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()
	return &mockInitContext{
		router: routing.NewRouter(),
		dict:   dict,
	}
}

func TestName(t *testing.T) {
	ext := &rtEreg{}
	if ext.Name() != "rt_ereg" {
		t.Errorf("expected name rt_ereg, got %s", ext.Name())
	}
}

func TestInit_MissingRules(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error when rules are missing")
	}
}

func TestInit_InvalidRulesType(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{
		"rules": "not-a-list",
	})
	if err == nil {
		t.Fatal("expected error for invalid rules type")
	}
}

func TestInit_RuleMissingFields(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": "^test",
				// Missing 'server'
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for rule missing server field")
	}
}

func TestInit_InvalidPattern(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": "[invalid",
				"server":  "peer1",
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestInit_ValidRules(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": "^test.*",
				"server":  "peer1",
				"score":   50,
			},
		},
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if len(ext.rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(ext.rules))
	}
	if ext.rules[0].score != 50 {
		t.Errorf("expected score 50, got %d", ext.rules[0].score)
	}
}

func TestInit_DefaultScore(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": ".*",
				"server":  "peer1",
			},
		},
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if ext.rules[0].score != 100 {
		t.Errorf("expected default score 100, got %d", ext.rules[0].score)
	}
}

func TestHandleRouting_MatchBoostsScore(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	if err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": "^alice",
				"server":  "hss1",
				"score":   80,
			},
		},
	}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeUserName, types.AVPFlagMandatory, "alice@example.com"))

	candidates := []routing.Route{
		{PeerIdentity: "hss1", Score: 100},
		{PeerIdentity: "hss2", Score: 100},
	}

	result, err := ext.handleRouting(nil, msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range result {
		switch r.PeerIdentity {
		case "hss1":
			if r.Score != 180 {
				t.Errorf("expected hss1 score 180, got %d", r.Score)
			}
		case "hss2":
			if r.Score != 100 {
				t.Errorf("expected hss2 score 100, got %d", r.Score)
			}
		}
	}
	if ext.matched.Load() != 1 {
		t.Errorf("expected matched counter=1, got %d", ext.matched.Load())
	}
}

func TestHandleRouting_NoMatch(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	if err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": "^alice",
				"server":  "hss1",
			},
		},
	}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeUserName, types.AVPFlagMandatory, "bob@example.com"))

	candidates := []routing.Route{
		{PeerIdentity: "hss1", Score: 100},
	}

	result, err := ext.handleRouting(nil, msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Score != 100 {
		t.Errorf("expected score unchanged at 100, got %d", result[0].Score)
	}
	if ext.matched.Load() != 0 {
		t.Errorf("expected no matches, got %d", ext.matched.Load())
	}
}

func TestHandleRouting_AnswerIgnored(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	if err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": ".*",
				"server":  "hss1",
			},
		},
	}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	ans := message.NewAnswer(req)
	ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeUserName, types.AVPFlagMandatory, "alice"))

	candidates := []routing.Route{
		{PeerIdentity: "hss1", Score: 100},
	}

	result, err := ext.handleRouting(nil, ans, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Score != 100 {
		t.Errorf("expected answer score unchanged, got %d", result[0].Score)
	}
}

func TestHandleRouting_MissingAVP(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	if err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": ".*",
				"server":  "hss1",
				"score":   50,
			},
		},
	}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	// No User-Name AVP

	candidates := []routing.Route{
		{PeerIdentity: "hss1", Score: 100},
	}

	result, err := ext.handleRouting(nil, msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Score != 100 {
		t.Errorf("expected score unchanged when AVP missing, got %d", result[0].Score)
	}
}

func TestHandleRouting_DiameterIdentityAVP(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	if err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "Destination-Realm",
				"pattern": "^realm-a",
				"server":  "hss-a",
				"score":   60,
			},
		},
	}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "realm-a.example.com"))

	candidates := []routing.Route{
		{PeerIdentity: "hss-a", Score: 100},
		{PeerIdentity: "hss-b", Score: 100},
	}

	result, err := ext.handleRouting(nil, msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range result {
		if r.PeerIdentity == "hss-a" && r.Score != 160 {
			t.Errorf("expected hss-a score 160, got %d", r.Score)
		}
		if r.PeerIdentity == "hss-b" && r.Score != 100 {
			t.Errorf("expected hss-b score 100, got %d", r.Score)
		}
	}
}

func TestHandleRouting_MultipleRules(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	if err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": "^alice",
				"server":  "hss1",
				"score":   50,
			},
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": "@example\\.com$",
				"server":  "hss2",
				"score":   30,
			},
		},
	}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeUserName, types.AVPFlagMandatory, "alice@example.com"))

	candidates := []routing.Route{
		{PeerIdentity: "hss1", Score: 100},
		{PeerIdentity: "hss2", Score: 100},
	}

	result, err := ext.handleRouting(nil, msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range result {
		switch r.PeerIdentity {
		case "hss1":
			if r.Score != 150 {
				t.Errorf("expected hss1 score 150, got %d", r.Score)
			}
		case "hss2":
			if r.Score != 130 {
				t.Errorf("expected hss2 score 130, got %d", r.Score)
			}
		}
	}
	if ext.matched.Load() != 2 {
		t.Errorf("expected 2 matches, got %d", ext.matched.Load())
	}
}

func TestLookupAVP_WellKnown(t *testing.T) {
	ext := &rtEreg{dict: dictionary.New()}

	tests := []struct {
		name string
		code types.AVPCode
	}{
		{"User-Name", types.AVPCodeUserName},
		{"Session-Id", types.AVPCodeSessionID},
		{"Origin-Host", types.AVPCodeOriginHost},
		{"Destination-Host", types.AVPCodeDestinationHost},
		{"Destination-Realm", types.AVPCodeDestinationRealm},
	}

	for _, tt := range tests {
		code, vendor := ext.lookupAVP(tt.name)
		if code != tt.code {
			t.Errorf("lookupAVP(%s): expected code %d, got %d", tt.name, tt.code, code)
		}
		if vendor != 0 {
			t.Errorf("lookupAVP(%s): expected vendor 0, got %d", tt.name, vendor)
		}
	}
}

func TestLookupAVP_Unknown(t *testing.T) {
	ext := &rtEreg{dict: dictionary.New()}
	code, _ := ext.lookupAVP("Nonexistent-AVP")
	if code != 0 {
		t.Errorf("expected code 0 for unknown AVP, got %d", code)
	}
}

func TestStop(t *testing.T) {
	ext := &rtEreg{}
	if err := ext.Stop(); err != nil {
		t.Errorf("unexpected Stop error: %v", err)
	}
}

func TestMetrics(t *testing.T) {
	ext := &rtEreg{}

	metrics := ext.Metrics()
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Name != "matched_total" {
		t.Errorf("expected metric name matched_total, got %s", metrics[0].Name)
	}
	if metrics[0].Type != extension.MetricCounter {
		t.Error("expected Counter type")
	}
}

func TestInit_ScoreFloat64(t *testing.T) {
	ext := &rtEreg{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"avp":     "User-Name",
				"pattern": ".*",
				"server":  "peer1",
				"score":   float64(75),
			},
		},
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if ext.rules[0].score != 75 {
		t.Errorf("expected score 75, got %d", ext.rules[0].score)
	}
}

func (m *mockInitContext) GetEdgeRegistry() *edge.Registry {
	return edge.NewRegistry(m.GetConfig())
}
