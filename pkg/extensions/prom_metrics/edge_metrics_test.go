// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package prom_metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
)

const edgeConfigYAML = `
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
    peers: ["dea1.partnera.com"]
  - name: partner-b
    zone: roaming
    realms: ["partnerb.com"]
    peers: ["dea1.partnerb.com"]
`

func newEdgeContext(t *testing.T, yaml string) *mockInitContext {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()
	return &mockInitContext{
		router:    routing.NewRouter(),
		dict:      dict,
		mgr:       extension.NewManager(nil),
		startTime: time.Now(),
		cfg:       cfg,
	}
}

func gather(t *testing.T, ctx *mockInitContext) string {
	t.Helper()
	reg := prometheus.NewRegistry()
	if err := reg.Register(newDiameterCollector(ctx)); err != nil {
		t.Fatalf("registering collector: %v", err)
	}
	var sb strings.Builder
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			sb.WriteString(f.GetName())
			sb.WriteString("{")
			for _, l := range m.GetLabel() {
				sb.WriteString(l.GetName() + "=" + l.GetValue() + ",")
			}
			sb.WriteString("}\n")
		}
	}
	return sb.String()
}

func TestEdgeMetricsReportZonesAndPartners(t *testing.T) {
	text := gather(t, newEdgeContext(t, edgeConfigYAML))

	for _, want := range []string{
		"diameter_edge_zones{role=internal,}",
		"diameter_edge_zones{role=external,}",
		"diameter_edge_partners{zone=roaming,}",
		"diameter_edge_partner_peers_up{partner=partner-a,zone=roaming,}",
		"diameter_edge_partner_peers_up{partner=partner-b,zone=roaming,}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing metric %q in:\n%s", want, text)
		}
	}
}

func TestEdgeMetricsAbsentForLegacyConfiguration(t *testing.T) {
	text := gather(t, newEdgeContext(t, "identity: \"dra.example.com\"\nrealm: \"example.com\"\n"))

	if strings.Contains(text, "diameter_edge_") {
		t.Errorf("a legacy configuration must not export edge metrics:\n%s", text)
	}
}
