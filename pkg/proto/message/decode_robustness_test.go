// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package message

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func diameterHeader(length int) []byte {
	data := make([]byte, types.DiameterHeaderSize)
	data[0] = types.DiameterVersion
	var lengthBytes [4]byte
	binary.BigEndian.PutUint32(lengthBytes[:], uint32(length)) //nolint:gosec // Test data stays in the protocol's 24-bit range.
	copy(data[1:4], lengthBytes[1:4])
	return data
}

func avpBytes(flags uint8, length int, body []byte) []byte {
	data := make([]byte, types.AVPHeaderSize+len(body))
	binary.BigEndian.PutUint32(data[0:4], 1)
	data[4] = flags
	var lengthBytes [4]byte
	binary.BigEndian.PutUint32(lengthBytes[:], uint32(length)) //nolint:gosec // Test data stays in the protocol's 24-bit range.
	copy(data[5:8], lengthBytes[1:4])
	copy(data[types.AVPHeaderSize:], body)
	return data
}

func appendToDiameterHeader(payload []byte) []byte {
	data := diameterHeader(types.DiameterHeaderSize + len(payload))
	return append(data, payload...)
}

func requireDecodeMessageError(t *testing.T, data []byte, want string) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DecodeMessage panicked on untrusted input: %v", r)
		}
	}()

	_, err := DecodeMessage(data)
	if err == nil {
		t.Fatal("DecodeMessage succeeded for malformed input")
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("DecodeMessage error = %q, want substring %q", err, want)
	}
}

// Malformed network frames must be rejected before any AVP is exposed to
// callers; accepting a bad boundary desynchronises the connection.
func TestDecodeMessageRejectsMalformedNetworkFrames(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "short header",
			data: []byte{types.DiameterVersion},
			want: "message too short",
		},
		{
			name: "unsupported version",
			data: func() []byte {
				data := diameterHeader(types.DiameterHeaderSize)
				data[0] = 2
				return data
			}(),
			want: "unsupported Diameter version",
		},
		{
			name: "advertised message length beyond buffer",
			data: diameterHeader(types.DiameterHeaderSize + 1),
			want: "exceeds data length",
		},
		{
			name: "trailing partial AVP header",
			data: appendToDiameterHeader([]byte{0, 0, 1}),
			want: "AVP too short",
		},
		{
			name: "AVP length smaller than fixed header",
			data: appendToDiameterHeader(avpBytes(0, types.AVPHeaderSize-1, nil)),
			want: "invalid AVP length",
		},
		{
			name: "vendor AVP omits vendor header bytes",
			data: appendToDiameterHeader(avpBytes(types.AVPFlagVendor, types.AVPHeaderSizeVendor, nil)),
			want: "vendor AVP too short",
		},
		{
			name: "vendor AVP length smaller than vendor header",
			data: appendToDiameterHeader(append(avpBytes(types.AVPFlagVendor, types.AVPHeaderSizeVendor-1, nil), 0, 0, 0, 1)),
			want: "invalid AVP length",
		},
		{
			name: "AVP claims data past message",
			data: appendToDiameterHeader(avpBytes(0, types.AVPHeaderSize+4, []byte{1, 2})),
			want: "AVP data exceeds buffer",
		},
		{
			name: "AVP padding points past message",
			data: appendToDiameterHeader(avpBytes(0, types.AVPHeaderSize+1, []byte{1})),
			want: "AVP padding exceeds buffer",
		},
		{
			name: "AVP padding is non-zero",
			data: appendToDiameterHeader(append(avpBytes(0, types.AVPHeaderSize+1, []byte{1}), 0, 7, 0)),
			want: "non-zero AVP padding",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requireDecodeMessageError(t, tc.data, tc.want)
		})
	}
}

// A peer controls the advertised length, so a value below the header size must
// be rejected rather than used to slice the buffer backwards. ReadMessage
// already guarded this; DecodeMessage is exported and did not.
func TestDecodeMessageRejectsAdvertisedLengthShorterThanHeader(t *testing.T) {
	for _, msgLen := range []int{0, 1, types.DiameterHeaderSize - 1} {
		if _, err := DecodeMessage(diameterHeader(msgLen)); err == nil {
			t.Fatalf("advertised length %d was accepted", msgLen)
		}
	}
}

func TestDecodeMessageHeaderRejectsShortBuffersAndExposesFlags(t *testing.T) {
	data := diameterHeader(types.DiameterHeaderSize)
	data[4] = types.CmdFlagRequest | types.CmdFlagProxiable
	data[6] = 1
	data[7] = 24

	h, err := DecodeMessageHeader(data)
	if err != nil {
		t.Fatalf("DecodeMessageHeader failed: %v", err)
	}
	if !h.IsRequest() {
		t.Fatal("request bit was not surfaced from the command header")
	}
	if h.MessageLength != uint32(types.DiameterHeaderSize) {
		t.Fatalf("message length = %d, want %d", h.MessageLength, types.DiameterHeaderSize)
	}

	if _, err := DecodeMessageHeader(data[:types.DiameterHeaderSize-1]); err == nil {
		t.Fatal("DecodeMessageHeader accepted a truncated header")
	}
}

func TestReadMessageRejectsMalformedStreams(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "short header",
			data: []byte{types.DiameterVersion},
			want: "reading message header",
		},
		{
			name: "unsupported version",
			data: func() []byte {
				data := diameterHeader(types.DiameterHeaderSize)
				data[0] = 9
				return data
			}(),
			want: "unsupported Diameter version",
		},
		{
			name: "invalid message length",
			data: diameterHeader(types.DiameterHeaderSize - 1),
			want: "invalid message length",
		},
		{
			name: "truncated body",
			data: func() []byte {
				data := diameterHeader(types.DiameterHeaderSize + 4)
				return append(data, 1, 2)
			}(),
			want: "reading message body",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadMessage(bytes.NewReader(tc.data))
			if err == nil {
				t.Fatal("ReadMessage succeeded for a malformed stream")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ReadMessage error = %q, want substring %q", err, tc.want)
			}
		})
	}
}

func TestReadMessageHandlesMessagesLargerThanThePoolBuffer(t *testing.T) {
	msg := NewRequest(types.CmdCodeDeviceWatchdog, types.AppIDCommon)
	msg.AddAVP(NewOctetStringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, bytes.Repeat([]byte{'a'}, 5000)))

	wire, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := ReadMessage(bytes.NewReader(wire))
	if err != nil {
		t.Fatalf("ReadMessage failed for a large but valid message: %v", err)
	}
	if got, ok := decoded.GetSessionID(); !ok || len(got) != 5000 {
		t.Fatalf("Session-Id length = %d (present=%v), want 5000", len(got), ok)
	}
}

type oneByteAtATimeReader struct {
	data []byte
}

func (r *oneByteAtATimeReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

func TestReadMessageUsesReadFullForFragmentedNetworkReads(t *testing.T) {
	msg := NewRequest(types.CmdCodeDeviceWatchdog, types.AppIDCommon)
	msg.AddAVP(NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "peer.example.com"))

	wire, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := ReadMessage(&oneByteAtATimeReader{data: wire})
	if err != nil {
		t.Fatalf("ReadMessage failed on fragmented input: %v", err)
	}
	if host, ok := decoded.GetOriginHost(); !ok || host != "peer.example.com" {
		t.Fatalf("Origin-Host = %q (present=%v), want peer.example.com", host, ok)
	}
}

func TestDecodeGroupedRejectsMalformedChildren(t *testing.T) {
	grouped := NewAVP(types.AVPCodeVendorSpecificAppID, types.AVPFlagMandatory, 0, avpBytes(0, 3, nil))

	err := grouped.DecodeGrouped()
	if err == nil {
		t.Fatal("DecodeGrouped accepted malformed child bytes")
	}
	if !strings.Contains(err.Error(), "decoding grouped AVP") {
		t.Fatalf("DecodeGrouped error = %q, want grouped context", err)
	}
}

func TestDecodeGroupedAllowsEmptyGroupedData(t *testing.T) {
	grouped := NewAVP(types.AVPCodeVendorSpecificAppID, types.AVPFlagMandatory, 0, nil)

	if err := grouped.DecodeGrouped(); err != nil {
		t.Fatalf("DecodeGrouped failed on an empty grouped value: %v", err)
	}
}

func TestDecodeAVPIgnoresBytesAfterOneCompleteAVP(t *testing.T) {
	encoded, err := NewOctetStringAVP(1, 0, []byte{1, 2, 3, 4}).Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	avp, err := DecodeAVP(append(encoded, 9, 9, 9, 9))
	if err != nil {
		t.Fatalf("DecodeAVP failed: %v", err)
	}
	if !bytes.Equal(avp.Data, []byte{1, 2, 3, 4}) {
		t.Fatalf("decoded data = %v, want the first AVP only", avp.Data)
	}
}

func TestReadMessagePropagatesBodyEOF(t *testing.T) {
	_, err := ReadMessage(bytes.NewReader(diameterHeader(4097)))
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadMessage error = %v, want an EOF-derived body error", err)
	}
}
