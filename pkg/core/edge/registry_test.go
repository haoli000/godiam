// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package edge

import (
	"testing"

	"github.com/haoli000/godiam/pkg/core/config"
)

func testConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte("identity: \"dea.example.com\"\nrealm: \"example.com\"\n" + body))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	return cfg
}

const sampleEdgeConfig = `
zones:
  - name: core
    role: internal
    listen:
      port: 3868
  - name: roaming
    role: external
    listen:
      port: 3869
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com", "*.partnera.com"]
    peers:
      - identity: "dea1.partnera.com"
        priority: 0
        weight: 10
      - "dea2.partnera.com"
  - name: partner-b
    zone: roaming
    realms: ["*.example.net"]
    peers: ["dea.roaming.example.net"]
peers:
  - identity: "hss.example.com"
    addresses: ["10.0.0.5"]
    zone: core
  - identity: "mme.example.com"
    addresses: ["10.0.0.6"]
`

func TestRegistryNotConfigured(t *testing.T) {
	cfg := testConfig(t, "listen:\n  port: 3868\n")
	r := NewRegistry(cfg)

	if r.Configured() {
		t.Fatal("registry should report not configured without zones/partners")
	}
	zones := r.Zones()
	if len(zones) != 1 || zones[0].Name != config.DefaultZoneName {
		t.Fatalf("expected implicit default zone, got %+v", zones)
	}
	if r.DefaultZone() != config.DefaultZoneName {
		t.Fatalf("unexpected default zone %q", r.DefaultZone())
	}
	if r.IsExternalZone(config.DefaultZoneName) {
		t.Fatal("default zone must be internal")
	}
	if z, ok := r.ZoneForPeer("anything.example.com"); !ok || z.Name != config.DefaultZoneName {
		t.Fatalf("unknown peer should fall back to default zone, got %v %v", z, ok)
	}
}

func TestRegistryLookups(t *testing.T) {
	r := NewRegistry(testConfig(t, sampleEdgeConfig))

	if !r.Configured() {
		t.Fatal("registry should report configured")
	}
	if r.Identity() != "dea.example.com" || r.Realm() != "example.com" {
		t.Fatalf("unexpected local identity/realm: %s/%s", r.Identity(), r.Realm())
	}
	if r.DefaultZone() != "core" {
		t.Fatalf("default zone should be first internal zone, got %q", r.DefaultZone())
	}
	if !r.IsExternalZone("roaming") || r.IsExternalZone("core") {
		t.Fatal("zone roles misreported")
	}

	if p, ok := r.PartnerForPeer("DEA1.PartnerA.com"); !ok || p.Name != "partner-a" {
		t.Fatalf("partner lookup by peer failed: %v %v", p, ok)
	}
	if p, ok := r.PartnerForPeer("dea2.partnera.com"); !ok || p.Name != "partner-a" {
		t.Fatalf("scalar peer not indexed: %v %v", p, ok)
	}
	if _, ok := r.PartnerForPeer("hss.example.com"); ok {
		t.Fatal("internal peer must not map to a partner")
	}

	if p, ok := r.PartnerForRealm("partnera.com"); !ok || p.Name != "partner-a" {
		t.Fatalf("exact realm lookup failed: %v %v", p, ok)
	}
	if p, ok := r.PartnerForRealm("vplmn.partnera.com"); !ok || p.Name != "partner-a" {
		t.Fatalf("wildcard realm lookup failed: %v %v", p, ok)
	}
	if p, ok := r.PartnerForRealm("x.example.net"); !ok || p.Name != "partner-b" {
		t.Fatalf("wildcard realm lookup for partner-b failed: %v %v", p, ok)
	}
	if _, ok := r.PartnerForRealm("example.net"); ok {
		t.Fatal("wildcard must not match the bare realm")
	}
	if _, ok := r.PartnerForRealm("unknown.com"); ok {
		t.Fatal("unknown realm must not match")
	}

	if z := r.ZoneNameForPeer("dea1.partnera.com"); z != "roaming" {
		t.Fatalf("partner peer zone: %q", z)
	}
	if z := r.ZoneNameForPeer("hss.example.com"); z != "core" {
		t.Fatalf("explicit peer zone: %q", z)
	}
	if z := r.ZoneNameForPeer("mme.example.com"); z != "core" {
		t.Fatalf("peer without zone should use default: %q", z)
	}

	ids := r.PeerIdentitiesForZone("roaming")
	if len(ids) != 3 {
		t.Fatalf("expected 3 roaming peers, got %v", ids)
	}

	if _, ok := r.Partner("partner-b"); !ok {
		t.Fatal("partner lookup by name failed")
	}
	if len(r.Partners()) != 2 {
		t.Fatalf("expected 2 partners, got %d", len(r.Partners()))
	}
}

func TestRegistryReload(t *testing.T) {
	r := NewRegistry(testConfig(t, sampleEdgeConfig))
	if _, ok := r.PartnerForPeer("dea1.partnera.com"); !ok {
		t.Fatal("precondition failed")
	}

	r.Reload(testConfig(t, `
zones:
  - name: core
    role: internal
`))
	if r.Configured() != true {
		t.Fatal("zones-only config is still configured")
	}
	if _, ok := r.PartnerForPeer("dea1.partnera.com"); ok {
		t.Fatal("stale partner index after reload")
	}
	if _, ok := r.PartnerForRealm("partnera.com"); ok {
		t.Fatal("stale realm index after reload")
	}

	r.Reload(nil)
	if r.Configured() {
		t.Fatal("nil config should reset the registry")
	}
}

func TestRealmMatches(t *testing.T) {
	patterns := []string{"partnera.com", "*.partnerb.com"}
	cases := []struct {
		realm string
		want  bool
	}{
		{"partnera.com", true},
		{"PARTNERA.COM", true},
		{"partnera.com.", true},
		{"sub.partnera.com", false},
		{"partnerb.com", false},
		{"vplmn.partnerb.com", true},
		{"", false},
		{"other.com", false},
	}
	for _, c := range cases {
		if got := RealmMatches(c.realm, patterns); got != c.want {
			t.Errorf("RealmMatches(%q) = %v, want %v", c.realm, got, c.want)
		}
	}
}

func TestHostInRealm(t *testing.T) {
	cases := []struct {
		host, realm string
		want        bool
	}{
		{"hss.example.com", "example.com", true},
		{"example.com", "example.com", true},
		{"hss.example.com.evil.com", "example.com", false},
		{"HSS.Example.com", "example.com", true},
		{"", "example.com", false},
		{"hss.example.com", "", false},
		{"notexample.com", "example.com", false},
	}
	for _, c := range cases {
		if got := HostInRealm(c.host, c.realm); got != c.want {
			t.Errorf("HostInRealm(%q,%q) = %v, want %v", c.host, c.realm, got, c.want)
		}
	}
}

const servedRealmConfig = `
zones:
  - name: core
    role: internal
    realms: ["*.internal.example.com"]
    listen:
      port: 3868
  - name: roaming
    role: external
    listen:
      port: 3869
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com"]
    peers: ["dea1.partnera.com"]
peers:
  - identity: "hss.core.example.com"
    realm: "core.example.com"
    addresses: ["10.0.0.5"]
    zone: core
  - identity: "dea1.partnera.com"
    realm: "partnera.com"
    addresses: ["10.9.9.9"]
    zone: roaming
routing:
  realm_routes:
    "apps.example.com":
      - "hss.core.example.com"
    "relayed.partnera.com":
      - "dea1.partnera.com"
`

func TestRegistryServedRealms(t *testing.T) {
	r := NewRegistry(testConfig(t, servedRealmConfig))

	served := []string{
		"example.com",             // local realm
		"core.example.com",        // realm of an internal peer
		"apps.example.com",        // realm route via an internal peer
		"eu.internal.example.com", // suffix declared on the internal zone
	}
	for _, realm := range served {
		if !r.ServesRealm(realm) {
			t.Errorf("realm %q should be served locally", realm)
		}
	}

	foreign := []string{
		"partnera.com",         // partner realm
		"relayed.partnera.com", // realm route via a partner peer
		"internal.example.org", // unrelated
		"",
	}
	for _, realm := range foreign {
		if r.ServesRealm(realm) {
			t.Errorf("realm %q must not be considered served locally", realm)
		}
	}

	if got := r.ServedRealms(); len(got) == 0 {
		t.Fatal("ServedRealms must not be empty")
	}
}

func TestRegistryServedRealmsWithoutEdgeModel(t *testing.T) {
	r := NewRegistry(testConfig(t, "listen:\n  port: 3868\npeers:\n  - identity: \"hss.example.com\"\n    realm: \"hss.example.com\"\n    addresses: [\"10.0.0.5\"]\n"))

	if !r.ServesRealm("example.com") || !r.ServesRealm("hss.example.com") {
		t.Fatal("legacy configs must still report their own realms as served")
	}
}

// A partner peer that is not named in the configuration is still admitted when
// its realm matches, so the binding recorded at admission time is the only way
// the hiding extensions can recognise it afterwards. Without it, that peer's
// answers leak internal identities.
func TestRegistryBoundPeers(t *testing.T) {
	r := NewRegistry(testConfig(t, sampleEdgeConfig))

	const unlisted = "dea9.partnera.com"
	if p, ok := r.PartnerForPeer(unlisted); ok {
		t.Fatalf("unlisted peer should not resolve before binding, got %q", p.Name)
	}

	r.BindPeer(unlisted, "partner-a")
	p, ok := r.PartnerForPeer(unlisted)
	if !ok || p.Name != "partner-a" {
		t.Fatalf("bound peer resolved to %v, want partner-a", p)
	}

	// A binding must outlive a reload; the peer stays connected across one.
	r.Reload(testConfig(t, sampleEdgeConfig))
	if p, ok := r.PartnerForPeer(unlisted); !ok || p.Name != "partner-a" {
		t.Fatalf("binding lost across reload, got %v", p)
	}

	r.UnbindPeer(unlisted)
	if p, ok := r.PartnerForPeer(unlisted); ok {
		t.Fatalf("binding survived disconnect, got %q", p.Name)
	}

	// Configured peers keep resolving from configuration, and an empty partner
	// is never recorded.
	r.BindPeer("dea1.partnera.com", "partner-b")
	if p, ok := r.PartnerForPeer("dea1.partnera.com"); !ok || p.Name != "partner-a" {
		t.Fatalf("static model must win over a binding, got %v", p)
	}
	r.BindPeer("dea8.partnera.com", "")
	if _, ok := r.PartnerForPeer("dea8.partnera.com"); ok {
		t.Fatal("empty partner name should not be recorded")
	}
}
