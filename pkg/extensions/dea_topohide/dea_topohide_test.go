// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dea_topohide

import (
	"fmt"
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
  - name: partner-stateful
    zone: roaming
    realms: ["partnera.com"]
    peers: ["dea1.partnera.com"]
    topology_hiding:
      mode: stateful
      pseudonym_prefix: "th"
      ttl: 10m
  - name: partner-static
    zone: roaming
    realms: ["static.com"]
    peers: ["dea1.static.com"]
    topology_hiding:
      mode: static
      edge_identity: "edge.example.com"
  - name: partner-plain
    zone: roaming
    realms: ["plain.com"]
    peers: ["dea1.plain.com"]
`

func newExt(t *testing.T, yaml string, extCfg map[string]interface{}) (*deaTopoHide, *config.Config) {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	ext := &deaTopoHide{}
	if extCfg == nil {
		extCfg = map[string]interface{}{}
	}
	if err := ext.Init(&mockInitContext{router: routing.NewRouter(), cfg: cfg}, extCfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = ext.Stop() })
	return ext, cfg
}

func partnerOf(t *testing.T, cfg *config.Config, name string) *config.PartnerConfig {
	t.Helper()
	for i := range cfg.Partners {
		if cfg.Partners[i].Name == name {
			return &cfg.Partners[i]
		}
	}
	t.Fatalf("partner %q not found", name)
	return nil
}

func newPeer(t *testing.T, zone, partner string) *peer.Peer {
	t.Helper()
	p := peer.New(peer.Config{Addresses: []string{"127.0.0.1"}}, dictionary.New())
	p.SetZone(zone, partner)
	return p
}

// internalRequest builds a request as it would arrive from an internal node.
func internalRequest(session string) *message.Message {
	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, session))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "hss1.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "partnera.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, "mme1.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, "gw.partnera.com"))
	msg.AddAVP(message.NewGroupedAVP(types.AVPCodeProxyInfo, types.AVPFlagMandatory, 0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "proxy.example.com"),
		message.NewOctetStringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, []byte("state")),
	))
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeOriginStateID, types.AVPFlagMandatory, 42))
	msg.AddAVP(message.NewAddressAVP(types.AVPCodeHostIPAddress, types.AVPFlagMandatory,
		types.Address{Type: types.AddressTypeIPv4, Data: []byte{10, 0, 0, 1}}))
	return msg
}

func hasAVP(msg *message.Message, code types.AVPCode) bool {
	return msg.FindAVP(code, 0) != nil
}

func avpValue(msg *message.Message, code types.AVPCode) string {
	if avp := msg.FindAVP(code, 0); avp != nil {
		return string(avp.Data)
	}
	return ""
}

// --- static mode -----------------------------------------------------------

func TestStaticHidingScrubsInternalTopology(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	msg := internalRequest("sess-static")
	ext.handlePostRoute(msg, "dea1.static.com")

	if got := avpValue(msg, types.AVPCodeOriginHost); got != "edge.example.com" {
		t.Errorf("Origin-Host = %q, want the edge identity", got)
	}
	if avpValue(msg, types.AVPCodeOriginRealm) != "example.com" {
		t.Error("Origin-Realm must become the outward realm")
	}
	if msg.FindAVP(types.AVPCodeOriginStateID, 0) != nil {
		t.Error("Origin-State-Id must be stripped")
	}
	if msg.FindAVP(types.AVPCodeHostIPAddress, 0) != nil {
		t.Error("Host-IP-Address must be stripped")
	}
	if msg.FindAVP(types.AVPCodeProxyInfo, 0) != nil {
		t.Error("internal Proxy-Info must be stripped")
	}

	records := msg.FindAllAVPs(types.AVPCodeRouteRecord, 0)
	var hosts []string
	for _, r := range records {
		hosts = append(hosts, string(r.Data))
	}
	for _, h := range hosts {
		if strings.HasSuffix(h, ".example.com") && h != "edge.example.com" {
			t.Errorf("internal Route-Record %q leaked to the partner", h)
		}
	}
	found := false
	for _, h := range hosts {
		if h == "edge.example.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("the edge identity must remain in the Route-Record chain, got %v", hosts)
	}
	if ext.hiddenRequests.Load() != 1 {
		t.Errorf("hiddenRequests = %d, want 1", ext.hiddenRequests.Load())
	}
}

func TestStaticModeAllocatesNoState(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	ext.handlePostRoute(internalRequest("sess-static"), "dea1.static.com")

	if st := ext.store.Stats(); st.Pseudonyms != 0 {
		t.Errorf("static mode must not allocate pseudonyms, got %d", st.Pseudonyms)
	}
	if ext.allocated.Load() != 0 {
		t.Errorf("allocated = %d, want 0", ext.allocated.Load())
	}
}

// --- stateful mode ---------------------------------------------------------

func TestStatefulHidingAllocatesReversiblePseudonym(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	msg := internalRequest("sess-1")
	ext.handlePostRoute(msg, "dea1.partnera.com")

	pseudonym := avpValue(msg, types.AVPCodeOriginHost)
	if pseudonym == "hss1.example.com" {
		t.Fatal("Origin-Host was not masked")
	}
	if !strings.HasPrefix(pseudonym, "th") || !strings.HasSuffix(pseudonym, ".example.com") {
		t.Fatalf("pseudonym %q does not follow prefix/realm configuration", pseudonym)
	}
	if strings.Contains(pseudonym, "hss1") {
		t.Fatalf("pseudonym %q leaks the internal host name", pseudonym)
	}

	realHost, ok := ext.store.RealHost(pseudonym)
	if !ok || realHost != "hss1.example.com" {
		t.Fatalf("store.RealHost(%q) = %q, %v; want hss1.example.com, true", pseudonym, realHost, ok)
	}
}

func TestStatefulPseudonymIsStableWithinASession(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	first := internalRequest("sess-1")
	ext.handlePostRoute(first, "dea1.partnera.com")
	second := internalRequest("sess-1")
	ext.handlePostRoute(second, "dea1.partnera.com")

	if a, b := avpValue(first, types.AVPCodeOriginHost), avpValue(second, types.AVPCodeOriginHost); a != b {
		t.Errorf("pseudonym changed within a session: %q vs %q", a, b)
	}
	if ext.allocated.Load() != 1 {
		t.Errorf("allocated = %d, want 1 (the mapping should be reused)", ext.allocated.Load())
	}
}

func TestStatefulPseudonymDiffersAcrossSessions(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	first := internalRequest("sess-1")
	ext.handlePostRoute(first, "dea1.partnera.com")
	second := internalRequest("sess-2")
	ext.handlePostRoute(second, "dea1.partnera.com")

	if a, b := avpValue(first, types.AVPCodeOriginHost), avpValue(second, types.AVPCodeOriginHost); a == b {
		t.Errorf("the same pseudonym %q was reused across sessions", a)
	}
}

func TestDestinationHostPseudonymIsReversed(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	out := internalRequest("sess-1")
	ext.handlePostRoute(out, "dea1.partnera.com")
	pseudonym := avpValue(out, types.AVPCodeOriginHost)

	// The partner now addresses the pseudonym in a follow-up request.
	in := message.NewRequest(317, 16777251)
	in.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess-1"))
	in.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "dea1.partnera.com"))
	in.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "partnera.com"))
	in.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, types.DiamID(pseudonym)))

	p := newPeer(t, "roaming", "partner-stateful")
	if _, err := ext.handleIn(p, in, nil); err != nil {
		t.Fatalf("handleIn returned %v", err)
	}
	if got := avpValue(in, types.AVPCodeDestinationHost); got != "hss1.example.com" {
		t.Errorf("Destination-Host = %q, want the restored internal host", got)
	}
	if ext.restored.Load() != 1 {
		t.Errorf("restored = %d, want 1", ext.restored.Load())
	}
}

func TestUnknownDestinationHostIsLeftIntact(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	in := message.NewRequest(317, 16777251)
	in.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "hss1.example.com"))
	if _, err := ext.handleIn(newPeer(t, "roaming", "partner-stateful"), in, nil); err != nil {
		t.Fatalf("handleIn returned %v", err)
	}
	if got := avpValue(in, types.AVPCodeDestinationHost); got != "hss1.example.com" {
		t.Errorf("Destination-Host = %q, want it unchanged", got)
	}
	if ext.restoreMisses.Load() != 1 {
		t.Errorf("restoreMisses = %d, want 1", ext.restoreMisses.Load())
	}
}

func TestPseudonymExpiresAfterTTL(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	now := time.Now()
	clock := func() time.Time { return now }
	ext.mu.Lock()
	ext.now = clock
	ext.store = newMemoryStore(0, 0, clock)
	ext.mu.Unlock()

	msg := internalRequest("sess-1")
	ext.handlePostRoute(msg, "dea1.partnera.com")
	pseudonym := avpValue(msg, types.AVPCodeOriginHost)

	now = now.Add(11 * time.Minute)
	if _, ok := ext.store.RealHost(pseudonym); ok {
		t.Error("pseudonym should have expired after its TTL")
	}
}

// --- answer path -----------------------------------------------------------

func TestAnswerToPartnerIsHidden(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	// The relay transaction is what identifies the partner an answer is going
	// back to; the request below is what the edge relayed inward on its behalf.
	ext.router = fakeRelay{21: "dea1.partnera.com"}

	req := message.NewRequest(316, 16777251)
	req.HopByHopID = 21
	req.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess-a"))
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "dea1.partnera.com"))
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "partnera.com"))
	if _, err := ext.handleIn(newPeer(t, "roaming", "partner-stateful"), req, nil); err != nil {
		t.Fatalf("handleIn(request) returned %v", err)
	}

	answer := message.NewAnswer(req)
	answer.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess-a"))
	answer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "hss1.example.com"))
	answer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeErrorReportingHost, types.AVPFlagMandatory, "hss1.example.com"))

	if _, err := ext.handleIn(newPeer(t, "core", ""), answer, nil); err != nil {
		t.Fatalf("handleIn(answer) returned %v", err)
	}

	if got := avpValue(answer, types.AVPCodeOriginHost); got == "hss1.example.com" {
		t.Error("the answer Origin-Host was not masked")
	}
	if got := avpValue(answer, types.AVPCodeErrorReportingHost); got != "dea.example.com" {
		t.Errorf("Error-Reporting-Host = %q, want the edge identity", got)
	}
	if ext.hiddenAnswers.Load() != 1 {
		t.Errorf("hiddenAnswers = %d, want 1", ext.hiddenAnswers.Load())
	}
}

func TestAnswerWithoutSessionMappingIsUntouched(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	answer := message.NewMessage(0, 316, 16777251)
	answer.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "unknown"))
	answer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "hss1.example.com"))

	if _, err := ext.handleIn(newPeer(t, "core", ""), answer, nil); err != nil {
		t.Fatalf("handleIn returned %v", err)
	}
	if got := avpValue(answer, types.AVPCodeOriginHost); got != "hss1.example.com" {
		t.Errorf("Origin-Host = %q, want it unchanged for internal delivery", got)
	}
}

// --- policy gating ---------------------------------------------------------

func TestPartnerWithoutHidingIsUntouched(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	msg := internalRequest("sess-plain")
	ext.handlePostRoute(msg, "dea1.plain.com")

	if got := avpValue(msg, types.AVPCodeOriginHost); got != "hss1.example.com" {
		t.Errorf("Origin-Host = %q, want it unchanged when hiding is off", got)
	}
	if msg.FindAVP(types.AVPCodeOriginStateID, 0) == nil {
		t.Error("Origin-State-Id must survive when hiding is off")
	}
}

func TestInternalNextHopIsUntouched(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	msg := internalRequest("sess-internal")
	ext.handlePostRoute(msg, "hss2.example.com")

	if got := avpValue(msg, types.AVPCodeOriginHost); got != "hss1.example.com" {
		t.Errorf("Origin-Host = %q, want it unchanged toward an internal peer", got)
	}
}

func TestInertWithoutEdgeConfiguration(t *testing.T) {
	ext, _ := newExt(t, "identity: \"dra.example.com\"\nrealm: \"example.com\"\n", nil)

	msg := internalRequest("sess-legacy")
	ext.handlePostRoute(msg, "peer.partnera.com")
	if got := avpValue(msg, types.AVPCodeOriginHost); got != "hss1.example.com" {
		t.Errorf("Origin-Host = %q, want a legacy configuration to be untouched", got)
	}
}

func TestDisabledExtensionIsInert(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, map[string]interface{}{"enabled": false})

	msg := internalRequest("sess-1")
	ext.handlePostRoute(msg, "dea1.partnera.com")
	if got := avpValue(msg, types.AVPCodeOriginHost); got != "hss1.example.com" {
		t.Errorf("Origin-Host = %q, want it unchanged while disabled", got)
	}

	if err := ext.Reconfigure(map[string]interface{}{"enabled": true}); err != nil {
		t.Fatalf("Reconfigure failed: %v", err)
	}
	msg = internalRequest("sess-2")
	ext.handlePostRoute(msg, "dea1.partnera.com")
	if got := avpValue(msg, types.AVPCodeOriginHost); got == "hss1.example.com" {
		t.Error("hiding did not resume after Reconfigure")
	}
}

func TestScrubAVPsRemovesConfiguredAVPs(t *testing.T) {
	ext, cfg := newExt(t, testConfigYAML, nil)
	partner := partnerOf(t, cfg, "partner-static")
	partner.TopologyHiding.ScrubAVPs = []config.AVPSelector{{Code: uint32(types.AVPCodeProxyState)}}

	msg := internalRequest("sess-scrub")
	msg.AddAVP(message.NewOctetStringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, []byte("secret")))
	ext.handlePostRoute(msg, "dea1.static.com")

	if msg.FindAVP(types.AVPCodeProxyState, 0) != nil {
		t.Error("configured scrub AVP was not removed")
	}
}

// --- introspection ---------------------------------------------------------

func TestMetricsAndHealth(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	ext.handlePostRoute(internalRequest("sess-1"), "dea1.partnera.com")

	var hiddenPerPartner float64
	for _, m := range ext.Metrics() {
		if m.Name == "hidden_messages_total" && m.Labels["partner"] == "partner-stateful" {
			hiddenPerPartner = m.Value
		}
	}
	if hiddenPerPartner != 1 {
		t.Errorf("hidden_messages_total{partner=partner-stateful} = %v, want 1", hiddenPerPartner)
	}

	status, details := ext.HealthCheck()
	if status != "ok" {
		t.Errorf("HealthCheck status = %q, want ok", status)
	}
	if details["pseudonyms"].(int64) != 1 {
		t.Errorf("pseudonyms = %v, want 1", details["pseudonyms"])
	}

	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if status, _ := ext.HealthCheck(); status != "disabled" {
		t.Errorf("HealthCheck status after Stop = %q, want disabled", status)
	}
}

// --- store -----------------------------------------------------------------

func TestMemoryStoreEvictsLeastRecentlyUsed(t *testing.T) {
	now := time.Now()
	store := newMemoryStore(2, 0, func() time.Time { return now })
	defer store.Close()

	store.PutPseudonym("partner-a", "p1", "h1", "s1", time.Hour)
	store.PutPseudonym("partner-a", "p2", "h2", "s2", time.Hour)
	if _, ok := store.RealHost("p1"); !ok {
		t.Fatal("p1 should still be present")
	}
	store.PutPseudonym("partner-a", "p3", "h3", "s3", time.Hour)

	if _, ok := store.RealHost("p2"); ok {
		t.Error("p2 was the least recently used entry and should have been evicted")
	}
	if _, ok := store.RealHost("p1"); !ok {
		t.Error("p1 was refreshed and should have survived")
	}
	if st := store.Stats(); st.Evictions != 1 {
		t.Errorf("evictions = %d, want 1", st.Evictions)
	}
}

func TestMemoryStoreSweepRemovesExpiredEntries(t *testing.T) {
	now := time.Now()
	store := newMemoryStore(0, 0, func() time.Time { return now })
	defer store.Close()

	store.PutPseudonym("partner-a", "p1", "h1", "s1", time.Minute)
	now = now.Add(2 * time.Minute)
	store.sweep()

	if st := store.Stats(); st.Pseudonyms != 0 {
		t.Errorf("stats after sweep = %+v, want empty store", st)
	}
	if _, ok := store.Pseudonym("h1", "s1"); ok {
		t.Error("forward index still resolves an expired mapping")
	}
}

func TestMemoryStoreReplacesPseudonym(t *testing.T) {
	store := newMemoryStore(0, 0, nil)
	defer store.Close()

	store.PutPseudonym("partner-a", "p1", "h1", "s1", time.Hour)
	store.PutPseudonym("partner-a", "p2", "h1", "s1", time.Hour)

	got, ok := store.Pseudonym("h1", "s1")
	if !ok || got != "p2" {
		t.Errorf("Pseudonym = %q, %v; want p2, true", got, ok)
	}
	if _, ok := store.RealHost("p1"); !ok {
		t.Error("the previous pseudonym should remain resolvable until it expires")
	}
}

func TestPseudonymFormat(t *testing.T) {
	ext := &deaTopoHide{randRead: func(b []byte) (int, error) {
		for i := range b {
			b[i] = 0xab
		}
		return len(b), nil
	}}
	got, err := ext.newPseudonym(&config.TopologyHidingPolicy{PseudonymPrefix: "th", PseudonymRealm: "example.com"})
	if err != nil {
		t.Fatalf("newPseudonym failed: %v", err)
	}
	if got != "thabababababababab.example.com" {
		t.Errorf("pseudonym = %q", got)
	}
}

const internalRealmConfigYAML = `
identity: "dea.edge.example.com"
realm: "edge.example.com"
zones:
  - name: core
    role: internal
    realms: ["core.example.com"]
  - name: roaming
    role: external
partners:
  - name: partner-stateful
    zone: roaming
    realms: ["partnera.com"]
    peers: ["dea1.partnera.com"]
    topology_hiding:
      mode: stateful
      pseudonym_prefix: "th"
      ttl: 10m
`

// A realm served behind an internal zone is internal even though it differs
// from the edge agent's own realm, and it must never reach the partner.
func TestHidingMasksServedInternalRealm(t *testing.T) {
	ext, cfg := newExt(t, internalRealmConfigYAML, nil)
	partner := partnerOf(t, cfg, "partner-stateful")

	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess-served"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "hss.core.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "core.example.com"))

	ext.handlePostRoute(msg, "dea1.partnera.com")

	host := avpValue(msg, types.AVPCodeOriginHost)
	if host == "hss.core.example.com" {
		t.Fatal("internal Origin-Host leaked to the partner")
	}
	if !strings.HasSuffix(host, ".edge.example.com") {
		t.Errorf("pseudonym %q must live in the outward realm", host)
	}
	if realm := avpValue(msg, types.AVPCodeOriginRealm); realm != "edge.example.com" {
		t.Errorf("Origin-Realm = %q, want the outward realm", realm)
	}

	if real, ok := ext.store.RealHost(host); !ok || real != "hss.core.example.com" {
		t.Errorf("pseudonym must reverse to the internal host, got %q ok=%v", real, ok)
	}
	_ = partner
}

// --- relay transaction based answer resolution -----------------------------

// fakeRelay stands in for the router's in-flight relay transaction table.
type fakeRelay map[types.HopByHopID]string

func (f fakeRelay) RelayOrigin(hbhID types.HopByHopID) (string, bool) {
	origin, ok := f[hbhID]
	return origin, ok
}

// TestAnswerIsHiddenWithoutSessionID covers the bypass where a partner simply
// omits Session-Id so that no session mapping can be recorded: the answer must
// still be masked, because the relay transaction says where it is going.
func TestAnswerIsHiddenWithoutSessionID(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	ext.router = fakeRelay{7: "dea1.partnera.com"}

	answer := message.NewMessage(0, 316, 16777251)
	answer.HopByHopID = 7
	answer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "hss1.example.com"))
	answer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeErrorReportingHost, types.AVPFlagMandatory, "hss1.example.com"))

	if _, err := ext.handleIn(newPeer(t, "core", ""), answer, nil); err != nil {
		t.Fatalf("handleIn returned %v", err)
	}
	if got := avpValue(answer, types.AVPCodeOriginHost); got == "hss1.example.com" {
		t.Error("the answer Origin-Host leaked: hiding was skipped because Session-Id was absent")
	}
	if got := avpValue(answer, types.AVPCodeErrorReportingHost); got != "dea.example.com" {
		t.Errorf("Error-Reporting-Host = %q, want the edge identity", got)
	}
}

// TestAnswerHeadingInwardIsNotScrubbed covers the opposite direction: the edge
// asked a partner something on behalf of an internal client, and the partner's
// answer must reach that client intact even though the session is associated
// with a partner.
func TestAnswerHeadingInwardIsNotScrubbed(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	ext.router = fakeRelay{9: "mme1.example.com"}

	answer := message.NewMessage(0, 316, 16777251)
	answer.HopByHopID = 9
	answer.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess-inward"))
	answer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "dea1.partnera.com"))
	answer.AddAVP(message.NewUnsigned32AVP(types.AVPCodeOriginStateID, types.AVPFlagMandatory, 42))

	if _, err := ext.handleIn(newPeer(t, "roaming", "partner-stateful"), answer, nil); err != nil {
		t.Fatalf("handleIn returned %v", err)
	}
	if got := avpValue(answer, types.AVPCodeOriginHost); got != "dea1.partnera.com" {
		t.Errorf("Origin-Host = %q, want it untouched on the inward path", got)
	}
	if !hasAVP(answer, types.AVPCodeOriginStateID) {
		t.Error("Origin-State-Id was stripped from an answer delivered inside the operator network")
	}
	if ext.hiddenAnswers.Load() != 0 {
		t.Errorf("hiddenAnswers = %d, want 0", ext.hiddenAnswers.Load())
	}
}

// TestSessionRebindingCannotUnmaskAnotherPartner ensures an attacker-chosen
// Session-Id cannot override the relay transaction.
func TestSessionRebindingCannotUnmaskAnotherPartner(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)
	ext.router = fakeRelay{11: "dea1.partnera.com"}
	// The hostile partner picks a Session-Id belonging to a policy that hides
	// nothing. No session mapping exists to honour it.

	answer := message.NewMessage(0, 316, 16777251)
	answer.HopByHopID = 11
	answer.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess-x"))
	answer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "hss1.example.com"))

	if _, err := ext.handleIn(newPeer(t, "core", ""), answer, nil); err != nil {
		t.Fatalf("handleIn returned %v", err)
	}
	if got := avpValue(answer, types.AVPCodeOriginHost); got == "hss1.example.com" {
		t.Error("session rebinding disabled topology hiding")
	}
}

// TestPseudonymRoundTripWithMixedCaseRealm covers a configuration whose realm
// or prefix is not lowercase: identities are case-insensitive, so the reverse
// lookup must still find the pseudonym it issued.
func TestPseudonymRoundTripWithMixedCaseRealm(t *testing.T) {
	const yaml = `
identity: "DEA.Example.COM"
realm: "Example.COM"
zones:
  - name: core
    role: internal
  - name: roaming
    role: external
partners:
  - name: partner-stateful
    zone: roaming
    realms: ["partnera.com"]
    peers: ["dea1.partnera.com"]
    topology_hiding:
      mode: stateful
      pseudonym_prefix: "TH"
`
	ext, _ := newExt(t, yaml, nil)

	req := internalRequest("sess-mixed")
	ext.handlePostRoute(req, "dea1.partnera.com")

	pseudonym := avpValue(req, types.AVPCodeOriginHost)
	if pseudonym == "hss1.example.com" {
		t.Fatal("Origin-Host was not masked")
	}

	follow := message.NewRequest(316, 16777251)
	follow.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sess-mixed"))
	follow.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, types.DiamID(pseudonym)))

	if _, err := ext.handleIn(newPeer(t, "roaming", "partner-stateful"), follow, nil); err != nil {
		t.Fatalf("handleIn returned %v", err)
	}
	if got := avpValue(follow, types.AVPCodeDestinationHost); got != "hss1.example.com" {
		t.Errorf("Destination-Host = %q, want the real internal host restored", got)
	}
}

// TestPartnerCapDoesNotEvictAnotherPartner covers the case where one partner
// floods the store: its own entries must be recycled, but a quiet partner's
// live pseudonyms must survive, or routing breaks for the victim.
func TestPartnerCapDoesNotEvictAnotherPartner(t *testing.T) {
	store := newMemoryStore(0, 0, nil)
	defer store.Close()

	store.SetPartnerLimit("noisy", 2)
	store.SetPartnerLimit("quiet", 2)

	store.PutPseudonym("quiet", "q1", "hq", "sq", time.Hour)
	for i := 0; i < 20; i++ {
		store.PutPseudonym("noisy", fmt.Sprintf("n%d", i), fmt.Sprintf("hn%d", i), "sn", time.Hour)
	}

	if _, ok := store.RealHost("q1"); !ok {
		t.Error("a quiet partner's pseudonym was evicted by another partner's flood")
	}
	if _, ok := store.RealHost("n0"); ok {
		t.Error("the noisy partner should have recycled its own oldest entry")
	}
}

// TestStoreWideCapTakesFromTheLargestPartner checks the aggregate ceiling
// sheds load from whoever is consuming the most.
func TestStoreWideCapTakesFromTheLargestPartner(t *testing.T) {
	store := newMemoryStore(4, 0, nil)
	defer store.Close()

	store.PutPseudonym("quiet", "q1", "hq", "sq", time.Hour)
	for i := 0; i < 8; i++ {
		store.PutPseudonym("noisy", fmt.Sprintf("n%d", i), fmt.Sprintf("hn%d", i), "sn", time.Hour)
	}

	if _, ok := store.RealHost("q1"); !ok {
		t.Error("the store-wide cap evicted the smallest partner instead of the largest")
	}
	if st := store.Stats(); st.Pseudonyms > 4 {
		t.Errorf("pseudonyms = %d, want the store-wide cap of 4 to hold", st.Pseudonyms)
	}
}

// TestPartnerStoreIsBoundedByDefault ensures an operator who never sets
// max_entries still gets a bounded store.
func TestPartnerStoreIsBoundedByDefault(t *testing.T) {
	_, cfg := newExt(t, testConfigYAML, nil)
	for i := range cfg.Partners {
		p := &cfg.Partners[i]
		if p.TopologyHiding.Mode == config.TopoHideNone {
			continue
		}
		if p.TopologyHiding.MaxEntries <= 0 {
			t.Errorf("partner %q has max_entries = %d, want a bounded default",
				p.Name, p.TopologyHiding.MaxEntries)
		}
	}
	if got := maxPartnerEntries(cfg); got <= 0 {
		t.Errorf("maxPartnerEntries = %d, want a bounded aggregate", got)
	}
}

// Topology hiding keeps no Session-Id state at all, so a partner cannot use a
// chosen Session-Id to occupy another partner's eviction budget.
func TestStoreHoldsNoSessionState(t *testing.T) {
	store := newMemoryStore(0, 0, nil)
	defer store.Close()

	store.SetPartnerLimit("victim", 2)
	store.PutPseudonym("victim", "v1", "hv1", "sv", time.Hour)
	store.PutPseudonym("victim", "v2", "hv2", "sv", time.Hour)

	if st := store.Stats(); st.Pseudonyms != 2 {
		t.Fatalf("pseudonyms = %d, want 2; the budget must hold only pseudonyms", st.Pseudonyms)
	}
	if _, ok := store.RealHost("v1"); !ok {
		t.Error("a partner's own entries were evicted inside its budget")
	}
}
