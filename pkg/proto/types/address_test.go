// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package types

import (
	"bytes"
	"net"
	"testing"
)

func TestNewAddressFromIPUsesRFC6733Families(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ip       string
		wantType uint16
		wantData []byte
	}{
		{
			name:     "ipv4",
			ip:       "203.0.113.7",
			wantType: AddressTypeIPv4,
			wantData: []byte{203, 0, 113, 7},
		},
		{
			name:     "ipv6",
			ip:       "2001:db8::1",
			wantType: AddressTypeIPv6,
			wantData: []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			addr := NewAddressFromIP(net.ParseIP(tt.ip))

			if addr.Type != tt.wantType {
				t.Fatalf("address family = %d, want %d", addr.Type, tt.wantType)
			}
			if !bytes.Equal(addr.Data, tt.wantData) {
				t.Fatalf("address bytes = %v, want %v", addr.Data, tt.wantData)
			}
		})
	}
}

func TestAddressEncodeUsesTwoByteBigEndianFamilyPrefix(t *testing.T) {
	t.Parallel()

	addr := Address{Type: 0x1234, Data: []byte{0xaa, 0xbb, 0xcc}}

	got := addr.Encode()
	want := []byte{0x12, 0x34, 0xaa, 0xbb, 0xcc}
	if !bytes.Equal(got, want) {
		t.Fatalf("encoded address = %#v, want %#v", got, want)
	}

	// Mutating the encoded value must not rewrite the address retained by AVP callers.
	got[2] = 0
	if addr.Data[0] != 0xaa {
		t.Fatalf("Encode exposed address storage: first byte = %#x", addr.Data[0])
	}
}

func TestDecodeAddressRejectsMissingFamilyPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "one byte", data: []byte{0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := DecodeAddress(tt.data); err == nil {
				t.Fatal("DecodeAddress accepted data without the two-byte family prefix")
			}
		})
	}
}

func TestAddressRoundTripsIPv4(t *testing.T) {
	t.Parallel()

	original := NewAddressFromIP(net.ParseIP("192.0.2.33"))
	decoded, err := DecodeAddress(original.Encode())
	if err != nil {
		t.Fatalf("DecodeAddress returned error: %v", err)
	}

	if decoded.Type != AddressTypeIPv4 || !bytes.Equal(decoded.Data, []byte{192, 0, 2, 33}) {
		t.Fatalf("decoded address = {Type:%d Data:%v}", decoded.Type, decoded.Data)
	}
	if !decoded.ToIP().Equal(net.ParseIP("192.0.2.33")) {
		t.Fatalf("ToIP() = %v, want 192.0.2.33", decoded.ToIP())
	}
}

func TestAddressRoundTripsIPv6(t *testing.T) {
	t.Parallel()

	original := NewAddressFromIP(net.ParseIP("2001:db8:85a3::8a2e:370:7334"))
	decoded, err := DecodeAddress(original.Encode())
	if err != nil {
		t.Fatalf("DecodeAddress returned error: %v", err)
	}

	wantIP := net.ParseIP("2001:db8:85a3::8a2e:370:7334")
	if decoded.Type != AddressTypeIPv6 || !decoded.ToIP().Equal(wantIP) {
		t.Fatalf("decoded address = {Type:%d IP:%v}, want family %d IP %v", decoded.Type, decoded.ToIP(), AddressTypeIPv6, wantIP)
	}
}

func TestAddressRoundTripsUnknownFamily(t *testing.T) {
	t.Parallel()

	// Relays still need lossless handling for future or non-IP IANA address families.
	original := Address{Type: 99, Data: []byte{0xde, 0xad, 0xbe, 0xef, 0x00}}
	decoded, err := DecodeAddress(original.Encode())
	if err != nil {
		t.Fatalf("DecodeAddress returned error: %v", err)
	}

	if decoded.Type != original.Type || !bytes.Equal(decoded.Data, original.Data) {
		t.Fatalf("decoded address = {Type:%d Data:%v}, want {Type:%d Data:%v}", decoded.Type, decoded.Data, original.Type, original.Data)
	}
}
