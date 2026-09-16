// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dea_ratelimit

import (
	"errors"
	"sync"
	"sync/atomic"
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
	cfg    *config.Config
}

func (m *mockInitContext) GetDictionary() *dictionary.Dictionary   { return nil }
func (m *mockInitContext) GetRouter() *routing.Router              { return m.router }
func (m *mockInitContext) GetConfig() *config.Config               { return m.cfg }
func (m *mockInitContext) GetPeers() []*peer.Peer                  { return nil }
func (m *mockInitContext) GetStartTime() time.Time                 { return time.Now() }
func (m *mockInitContext) GetExtensionManager() *extension.Manager { return nil }
func (m *mockInitContext) GetEdgeRegistry() *edge.Registry         { return edge.NewRegistry(m.cfg) }

const testConfigYAML = `
identity: "dea.example.com"
realm: "example.com"
zones:
  - name: core
    role: internal
  - name: roaming
    role: external
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com"]
    peers: ["dea1.partnera.com", "dea2.partnera.com"]
    rate_limit:
      enabled: true
      requests_per_second: 100
      burst: 2
      per_peer:
        requests_per_second: 50
        burst: 2
      per_application:
        16777251:
          requests_per_second: 20
          burst: 2
      per_command:
        316:
          requests_per_second: 10
          burst: 1
        317:
          requests_per_second: 5
          burst: 1
          action: drop
  - name: partner-unlimited
    zone: roaming
    realms: ["open.com"]
    peers: ["dea1.open.com"]
`

func newExt(t testing.TB, yaml string, extCfg map[string]interface{}) (*deaRateLimit, *config.Config) {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	ext := &deaRateLimit{}
	if extCfg == nil {
		extCfg = map[string]interface{}{}
	}
	if err := ext.Init(&mockInitContext{router: routing.NewRouter(), cfg: cfg}, extCfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = ext.Stop() })
	return ext, cfg
}

// withClock replaces the limiter with one driven by a controllable clock.
func withClock(ext *deaRateLimit) func(time.Duration) {
	now := time.Now()
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	ext.mu.Lock()
	ext.limiter = newTokenLimiter(0, clock)
	ext.mu.Unlock()
	return func(d time.Duration) {
		mu.Lock()
		now = now.Add(d)
		mu.Unlock()
	}
}

func newPeer(t testing.TB, zone, partner string) *peer.Peer {
	t.Helper()
	p := peer.New(peer.Config{Addresses: []string{"127.0.0.1"}}, dictionary.New())
	p.SetZone(zone, partner)
	return p
}

func request(cmd types.CommandCode, app types.ApplicationID) *message.Message {
	msg := message.NewRequest(cmd, app)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "dea1.partnera.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "partnera.com"))
	return msg
}

func partnerOf(t testing.TB, cfg *config.Config, name string) *config.PartnerConfig {
	t.Helper()
	for i := range cfg.Partners {
		if cfg.Partners[i].Name == name {
			return &cfg.Partners[i]
		}
	}
	t.Fatalf("partner %q not found", name)
	return nil
}

// --- scope precedence ------------------------------------------------------

func TestScopePrecedence(t *testing.T) {
	ext, cfg := newExt(t, testConfigYAML, nil)
	partner := partnerOf(t, cfg, "partner-a")

	tests := []struct {
		name      string
		cmd       types.CommandCode
		app       types.ApplicationID
		wantScope string
		wantRate  float64
	}{
		{"command wins over everything", 316, 16777251, ScopeCommand, 10},
		{"application wins when no command rule", 318, 16777251, ScopeApplication, 20},
		{"peer wins when no application rule", 318, 4, ScopePeer, 50},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := selectRule(partner, ext.keysFor(partner.Name), "dea1.partnera.com", request(tc.cmd, tc.app))
			if !ok {
				t.Fatal("no rule selected")
			}
			if d.scope != tc.wantScope || d.rule.RequestsPerSecond != tc.wantRate {
				t.Errorf("scope=%s rate=%v, want scope=%s rate=%v", d.scope, d.rule.RequestsPerSecond, tc.wantScope, tc.wantRate)
			}
		})
	}
}

func TestPartnerAggregateUsedWhenNoFinerRule(t *testing.T) {
	ext, cfg := newExt(t, testConfigYAML, nil)
	partner := partnerOf(t, cfg, "partner-a")
	partner.RateLimit.PerPeer = nil
	partner.RateLimit.PerApplication = nil
	partner.RateLimit.PerCommand = nil

	d, ok := selectRule(partner, ext.keysFor(partner.Name), "dea1.partnera.com", request(318, 4))
	if !ok || d.scope != ScopePartner || d.rule.RequestsPerSecond != 100 {
		t.Fatalf("selectRule = %+v ok=%v, want the partner aggregate", d, ok)
	}
}

func TestPerPeerBucketsAreIndependent(t *testing.T) {
	ext, cfg := newExt(t, testConfigYAML, nil)
	partner := partnerOf(t, cfg, "partner-a")

	keys := ext.keysFor(partner.Name)
	first, _ := selectRule(partner, keys, "dea1.partnera.com", request(318, 4))
	second, _ := selectRule(partner, keys, "dea2.partnera.com", request(318, 4))
	if first.key == second.key {
		t.Errorf("both peers share the bucket key %q", first.key)
	}
}

// --- enforcement -----------------------------------------------------------

func TestBurstThenRejectThenRefill(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	advance := withClock(ext)

	var answers []*message.Message
	ext.sendFn = func(_ *peer.Peer, m *message.Message) error {
		answers = append(answers, m)
		return nil
	}

	p := newPeer(t, "roaming", "partner-a")
	// The 316 rule allows 10/s with a burst of 1.
	if _, err := ext.handleRouting(p, request(316, 16777251), nil); err != nil {
		t.Fatalf("the first request must pass, got %v", err)
	}
	_, err := ext.handleRouting(p, request(316, 16777251), nil)
	if !errors.Is(err, routing.ErrConsumed) {
		t.Fatalf("the second request must be throttled, got %v", err)
	}
	if len(answers) != 1 {
		t.Fatalf("got %d answers, want 1", len(answers))
	}
	if code, _ := answers[0].GetResultCode(); code != types.ResultTooBusy {
		t.Errorf("Result-Code = %d, want %d", code, types.ResultTooBusy)
	}
	if !answers[0].IsError() {
		t.Error("the throttling answer must have the error bit set")
	}
	if host, ok := answers[0].GetOriginHost(); !ok || host != "dea.example.com" {
		t.Errorf("Origin-Host = %q, want the edge identity", host)
	}

	// One token is regenerated after 100ms at 10/s.
	advance(150 * time.Millisecond)
	if _, err := ext.handleRouting(p, request(316, 16777251), nil); err != nil {
		t.Fatalf("the bucket should have refilled, got %v", err)
	}

	if ext.rejected.Load() != 1 || ext.allowed.Load() != 2 {
		t.Errorf("allowed=%d rejected=%d, want 2 and 1", ext.allowed.Load(), ext.rejected.Load())
	}
}

func TestDropActionSendsNoAnswer(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	withClock(ext)

	sent := 0
	ext.sendFn = func(*peer.Peer, *message.Message) error { sent++; return nil }

	p := newPeer(t, "roaming", "partner-a")
	if _, err := ext.handleRouting(p, request(317, 16777251), nil); err != nil {
		t.Fatalf("the first request must pass, got %v", err)
	}
	_, err := ext.handleRouting(p, request(317, 16777251), nil)
	if !errors.Is(err, routing.ErrConsumed) {
		t.Fatalf("the second request must be dropped, got %v", err)
	}
	if sent != 0 {
		t.Errorf("a dropped request must not be answered, got %d answers", sent)
	}
	if ext.dropped.Load() != 1 {
		t.Errorf("dropped = %d, want 1", ext.dropped.Load())
	}
}

func TestAnswersAreNeverThrottled(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	withClock(ext)
	p := newPeer(t, "roaming", "partner-a")

	for i := 0; i < 5; i++ {
		answer := message.NewAnswer(request(316, 16777251))
		if _, err := ext.handleRouting(p, answer, nil); err != nil {
			t.Fatalf("answers must never be throttled, got %v", err)
		}
	}
	if ext.checked.Load() != 0 {
		t.Errorf("checked = %d, want 0", ext.checked.Load())
	}
}

// --- gating ----------------------------------------------------------------

func TestPartnerWithoutRateLimitIsUntouched(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	p := newPeer(t, "roaming", "partner-unlimited")

	for i := 0; i < 100; i++ {
		if _, err := ext.handleRouting(p, request(316, 16777251), nil); err != nil {
			t.Fatalf("unlimited partner must never be throttled, got %v", err)
		}
	}
	if ext.checked.Load() != 0 {
		t.Errorf("checked = %d, want 0", ext.checked.Load())
	}
}

func TestInertWithoutEdgeConfiguration(t *testing.T) {
	ext, _ := newExt(t, "identity: \"dra.example.com\"\nrealm: \"example.com\"\n", nil)
	p := newPeer(t, "", "")

	for i := 0; i < 50; i++ {
		if _, err := ext.handleRouting(p, request(316, 16777251), nil); err != nil {
			t.Fatalf("a legacy configuration must not be throttled, got %v", err)
		}
	}
}

func TestDisabledExtensionIsInert(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, map[string]interface{}{"enabled": false})
	withClock(ext)
	p := newPeer(t, "roaming", "partner-a")

	for i := 0; i < 10; i++ {
		if _, err := ext.handleRouting(p, request(316, 16777251), nil); err != nil {
			t.Fatalf("a disabled extension must not throttle, got %v", err)
		}
	}
	if err := ext.Reconfigure(map[string]interface{}{"enabled": true}); err != nil {
		t.Fatalf("Reconfigure failed: %v", err)
	}
	ext.sendFn = func(*peer.Peer, *message.Message) error { return nil }
	if _, err := ext.handleRouting(p, request(316, 16777251), nil); err != nil {
		t.Fatalf("the first request after re-enabling must pass, got %v", err)
	}
	if _, err := ext.handleRouting(p, request(316, 16777251), nil); !errors.Is(err, routing.ErrConsumed) {
		t.Fatalf("throttling did not resume after Reconfigure, got %v", err)
	}
}

// --- introspection ---------------------------------------------------------

func TestMetricsAndHealth(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	withClock(ext)
	ext.sendFn = func(*peer.Peer, *message.Message) error { return nil }
	p := newPeer(t, "roaming", "partner-a")

	_, _ = ext.handleRouting(p, request(316, 16777251), nil)
	_, _ = ext.handleRouting(p, request(316, 16777251), nil)

	var limited float64
	for _, m := range ext.Metrics() {
		if m.Name == "limited_total" && m.Labels["partner"] == "partner-a" && m.Labels["scope"] == ScopeCommand {
			limited = m.Value
		}
	}
	if limited != 1 {
		t.Errorf("limited_total{partner=partner-a,scope=command} = %v, want 1", limited)
	}

	status, details := ext.HealthCheck()
	if status != "ok" {
		t.Errorf("HealthCheck status = %q, want ok", status)
	}
	if details["buckets"].(int) != 1 {
		t.Errorf("buckets = %v, want 1", details["buckets"])
	}

	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if status, _ := ext.HealthCheck(); status != "disabled" {
		t.Errorf("HealthCheck status after Stop = %q, want disabled", status)
	}
}

// --- limiter ---------------------------------------------------------------

func TestLimiterUnlimitedRate(t *testing.T) {
	l := newTokenLimiter(0, nil)
	defer l.Close()

	for i := 0; i < 1000; i++ {
		if !l.Allow("k", 0, 0) {
			t.Fatal("a zero rate means unlimited")
		}
	}
	if l.Len() != 0 {
		t.Errorf("unlimited keys must not allocate buckets, got %d", l.Len())
	}
}

func TestLimiterRefillIsCappedAtBurst(t *testing.T) {
	now := time.Now()
	l := newTokenLimiter(0, func() time.Time { return now })
	defer l.Close()

	for i := 0; i < 3; i++ {
		if !l.Allow("k", 10, 3) {
			t.Fatalf("token %d of the burst was rejected", i)
		}
	}
	if l.Allow("k", 10, 3) {
		t.Fatal("the burst should be exhausted")
	}

	now = now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		if !l.Allow("k", 10, 3) {
			t.Fatalf("token %d should be available after a long idle period", i)
		}
	}
	if l.Allow("k", 10, 3) {
		t.Fatal("the bucket must not refill beyond its burst")
	}
}

func TestLimiterRescalesOnPolicyChange(t *testing.T) {
	now := time.Now()
	l := newTokenLimiter(0, func() time.Time { return now })
	defer l.Close()

	if !l.Allow("k", 10, 10) {
		t.Fatal("the first request must pass")
	}
	// Tightening the policy must take effect immediately.
	if !l.Allow("k", 1, 1) {
		t.Fatal("one token should remain after rescaling")
	}
	if l.Allow("k", 1, 1) {
		t.Fatal("the tightened burst should be exhausted")
	}
}

func TestLimiterUtilization(t *testing.T) {
	now := time.Now()
	l := newTokenLimiter(0, func() time.Time { return now })
	defer l.Close()

	if _, ok := l.Utilization("k"); ok {
		t.Error("an unknown key has no utilization")
	}
	l.Allow("k", 10, 2)
	u, ok := l.Utilization("k")
	if !ok || u != 0.5 {
		t.Errorf("Utilization = %v, %v; want 0.5, true", u, ok)
	}
}

func TestLimiterSweepReclaimsIdleBuckets(t *testing.T) {
	now := time.Now()
	l := newTokenLimiter(0, func() time.Time { return now })
	defer l.Close()

	l.Allow("k", 10, 1)
	if l.Len() != 1 {
		t.Fatalf("Len = %d, want 1", l.Len())
	}
	now = now.Add(2 * idleBucketTTL)
	l.sweep()
	if l.Len() != 0 {
		t.Errorf("Len after sweep = %d, want 0", l.Len())
	}
}

func TestLimiterIsConcurrencySafe(t *testing.T) {
	l := newTokenLimiter(0, nil)
	defer l.Close()

	const goroutines, perGoroutine = 16, 200
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if l.Allow("shared", 0.0001, 100) {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	if got := allowed.Load(); got < 100 || got > 105 {
		t.Errorf("allowed = %d, want approximately the burst of 100", got)
	}
}
