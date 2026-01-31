// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package message provides Diameter message and AVP manipulation.
package message

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/haoli000/godiam/pkg/proto/types"
)

// Buffer pool for read operations (decoding)
var readBufPool = sync.Pool{
	New: func() interface{} {
		// 4KB is typical max message size
		b := make([]byte, 4096)
		return &b
	},
}

// DecodeMessage decodes a Diameter message from wire format.
func DecodeMessage(data []byte) (*Message, error) {
	if len(data) < types.DiameterHeaderSize {
		return nil, fmt.Errorf("message too short: %d bytes (need at least %d)", len(data), types.DiameterHeaderSize)
	}

	m := &Message{}

	// Byte 0: Version
	m.Version = data[0]
	if m.Version != types.DiameterVersion {
		return nil, fmt.Errorf("unsupported Diameter version: %d", m.Version)
	}

	// Bytes 1-3: Message Length (24-bit)
	msgLen := int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	if msgLen > len(data) {
		return nil, fmt.Errorf("message length %d exceeds data length %d", msgLen, len(data))
	}

	// Byte 4: Command Flags
	m.Flags = data[4]

	// Bytes 5-7: Command-Code (24-bit)
	m.CommandCode = types.CommandCode(int(data[5])<<16 | int(data[6])<<8 | int(data[7])) //nolint:gosec // G115: value range is protocol-constrained

	// Bytes 8-11: Application-ID
	m.ApplicationID = types.ApplicationID(binary.BigEndian.Uint32(data[8:12]))

	// Bytes 12-15: Hop-by-Hop Identifier
	m.HopByHopID = types.HopByHopID(binary.BigEndian.Uint32(data[12:16]))

	// Bytes 16-19: End-to-End Identifier
	m.EndToEndID = types.EndToEndID(binary.BigEndian.Uint32(data[16:20]))

	// Decode AVPs
	avpData := data[types.DiameterHeaderSize:msgLen]
	avps, err := decodeAVPs(avpData)
	if err != nil {
		return nil, fmt.Errorf("decoding AVPs: %w", err)
	}
	m.AVPs = avps

	return m, nil
}

// ReadMessage reads and decodes a Diameter message from an io.Reader.
func ReadMessage(r io.Reader) (*Message, error) {
	// Get buffer from pool for header read
	bufPtr := readBufPool.Get().(*[]byte)
	buf := *bufPtr

	// Read the header first (20 bytes)
	if _, err := io.ReadFull(r, buf[:types.DiameterHeaderSize]); err != nil {
		readBufPool.Put(bufPtr)
		return nil, fmt.Errorf("reading message header: %w", err)
	}

	// Check version
	if buf[0] != types.DiameterVersion {
		readBufPool.Put(bufPtr)
		return nil, fmt.Errorf("unsupported Diameter version: %d", buf[0])
	}

	// Get message length
	msgLen := int(buf[1])<<16 | int(buf[2])<<8 | int(buf[3])
	if msgLen < types.DiameterHeaderSize {
		readBufPool.Put(bufPtr)
		return nil, fmt.Errorf("invalid message length: %d", msgLen)
	}

	// If message is larger than pooled buffer, allocate new one
	var data []byte
	if msgLen <= cap(buf) {
		data = buf[:msgLen]
	} else {
		// Large message - allocate and copy header
		readBufPool.Put(bufPtr)
		bufPtr = nil
		data = make([]byte, msgLen)
		copy(data[:types.DiameterHeaderSize], buf[:types.DiameterHeaderSize])
	}

	// Read the rest of the message
	remaining := msgLen - types.DiameterHeaderSize
	if remaining > 0 {
		if _, err := io.ReadFull(r, data[types.DiameterHeaderSize:]); err != nil {
			if bufPtr != nil {
				readBufPool.Put(bufPtr)
			}
			return nil, fmt.Errorf("reading message body: %w", err)
		}
	}

	// Decode the message (this copies the data internally)
	msg, err := DecodeMessage(data)

	// Return buffer to pool
	if bufPtr != nil {
		readBufPool.Put(bufPtr)
	}

	return msg, err
}

// decodeAVPs decodes a sequence of AVPs from wire format.
func decodeAVPs(data []byte) ([]*AVP, error) {
	// Pre-allocate with estimated capacity (typical message has 5-15 AVPs)
	// Minimum AVP size is 8 bytes (header), use that to estimate max count
	estimatedCount := len(data) / 16 // Conservative estimate
	if estimatedCount < 8 {
		estimatedCount = 8
	}
	if estimatedCount > 32 {
		estimatedCount = 32
	}
	avps := make([]*AVP, 0, estimatedCount)
	offset := 0

	for offset < len(data) {
		avp, consumed, err := decodeAVP(data[offset:])
		if err != nil {
			return nil, fmt.Errorf("at offset %d: %w", offset, err)
		}
		avps = append(avps, avp)
		offset += consumed
	}

	return avps, nil
}

// decodeAVP decodes a single AVP from wire format.
// Returns the AVP and the number of bytes consumed (including padding).
func decodeAVP(data []byte) (*AVP, int, error) {
	if len(data) < types.AVPHeaderSize {
		return nil, 0, fmt.Errorf("AVP too short: %d bytes", len(data))
	}

	avp := &AVP{}

	// Bytes 0-3: AVP Code
	avp.Code = types.AVPCode(binary.BigEndian.Uint32(data[0:4]))

	// Byte 4: AVP Flags
	avp.Flags = data[4]

	// Bytes 5-7: AVP Length (24-bit, does NOT include padding)
	avpLen := int(data[5])<<16 | int(data[6])<<8 | int(data[7])

	headerSize := types.AVPHeaderSize
	if avp.IsVendor() {
		headerSize = types.AVPHeaderSizeVendor
		if len(data) < headerSize {
			return nil, 0, fmt.Errorf("vendor AVP too short: %d bytes", len(data))
		}
		// Bytes 8-11: Vendor-ID
		avp.VendorID = types.VendorID(binary.BigEndian.Uint32(data[8:12]))
	}

	if avpLen < headerSize {
		return nil, 0, fmt.Errorf("invalid AVP length: %d (header is %d)", avpLen, headerSize)
	}

	dataLen := avpLen - headerSize
	if headerSize+dataLen > len(data) {
		return nil, 0, fmt.Errorf("AVP data exceeds buffer: need %d, have %d", headerSize+dataLen, len(data))
	}

	// Extract data (excluding padding)
	avp.Data = make([]byte, dataLen)
	copy(avp.Data, data[headerSize:avpLen])

	// Calculate padded length (AVP data must be padded to 4-byte boundary)
	paddedLen := avpLen
	if avpLen%4 != 0 {
		paddedLen = avpLen + (4 - (avpLen % 4))
	}
	// Don't read past the buffer
	if paddedLen > len(data) {
		paddedLen = len(data)
	}

	return avp, paddedLen, nil
}

// DecodeAVP decodes a single AVP from wire format.
func DecodeAVP(data []byte) (*AVP, error) {
	avp, _, err := decodeAVP(data)
	return avp, err
}

// DecodeGrouped decodes the data of a grouped AVP into child AVPs.
func (a *AVP) DecodeGrouped() error {
	if len(a.Data) == 0 {
		return nil
	}
	children, err := decodeAVPs(a.Data)
	if err != nil {
		return fmt.Errorf("decoding grouped AVP %d: %w", a.Code, err)
	}
	a.children = children
	return nil
}

// Header is a minimal structure for peeking at message headers.
type Header struct {
	Version       uint8
	MessageLength uint32
	Flags         uint8
	CommandCode   types.CommandCode
	ApplicationID types.ApplicationID
	HopByHopID    types.HopByHopID
	EndToEndID    types.EndToEndID
}

// DecodeMessageHeader decodes just the message header.
func DecodeMessageHeader(data []byte) (*Header, error) {
	if len(data) < types.DiameterHeaderSize {
		return nil, fmt.Errorf("data too short for header: %d bytes", len(data))
	}

	h := &Header{
		Version:       data[0],
		MessageLength: uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3]),
		Flags:         data[4],
		CommandCode:   types.CommandCode(int(data[5])<<16 | int(data[6])<<8 | int(data[7])), //nolint:gosec // G115: value range is protocol-constrained
		ApplicationID: types.ApplicationID(binary.BigEndian.Uint32(data[8:12])),
		HopByHopID:    types.HopByHopID(binary.BigEndian.Uint32(data[12:16])),
		EndToEndID:    types.EndToEndID(binary.BigEndian.Uint32(data[16:20])),
	}

	return h, nil
}

// IsRequest returns true if the R (request) flag is set.
func (h *Header) IsRequest() bool {
	return h.Flags&types.CmdFlagRequest != 0
}
