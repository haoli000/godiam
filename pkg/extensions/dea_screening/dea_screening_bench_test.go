// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dea_screening

import (
	"testing"

	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// BenchmarkEvaluateClean measures the full rule catalogue on a compliant
// message, which is the hot path: almost all traffic passes screening.
func BenchmarkEvaluateClean(b *testing.B) {
	ext, cfg := newExt(b, testConfigYAML, nil)
	partner := partnerOf(b, cfg, "partner-a")
	msg := ulr()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if v := ext.evaluate(ext.reg, partner, "dea1.partnera.com", msg, "example.com"); v != nil {
			b.Fatalf("unexpected violation: %+v", v)
		}
	}
}

// BenchmarkEvaluateWithSubscriptionData measures screening of a realistic S6a
// request carrying a Visited-PLMN-Id, an IMSI and a Route-Record chain.
func BenchmarkEvaluateWithSubscriptionData(b *testing.B) {
	ext, cfg := newExt(b, testConfigYAML, nil)
	partner := partnerOf(b, cfg, "partner-a")

	msg := ulr()
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeUserName, types.AVPFlagMandatory, "240011234567890"))
	msg.AddAVP(message.NewVendorAVP(types.AVPCode3GPPVisitedPLMNID, types.AVPFlagMandatory, vendor3GPP, encodePLMN("24001")))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, "dea1.partnera.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, "ipx.example.net"))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ext.evaluate(ext.reg, partner, "dea1.partnera.com", msg, "example.com")
	}
}

// BenchmarkHandleRoutingInert measures the cost paid by a plain DRA that has
// the extension loaded but no edge model configured.
func BenchmarkHandleRoutingInert(b *testing.B) {
	ext, _ := newExt(b, "identity: \"dra.example.com\"\nrealm: \"example.com\"\n", nil)
	p := newPeer(b, "", "")
	msg := ulr()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ext.handleRouting(p, msg, nil); err != nil {
			b.Fatal(err)
		}
	}
}
