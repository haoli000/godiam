// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const edgeHeader = `
identity: "dea.example.com"
realm: "example.com"
`

func parseEdge(t *testing.T, body string) (*Config, error) {
	t.Helper()
	return Parse([]byte(edgeHeader + body))
}

func mustParseEdge(t *testing.T, body string) *Config {
	t.Helper()
	cfg, err := parseEdge(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return cfg
}

func TestEffectiveZonesDefault(t *testing.T) {
	cfg := mustParseEdge(t, `
listen:
  port: 3868
  enable_tcp: true
peers_policy:
  allow_unknown: true
`)
	zones := cfg.EffectiveZones()
	if len(zones) != 1 {
		t.Fatalf("expected 1 implicit zone, got %d", len(zones))
	}
	z := zones[0]
	if z.Name != DefaultZoneName || z.Role != ZoneRoleInternal {
		t.Fatalf("unexpected implicit zone: %+v", z)
	}
	if z.Listen.Port != 3868 || !z.Listen.EnableTCP {
		t.Fatalf("implicit zone did not inherit listen config: %+v", z.Listen)
	}
	if !z.PeersPolicy.AllowUnknown {
		t.Fatal("implicit zone did not inherit peers_policy")
	}
	if z.IsExternal() {
		t.Fatal("implicit zone must be internal")
	}
}

func TestZoneAndPartnerDefaults(t *testing.T) {
	cfg := mustParseEdge(t, `
zones:
  - name: core
    role: internal
  - name: roaming
    role: external
    listen:
      port: 3869
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com"]
    peers:
      - "dea.partnera.com"
      - identity: "dea2.partnera.com"
        priority: 1
        weight: 5
    rate_limit:
      enabled: true
      requests_per_second: 100
      per_application:
        16777251:
          requests_per_second: 50
`)
	if len(cfg.EffectiveZones()) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(cfg.EffectiveZones()))
	}
	if cfg.Zones[0].Listen.Port != 3868 {
		t.Fatalf("zone default port not applied: %d", cfg.Zones[0].Listen.Port)
	}
	if cfg.Zones[1].TLS.MinVersion != "1.2" {
		t.Fatalf("zone default TLS min version not applied: %q", cfg.Zones[1].TLS.MinVersion)
	}

	p := cfg.Partners[0]
	if len(p.Peers) != 2 {
		t.Fatalf("expected 2 partner peers, got %d", len(p.Peers))
	}
	if p.Peers[0].Identity != "dea.partnera.com" || p.Peers[0].Weight != 1 {
		t.Fatalf("scalar peer form not decoded: %+v", p.Peers[0])
	}
	if p.Peers[1].Priority != 1 || p.Peers[1].Weight != 5 {
		t.Fatalf("mapping peer form not decoded: %+v", p.Peers[1])
	}
	if p.Screening.DefaultAction != ActionReject || p.Screening.RejectResultCode != 5003 {
		t.Fatalf("screening defaults not applied: %+v", p.Screening)
	}
	if p.TopologyHiding.Mode != TopoHideNone {
		t.Fatalf("topology hiding default mode: %q", p.TopologyHiding.Mode)
	}
	if p.TopologyHiding.EdgeIdentity != "dea.example.com" || p.TopologyHiding.PseudonymRealm != "example.com" {
		t.Fatalf("topology hiding identity defaults: %+v", p.TopologyHiding)
	}
	if p.TopologyHiding.TTL != 30*time.Minute {
		t.Fatalf("topology hiding TTL default: %v", p.TopologyHiding.TTL)
	}
	if p.RateLimit.Partner.Burst != 100 || p.RateLimit.Partner.ResultCode != 3004 || p.RateLimit.Partner.Action != ActionReject {
		t.Fatalf("rate limit defaults not applied: %+v", p.RateLimit.Partner)
	}
	if got := p.RateLimit.PerApplication[16777251]; got.Burst != 50 || got.ResultCode != 3004 {
		t.Fatalf("per-application rate limit defaults not applied: %+v", got)
	}
	if !p.ScreeningEnabled(true) {
		t.Fatal("screening should default to enabled for external zones")
	}
	if p.ScreeningEnabled(false) {
		t.Fatal("screening should default to disabled for internal zones")
	}
}

func TestEdgeValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "duplicate zone",
			body: `
zones:
  - name: a
    role: internal
  - name: a
    role: external
`,
			wantErr: "duplicate name",
		},
		{
			name: "bad role",
			body: `
zones:
  - name: a
    role: dmz
`,
			wantErr: "role must be",
		},
		{
			name: "partner unknown zone",
			body: `
partners:
  - name: p
    zone: nope
    realms: ["p.com"]
`,
			wantErr: "unknown zone",
		},
		{
			name: "partner missing realms",
			body: `
zones:
  - name: ext
    role: external
partners:
  - name: p
    zone: ext
`,
			wantErr: "realms is required",
		},
		{
			name: "partner bad realm",
			body: `
zones:
  - name: ext
    role: external
partners:
  - name: p
    zone: ext
    realms: ["*."]
`,
			wantErr: "is not a valid realm",
		},
		{
			name: "peer claimed twice",
			body: `
zones:
  - name: ext
    role: external
partners:
  - name: p1
    zone: ext
    realms: ["p1.com"]
    peers: ["dea.shared.com"]
  - name: p2
    zone: ext
    realms: ["p2.com"]
    peers: ["dea.shared.com"]
`,
			wantErr: "already belongs to partner",
		},
		{
			name: "bad plmn",
			body: `
zones:
  - name: ext
    role: external
partners:
  - name: p
    zone: ext
    realms: ["p.com"]
    plmn_ids: ["12ab5"]
`,
			wantErr: "must be 5 or 6 digits",
		},
		{
			name: "bad screening action",
			body: `
zones:
  - name: ext
    role: external
partners:
  - name: p
    zone: ext
    realms: ["p.com"]
    screening:
      default_action: explode
`,
			wantErr: "default_action",
		},
		{
			name: "bad topology mode",
			body: `
zones:
  - name: ext
    role: external
partners:
  - name: p
    zone: ext
    realms: ["p.com"]
    topology_hiding:
      mode: magic
`,
			wantErr: "topology_hiding.mode",
		},
		{
			name: "negative rate",
			body: `
zones:
  - name: ext
    role: external
partners:
  - name: p
    zone: ext
    realms: ["p.com"]
    rate_limit:
      requests_per_second: -1
`,
			wantErr: "requests_per_second must be >= 0",
		},
		{
			name: "cert identity without tls",
			body: `
zones:
  - name: ext
    role: external
    tls:
      require_client_cert_identity: true
`,
			wantErr: "requires tls.enabled",
		},
		{
			name: "peer unknown zone",
			body: `
zones:
  - name: core
    role: internal
peers:
  - identity: "p.example.com"
    addresses: ["10.0.0.1"]
    zone: nope
`,
			wantErr: "peers[0].zone",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseEdge(t, tc.body)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestEdgeValidConfigAccepted(t *testing.T) {
	cfg := mustParseEdge(t, `
zones:
  - name: core
    role: internal
    listen:
      port: 3868
      enable_tcp: true
  - name: roaming
    role: external
    listen:
      port: 3869
      enable_tcp: true
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com", "*.partnera.com"]
    peers: ["dea.partnera.com"]
    plmn_ids: ["24001", "310410"]
    applications: [16777251]
    commands: [316]
    screening:
      enabled: true
      default_action: drop
      rule_actions:
        origin_realm: reject
    topology_hiding:
      mode: stateful
      ttl: 5m
    rate_limit:
      enabled: true
      requests_per_second: 250
      per_peer:
        requests_per_second: 100
peers:
  - identity: "hss.example.com"
    addresses: ["10.0.0.5"]
    zone: core
`)
	if cfg.Partners[0].Screening.DefaultAction != ActionDrop {
		t.Fatalf("explicit default action overwritten: %q", cfg.Partners[0].Screening.DefaultAction)
	}
	if cfg.Partners[0].TopologyHiding.TTL != 5*time.Minute {
		t.Fatalf("explicit TTL overwritten: %v", cfg.Partners[0].TopologyHiding.TTL)
	}
	if cfg.Partners[0].RateLimit.PerPeer.Burst != 100 {
		t.Fatalf("per-peer burst default: %+v", cfg.Partners[0].RateLimit.PerPeer)
	}
}

// verify_peer means two different things depending on which side of the
// connection the block configures. On a listener it selects the anchors used to
// check client certificates, so ca_file is mandatory. On a client it means
// "check the server", which the system root store answers perfectly well, and
// demanding ca_file there would push operators toward disabling verification.
func TestVerifyPeerRequiresCAFileOnlyOnListeners(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	for _, p := range []string{cert, key} {
		if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	tlsBlock := fmt.Sprintf(`
      enabled: true
      cert_file: %q
      key_file: %q
      verify_peer: true
`, cert, key)

	_, err := parseEdge(t, `
zones:
  - name: roaming
    role: external
    tls:`+tlsBlock)
	if err == nil || !strings.Contains(err.Error(), "verify_peer requires ca_file") {
		t.Fatalf("zone listener error = %v, want one about verify_peer requiring ca_file", err)
	}

	if _, err := parseEdge(t, `
zones:
  - name: roaming
    role: external
partners:
  - name: p
    zone: roaming
    realms: ["p.com"]
    tls:`+tlsBlock); err != nil {
		t.Fatalf("client TLS without ca_file must stay valid: %v", err)
	}
}
