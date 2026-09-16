// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package message

import (
	"bytes"
	"math"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func TestEncodeDecodeEncodeIsStableForMixedAVPs(t *testing.T) {
	grouped := NewGroupedAVP(
		types.AVPCodeVendorSpecificAppID,
		types.AVPFlagMandatory,
		0,
		NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(types.VendorID3GPP)),
		NewVendorAVP(7000, types.AVPFlagMandatory, types.VendorID3GPP, []byte{1, 2, 3}),
	)
	msg := NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	msg.HopByHopID = 0x01020304
	msg.EndToEndID = 0x05060708
	msg.SetProxiable(true)
	msg.AddAVPs(
		NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"),
		NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"),
		NewOctetStringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, []byte{0, 1, 2}),
		grouped,
	)

	first, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	decoded, err := DecodeMessage(first)
	if err != nil {
		t.Fatalf("DecodeMessage failed: %v", err)
	}
	second, err := decoded.Encode()
	if err != nil {
		t.Fatalf("re-encoding decoded message failed: %v", err)
	}
	if !bytes.Equal(second, first) {
		t.Fatal("encode/decode/encode changed the wire representation")
	}

	decodedGroup := decoded.FindAVP(types.AVPCodeVendorSpecificAppID, 0)
	if decodedGroup == nil {
		t.Fatal("grouped AVP missing after decode")
	}
	if err := decodedGroup.DecodeGrouped(); err != nil {
		t.Fatalf("DecodeGrouped failed: %v", err)
	}
	if child := decodedGroup.FindChild(7000, types.VendorID3GPP); child == nil || !child.IsVendor() {
		t.Fatalf("vendor child was not decoded with its Vendor-Id: %#v", child)
	}
}

func TestEncodeToRejectsUndersizedBuffers(t *testing.T) {
	avp := NewOctetStringAVP(100, 0, []byte("abcd"))

	if _, err := avp.EncodeTo(make([]byte, avp.EncodedLen()-1)); err == nil {
		t.Fatal("EncodeTo accepted a buffer one byte smaller than the wire AVP")
	}
}

func TestTypedAVPRoundTripsBoundaryValues(t *testing.T) {
	when := time.Unix(1700000000, 0).UTC()
	ip := types.NewAddressFromIP(net.ParseIP("2001:db8::1"))
	tests := []struct {
		name  string
		avp   *AVP
		check func(*testing.T, *AVP)
	}{
		{
			name: "integer32 minimum",
			avp:  NewInteger32AVP(1, 0, math.MinInt32),
			check: func(t *testing.T, avp *AVP) {
				t.Helper()
				got, err := avp.GetInteger32()
				if err != nil || got != math.MinInt32 {
					t.Fatalf("Integer32 = %d, %v; want %d", got, err, math.MinInt32)
				}
			},
		},
		{
			name: "integer64 maximum",
			avp:  NewInteger64AVP(2, 0, math.MaxInt64),
			check: func(t *testing.T, avp *AVP) {
				t.Helper()
				got, err := avp.GetInteger64()
				if err != nil || got != math.MaxInt64 {
					t.Fatalf("Integer64 = %d, %v; want %d", got, err, int64(math.MaxInt64))
				}
			},
		},
		{
			name: "unsigned64 maximum",
			avp:  NewUnsigned64AVP(3, 0, math.MaxUint64),
			check: func(t *testing.T, avp *AVP) {
				t.Helper()
				got, err := avp.GetUnsigned64()
				if err != nil || got != math.MaxUint64 {
					t.Fatalf("Unsigned64 = %d, %v; want %d", got, err, uint64(math.MaxUint64))
				}
			},
		},
		{
			name: "enumerated negative",
			avp:  NewEnumeratedAVP(4, 0, -2),
			check: func(t *testing.T, avp *AVP) {
				t.Helper()
				got, err := avp.GetInteger32()
				if err != nil || got != -2 {
					t.Fatalf("Enumerated = %d, %v; want -2", got, err)
				}
			},
		},
		{
			name: "float32 negative infinity",
			avp:  NewFloat32AVP(5, 0, float32(math.Inf(-1))),
			check: func(t *testing.T, avp *AVP) {
				t.Helper()
				got, err := avp.GetFloat32()
				if err != nil || !math.IsInf(float64(got), -1) {
					t.Fatalf("Float32 = %v, %v; want -Inf", got, err)
				}
			},
		},
		{
			name: "float64 nan preserves nan class",
			avp:  NewFloat64AVP(6, 0, math.NaN()),
			check: func(t *testing.T, avp *AVP) {
				t.Helper()
				got, err := avp.GetFloat64()
				if err != nil || !math.IsNaN(got) {
					t.Fatalf("Float64 = %v, %v; want NaN", got, err)
				}
			},
		},
		{
			name: "address IPv6",
			avp:  NewAddressAVP(7, 0, ip),
			check: func(t *testing.T, avp *AVP) {
				t.Helper()
				got, err := avp.GetAddress()
				if err != nil || !got.ToIP().Equal(net.ParseIP("2001:db8::1")) {
					t.Fatalf("Address = %v, %v; want 2001:db8::1", got.ToIP(), err)
				}
			},
		},
		{
			name: "time",
			avp:  NewTimeAVP(8, 0, when),
			check: func(t *testing.T, avp *AVP) {
				t.Helper()
				got, err := avp.GetTime()
				if err != nil || !got.Equal(when) {
					t.Fatalf("Time = %v, %v; want %v", got, err, when)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := tc.avp.Encode()
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}
			decoded, err := DecodeAVP(wire)
			if err != nil {
				t.Fatalf("DecodeAVP failed: %v", err)
			}
			tc.check(t, decoded)
		})
	}
}

func TestTypedAccessorsRejectShortData(t *testing.T) {
	tests := []struct {
		name string
		avp  *AVP
		call func(*AVP) error
	}{
		{name: "integer32", avp: NewAVP(1, 0, 0, []byte{1, 2, 3}), call: func(a *AVP) error { _, err := a.GetInteger32(); return err }},
		{name: "integer64", avp: NewAVP(1, 0, 0, []byte{1, 2, 3, 4, 5, 6, 7}), call: func(a *AVP) error { _, err := a.GetInteger64(); return err }},
		{name: "unsigned32", avp: NewAVP(1, 0, 0, []byte{1}), call: func(a *AVP) error { _, err := a.GetUnsigned32(); return err }},
		{name: "unsigned64", avp: NewAVP(1, 0, 0, []byte{1}), call: func(a *AVP) error { _, err := a.GetUnsigned64(); return err }},
		{name: "float32", avp: NewAVP(1, 0, 0, []byte{1}), call: func(a *AVP) error { _, err := a.GetFloat32(); return err }},
		{name: "float64", avp: NewAVP(1, 0, 0, []byte{1}), call: func(a *AVP) error { _, err := a.GetFloat64(); return err }},
		{name: "time", avp: NewAVP(1, 0, 0, []byte{1}), call: func(a *AVP) error { _, err := a.GetTime(); return err }},
		{name: "address", avp: NewAVP(1, 0, 0, []byte{1}), call: func(a *AVP) error { _, err := a.GetAddress(); return err }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(tc.avp); err == nil {
				t.Fatal("short data was accepted")
			}
		})
	}
}

func TestMessageHelpersFindAndUpdateProtocolAVPs(t *testing.T) {
	msg := NewRequest(types.CmdCodeAccounting, types.AppIDCommon)
	msg.AddAVPs(
		NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "session-1"),
		NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "origin.example.com"),
		NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"),
		NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "server.example.net"),
		NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.net"),
		NewUTF8StringAVP(types.AVPCodeUserName, 0, "alice"),
		NewUnsigned32AVP(types.AVPCodeAccountingRecordType, types.AVPFlagMandatory, uint32(types.AccountingStartRecord)),
		NewUnsigned32AVP(types.AVPCodeAccountingRecordNumber, types.AVPFlagMandatory, 42),
		NewVendorAVP(9000, 0, 10415, []byte("first")),
		NewVendorAVP(9000, 0, 99999, []byte("second")),
	)
	msg.SetResultCode(types.ResultSuccess)
	msg.SetResultCode(types.ResultLimitedSuccess)
	msg.SetError(true)
	msg.SetRetransmit(true)
	msg.SetProxiable(false)

	if sessionID, ok := msg.GetSessionID(); !ok || sessionID != "session-1" {
		t.Fatalf("Session-Id = %q (present=%v)", sessionID, ok)
	}
	if host, ok := msg.GetDestinationHost(); !ok || host != "server.example.net" {
		t.Fatalf("Destination-Host = %q (present=%v)", host, ok)
	}
	if realm, ok := msg.GetDestinationRealm(); !ok || realm != "example.net" {
		t.Fatalf("Destination-Realm = %q (present=%v)", realm, ok)
	}
	if user, ok := msg.GetUserName(); !ok || user != "alice" {
		t.Fatalf("User-Name = %q (present=%v)", user, ok)
	}
	if result, ok := msg.GetResultCode(); !ok || result != types.ResultLimitedSuccess {
		t.Fatalf("Result-Code = %d (present=%v)", result, ok)
	}
	if typ, ok := msg.GetAccountingRecordType(); !ok || typ != types.AccountingStartRecord {
		t.Fatalf("Accounting-Record-Type = %d (present=%v)", typ, ok)
	}
	if number, ok := msg.GetAccountingRecordNumber(); !ok || number != 42 {
		t.Fatalf("Accounting-Record-Number = %d (present=%v)", number, ok)
	}
	if msg.IsProxiable() || !msg.IsError() || !msg.IsRetransmit() {
		t.Fatalf("flags proxiable=%v error=%v retransmit=%v", msg.IsProxiable(), msg.IsError(), msg.IsRetransmit())
	}
	if got := msg.FindAVP(9000, 10415); got == nil || got.GetUTF8String() != "first" {
		t.Fatalf("vendor-specific FindAVP returned %#v", got)
	}
	if got := msg.FindAllAVPs(9000, 0); len(got) != 2 {
		t.Fatalf("FindAllAVPs found %d vendor AVPs, want 2", len(got))
	}
}

func TestMessageHelpersReportMissingOrMalformedAVPs(t *testing.T) {
	msg := NewAnswer(NewRequest(types.CmdCodeAccounting, types.AppIDCommon))
	msg.AddAVPs(
		NewAVP(types.AVPCodeResultCode, 0, 0, []byte{1}),
		NewAVP(types.AVPCodeAccountingRecordType, 0, 0, []byte{1}),
		NewAVP(types.AVPCodeAccountingRecordNumber, 0, 0, []byte{1}),
	)

	if _, ok := msg.GetOriginHost(); ok {
		t.Fatal("missing Origin-Host was reported as present")
	}
	if _, ok := msg.GetResultCode(); ok {
		t.Fatal("short Result-Code was reported as present")
	}
	if _, ok := msg.GetAccountingRecordType(); ok {
		t.Fatal("short Accounting-Record-Type was reported as present")
	}
	if _, ok := msg.GetAccountingRecordNumber(); ok {
		t.Fatal("short Accounting-Record-Number was reported as present")
	}
}

func TestAVPFlagAndChildHelpers(t *testing.T) {
	parent := NewGroupedAVP(1000, types.AVPFlagProtected, 0)
	parent.SetVendorID(10415)
	parent.AddChild(NewUTF8StringAVP(1, 0, "first"))
	parent.AddChild(NewVendorAVP(1, 0, 7, []byte("vendor")))
	parent.RemoveChild(-1)
	parent.RemoveChild(99)

	if !parent.IsProtected() || !parent.IsVendor() || parent.VendorID != 10415 {
		t.Fatalf("flags protected=%v vendor=%v vendorID=%d", parent.IsProtected(), parent.IsVendor(), parent.VendorID)
	}
	if child := parent.FindChild(1, 7); child == nil || string(child.GetOctetString()) != "vendor" {
		t.Fatalf("FindChild did not respect Vendor-Id: %#v", child)
	}
	parent.RemoveChild(0)
	if len(parent.Children()) != 1 || parent.Children()[0].VendorID != 7 {
		t.Fatalf("RemoveChild left children %#v", parent.Children())
	}
}

func TestNewAnswerWithResultSetsErrorOnlyForNonSuccessResults(t *testing.T) {
	req := NewRequest(types.CmdCodeDeviceWatchdog, types.AppIDCommon)

	success := NewAnswerWithResult(req, types.ResultSuccess)
	if success.IsRequest() || success.IsError() {
		t.Fatalf("success answer request=%v error=%v", success.IsRequest(), success.IsError())
	}
	failure := NewAnswerWithResult(req, types.ResultUnableToDeliver)
	if failure.IsRequest() || !failure.IsError() {
		t.Fatalf("failure answer request=%v error=%v", failure.IsRequest(), failure.IsError())
	}
}

func TestDumpFormatsIncludeRoutingContext(t *testing.T) {
	msg := NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	msg.HopByHopID = 10
	msg.EndToEndID = 11
	msg.AddAVPs(
		NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "origin.example.com"),
		NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"),
		NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "dest.example.net"),
		NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.net"),
		NewGroupedAVP(1000, types.AVPFlagMandatory, 0, NewOctetStringAVP(1, types.AVPFlagProtected, []byte{1})),
	)

	for name, dump := range map[string]string{
		"summary": msg.DumpSummary(),
		"full":    msg.DumpFull(),
		"tree":    msg.DumpTree(),
		"avp":     msg.AVPs[4].String(),
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(dump, "origin.example.com") && !strings.Contains(dump, "Grouped AVP") {
				t.Fatalf("dump %q did not include useful routing or grouping context: %s", name, dump)
			}
		})
	}
}
