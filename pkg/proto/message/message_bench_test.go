// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package message

import (
	"bytes"
	"testing"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func BenchmarkAVPEncodeUTF8String(b *testing.B) {
	avp := NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "session-1234567890-abcdefghijklmnopqrstuvwxyz")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = avp.Encode()
	}
}

func BenchmarkAVPDecodeUTF8String(b *testing.B) {
	avp := NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "session-1234567890-abcdefghijklmnopqrstuvwxyz")
	encoded, _ := avp.Encode()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeAVP(encoded)
	}
}

func BenchmarkMessageEncodeCER(b *testing.B) {
	msg := NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))
	msg.AddAVP(NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, 0))
	msg.AddAVP(NewUTF8StringAVP(types.AVPCodeProductName, 0, "godiam"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = msg.Encode()
	}
}

func BenchmarkMessageDecodeCER(b *testing.B) {
	msg := NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))
	msg.AddAVP(NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, 0))
	encoded, _ := msg.Encode()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeMessage(encoded)
	}
}

func BenchmarkMessageEncodeCCR(b *testing.B) {
	msg := NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "session;12345;67890"))
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	msg.AddAVP(NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppIDCreditControl)))
	msg.AddAVP(NewUnsigned32AVP(types.AVPCodeCCRequestType, types.AVPFlagMandatory, 1))
	msg.AddAVP(NewUnsigned32AVP(types.AVPCodeCCRequestNumber, types.AVPFlagMandatory, 0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = msg.Encode()
	}
}

func BenchmarkMessageDecodeCCR(b *testing.B) {
	msg := NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "session;12345;67890"))
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	msg.AddAVP(NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppIDCreditControl)))
	msg.AddAVP(NewUnsigned32AVP(types.AVPCodeCCRequestType, types.AVPFlagMandatory, 1))
	msg.AddAVP(NewUnsigned32AVP(types.AVPCodeCCRequestNumber, types.AVPFlagMandatory, 0))
	encoded, _ := msg.Encode()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeMessage(encoded)
	}
}

func BenchmarkReadMessage(b *testing.B) {
	msg := NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "session;12345;67890"))
	encoded, _ := msg.Encode()
	reader := bytes.NewReader(encoded)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reader.Seek(0, 0)
		_, _ = ReadMessage(reader)
	}
}
