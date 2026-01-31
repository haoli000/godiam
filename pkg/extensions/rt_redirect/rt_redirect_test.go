// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_redirect

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

func newExt(t *testing.T, cfg map[string]interface{}) *rtRedirect {
	t.Helper()
	ext := &rtRedirect{}
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
	ext := &rtRedirect{}
	if ext.Name() != "rt_redirect" {
		t.Errorf("expected name rt_redirect, got %s", ext.Name())
	}
}

func TestInit_Defaults(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	if ext.scoreBoost != defaultScoreBoost {
		t.Errorf("expected scoreBoost=%d, got %d", defaultScoreBoost, ext.scoreBoost)
	}
	if ext.maxEntries != 10000 {
		t.Errorf("expected maxEntries=10000, got %d", ext.maxEntries)
	}
	if ext.cacheTTL != time.Duration(defaultCacheTime)*time.Second {
		t.Errorf("expected cacheTTL=%v, got %v", time.Duration(defaultCacheTime)*time.Second, ext.cacheTTL)
	}
}

func TestInit_CustomConfig(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"score_boost":        50,
		"max_entries":        500,
		"default_cache_time": "30m",
	})
	defer func() { _ = ext.Stop() }()

	if ext.scoreBoost != 50 {
		t.Errorf("expected scoreBoost=50, got %d", ext.scoreBoost)
	}
	if ext.maxEntries != 500 {
		t.Errorf("expected maxEntries=500, got %d", ext.maxEntries)
	}
	if ext.cacheTTL != 30*time.Minute {
		t.Errorf("expected cacheTTL=30m, got %v", ext.cacheTTL)
	}
}

func TestInit_Float64Config(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"score_boost": float64(75),
		"max_entries": float64(200),
	})
	defer func() { _ = ext.Stop() }()

	if ext.scoreBoost != 75 {
		t.Errorf("expected scoreBoost=75, got %d", ext.scoreBoost)
	}
	if ext.maxEntries != 200 {
		t.Errorf("expected maxEntries=200, got %d", ext.maxEntries)
	}
}

func makeRedirectAnswer(appID types.ApplicationID, realm string, redirectHosts ...string) *message.Message {
	req := message.NewRequest(types.CmdCodeCreditControl, appID)
	if realm != "" {
		req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, types.DiamID(realm)))
	}
	ans := message.NewAnswer(req)
	ans.SetResultCode(types.ResultRedirectIndication)
	if realm != "" {
		ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, types.DiamID(realm)))
	}
	for _, host := range redirectHosts {
		ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRedirectHost, types.AVPFlagMandatory, types.DiamID(host)))
	}
	return ans
}

func TestHandleAnswer_CachesRedirect(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	ans := makeRedirectAnswer(types.AppIDCreditControl, "example.com", "hss2.example.com")

	result, err := ext.handleAnswer(nil, ans, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result

	if ext.redirectsStored.Load() != 1 {
		t.Errorf("expected redirectsStored=1, got %d", ext.redirectsStored.Load())
	}

	ext.mu.RLock()
	entry, ok := ext.cache[redirectKey{appID: types.AppIDCreditControl, destRealm: "example.com"}]
	ext.mu.RUnlock()

	if !ok {
		t.Fatal("expected cache entry to exist")
	}
	if len(entry.redirectHosts) != 1 || entry.redirectHosts[0] != "hss2.example.com" {
		t.Errorf("unexpected redirect hosts: %v", entry.redirectHosts)
	}
}

func TestHandleAnswer_MultipleRedirectHosts(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	ans := makeRedirectAnswer(types.AppIDCreditControl, "example.com", "hss1.example.com", "hss2.example.com")

	_, _ = ext.handleAnswer(nil, ans, nil)

	ext.mu.RLock()
	entry, ok := ext.cache[redirectKey{appID: types.AppIDCreditControl, destRealm: "example.com"}]
	ext.mu.RUnlock()

	if !ok {
		t.Fatal("expected cache entry")
	}
	if len(entry.redirectHosts) != 2 {
		t.Errorf("expected 2 redirect hosts, got %d", len(entry.redirectHosts))
	}
}

func TestHandleAnswer_IgnoresRequests(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)

	_, err := ext.handleAnswer(nil, msg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ext.redirectsStored.Load() != 0 {
		t.Error("should not cache for requests")
	}
}

func TestHandleAnswer_IgnoresNonRedirectAnswers(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	ans := message.NewAnswer(req)
	ans.SetResultCode(types.ResultSuccess) // Not a redirect

	_, _ = ext.handleAnswer(nil, ans, nil)
	if ext.redirectsStored.Load() != 0 {
		t.Error("should not cache non-redirect answers")
	}
}

func TestHandleAnswer_IgnoresRedirectWithoutHosts(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	ans := message.NewAnswer(req)
	ans.SetResultCode(types.ResultRedirectIndication)
	// No Redirect-Host AVPs

	_, _ = ext.handleAnswer(nil, ans, nil)
	if ext.redirectsStored.Load() != 0 {
		t.Error("should not cache redirect without hosts")
	}
}

func TestHandleAnswer_CustomCacheTime(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	ans := makeRedirectAnswer(types.AppIDCreditControl, "example.com", "hss1.example.com")
	// Add Redirect-Max-Cache-Time = 60 seconds
	ans.AddAVP(message.NewUnsigned32AVP(types.AVPCodeRedirectMaxCacheTime, types.AVPFlagMandatory, 60))

	before := time.Now()
	_, _ = ext.handleAnswer(nil, ans, nil)

	ext.mu.RLock()
	entry := ext.cache[redirectKey{appID: types.AppIDCreditControl, destRealm: "example.com"}]
	ext.mu.RUnlock()

	// Should expire ~60 seconds from now, not the default 3600
	expectedExpiry := before.Add(60 * time.Second)
	if entry.expiresAt.Before(expectedExpiry.Add(-5*time.Second)) || entry.expiresAt.After(expectedExpiry.Add(5*time.Second)) {
		t.Errorf("expected expiry ~60s from now, got %v (now=%v)", entry.expiresAt, before)
	}
}

func TestHandleRequest_CacheHit(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	// Pre-populate cache
	key := redirectKey{appID: types.AppIDCreditControl, destRealm: "example.com"}
	ext.mu.Lock()
	ext.cache[key] = &redirectEntry{
		redirectHosts: []types.DiamID{"hss2.example.com"},
		expiresAt:     time.Now().Add(1 * time.Hour),
	}
	ext.mu.Unlock()

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	candidates := []routing.Route{
		{PeerIdentity: "hss1.example.com", Score: 100},
		{PeerIdentity: "hss2.example.com", Score: 100},
		{PeerIdentity: "hss3.example.com", Score: 100},
	}

	result, err := ext.handleRequest(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range result {
		switch r.PeerIdentity {
		case "hss2.example.com":
			if r.Score != 200 {
				t.Errorf("expected hss2 score 200, got %d", r.Score)
			}
		default:
			if r.Score != 100 {
				t.Errorf("expected %s score 100, got %d", r.PeerIdentity, r.Score)
			}
		}
	}

	if ext.cacheHits.Load() != 1 {
		t.Errorf("expected cacheHits=1, got %d", ext.cacheHits.Load())
	}
	if ext.redirectsApplied.Load() != 1 {
		t.Errorf("expected redirectsApplied=1, got %d", ext.redirectsApplied.Load())
	}
}

func TestHandleRequest_CacheMiss(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "unknown.com"))

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
	}

	result, err := ext.handleRequest(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Score != 100 {
		t.Error("score should be unchanged on cache miss")
	}
	if ext.cacheMisses.Load() != 1 {
		t.Errorf("expected cacheMisses=1, got %d", ext.cacheMisses.Load())
	}
}

func TestHandleRequest_ExpiredEntry(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	key := redirectKey{appID: types.AppIDCreditControl, destRealm: "example.com"}
	ext.mu.Lock()
	ext.cache[key] = &redirectEntry{
		redirectHosts: []types.DiamID{"hss2.example.com"},
		expiresAt:     time.Now().Add(-1 * time.Hour), // Already expired
	}
	ext.mu.Unlock()

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	candidates := []routing.Route{
		{PeerIdentity: "hss2.example.com", Score: 100},
	}

	result, err := ext.handleRequest(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Score != 100 {
		t.Error("expired entry should not boost score")
	}
	if ext.cacheMisses.Load() != 1 {
		t.Errorf("expected cacheMisses=1 for expired entry, got %d", ext.cacheMisses.Load())
	}

	// Verify entry was removed from cache
	ext.mu.RLock()
	_, exists := ext.cache[key]
	ext.mu.RUnlock()
	if exists {
		t.Error("expired entry should be removed from cache")
	}
}

func TestHandleRequest_IgnoresAnswers(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	ans := message.NewAnswer(req)

	candidates := []routing.Route{
		{PeerIdentity: "peer1", Score: 100},
	}

	result, err := ext.handleRequest(ans, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Score != 100 {
		t.Error("answer should not be processed by OutHandler")
	}
}

func TestIntegration_FullFlow(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"score_boost": 80,
	})
	defer func() { _ = ext.Stop() }()

	// Step 1: Receive redirect answer
	ans := makeRedirectAnswer(types.AppIDCreditControl, "realm-a.com", "target-hss.realm-a.com")
	_, _ = ext.handleAnswer(nil, ans, nil)

	// Step 2: Next request should have the redirect target boosted
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "realm-a.com"))

	candidates := []routing.Route{
		{PeerIdentity: "hss1.realm-a.com", Score: 100},
		{PeerIdentity: "target-hss.realm-a.com", Score: 100},
	}

	result, err := ext.handleRequest(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var targetScore, otherScore int
	for _, r := range result {
		if r.PeerIdentity == "target-hss.realm-a.com" {
			targetScore = r.Score
		} else {
			otherScore = r.Score
		}
	}

	if targetScore != 180 {
		t.Errorf("expected target score 180 (100+80), got %d", targetScore)
	}
	if otherScore != 100 {
		t.Errorf("expected other score 100, got %d", otherScore)
	}
}

func TestEvictExpired(t *testing.T) {
	ext := newExt(t, map[string]interface{}{
		"max_entries": 2,
	})
	defer func() { _ = ext.Stop() }()

	// Add an expired entry
	ext.mu.Lock()
	ext.cache[redirectKey{appID: 1, destRealm: "old.com"}] = &redirectEntry{
		redirectHosts: []types.DiamID{"host1"},
		expiresAt:     time.Now().Add(-1 * time.Hour),
	}
	ext.cache[redirectKey{appID: 2, destRealm: "current.com"}] = &redirectEntry{
		redirectHosts: []types.DiamID{"host2"},
		expiresAt:     time.Now().Add(1 * time.Hour),
	}
	ext.mu.Unlock()

	// Adding a new entry should trigger eviction since max_entries=2
	ans := makeRedirectAnswer(3, "new.com", "host3")
	_, _ = ext.handleAnswer(nil, ans, nil)

	ext.mu.RLock()
	_, oldExists := ext.cache[redirectKey{appID: 1, destRealm: "old.com"}]
	_, currentExists := ext.cache[redirectKey{appID: 2, destRealm: "current.com"}]
	_, newExists := ext.cache[redirectKey{appID: 3, destRealm: "new.com"}]
	ext.mu.RUnlock()

	if oldExists {
		t.Error("expected expired entry to be evicted")
	}
	if !currentExists {
		t.Error("expected current entry to remain")
	}
	if !newExists {
		t.Error("expected new entry to exist")
	}
}

func TestStop_ClearsCache(t *testing.T) {
	ext := newExt(t, nil)

	ans := makeRedirectAnswer(types.AppIDCreditControl, "example.com", "hss1")
	_, _ = ext.handleAnswer(nil, ans, nil)

	_ = ext.Stop()

	ext.mu.RLock()
	count := len(ext.cache)
	ext.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected cache cleared after Stop, got %d entries", count)
	}
}

func TestMetrics(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	metrics := ext.Metrics()
	if len(metrics) != 5 {
		t.Fatalf("expected 5 metrics, got %d", len(metrics))
	}

	m := make(map[string]extension.Metric)
	for _, metric := range metrics {
		m[metric.Name] = metric
	}

	expected := []string{"cache_size", "cache_hits_total", "cache_misses_total", "redirects_stored_total", "redirects_applied_total"}
	for _, name := range expected {
		if _, ok := m[name]; !ok {
			t.Errorf("missing metric %s", name)
		}
	}
	if m["cache_size"].Type != extension.MetricGauge {
		t.Error("cache_size should be Gauge")
	}
	if m["cache_hits_total"].Type != extension.MetricCounter {
		t.Error("cache_hits_total should be Counter")
	}
}

func TestMetrics_AfterOperations(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	// Store a redirect
	ans := makeRedirectAnswer(types.AppIDCreditControl, "example.com", "hss2")
	_, _ = ext.handleAnswer(nil, ans, nil)

	// Hit it
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	_, _ = ext.handleRequest(msg, []routing.Route{{PeerIdentity: "hss2", Score: 100}})

	// Miss
	msg2 := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg2.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "other.com"))
	_, _ = ext.handleRequest(msg2, nil)

	metrics := ext.Metrics()
	m := make(map[string]extension.Metric)
	for _, metric := range metrics {
		m[metric.Name] = metric
	}

	if m["cache_size"].Value != 1 {
		t.Errorf("expected cache_size=1, got %f", m["cache_size"].Value)
	}
	if m["redirects_stored_total"].Value != 1 {
		t.Errorf("expected redirects_stored=1, got %f", m["redirects_stored_total"].Value)
	}
	if m["cache_hits_total"].Value != 1 {
		t.Errorf("expected hits=1, got %f", m["cache_hits_total"].Value)
	}
	if m["cache_misses_total"].Value != 1 {
		t.Errorf("expected misses=1, got %f", m["cache_misses_total"].Value)
	}
	if m["redirects_applied_total"].Value != 1 {
		t.Errorf("expected applied=1, got %f", m["redirects_applied_total"].Value)
	}
}

func TestHandleRequest_MultipleRedirectHostsBoost(t *testing.T) {
	ext := newExt(t, nil)
	defer func() { _ = ext.Stop() }()

	// Cache with two redirect hosts
	key := redirectKey{appID: types.AppIDCreditControl, destRealm: "example.com"}
	ext.mu.Lock()
	ext.cache[key] = &redirectEntry{
		redirectHosts: []types.DiamID{"hss1", "hss2"},
		expiresAt:     time.Now().Add(1 * time.Hour),
	}
	ext.mu.Unlock()

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	candidates := []routing.Route{
		{PeerIdentity: "hss1", Score: 100},
		{PeerIdentity: "hss2", Score: 100},
		{PeerIdentity: "hss3", Score: 100},
	}

	result, err := ext.handleRequest(msg, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	boostedCount := 0
	for _, r := range result {
		if r.Score == 200 {
			boostedCount++
		}
	}
	if boostedCount != 2 {
		t.Errorf("expected 2 candidates boosted, got %d", boostedCount)
	}
}
