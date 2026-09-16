// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_session_bind

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
	cfg    *config.Config
}

func (m *mockInitContext) GetDictionary() *dictionary.Dictionary { return m.dict }
func (m *mockInitContext) GetRouter() *routing.Router            { return m.router }
func (m *mockInitContext) GetConfig() *config.Config             { return m.cfg }
func (m *mockInitContext) GetPeers() []*peer.Peer                { return nil }
func (m *mockInitContext) GetStartTime() time.Time               { return time.Now() }
func (m *mockInitContext) GetExtensionManager() *extension.Manager {
	return nil
}

func newTestContext() *mockInitContext {
	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()
	_ = dict.LoadCreditControl()
	return &mockInitContext{
		router: routing.NewRouter(),
		dict:   dict,
		cfg: &config.Config{
			Identity: "test.example.com",
			Realm:    "example.com",
		},
	}
}

func TestInit_DefaultConfig(t *testing.T) {
	ext := &rtSessionBind{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer func() { _ = ext.Stop() }()

	if ext.bindingKey != "session_id" {
		t.Errorf("expected default binding_key=session_id, got %s", ext.bindingKey)
	}
	if ext.scoreBoost != 80 {
		t.Errorf("expected default score_boost=80, got %d", ext.scoreBoost)
	}
	if ext.store == nil {
		t.Fatal("expected store to be initialized")
	}
	if ext.store.ttl != 24*time.Hour {
		t.Errorf("expected default ttl=24h, got %s", ext.store.ttl)
	}
	if ext.store.maxSize != 100000 {
		t.Errorf("expected default max_bindings=100000, got %d", ext.store.maxSize)
	}
}

func TestInit_CustomConfig(t *testing.T) {
	ext := &rtSessionBind{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{
		"binding_key":      "subscription_id",
		"score_boost":      50,
		"ttl":              "1h",
		"max_bindings":     5000,
		"cleanup_interval": "1m",
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer func() { _ = ext.Stop() }()

	if ext.bindingKey != "subscription_id" {
		t.Errorf("expected binding_key=subscription_id, got %s", ext.bindingKey)
	}
	if ext.scoreBoost != 50 {
		t.Errorf("expected score_boost=50, got %d", ext.scoreBoost)
	}
	if ext.store.ttl != time.Hour {
		t.Errorf("expected ttl=1h, got %s", ext.store.ttl)
	}
	if ext.store.maxSize != 5000 {
		t.Errorf("expected max_bindings=5000, got %d", ext.store.maxSize)
	}
}

func TestInit_InvalidBindingKey(t *testing.T) {
	ext := &rtSessionBind{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{
		"binding_key": "invalid_key",
	})
	if err == nil {
		t.Fatal("expected error for invalid binding_key")
	}
}

func TestExtractKey_SessionID(t *testing.T) {
	ext := &rtSessionBind{bindingKey: "session_id"}
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "test.example.com;123;abc"))

	key, ok := ext.extractKey(msg)
	if !ok {
		t.Fatal("expected key extraction to succeed")
	}
	if key != "test.example.com;123;abc" {
		t.Fatalf("expected session id, got %s", key)
	}
}

func TestExtractKey_SubscriptionID(t *testing.T) {
	ext := &rtSessionBind{bindingKey: "subscription_id"}
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)

	subIDAVP := message.NewGroupedAVP(types.AVPCodeSubscriptionID, types.AVPFlagMandatory, 0,
		message.NewEnumeratedAVP(types.AVPCodeSubscriptionIDType, types.AVPFlagMandatory, 1), // END_USER_IMSI
		message.NewUTF8StringAVP(types.AVPCodeSubscriptionIDData, types.AVPFlagMandatory, "262010123456789"),
	)
	msg.AddAVP(subIDAVP)

	key, ok := ext.extractKey(msg)
	if !ok {
		t.Fatal("expected key extraction to succeed")
	}
	if key != "262010123456789" {
		t.Fatalf("expected IMSI, got %s", key)
	}
}

func TestExtractKey_Missing(t *testing.T) {
	ext := &rtSessionBind{bindingKey: "session_id"}
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	// No Session-ID AVP added

	_, ok := ext.extractKey(msg)
	if ok {
		t.Fatal("expected key extraction to fail for missing AVP")
	}
}

func TestExtractKey_SubscriptionIDMissing(t *testing.T) {
	ext := &rtSessionBind{bindingKey: "subscription_id"}
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)

	_, ok := ext.extractKey(msg)
	if ok {
		t.Fatal("expected key extraction to fail for missing Subscription-ID")
	}
}

func TestOutHandler_NewSession(t *testing.T) {
	ext := &rtSessionBind{
		bindingKey: "session_id",
		scoreBoost: 80,
		store:      newBindingStore(time.Hour, 100),
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess1"))

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100, Reason: "realm"},
		{PeerIdentity: "peer2", Score: 100, Reason: "realm"},
	}

	result, err := ext.handleOutgoing(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No binding exists yet, scores should be unchanged
	for _, r := range result {
		if r.Score != 100 {
			t.Errorf("expected score 100 for %s, got %d", r.PeerIdentity, r.Score)
		}
	}
}

func TestOutHandler_ExistingBinding(t *testing.T) {
	ext := &rtSessionBind{
		bindingKey: "session_id",
		scoreBoost: 80,
		store:      newBindingStore(time.Hour, 100),
	}
	ext.store.Set("sess1", "peer2")

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess1"))

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100, Reason: "realm"},
		{PeerIdentity: "peer2", Score: 100, Reason: "realm"},
	}

	result, err := ext.handleOutgoing(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range result {
		switch r.PeerIdentity {
		case "peer2":
			if r.Score != 180 {
				t.Errorf("expected peer2 score 180, got %d", r.Score)
			}
			if r.Reason != "session-bind" {
				t.Errorf("expected reason 'session-bind', got %s", r.Reason)
			}
		case "peer1":
			if r.Score != 100 {
				t.Errorf("expected peer1 score 100, got %d", r.Score)
			}
		}
	}
}

func TestOutHandler_BoundPeerNotInCandidates(t *testing.T) {
	ext := &rtSessionBind{
		bindingKey: "session_id",
		scoreBoost: 80,
		store:      newBindingStore(time.Hour, 100),
	}
	ext.store.Set("sess1", "peer_gone")

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess1"))

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100, Reason: "realm"},
		{PeerIdentity: "peer2", Score: 100, Reason: "realm"},
	}

	result, err := ext.handleOutgoing(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Bound peer not found; scores should be unchanged
	for _, r := range result {
		if r.Score != 100 {
			t.Errorf("expected score 100 for %s, got %d", r.PeerIdentity, r.Score)
		}
	}
}

func TestOutHandler_AnswerIgnored(t *testing.T) {
	ext := &rtSessionBind{
		bindingKey: "session_id",
		scoreBoost: 80,
		store:      newBindingStore(time.Hour, 100),
	}
	ext.store.Set("sess1", "peer1")

	// Create an answer (not a request)
	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess1"))
	ans := message.NewAnswer(req)
	ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess1"))

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100, Reason: "realm"},
	}

	result, err := ext.handleOutgoing(ans, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Score != 100 {
		t.Errorf("expected answer to not be processed, score unchanged at 100, got %d", result[0].Score)
	}
}

func TestPostRouteHandler_RecordsBinding(t *testing.T) {
	ext := &rtSessionBind{
		bindingKey: "session_id",
		store:      newBindingStore(time.Hour, 100),
	}

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess1"))

	ext.handlePostRoute(msg, "peer1.example.com")

	peer, ok := ext.store.Get("sess1")
	if !ok {
		t.Fatal("expected binding to be recorded")
	}
	if peer != "peer1.example.com" {
		t.Fatalf("expected peer1.example.com, got %s", peer)
	}
}

func TestPostRouteHandler_UpdatesBinding(t *testing.T) {
	ext := &rtSessionBind{
		bindingKey: "session_id",
		store:      newBindingStore(time.Hour, 100),
	}
	ext.store.Set("sess1", "peer1.example.com")

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess1"))

	ext.handlePostRoute(msg, "peer2.example.com")

	peer, ok := ext.store.Get("sess1")
	if !ok {
		t.Fatal("expected binding to exist")
	}
	if peer != "peer2.example.com" {
		t.Fatalf("expected peer2.example.com, got %s", peer)
	}
}

func TestPostRouteHandler_IgnoresAnswers(t *testing.T) {
	ext := &rtSessionBind{
		bindingKey: "session_id",
		store:      newBindingStore(time.Hour, 100),
	}

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	ans := message.NewAnswer(req)
	ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess1"))

	ext.handlePostRoute(ans, "peer1.example.com")

	if ext.store.Len() != 0 {
		t.Fatal("expected no binding for answer messages")
	}
}

func TestIntegration_FullFlow(t *testing.T) {
	ext := &rtSessionBind{}
	ctx := newTestContext()
	err := ext.Init(ctx, map[string]interface{}{
		"binding_key": "session_id",
		"score_boost": 80,
		"ttl":         "1h",
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer func() { _ = ext.Stop() }()

	// First request: no binding exists
	msg1 := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg1.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess-flow"))

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100, Reason: "realm"},
		{PeerIdentity: "peer2", Score: 100, Reason: "realm"},
	}

	result, err := ext.handleOutgoing(msg1, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No binding yet, scores unchanged
	for _, r := range result {
		if r.Score != 100 {
			t.Errorf("expected score 100, got %d for %s", r.Score, r.PeerIdentity)
		}
	}

	// Simulate PostRouteHandler being called after peer1 was selected
	ext.handlePostRoute(msg1, "peer1")

	// Second request with same session: binding should boost peer1
	msg2 := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg2.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess-flow"))

	candidates2 := []routing.Route{
		{PeerIdentity: "peer1", Score: 100, Reason: "realm"},
		{PeerIdentity: "peer2", Score: 100, Reason: "realm"},
	}

	result2, err := ext.handleOutgoing(msg2, candidates2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var peer1Score, peer2Score int
	for _, r := range result2 {
		if r.PeerIdentity == "peer1" {
			peer1Score = r.Score
		} else {
			peer2Score = r.Score
		}
	}
	if peer1Score != 180 {
		t.Errorf("expected peer1 score 180 (100+80), got %d", peer1Score)
	}
	if peer2Score != 100 {
		t.Errorf("expected peer2 score 100, got %d", peer2Score)
	}
}

func TestMetricsProvider(t *testing.T) {
	ext := &rtSessionBind{}
	ctx := &mockInitContext{
		router: routing.NewRouter(),
		dict:   dictionary.New(),
		cfg:    &config.Config{},
	}
	err := ext.Init(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer func() { _ = ext.Stop() }()

	// Do some operations to generate counter values
	ext.store.Set("sess-1", "peer1")
	ext.store.Set("sess-2", "peer2")
	ext.store.Get("sess-1")  // hit
	ext.store.Get("missing") // miss

	metrics := ext.Metrics()
	if len(metrics) != 6 {
		t.Fatalf("expected 6 metrics, got %d", len(metrics))
	}

	metricMap := make(map[string]extension.Metric)
	for _, m := range metrics {
		metricMap[m.Name] = m
	}

	if m, ok := metricMap["bindings_total"]; !ok {
		t.Error("missing bindings_total metric")
	} else if m.Value != 2 {
		t.Errorf("expected bindings_total=2, got %f", m.Value)
	} else if m.Type != extension.MetricGauge {
		t.Errorf("expected bindings_total to be Gauge, got %d", m.Type)
	}

	if m, ok := metricMap["lookups_total"]; !ok {
		t.Error("missing lookups_total metric")
	} else if m.Value != 2 {
		t.Errorf("expected lookups_total=2, got %f", m.Value)
	} else if m.Type != extension.MetricCounter {
		t.Errorf("expected lookups_total to be Counter, got %d", m.Type)
	}

	if m, ok := metricMap["hits_total"]; !ok {
		t.Error("missing hits_total metric")
	} else if m.Value != 1 {
		t.Errorf("expected hits_total=1, got %f", m.Value)
	}

	if m, ok := metricMap["misses_total"]; !ok {
		t.Error("missing misses_total metric")
	} else if m.Value != 1 {
		t.Errorf("expected misses_total=1, got %f", m.Value)
	}
}

func (m *mockInitContext) GetEdgeRegistry() *edge.Registry {
	return edge.NewRegistry(m.GetConfig())
}
