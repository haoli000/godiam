// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package edge

import (
	"testing"

	"github.com/haoli000/godiam/pkg/core/config"
)

// benchConfig has a partner set of a size a real interconnect would have.
func benchRegistry(b *testing.B) *Registry {
	b.Helper()
	yaml := `
zones:
  - name: core
    role: internal
    realms: ["core.example.com"]
  - name: roaming
    role: external
partners:
`
	for i := 0; i < 50; i++ {
		name := string(rune('a' + i%26))
		yaml += "  - name: partner-" + name + string(rune('0'+i/26)) + "\n" +
			"    zone: roaming\n" +
			"    realms: [\"p" + name + string(rune('0'+i/26)) + ".example.net\"]\n" +
			"    peers: [\"dea1.p" + name + string(rune('0'+i/26)) + ".example.net\"]\n"
	}
	cfg, err := config.Parse([]byte("identity: \"dea.example.com\"\nrealm: \"example.com\"\n" + yaml))
	if err != nil {
		b.Fatalf("parsing config: %v", err)
	}
	return NewRegistry(cfg)
}

func BenchmarkRegistryPartnerForPeer(b *testing.B) {
	r := benchRegistry(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := r.PartnerForPeer("dea1.pa0.example.net"); !ok {
			b.Fatal("partner not found")
		}
	}
}

func BenchmarkRegistryZoneNameForPeer(b *testing.B) {
	r := benchRegistry(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if r.ZoneNameForPeer("dea1.pa0.example.net") == "" {
			b.Fatal("zone not found")
		}
	}
}

func BenchmarkRegistryServesRealm(b *testing.B) {
	r := benchRegistry(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !r.ServesRealm("core.example.com") {
			b.Fatal("realm should be served")
		}
	}
}

func BenchmarkRegistryIsInternalHost(b *testing.B) {
	r := benchRegistry(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !r.IsInternalHost("hss1.core.example.com") {
			b.Fatal("host should be internal")
		}
	}
}
