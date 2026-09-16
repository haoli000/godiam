// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dea_ratelimit

import (
	"testing"
)

const benchConfigYAML = `
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
      requests_per_second: 1000000
      burst: 1000000
      per_peer:
        requests_per_second: 1000000
        burst: 1000000
      per_application:
        16777251:
          requests_per_second: 1000000
          burst: 1000000
      per_command:
        316:
          requests_per_second: 1000000
          burst: 1000000
`

// BenchmarkHandleRoutingAllowed measures the hot path: a request that is under
// every limit and therefore debits a single bucket and passes.
func BenchmarkHandleRoutingAllowed(b *testing.B) {
	ext, _ := newExt(b, benchConfigYAML, nil)
	p := newPeer(b, "roaming", "partner-a")
	msg := request(316, 16777251)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ext.handleRouting(p, msg, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHandleRoutingAllowedParallel measures bucket contention across
// goroutines, which is what the sharded limiter exists to avoid.
func BenchmarkHandleRoutingAllowedParallel(b *testing.B) {
	ext, _ := newExt(b, benchConfigYAML, nil)
	p := newPeer(b, "roaming", "partner-a")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		msg := request(316, 16777251)
		for pb.Next() {
			if _, err := ext.handleRouting(p, msg, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkSelectRule measures scope selection on its own, without the
// token-bucket update.
func BenchmarkSelectRule(b *testing.B) {
	ext, cfg := newExt(b, benchConfigYAML, nil)
	partner := partnerOf(b, cfg, "partner-a")
	keys := ext.keysFor(partner.Name)

	msg := request(316, 16777251)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := selectRule(partner, keys, "dea1.partnera.com", msg); !ok {
			b.Fatal("expected a matching rule")
		}
	}
}

// BenchmarkHandleRoutingInert measures the cost paid by a plain DRA that has
// the extension loaded but no edge model configured.
func BenchmarkHandleRoutingInert(b *testing.B) {
	ext, _ := newExt(b, "identity: \"dra.example.com\"\nrealm: \"example.com\"\n", nil)
	p := newPeer(b, "", "")
	msg := request(316, 16777251)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ext.handleRouting(p, msg, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSelectRuleNoRule measures the miss path: a partner that carries no
// rate limit at all, which must be recognised as cheaply as possible.
func BenchmarkSelectRuleNoRule(b *testing.B) {
	ext, cfg := newExt(b, testConfigYAML, nil)
	partner := partnerOf(b, cfg, "partner-unlimited")
	keys := ext.keysFor(partner.Name)
	msg := request(316, 16777251)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := selectRule(partner, keys, "dea1.open.com", msg); ok {
			b.Fatal("no rule should apply to an unlimited partner")
		}
	}
}
