// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package message

import (
	"bytes"
	"testing"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func TestNewMessage(t *testing.T) {
	msg := NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)

	if msg.Version != types.DiameterVersion {
		t.Errorf("expected version %d, got %d", types.DiameterVersion, msg.Version)
	}
	if !msg.IsRequest() {
		t.Error("expected message to be a request")
	}
	if msg.CommandCode != types.CmdCodeCapabilitiesExchange {
		t.Errorf("expected command code %d, got %d", types.CmdCodeCapabilitiesExchange, msg.CommandCode)
	}
	if msg.ApplicationID != types.AppIDCommon {
		t.Errorf("expected app ID %d, got %d", types.AppIDCommon, msg.ApplicationID)
	}
}

func TestNewAnswer(t *testing.T) {
	req := NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	req.HopByHopID = 12345
	req.EndToEndID = 67890

	ans := NewAnswer(req)

	if ans.IsRequest() {
		t.Error("expected answer to not be a request")
	}
	if ans.CommandCode != req.CommandCode {
		t.Errorf("expected command code %d, got %d", req.CommandCode, ans.CommandCode)
	}
	if ans.HopByHopID != req.HopByHopID {
		t.Errorf("expected hop-by-hop ID %d, got %d", req.HopByHopID, ans.HopByHopID)
	}
	if ans.EndToEndID != req.EndToEndID {
		t.Errorf("expected end-to-end ID %d, got %d", req.EndToEndID, ans.EndToEndID)
	}
}

func TestAVPCreation(t *testing.T) {
	// Test UTF8String AVP
	avp := NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "session123")
	if avp.Code != types.AVPCodeSessionID {
		t.Errorf("expected code %d, got %d", types.AVPCodeSessionID, avp.Code)
	}
	if !avp.IsMandatory() {
		t.Error("expected AVP to be mandatory")
	}
	if avp.GetUTF8String() != "session123" {
		t.Errorf("expected 'session123', got %q", avp.GetUTF8String())
	}

	// Test Unsigned32 AVP
	avp2 := NewUnsigned32AVP(types.AVPCodeResultCode, types.AVPFlagMandatory, 2001)
	val, err := avp2.GetUnsigned32()
	if err != nil {
		t.Fatalf("GetUnsigned32 failed: %v", err)
	}
	if val != 2001 {
		t.Errorf("expected 2001, got %d", val)
	}
}

func TestAVPEncodeDecode(t *testing.T) {
	// Create an AVP
	original := NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "test-session-id")

	// Encode it
	encoded, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode it
	decoded, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("DecodeAVP failed: %v", err)
	}

	// Verify
	if decoded.Code != original.Code {
		t.Errorf("code mismatch: expected %d, got %d", original.Code, decoded.Code)
	}
	if decoded.Flags != original.Flags {
		t.Errorf("flags mismatch: expected %d, got %d", original.Flags, decoded.Flags)
	}
	if decoded.GetUTF8String() != original.GetUTF8String() {
		t.Errorf("data mismatch: expected %q, got %q", original.GetUTF8String(), decoded.GetUTF8String())
	}
}

func TestVendorAVPEncodeDecode(t *testing.T) {
	// Create a vendor-specific AVP
	data := []byte{0x01, 0x02, 0x03, 0x04}
	original := NewVendorAVP(1000, types.AVPFlagMandatory, types.VendorID3GPP, data)

	// Encode it
	encoded, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode it
	decoded, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("DecodeAVP failed: %v", err)
	}

	// Verify
	if !decoded.IsVendor() {
		t.Error("expected decoded AVP to have vendor flag")
	}
	if decoded.VendorID != types.VendorID3GPP {
		t.Errorf("vendor ID mismatch: expected %d, got %d", types.VendorID3GPP, decoded.VendorID)
	}
	if !bytes.Equal(decoded.Data, data) {
		t.Errorf("data mismatch: expected %v, got %v", data, decoded.Data)
	}
}

func TestMessageEncodeDecode(t *testing.T) {
	// Create a CER message
	msg := NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	msg.HopByHopID = 0x12345678
	msg.EndToEndID = 0xABCDEF01

	// Add AVPs
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))
	msg.AddAVP(NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, 0))

	// Encode
	encoded, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage failed: %v", err)
	}

	// Verify header
	if decoded.Version != msg.Version {
		t.Errorf("version mismatch: expected %d, got %d", msg.Version, decoded.Version)
	}
	if decoded.Flags != msg.Flags {
		t.Errorf("flags mismatch: expected 0x%02x, got 0x%02x", msg.Flags, decoded.Flags)
	}
	if decoded.CommandCode != msg.CommandCode {
		t.Errorf("command code mismatch: expected %d, got %d", msg.CommandCode, decoded.CommandCode)
	}
	if decoded.ApplicationID != msg.ApplicationID {
		t.Errorf("app ID mismatch: expected %d, got %d", msg.ApplicationID, decoded.ApplicationID)
	}
	if decoded.HopByHopID != msg.HopByHopID {
		t.Errorf("hop-by-hop ID mismatch: expected 0x%08x, got 0x%08x", msg.HopByHopID, decoded.HopByHopID)
	}
	if decoded.EndToEndID != msg.EndToEndID {
		t.Errorf("end-to-end ID mismatch: expected 0x%08x, got 0x%08x", msg.EndToEndID, decoded.EndToEndID)
	}

	// Verify AVPs
	if len(decoded.AVPs) != len(msg.AVPs) {
		t.Fatalf("AVP count mismatch: expected %d, got %d", len(msg.AVPs), len(decoded.AVPs))
	}

	originHost, ok := decoded.GetOriginHost()
	if !ok {
		t.Error("Origin-Host not found")
	} else if originHost != "client.example.com" {
		t.Errorf("Origin-Host mismatch: expected 'client.example.com', got %q", originHost)
	}

	originRealm, ok := decoded.GetOriginRealm()
	if !ok {
		t.Error("Origin-Realm not found")
	} else if originRealm != "example.com" {
		t.Errorf("Origin-Realm mismatch: expected 'example.com', got %q", originRealm)
	}
}

func TestGroupedAVP(t *testing.T) {
	// Create a grouped AVP (Vendor-Specific-Application-ID)
	grouped := NewGroupedAVP(
		types.AVPCodeVendorSpecificAppID,
		types.AVPFlagMandatory,
		0,
		NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(types.VendorID3GPP)),
		NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppID3GPPGx)),
	)

	if !grouped.IsGrouped() {
		t.Error("expected AVP to be grouped")
	}
	if len(grouped.Children()) != 2 {
		t.Errorf("expected 2 children, got %d", len(grouped.Children()))
	}

	// Encode
	encoded, err := grouped.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("DecodeAVP failed: %v", err)
	}

	// Decode grouped content
	if err := decoded.DecodeGrouped(); err != nil {
		t.Fatalf("DecodeGrouped failed: %v", err)
	}

	if len(decoded.Children()) != 2 {
		t.Errorf("expected 2 children, got %d", len(decoded.Children()))
	}

	// Find vendor ID child
	vendorChild := decoded.FindChild(types.AVPCodeVendorID, 0)
	if vendorChild == nil {
		t.Fatal("Vendor-ID child not found")
	}
	vendorVal, err := vendorChild.GetUnsigned32()
	if err != nil {
		t.Fatalf("GetUnsigned32 failed: %v", err)
	}
	if types.VendorID(vendorVal) != types.VendorID3GPP {
		t.Errorf("expected vendor ID %d, got %d", types.VendorID3GPP, vendorVal)
	}
}

func TestAVPPadding(t *testing.T) {
	testCases := []struct {
		name     string
		dataLen  int
		expected int // expected encoded length including padding
	}{
		{"1 byte data", 1, 12}, // 8 header + 1 data + 3 padding
		{"2 byte data", 2, 12}, // 8 header + 2 data + 2 padding
		{"3 byte data", 3, 12}, // 8 header + 3 data + 1 padding
		{"4 byte data", 4, 12}, // 8 header + 4 data + 0 padding
		{"5 byte data", 5, 16}, // 8 header + 5 data + 3 padding
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, tc.dataLen)
			avp := NewOctetStringAVP(100, 0, data)

			encoded, err := avp.Encode()
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			if len(encoded) != tc.expected {
				t.Errorf("expected encoded length %d, got %d", tc.expected, len(encoded))
			}
		})
	}
}

func TestReadMessage(t *testing.T) {
	// Create and encode a message
	msg := NewRequest(types.CmdCodeDeviceWatchdog, types.AppIDCommon)
	msg.HopByHopID = 1
	msg.EndToEndID = 2
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "test.example.com"))

	encoded, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Read from a buffer
	reader := bytes.NewReader(encoded)
	decoded, err := ReadMessage(reader)
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if decoded.CommandCode != msg.CommandCode {
		t.Errorf("command code mismatch: expected %d, got %d", msg.CommandCode, decoded.CommandCode)
	}
}
