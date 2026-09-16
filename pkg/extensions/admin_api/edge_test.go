// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package admin_api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
)

// edgeConfigTemplate needs real files for the TLS material, so %s is replaced
// with a temporary directory by edgeAPI.
const edgeConfigTemplate = `
identity: "dea.example.com"
realm: "example.com"
zones:
  - name: core
    role: internal
  - name: roaming
    role: external
    listen:
      addresses: ["10.0.0.1"]
      port: 3868
      secure_port: 5868
      enable_tls: true
    tls:
      enabled: true
      verify_peer: true
      cert_file: "%[1]s/edge.pem"
      key_file: "%[1]s/edge.key"
      ca_file: "%[1]s/ca.pem"
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com"]
    peers: ["dea1.partnera.com"]
    topology_hiding:
      mode: stateful
    rate_limit:
      enabled: true
      requests_per_second: 100
`

// edgeConfigYAML materialises the template with throwaway TLS files.
func edgeConfigYAML(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"edge.pem", "edge.key", "ca.pem"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return fmt.Sprintf(edgeConfigTemplate, dir)
}

func edgeAPI(t *testing.T, yaml string) *adminAPI {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	return &adminAPI{cfg: cfg, reg: edge.NewRegistry(cfg)}
}

func TestHandleEdgeReportsZonesAndPartners(t *testing.T) {
	api := edgeAPI(t, edgeConfigYAML(t))

	rec := httptest.NewRecorder()
	api.handleEdge(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/edge", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Configured bool          `json:"configured"`
		Identity   string        `json:"identity"`
		Zones      []zoneView    `json:"zones"`
		Partners   []partnerView `json:"partners"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if !body.Configured || body.Identity != "dea.example.com" {
		t.Errorf("configured=%v identity=%q", body.Configured, body.Identity)
	}
	if len(body.Zones) != 2 || len(body.Partners) != 1 {
		t.Fatalf("got %d zones and %d partners, want 2 and 1", len(body.Zones), len(body.Partners))
	}

	var roaming zoneView
	for _, z := range body.Zones {
		if z.Name == "roaming" {
			roaming = z
		}
	}
	if roaming.Role != "external" || !roaming.TLS || !roaming.MutualTLS {
		t.Errorf("roaming zone = %+v, want an external mutual TLS zone", roaming)
	}
	if roaming.Partners != 1 {
		t.Errorf("roaming zone partners = %d, want 1", roaming.Partners)
	}

	partner := body.Partners[0]
	if partner.TopologyHiding != "stateful" || !partner.RateLimit || !partner.Screening {
		t.Errorf("partner = %+v, want the configured policies reported", partner)
	}
	if partner.TLSOverride {
		t.Error("the partner has no TLS override")
	}
}

func TestHandleEdgeNeverLeaksKeyMaterial(t *testing.T) {
	api := edgeAPI(t, edgeConfigYAML(t))

	rec := httptest.NewRecorder()
	api.handleEdge(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/edge", nil))

	for _, secret := range []string{"edge.key", "edge.pem", "ca.pem"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("response leaks %q", secret)
		}
	}
}

func TestHandleEdgeLegacyConfiguration(t *testing.T) {
	api := edgeAPI(t, "identity: \"dra.example.com\"\nrealm: \"example.com\"\n")

	rec := httptest.NewRecorder()
	api.handleEdge(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/edge", nil))

	var body struct {
		Configured bool       `json:"configured"`
		Zones      []zoneView `json:"zones"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Configured {
		t.Error("a legacy configuration must report configured=false")
	}
	if len(body.Zones) != 1 || body.Zones[0].Role != "internal" {
		t.Errorf("zones = %+v, want the implicit default zone", body.Zones)
	}
}

func TestHandleEdgeRejectsNonGET(t *testing.T) {
	api := edgeAPI(t, edgeConfigYAML(t))

	rec := httptest.NewRecorder()
	api.handleEdge(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/edge", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
