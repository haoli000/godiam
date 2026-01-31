// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package message provides Diameter message and AVP manipulation.
package message

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/haoli000/godiam/pkg/proto/types"
)

// Buffer pools for reducing allocations during encoding
var (
	// Pool for small buffers (typical AVPs, up to 256 bytes)
	smallBufPool = sync.Pool{
		New: func() interface{} {
			b := make([]byte, 256)
			return &b
		},
	}
	// Pool for medium buffers (typical messages, up to 4KB)
	mediumBufPool = sync.Pool{
		New: func() interface{} {
			b := make([]byte, 4096)
			return &b
		},
	}
)

// getBuffer returns a buffer of at least the requested size from the appropriate pool.
// The returned buffer should be released with putBuffer when done.
func getBuffer(size int) []byte {
	if size <= 256 {
		bp := smallBufPool.Get().(*[]byte)
		if cap(*bp) >= size {
			return (*bp)[:size]
		}
		// Buffer too small, allocate new one
		smallBufPool.Put(bp)
	} else if size <= 4096 {
		bp := mediumBufPool.Get().(*[]byte)
		if cap(*bp) >= size {
			return (*bp)[:size]
		}
		mediumBufPool.Put(bp)
	}
	// Fall back to allocation for large messages
	return make([]byte, size)
}

// putBuffer returns a buffer to the appropriate pool.
func putBuffer(buf []byte) {
	c := cap(buf)
	if c >= 256 && c <= 256 {
		b := buf[:c]
		smallBufPool.Put(&b)
	} else if c >= 4096 && c <= 4096 {
		b := buf[:c]
		mediumBufPool.Put(&b)
	}
	// Large buffers are not pooled
}

// Encode encodes the message to wire format.
func (m *Message) Encode() ([]byte, error) {
	// Calculate total size first to minimize allocations
	msgLen := m.EncodedLen()
	if msgLen > 0xFFFFFF {
		return nil, fmt.Errorf("message too large: %d bytes", msgLen)
	}

	// Allocate buffer for the entire message
	buf := make([]byte, msgLen)

	// Encode header
	// Byte 0: Version
	buf[0] = m.Version

	// Bytes 1-3: Message Length (24-bit)
	buf[1] = byte((msgLen >> 16) & 0xFF)
	buf[2] = byte((msgLen >> 8) & 0xFF)
	buf[3] = byte(msgLen & 0xFF)

	// Byte 4: Command Flags
	buf[4] = m.Flags

	// Bytes 5-7: Command-Code (24-bit)
	buf[5] = byte((m.CommandCode >> 16) & 0xFF)
	buf[6] = byte((m.CommandCode >> 8) & 0xFF)
	buf[7] = byte(m.CommandCode & 0xFF)

	// Bytes 8-11: Application-ID
	binary.BigEndian.PutUint32(buf[8:12], uint32(m.ApplicationID))

	// Bytes 12-15: Hop-by-Hop Identifier
	binary.BigEndian.PutUint32(buf[12:16], uint32(m.HopByHopID))

	// Bytes 16-19: End-to-End Identifier
	binary.BigEndian.PutUint32(buf[16:20], uint32(m.EndToEndID))

	// Encode AVPs directly into buffer
	offset := types.DiameterHeaderSize
	for _, avp := range m.AVPs {
		n, err := avp.EncodeTo(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("encoding AVP %d: %w", avp.Code, err)
		}
		offset += n
	}

	return buf, nil
}

// EncodeTo encodes the AVP directly into the provided buffer.
// Returns the number of bytes written.
func (a *AVP) EncodeTo(buf []byte) (int, error) {
	var data []byte

	// For grouped AVPs, encode children first
	if a.IsGrouped() {
		// Calculate children size
		childSize := 0
		for _, child := range a.children {
			childSize += child.EncodedLen()
		}
		// Encode children into temporary buffer
		childBuf := getBuffer(childSize)
		defer putBuffer(childBuf)

		offset := 0
		for _, child := range a.children {
			n, err := child.EncodeTo(childBuf[offset:])
			if err != nil {
				return 0, err
			}
			offset += n
		}
		data = childBuf[:offset]
	} else {
		data = a.Data
	}

	// Calculate header size and total length
	headerSize := types.AVPHeaderSize
	if a.IsVendor() {
		headerSize = types.AVPHeaderSizeVendor
	}

	dataLen := len(data)
	avpLen := headerSize + dataLen

	// Calculate padding (AVP data must be padded to 4-byte boundary)
	padding := (4 - (avpLen % 4)) % 4
	totalLen := avpLen + padding

	if len(buf) < totalLen {
		return 0, fmt.Errorf("buffer too small: need %d, have %d", totalLen, len(buf))
	}

	// Encode AVP header
	// Bytes 0-3: AVP Code
	binary.BigEndian.PutUint32(buf[0:4], uint32(a.Code))

	// Byte 4: AVP Flags
	buf[4] = a.Flags

	// Bytes 5-7: AVP Length (24-bit, does NOT include padding)
	buf[5] = byte((avpLen >> 16) & 0xFF)
	buf[6] = byte((avpLen >> 8) & 0xFF)
	buf[7] = byte(avpLen & 0xFF)

	offset := 8
	if a.IsVendor() {
		// Bytes 8-11: Vendor-ID
		binary.BigEndian.PutUint32(buf[8:12], uint32(a.VendorID))
		offset = 12
	}

	// Copy data
	copy(buf[offset:], data)

	// Zero padding bytes
	for i := 0; i < padding; i++ {
		buf[avpLen+i] = 0
	}

	return totalLen, nil
}

// Encode encodes the AVP to wire format.
// For better performance in hot paths, use EncodeTo with a pre-allocated buffer.
func (a *AVP) Encode() ([]byte, error) {
	totalLen := a.EncodedLen()
	buf := make([]byte, totalLen)
	_, err := a.EncodeTo(buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// EncodedLen returns the wire format length of the AVP including padding.
func (a *AVP) EncodedLen() int {
	dataLen := len(a.Data)
	if a.IsGrouped() {
		dataLen = 0
		for _, child := range a.children {
			dataLen += child.EncodedLen()
		}
	}

	headerSize := types.AVPHeaderSize
	if a.IsVendor() {
		headerSize = types.AVPHeaderSizeVendor
	}

	avpLen := headerSize + dataLen
	padding := (4 - (avpLen % 4)) % 4
	return avpLen + padding
}

// EncodedLen returns the wire format length of the message.
func (m *Message) EncodedLen() int {
	length := types.DiameterHeaderSize
	for _, avp := range m.AVPs {
		length += avp.EncodedLen()
	}
	return length
}
