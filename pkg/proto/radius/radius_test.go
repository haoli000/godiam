// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package radius

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"
)

func requestAuthenticator() [16]byte {
	return [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
}

func TestPacketRoundTripPreservesHeaderAndAttributes(t *testing.T) {
	pkt := &Packet{Code: CodeAccessRequest, Identifier: 7, Authenticator: requestAuthenticator(), Secret: "shared"}
	pkt.AddAttribute(TypeUserName, []byte("alice"))
	pkt.AddAttribute(TypeNASIdentifier, []byte("edge-a"))

	data, err := pkt.Serialize()
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	parsed, err := Parse(data, "shared")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if parsed.Code != CodeAccessRequest || parsed.Identifier != 7 {
		t.Fatalf("header = code %d id %d, want Access-Request id 7", parsed.Code, parsed.Identifier)
	}
	if parsed.Authenticator != requestAuthenticator() {
		t.Fatal("request authenticator changed during round trip")
	}
	if got, ok := parsed.GetAttribute(TypeUserName); !ok || string(got) != "alice" {
		t.Fatalf("User-Name = %q (present=%v), want alice", got, ok)
	}
	if got, ok := parsed.GetAttribute(TypeNASIdentifier); !ok || string(got) != "edge-a" {
		t.Fatalf("NAS-Identifier = %q (present=%v), want edge-a", got, ok)
	}
	if _, ok := parsed.GetAttribute(TypeReplyMessage); ok {
		t.Fatal("GetAttribute reported a missing attribute")
	}
}

func TestResponseAuthenticatorUsesRequestAuthenticatorAndSecret(t *testing.T) {
	pkt := &Packet{Code: CodeAccessAccept, Identifier: 9, Authenticator: requestAuthenticator(), Secret: "shared"}
	pkt.AddAttribute(TypeReplyMessage, []byte("welcome"))

	data, err := pkt.Serialize()
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	auth := requestAuthenticator()
	if bytes.Equal(data[4:20], auth[:]) {
		t.Fatal("response authenticator was left as the request authenticator")
	}
	want, _ := hex.DecodeString("aa64fd4f7734fd569d5da7685a152cc4")
	if !bytes.Equal(data[4:20], want) {
		t.Fatalf("response authenticator = %x, want %x", data[4:20], want)
	}
}

func TestParseRejectsPacketsTooShortForHeader(t *testing.T) {
	if _, err := Parse(make([]byte, 19), "shared"); err == nil {
		t.Fatal("short packet parsed successfully")
	}
}

func TestParseRejectsAdvertisedLengthBelowHeaderWithoutPanic(t *testing.T) {
	for _, length := range []uint16{0, 1, 19} {
		t.Run(fmt.Sprintf("length_%d", length), func(t *testing.T) {
			data := make([]byte, 20)
			data[2], data[3] = byte(length>>8), byte(length) //nolint:gosec // G115: values are fixed malformed RADIUS lengths

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse panicked instead of returning an error: %v", r)
				}
			}()
			if _, err := Parse(data, "shared"); err == nil {
				t.Fatalf("packet with advertised length %d parsed successfully", length)
			}
		})
	}
}

func TestParseRejectsAdvertisedLengthBeyondBuffer(t *testing.T) {
	data := make([]byte, 20)
	data[2], data[3] = 0, 21

	if _, err := Parse(data, "shared"); err == nil {
		t.Fatal("packet with length beyond buffer parsed successfully")
	}
}

// A packet whose attribute lengths do not tile the payload exactly used to be
// accepted with whatever attributes had been read before the loop gave up.
// That is a parsing differential: this gateway and the RADIUS server behind it
// would disagree about what the sender actually asked for.
func TestParseRejectsMalformedAttributeLengths(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "length below the two-byte minimum", payload: []byte{1, 0, 'a', 'b'}},
		{name: "length of one", payload: []byte{1, 1, 'a', 'b'}},
		{name: "length beyond the payload", payload: []byte{1, 9, 'a', 'b'}},
		{name: "attribute truncated to a single byte", payload: []byte{1, 4, 'a', 'b', 2}},
		{name: "valid attribute followed by a stray byte", payload: []byte{1, 3, 'a', 7}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := append(make([]byte, 20), tc.payload...)
			length := len(data)
			data[2], data[3] = byte(length>>8), byte(length) //nolint:gosec // G115: fixed-size test packets

			if _, err := Parse(data, "shared"); err == nil {
				t.Fatal("malformed attribute list parsed successfully")
			}
		})
	}
}
