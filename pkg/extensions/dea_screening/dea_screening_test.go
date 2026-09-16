// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dea_screening

import (
	"errors"
	"strings"
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
    realms: ["partnera.com", "*.partnera.com"]
    peers: ["dea1.partnera.com"]
    plmn_ids: ["24001"]
    applications: [16777251]
    commands: [316]
    screening:
      max_route_records: 3
  - name: partner-open
    zone: roaming
    realms: ["open.com"]
    peers: ["dea1.open.com"]
    allow_transit: true
    screening:
      default_action: drop
`

func testConfig(t testing.TB, yaml string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	return cfg
}

func newExt(t testing.TB, yaml string, extCfg map[string]interface{}) (*deaScreening, *config.Config) {
	t.Helper()
	cfg := testConfig(t, yaml)
	ext := &deaScreening{}
	ctx := &mockInitContext{router: routing.NewRouter(), cfg: cfg}
	if extCfg == nil {
		extCfg = map[string]interface{}{}
	}
	if err := ext.Init(ctx, extCfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return ext, cfg
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

// ulr builds a well-formed S6a Update-Location-Request from partner-a.
func ulr() *message.Message {
	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "dea1.partnera.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "partnera.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	return msg
}

func setAVP(msg *message.Message, avp *message.AVP) {
	msg.AddAVP(avp)
}

func replaceAVP(msg *message.Message, code types.AVPCode, value string) {
	for _, avp := range msg.AVPs {
		if avp.Code == code {
			avp.Data = []byte(value)
			return
		}
	}
	msg.AddAVP(message.NewDiameterIdentityAVP(code, types.AVPFlagMandatory, types.DiamID(value)))
}

func removeAVP(msg *message.Message, code types.AVPCode) {
	for i, avp := range msg.AVPs {
		if avp.Code == code {
			msg.AVPs = append(msg.AVPs[:i], msg.AVPs[i+1:]...)
			return
		}
	}
}

func TestName(t *testing.T) {
	if (&deaScreening{}).Name() != "dea_screening" {
		t.Fatal("unexpected extension name")
	}
}

func TestCleanMessagePasses(t *testing.T) {
	ext, cfg := newExt(t, testConfigYAML, nil)
	if v := ext.evaluate(ext.reg, partnerOf(t, cfg, "partner-a"), "dea1.partnera.com", ulr(), "example.com"); v != nil {
		t.Fatalf("unexpected violation: %+v", v)
	}
}

func TestRuleCatalogue(t *testing.T) {
	ext, cfg := newExt(t, testConfigYAML, nil)
	partner := partnerOf(t, cfg, "partner-a")

	tests := []struct {
		name     string
		mutate   func(*message.Message)
		peerID   string
		wantRule string
	}{
		{
			name:     "origin realm missing",
			mutate:   func(m *message.Message) { removeAVP(m, types.AVPCodeOriginRealm) },
			wantRule: RuleOriginRealm,
		},
		{
			name:     "origin realm spoofed",
			mutate:   func(m *message.Message) { replaceAVP(m, types.AVPCodeOriginRealm, "attacker.com") },
			wantRule: RuleOriginRealm,
		},
		{
			name:     "origin host outside origin realm",
			mutate:   func(m *message.Message) { replaceAVP(m, types.AVPCodeOriginHost, "hss.example.com") },
			wantRule: RuleOriginHost,
		},
		{
			name:     "origin host missing",
			mutate:   func(m *message.Message) { removeAVP(m, types.AVPCodeOriginHost) },
			wantRule: RuleOriginHost,
		},
		{
			name:     "destination realm missing",
			mutate:   func(m *message.Message) { removeAVP(m, types.AVPCodeDestinationRealm) },
			wantRule: RuleDestinationRealm,
		},
		{
			name:     "transit to a foreign realm",
			mutate:   func(m *message.Message) { replaceAVP(m, types.AVPCodeDestinationRealm, "thirdparty.com") },
			wantRule: RuleDestinationRealm,
		},
		{
			name:     "application not permitted",
			mutate:   func(m *message.Message) { m.ApplicationID = 4 },
			wantRule: RuleApplication,
		},
		{
			name:     "command not permitted",
			mutate:   func(m *message.Message) { m.CommandCode = 272 },
			wantRule: RuleCommand,
		},
		{
			name: "visited plmn not owned",
			mutate: func(m *message.Message) {
				setAVP(m, message.NewVendorAVP(types.AVPCode3GPPVisitedPLMNID, types.AVPFlagMandatory, vendor3GPP, encodePLMN("310410")))
			},
			wantRule: RulePLMN,
		},
		{
			name: "imsi not owned",
			mutate: func(m *message.Message) {
				setAVP(m, message.NewUTF8StringAVP(types.AVPCodeUserName, types.AVPFlagMandatory, "310410123456789"))
			},
			wantRule: RulePLMN,
		},
		{
			name: "too many route records",
			mutate: func(m *message.Message) {
				for i := 0; i < 4; i++ {
					setAVP(m, message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, "hop.partnera.com"))
				}
			},
			wantRule: RuleRouteRecords,
		},
		{
			name:     "origin host does not match peer",
			peerID:   "dea1.partnera.com",
			mutate:   func(m *message.Message) { replaceAVP(m, types.AVPCodeOriginHost, "mme.evil.com") },
			wantRule: RuleOriginHost,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := ulr()
			tc.mutate(msg)
			v := ext.evaluate(ext.reg, partner, tc.peerID, msg, "example.com")
			if v == nil {
				t.Fatalf("expected violation of rule %q, got none", tc.wantRule)
			}
			if v.rule != tc.wantRule {
				t.Fatalf("rule = %q (%s), want %q", v.rule, v.detail, tc.wantRule)
			}
		})
	}
}

func TestPeerIdentityRuleAllowsPartnerProxy(t *testing.T) {
	ext, cfg := newExt(t, testConfigYAML, nil)
	partner := partnerOf(t, cfg, "partner-a")

	// A partner proxy relaying one of its own hosts is legitimate.
	msg := ulr()
	replaceAVP(msg, types.AVPCodeOriginHost, "mme.vplmn.partnera.com")
	replaceAVP(msg, types.AVPCodeOriginRealm, "vplmn.partnera.com")
	if v := ext.evaluate(ext.reg, partner, "dea1.partnera.com", msg, "example.com"); v != nil {
		t.Fatalf("unexpected violation for partner proxy: %+v", v)
	}

	// A host from a realm outside the partner is not.
	msg = ulr()
	replaceAVP(msg, types.AVPCodeOriginHost, "mme.other.com")
	replaceAVP(msg, types.AVPCodeOriginRealm, "other.com")
	v := ext.evaluate(ext.reg, partner, "dea1.partnera.com", msg, "example.com")
	if v == nil || v.rule != RuleOriginRealm {
		t.Fatalf("expected origin realm violation, got %+v", v)
	}
}

func TestAllowTransitSkipsDestinationRealmRule(t *testing.T) {
	ext, cfg := newExt(t, testConfigYAML, nil)
	partner := partnerOf(t, cfg, "partner-open")

	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "dea1.open.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "open.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "thirdparty.com"))

	if v := ext.evaluate(ext.reg, partner, "dea1.open.com", msg, "example.com"); v != nil {
		t.Fatalf("transit partner should not be screened for destination realm: %+v", v)
	}
}

func TestAVPAllowAndDenyLists(t *testing.T) {
	cfgYAML := `
identity: "dea.example.com"
realm: "example.com"
zones:
  - name: roaming
    role: external
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com"]
    screening:
      check_plmn: false
      deny_avps:
        - code: 1407
          vendor_id: 10415
`
	ext, cfg := newExt(t, cfgYAML, nil)
	partner := partnerOf(t, cfg, "partner-a")

	// Denied AVP nested inside a grouped AVP must still be found.
	msg := ulr()
	grouped := message.NewGroupedAVP(types.AVPCodeProxyInfo, types.AVPFlagMandatory, 0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "dea1.partnera.com"),
		message.NewVendorAVP(types.AVPCode3GPPVisitedPLMNID, types.AVPFlagMandatory, vendor3GPP, encodePLMN("24001")),
	)
	msg.AddAVP(grouped)
	v := ext.evaluate(ext.reg, partner, "", msg, "example.com")
	if v == nil || v.rule != RuleAVPDenied {
		t.Fatalf("expected nested denied AVP violation, got %+v", v)
	}

	// Allow list rejects anything not explicitly listed.
	partner.Screening.DenyAVPs = nil
	partner.Screening.AllowAVPs = []config.AVPSelector{
		{Code: uint32(types.AVPCodeOriginHost)},
		{Code: uint32(types.AVPCodeOriginRealm)},
	}
	v = ext.evaluate(ext.reg, partner, "", ulr(), "example.com")
	if v == nil || v.rule != RuleAVPNotAllowed {
		t.Fatalf("expected allow-list violation, got %+v", v)
	}
}

func TestDisabledChecksArePassed(t *testing.T) {
	cfgYAML := `
identity: "dea.example.com"
realm: "example.com"
zones:
  - name: roaming
    role: external
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com"]
    screening:
      check_origin_realm: false
      check_origin_host: false
      check_peer_identity: false
      check_destination_realm: false
`
	ext, cfg := newExt(t, cfgYAML, nil)
	partner := partnerOf(t, cfg, "partner-a")

	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "evil.attacker.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "attacker.com"))

	if v := ext.evaluate(ext.reg, partner, "dea1.partnera.com", msg, "example.com"); v != nil {
		t.Fatalf("all checks disabled, expected no violation, got %+v", v)
	}
}

// --- enforcement -----------------------------------------------------------

func newPeer(t testing.TB, zone, partner string) *peer.Peer {
	t.Helper()
	p := peer.New(peer.Config{Addresses: []string{"127.0.0.1"}}, dictionary.New())
	p.SetZone(zone, partner)
	return p
}

func TestEnforceRejectSendsAnswer(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	var sent *message.Message
	ext.sendFn = func(_ *peer.Peer, m *message.Message) error {
		sent = m
		return nil
	}

	p := newPeer(t, "roaming", "partner-a")
	msg := ulr()
	replaceAVP(msg, types.AVPCodeOriginRealm, "attacker.com")

	routes, err := ext.handleRouting(p, msg, nil)
	if !errors.Is(err, routing.ErrConsumed) {
		t.Fatalf("expected ErrConsumed, got %v", err)
	}
	if routes != nil {
		t.Fatal("expected no candidate routes")
	}
	if sent == nil {
		t.Fatal("no rejection answer was sent")
	}
	if code, ok := sent.GetResultCode(); !ok || code != types.ResultAuthorizationRejected {
		t.Fatalf("result code = %v (ok=%v), want 5003", code, ok)
	}
	if !sent.IsError() {
		t.Fatal("rejection answer should have the error bit set")
	}
	if host := sent.FindAVP(types.AVPCodeErrorReportingHost, 0); host != nil {
		t.Fatal("rejection answer must not disclose the internal reporting host")
	}
	if errMsg := sent.FindAVP(types.AVPCodeErrorMessage, 0); errMsg == nil ||
		!strings.Contains(errMsg.GetUTF8String(), RuleOriginRealm) {
		t.Fatal("rejection answer should name the rule that fired")
	}
	if ext.rejected.Load() != 1 {
		t.Fatalf("rejected counter = %d, want 1", ext.rejected.Load())
	}
}

func TestEnforceDropIsSilent(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	sentCount := 0
	ext.sendFn = func(_ *peer.Peer, _ *message.Message) error {
		sentCount++
		return nil
	}

	p := newPeer(t, "roaming", "partner-open")
	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "dea1.open.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "evil.com"))

	if _, err := ext.handleRouting(p, msg, nil); !errors.Is(err, routing.ErrConsumed) {
		t.Fatalf("expected ErrConsumed, got %v", err)
	}
	if sentCount != 0 {
		t.Fatal("drop action must not send an answer")
	}
	if ext.dropped.Load() != 1 {
		t.Fatalf("dropped counter = %d, want 1", ext.dropped.Load())
	}
}

func TestEnforceLogActionPassesMessageThrough(t *testing.T) {
	ext, cfg := newExt(t, testConfigYAML, nil)
	partnerOf(t, cfg, "partner-a").Screening.DefaultAction = config.ActionLog

	p := newPeer(t, "roaming", "partner-a")
	msg := ulr()
	replaceAVP(msg, types.AVPCodeOriginRealm, "attacker.com")

	if _, err := ext.handleRouting(p, msg, nil); err != nil {
		t.Fatalf("log action must not consume the message: %v", err)
	}
	if ext.logged.Load() != 1 {
		t.Fatalf("logged counter = %d, want 1", ext.logged.Load())
	}
}

func TestInternalZoneAndAnswersAreNotScreened(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	// Internal zone peers are never screened, even with a spoofed realm.
	p := newPeer(t, "core", "")
	msg := ulr()
	replaceAVP(msg, types.AVPCodeOriginRealm, "attacker.com")
	if _, err := ext.handleRouting(p, msg, nil); err != nil {
		t.Fatalf("internal zone must not be screened: %v", err)
	}

	// Answers are not screened.
	ext2, _ := newExt(t, testConfigYAML, nil)
	ep := newPeer(t, "roaming", "partner-a")
	ans := message.NewAnswer(ulr())
	if _, err := ext2.handleRouting(ep, ans, nil); err != nil {
		t.Fatalf("answers must not be screened: %v", err)
	}
	if ext2.screened.Load() != 0 {
		t.Fatal("answers should not count as screened")
	}
}

func TestDisabledExtensionPassesEverything(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, map[string]interface{}{"enabled": false})

	p := newPeer(t, "roaming", "partner-a")
	msg := ulr()
	replaceAVP(msg, types.AVPCodeOriginRealm, "attacker.com")
	if _, err := ext.handleRouting(p, msg, nil); err != nil {
		t.Fatalf("disabled extension must pass everything: %v", err)
	}

	if err := ext.Reconfigure(map[string]interface{}{"enabled": true}); err != nil {
		t.Fatalf("Reconfigure failed: %v", err)
	}
	if _, err := ext.handleRouting(p, msg, nil); !errors.Is(err, routing.ErrConsumed) {
		t.Fatalf("re-enabled extension should screen again, got %v", err)
	}
}

func TestLegacyConfigWithoutPartnersIsInert(t *testing.T) {
	ext, _ := newExt(t, "identity: \"dra.example.com\"\nrealm: \"example.com\"\n", nil)

	p := newPeer(t, "", "")
	msg := ulr()
	replaceAVP(msg, types.AVPCodeOriginRealm, "attacker.com")
	if _, err := ext.handleRouting(p, msg, nil); err != nil {
		t.Fatalf("plain DRA config must not be screened: %v", err)
	}
	if ext.screened.Load() != 0 {
		t.Fatal("no message should have been screened")
	}
}

func TestMetricsAndHealth(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	ext.sendFn = func(_ *peer.Peer, _ *message.Message) error { return nil }

	p := newPeer(t, "roaming", "partner-a")
	msg := ulr()
	replaceAVP(msg, types.AVPCodeOriginRealm, "attacker.com")
	_, _ = ext.handleRouting(p, msg, nil)

	var found bool
	for _, m := range ext.Metrics() {
		if m.Name == "violations_total" && m.Labels["partner"] == "partner-a" && m.Labels["rule"] == RuleOriginRealm {
			found = true
			if m.Value != 1 {
				t.Fatalf("violation metric = %v, want 1", m.Value)
			}
		}
	}
	if !found {
		t.Fatal("per-partner violation metric missing")
	}

	status, details := ext.HealthCheck()
	if status != "ok" {
		t.Fatalf("health status = %q", status)
	}
	if details["partners"] != 2 {
		t.Fatalf("health details partners = %v, want 2", details["partners"])
	}

	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if status, _ := ext.HealthCheck(); status != "disabled" {
		t.Fatalf("health status after stop = %q", status)
	}
}

// --- PLMN helpers ----------------------------------------------------------

// encodePLMN builds the 3-octet TBCD encoding of an MCC+MNC string.
func encodePLMN(plmn string) []byte {
	digits := make([]byte, 0, 6)
	for i := 0; i < len(plmn); i++ {
		digits = append(digits, plmn[i]-'0')
	}
	var mnc3 byte = 0x0f
	if len(digits) == 6 {
		mnc3 = digits[5]
	}
	return []byte{
		digits[1]<<4 | digits[0],
		mnc3<<4 | digits[2],
		digits[4]<<4 | digits[3],
	}
}

func TestDecodeVisitedPLMN(t *testing.T) {
	cases := []string{"24001", "310410", "99999"}
	for _, want := range cases {
		got, ok := decodeVisitedPLMN(encodePLMN(want))
		if !ok || got != want {
			t.Errorf("decodeVisitedPLMN(%s) = %q, %v", want, got, ok)
		}
	}

	if _, ok := decodeVisitedPLMN([]byte{0x00, 0x00}); ok {
		t.Error("short PLMN should not decode")
	}
	if _, ok := decodeVisitedPLMN([]byte{0xff, 0xff, 0xff}); ok {
		t.Error("invalid TBCD digits should not decode")
	}
}

func TestPLMNMatching(t *testing.T) {
	// A 2-digit MNC "01" is written "001" in the 3-digit form.
	if !plmnMatches("24001", []string{"240001"}) {
		t.Error("5-digit PLMN should match 6-digit entry with leading zero MNC")
	}
	if !plmnMatches("240001", []string{"24001"}) {
		t.Error("6-digit PLMN should match the equivalent 5-digit entry")
	}
	if plmnMatches("24001", []string{"240010"}) {
		t.Error("MNC 010 must not match MNC 01")
	}
	if plmnMatches("24001", []string{"24002"}) {
		t.Error("different MNC must not match")
	}
	if !imsiMatches("240011234567890", []string{"24001"}) {
		t.Error("IMSI prefix should match")
	}
	if imsiMatches("310410123456789", []string{"24001"}) {
		t.Error("foreign IMSI must not match")
	}
	if isIMSI("user@example.com") {
		t.Error("NAI must not be treated as an IMSI")
	}
}

// --- fail-closed behaviour on external zones -------------------------------

// TestUnknownPartnerOnExternalZoneIsRejected covers a peer that reached an
// external zone without being mapped to a partner. No policy exists for it, so
// it must be refused rather than silently let through unscreened.
func TestUnknownPartnerOnExternalZoneIsRejected(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	var sent *message.Message
	ext.sendFn = func(_ *peer.Peer, m *message.Message) error {
		sent = m
		return nil
	}

	p := newPeer(t, "roaming", "")
	routes, err := ext.handleRouting(p, ulr(), nil)
	if !errors.Is(err, routing.ErrConsumed) {
		t.Fatalf("expected ErrConsumed, got %v", err)
	}
	if routes != nil {
		t.Fatal("expected no candidate routes")
	}
	if sent == nil {
		t.Fatal("no rejection answer was sent")
	}
	if code, ok := sent.GetResultCode(); !ok || code != types.ResultAuthorizationRejected {
		t.Fatalf("result code = %v (ok=%v), want 5003", code, ok)
	}
}

// TestUnknownPartnerOnInternalZoneIsAllowed keeps internal traffic unaffected.
func TestUnknownPartnerOnInternalZoneIsAllowed(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	ext.sendFn = func(_ *peer.Peer, _ *message.Message) error {
		t.Error("internal traffic must not be rejected")
		return nil
	}

	if _, err := ext.handleRouting(newPeer(t, "core", ""), ulr(), nil); err != nil {
		t.Fatalf("handleRouting returned %v", err)
	}
}

// TestOriginRealmCannotSelectPartnerOnExternalZone ensures a peer cannot pick
// which partner policy it is judged against by claiming another realm.
func TestOriginRealmCannotSelectPartnerOnExternalZone(t *testing.T) {
	ext, cfg := newExt(t, testConfigYAML, nil)
	partner := partnerOf(t, cfg, "partner-a")

	p := newPeer(t, "roaming", "")
	msg := ulr()
	replaceAVP(msg, types.AVPCodeOriginRealm, partner.Realms[0])

	if got := ext.partnerFor(ext.reg, p, msg, true); got != nil {
		t.Errorf("partnerFor = %q, want no partner for an unmapped external peer", got.Name)
	}
	if got := ext.partnerFor(ext.reg, p, msg, false); got == nil {
		t.Error("internal traffic should still resolve a partner by realm")
	}
}
