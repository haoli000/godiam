// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTLSFiles(t *testing.T) (string, string, string) {
	t.Helper()
	dir := filepath.Join("testdata", "generated-tls")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	ca := filepath.Join(dir, "ca.pem")
	for _, p := range []string{cert, key, ca} {
		if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return cert, key, ca
}

func TestEdgeValidationRefusesPolicyBypassConfigurations(t *testing.T) {
	cert, key, _ := writeTLSFiles(t)
	tlsNoTransport := fmt.Sprintf(`
zones:
  - name: ext
    role: external
    listen:
      enable_tcp: false
      enable_tls: false
      enable_sctp: false
    tls:
      enabled: true
      cert_file: %q
      key_file: %q
`, cert, key)

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "external zone cannot admit unknown peers",
			body: `
zones:
  - name: ipx
    role: external
    peers_policy:
      allow_unknown: true
`,
			wantErr: "belongs to no partner",
		},
		{
			name: "configured peer on external zone must belong to a partner",
			body: `
zones:
  - name: ipx
    role: external
peers:
  - identity: dea.unowned.example.com
    addresses: [192.0.2.30]
    zone: ipx
`,
			wantErr: "no screening, rate limit or topology hiding policy would apply",
		},
		{
			name: "secure listener requires TLS material",
			body: `
zones:
  - name: ipx
    role: external
    listen:
      enable_tls: true
`,
			wantErr: "listen.enable_tls requires tls.enabled",
		},
		{
			name:    "TLS zone must expose a transport",
			body:    tlsNoTransport,
			wantErr: "tls.enabled but no transport is enabled",
		},
		{
			name: "client certificate identity requires peer verification",
			body: fmt.Sprintf(`
zones:
  - name: ipx
    role: external
    tls:
      enabled: true
      cert_file: %q
      key_file: %q
      require_client_cert_identity: true
`, cert, key),
			wantErr: "requires tls.verify_peer",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseEdge(t, tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to identify %q", err, tc.wantErr)
			}
		})
	}
}

func TestPartnerValidationReportsTheUnsafeField(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "duplicate partner name",
			body: `
zones:
  - name: ipx
    role: external
partners:
  - name: p
    zone: ipx
    realms: [p.example.com]
  - name: p
    zone: ipx
    realms: [other.example.com]
`,
			wantErr: `duplicate name "p"`,
		},
		{
			name: "empty peer identity",
			body: `
zones:
  - name: ipx
    role: external
partners:
  - name: p
    zone: ipx
    realms: [p.example.com]
    peers:
      - identity: ""
`,
			wantErr: "partners[0].peers[0].identity is required",
		},
		{
			name: "negative partner peer weight",
			body: `
zones:
  - name: ipx
    role: external
partners:
  - name: p
    zone: ipx
    realms: [p.example.com]
    peers:
      - identity: dea.p.example.com
        weight: -1
`,
			wantErr: "partners[0].peers[0].weight must be >= 0",
		},
		{
			name: "rule action names offending rule",
			body: `
zones:
  - name: ipx
    role: external
partners:
  - name: p
    zone: ipx
    realms: [p.example.com]
    screening:
      rule_actions:
        origin_host: quarantine
`,
			wantErr: "screening.rule_actions[origin_host]",
		},
		{
			name: "topology store lower bound",
			body: `
zones:
  - name: ipx
    role: external
partners:
  - name: p
    zone: ipx
    realms: [p.example.com]
    topology_hiding:
      max_entries: -2
`,
			wantErr: "topology_hiding.max_entries",
		},
		{
			name: "per command rate action",
			body: `
zones:
  - name: ipx
    role: external
partners:
  - name: p
    zone: ipx
    realms: [p.example.com]
    rate_limit:
      per_command:
        272:
          action: log
`,
			wantErr: "rate_limit.per_command[272]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseEdge(t, tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to identify %q", err, tc.wantErr)
			}
		})
	}
}

func TestRateLimitDefaultsKeepSmallPositiveRatesEnforceable(t *testing.T) {
	cfg := mustParseEdge(t, `
zones:
  - name: ipx
    role: external
partners:
  - name: p
    zone: ipx
    realms: [p.example.com]
    rate_limit:
      enabled: true
      requests_per_second: 0.5
      per_peer:
        requests_per_second: 0.25
      per_command:
        272:
          requests_per_second: 2
`)

	rl := cfg.Partners[0].RateLimit
	if rl.Partner.Burst != 1 || rl.PerPeer.Burst != 1 || rl.PerCommand[272].Burst != 2 {
		t.Fatalf("small positive rate limits must still get a usable burst: %+v", rl)
	}
}
