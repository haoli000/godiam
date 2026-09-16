// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dea_route

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
peers:
  - identity: "hss1.example.com"
    realm: "example.com"
    zone: core
    addresses: ["10.0.0.1"]
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com"]
    peers:
      - identity: "dea1.partnera.com"
        priority: 0
        weight: 3
      - identity: "dea2.partnera.com"
        priority: 0
        weight: 1
      - identity: "backup.partnera.com"
        priority: 1
  - name: partner-b
    zone: roaming
    realms: ["partnerb.com"]
    peers: ["dea1.partnerb.com"]
  - name: partner-transit
    zone: roaming
    realms: ["transit.com"]
    peers: ["dea1.transit.com"]
    allow_transit: true
`

func newExt(t *testing.T, yaml string, extCfg map[string]interface{}) *deaRoute {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	ext := &deaRoute{intn: func(int) int { return 0 }}
	if extCfg == nil {
		extCfg = map[string]interface{}{}
	}
	if err := ext.Init(&mockInitContext{router: routing.NewRouter(), cfg: cfg}, extCfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return ext
}

// request builds a request with the given origin and destination realms.
func request(originRealm, destRealm string) *message.Message {
	msg := message.NewRequest(316, 16777251)
	if originRealm != "" {
		msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID(originRealm)))
	}
	if destRealm != "" {
		msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, types.DiamID(destRealm)))
	}
	return msg
}

func scoreOf(candidates []routing.Route, identity string) (int, bool) {
	for _, c := range candidates {
		if string(c.PeerIdentity) == identity {
			return c.Score, true
		}
	}
	return 0, false
}

func identities(candidates []routing.Route) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, string(c.PeerIdentity))
	}
	return out
}

// --- candidate construction ------------------------------------------------

func TestPartnerPeersAreAddedInPriorityTiers(t *testing.T) {
	ext := newExt(t, testConfigYAML, nil)

	got, err := ext.handleRouting(request("example.com", "partnera.com"), nil)
	if err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d candidates (%v), want 3", len(got), identities(got))
	}

	primary, _ := scoreOf(got, "dea1.partnera.com")
	secondary, _ := scoreOf(got, "dea2.partnera.com")
	backup, _ := scoreOf(got, "backup.partnera.com")

	if primary <= secondary {
		t.Errorf("the elected winner must outrank its tier: %d vs %d", primary, secondary)
	}
	if secondary <= backup {
		t.Errorf("priority 0 must outrank priority 1: %d vs %d", secondary, backup)
	}
	// The lone member of a tier is always its winner.
	if backup != defaultBaseScore-tierStep+winnerBonus {
		t.Errorf("backup score = %d, want %d", backup, defaultBaseScore-tierStep+winnerBonus)
	}
}

func TestWeightedElectionFollowsWeights(t *testing.T) {
	// The tier is dea1 (weight 3) then dea2 (weight 1), so draws 0..2 elect
	// dea1 and draw 3 elects dea2.
	for draw, want := range map[int]string{0: "dea1.partnera.com", 2: "dea1.partnera.com", 3: "dea2.partnera.com"} {
		ext := newExt(t, testConfigYAML, nil)
		ext.intn = func(int) int { return draw }

		got, err := ext.handleRouting(request("example.com", "partnera.com"), nil)
		if err != nil {
			t.Fatalf("handleRouting returned %v", err)
		}
		winner, best := "", 0
		for _, c := range got {
			if c.Score > best {
				winner, best = string(c.PeerIdentity), c.Score
			}
		}
		if winner != want {
			t.Errorf("draw %d elected %q, want %q", draw, winner, want)
		}
	}
}

func TestExistingHigherScoreIsPreserved(t *testing.T) {
	ext := newExt(t, testConfigYAML, nil)

	existing := []routing.Route{{PeerIdentity: "dea2.partnera.com", Score: 1000, Reason: "direct host route"}}
	got, err := ext.handleRouting(request("example.com", "partnera.com"), existing)
	if err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}
	if score, _ := scoreOf(got, "dea2.partnera.com"); score != 1000 {
		t.Errorf("score = %d, want the direct host route to win", score)
	}
}

func TestDestinationHostSelectsThePartner(t *testing.T) {
	ext := newExt(t, testConfigYAML, nil)

	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "backup.partnera.com"))

	got, err := ext.handleRouting(msg, nil)
	if err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %v, want the partner peer list", identities(got))
	}
}

// --- delivery rules --------------------------------------------------------

func TestForeignExternalCandidatesAreDropped(t *testing.T) {
	ext := newExt(t, testConfigYAML, nil)

	candidates := []routing.Route{
		{PeerIdentity: "dea1.partnerb.com", Score: 100},
		{PeerIdentity: "hss1.example.com", Score: 100},
	}
	got, err := ext.handleRouting(request("example.com", "partnera.com"), candidates)
	if err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}
	for _, id := range identities(got) {
		if id == "dea1.partnerb.com" {
			t.Fatalf("a peer of another partner survived: %v", identities(got))
		}
	}
	if _, ok := scoreOf(got, "hss1.example.com"); !ok {
		t.Error("internal candidates must be preserved")
	}
	if ext.misdeliver.Load() != 1 {
		t.Errorf("foreign candidates dropped = %d, want 1", ext.misdeliver.Load())
	}
}

func TestTransitBetweenPartnersIsRefused(t *testing.T) {
	ext := newExt(t, testConfigYAML, nil)

	candidates := []routing.Route{
		{PeerIdentity: "dea1.partnerb.com", Score: 100},
		{PeerIdentity: "hss1.example.com", Score: 100},
	}
	got, err := ext.handleRouting(request("partnera.com", "partnerb.com"), candidates)
	if err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}
	// Refusing transit must fail closed. Keeping the internal candidate would
	// deliver the refused request to the HSS instead of answering
	// DIAMETER_UNABLE_TO_DELIVER.
	if ids := identities(got); len(ids) != 0 {
		t.Errorf("candidates = %v, want none so the router refuses the request", ids)
	}
	if ext.transit.Load() != 1 {
		t.Errorf("transit_refused = %d, want 1", ext.transit.Load())
	}
	if ext.routed.Load() != 0 {
		t.Error("a refused transit must not count as routed")
	}
}

func TestTransitPartnerMayReachAnotherPartner(t *testing.T) {
	ext := newExt(t, testConfigYAML, nil)

	got, err := ext.handleRouting(request("transit.com", "partnera.com"), nil)
	if err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}
	if len(got) != 3 {
		t.Errorf("candidates = %v, want the partner-a peer list", identities(got))
	}
	if ext.transit.Load() != 0 {
		t.Error("an allowed transit must not be counted as refused")
	}
}

func TestInternalDestinationIsUntouched(t *testing.T) {
	ext := newExt(t, testConfigYAML, nil)

	candidates := []routing.Route{{PeerIdentity: "hss1.example.com", Score: 100}}
	got, err := ext.handleRouting(request("partnera.com", "example.com"), candidates)
	if err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}
	if len(got) != 1 || got[0].Score != 100 {
		t.Errorf("candidates = %+v, want them unchanged", got)
	}
}

// --- gating ----------------------------------------------------------------

func TestEvaluatedCountsInternalDestinations(t *testing.T) {
	ext := newExt(t, testConfigYAML, nil)

	// An external-to-internal message moves none of the outcome counters, so
	// the evaluation counter is the only evidence the control ran.
	candidates := []routing.Route{{PeerIdentity: "hss1.example.com", Score: 100}}
	if _, err := ext.handleRouting(request("partnera.com", "example.com"), candidates); err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}

	if got := ext.evaluated.Load(); got != 1 {
		t.Errorf("evaluated = %d, want 1", got)
	}
	if got := ext.routed.Load(); got != 0 {
		t.Errorf("routed = %d, want 0 for an internal destination", got)
	}
}

func TestEvaluatedStaysZeroWhileInert(t *testing.T) {
	ext := newExt(t, "identity: \"dra.example.com\"\nrealm: \"example.com\"\n", nil)

	if _, err := ext.handleRouting(request("partnera.com", "example.com"), nil); err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}
	if got := ext.evaluated.Load(); got != 0 {
		t.Errorf("evaluated = %d, want 0 without an edge configuration", got)
	}
}

func TestInertWithoutEdgeConfiguration(t *testing.T) {
	ext := newExt(t, "identity: \"dra.example.com\"\nrealm: \"example.com\"\n", nil)

	candidates := []routing.Route{{PeerIdentity: "peer.partnera.com", Score: 100}}
	got, err := ext.handleRouting(request("partnera.com", "partnerb.com"), candidates)
	if err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}
	if len(got) != 1 {
		t.Errorf("candidates = %v, want a legacy configuration to be untouched", identities(got))
	}
}

func TestDisabledExtensionIsInert(t *testing.T) {
	ext := newExt(t, testConfigYAML, map[string]interface{}{"enabled": false})

	got, _ := ext.handleRouting(request("example.com", "partnera.com"), nil)
	if len(got) != 0 {
		t.Errorf("candidates = %v, want none while disabled", identities(got))
	}

	if err := ext.Reconfigure(map[string]interface{}{"enabled": true, "base_score": 400}); err != nil {
		t.Fatalf("Reconfigure failed: %v", err)
	}
	got, _ = ext.handleRouting(request("example.com", "partnera.com"), nil)
	if score, ok := scoreOf(got, "backup.partnera.com"); !ok || score != 400-tierStep+winnerBonus {
		t.Errorf("score = %d, want the reconfigured base score to apply", score)
	}
}

// --- introspection ---------------------------------------------------------

func TestMetricsAndHealth(t *testing.T) {
	ext := newExt(t, testConfigYAML, nil)
	_, _ = ext.handleRouting(request("example.com", "partnera.com"), nil)

	var perPartner float64
	for _, m := range ext.Metrics() {
		if m.Name == "partner_routed_total" && m.Labels["partner"] == "partner-a" {
			perPartner = m.Value
		}
	}
	if perPartner != 1 {
		t.Errorf("partner_routed_total{partner=partner-a} = %v, want 1", perPartner)
	}

	status, details := ext.HealthCheck()
	if status != "ok" || details["partners"].(int) != 3 {
		t.Errorf("HealthCheck = %q, %v", status, details)
	}
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if status, _ := ext.HealthCheck(); status != "disabled" {
		t.Errorf("HealthCheck status after Stop = %q, want disabled", status)
	}
}
