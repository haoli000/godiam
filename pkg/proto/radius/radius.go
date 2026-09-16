// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package radius provides RADIUS protocol encoding, decoding, and message handling.
package radius

import (
	"crypto/md5" //nolint:gosec // G501: MD5 required by RADIUS protocol
	"encoding/binary"
	"fmt"
)

// RADIUS Packet Codes
const (
	CodeAccessRequest      uint8 = 1
	CodeAccessAccept       uint8 = 2
	CodeAccessReject       uint8 = 3
	CodeAccountingRequest  uint8 = 4
	CodeAccountingResponse uint8 = 5
	CodeAccessChallenge    uint8 = 11
	CodeStatusServer       uint8 = 12
	CodeStatusClient       uint8 = 13
)

// RADIUS Attribute Types
const (
	TypeUserName           uint8 = 1
	TypeUserPassword       uint8 = 2
	TypeCHAPPassword       uint8 = 3
	TypeNASIPAddress       uint8 = 4
	TypeNASPort            uint8 = 5
	TypeServiceType        uint8 = 6
	TypeFramedProtocol     uint8 = 7
	TypeFramedIPAddress    uint8 = 8
	TypeFramedIPNetmask    uint8 = 9
	TypeReplyMessage       uint8 = 18
	TypeState              uint8 = 24
	TypeClass              uint8 = 25
	TypeNASIdentifier      uint8 = 32
	TypeProxyState         uint8 = 33
	TypeAcctStatusType     uint8 = 40
	TypeAcctSessionID      uint8 = 44
	TypeAcctInputOctets    uint8 = 42
	TypeAcctOutputOctets   uint8 = 43
	TypeAcctSessionTime    uint8 = 46
	TypeAcctTerminateCause uint8 = 49
)

// Packet represents a RADIUS packet.
type Packet struct {
	Code          uint8
	Identifier    uint8
	Authenticator [16]byte
	Attributes    []Attribute
	Secret        string // Shared secret for HMAC/Hashing
}

// Attribute represents a RADIUS attribute.
type Attribute struct {
	Type  uint8
	Value []byte
}

// Parse parses a RADIUS packet from a byte slice.
func Parse(data []byte, secret string) (*Packet, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}

	p := &Packet{
		Code:       data[0],
		Identifier: data[1],
		Secret:     secret,
	}
	length := binary.BigEndian.Uint16(data[2:4])
	if int(length) < 20 {
		// RFC 2865 3: Length covers the 20-byte header, so anything shorter is
		// malformed. Without this the attribute slice below runs backwards and
		// panics on input an attacker controls.
		return nil, fmt.Errorf("packet length %d is shorter than the 20-byte header", length)
	}
	if int(length) > len(data) {
		return nil, fmt.Errorf("packet length mismatch: header says %d, got %d", length, len(data))
	}

	copy(p.Authenticator[:], data[4:20])

	attrData := data[20:length]
	for len(attrData) > 0 {
		// RFC 2865 5 requires a packet with a malformed attribute to be
		// discarded. Breaking out instead left the packet valid but carrying a
		// truncated attribute list, so a sender could craft one packet that
		// this gateway and the RADIUS server behind it read differently.
		if len(attrData) < 2 {
			return nil, fmt.Errorf("trailing %d byte(s) are too short for an attribute", len(attrData))
		}
		aType := attrData[0]
		aLen := attrData[1]
		if int(aLen) < 2 || int(aLen) > len(attrData) {
			return nil, fmt.Errorf("attribute %d has invalid length %d", aType, aLen)
		}
		p.Attributes = append(p.Attributes, Attribute{
			Type:  aType,
			Value: attrData[2:aLen],
		})
		attrData = attrData[aLen:]
	}

	return p, nil
}

// Serialize serializes a RADIUS packet to a byte slice.
func (p *Packet) Serialize() ([]byte, error) {
	var attrData []byte
	for _, a := range p.Attributes {
		attrData = append(attrData, a.Type, uint8(len(a.Value)+2)) //nolint:gosec // G115: value range is protocol-constrained
		attrData = append(attrData, a.Value...)
	}

	length := 20 + len(attrData)
	data := make([]byte, length)
	data[0] = p.Code
	data[1] = p.Identifier
	binary.BigEndian.PutUint16(data[2:4], uint16(length)) //nolint:gosec // G115: value range is protocol-constrained
	copy(data[4:20], p.Authenticator[:])
	copy(data[20:], attrData)

	// If it's a response, we need to calculate the authenticator
	if p.Code == CodeAccessAccept || p.Code == CodeAccessReject || p.Code == CodeAccountingResponse {
		// Response Authenticator = MD5(Code + ID + Length + RequestAuthenticator + Attributes + Secret)
		hash := md5.New()              //nolint:gosec // G401: MD5 required by RADIUS protocol
		hash.Write(data[0:4])          // Code, ID, Length
		hash.Write(p.Authenticator[:]) // Request Authenticator
		hash.Write(data[20:])          // Attributes
		hash.Write([]byte(p.Secret))
		copy(data[4:20], hash.Sum(nil))
	}

	return data, nil
}

// GetAttribute returns the value of the first attribute of the given type.
func (p *Packet) GetAttribute(aType uint8) ([]byte, bool) {
	for _, a := range p.Attributes {
		if a.Type == aType {
			return a.Value, true
		}
	}
	return nil, false
}

// AddAttribute adds an attribute to the packet.
func (p *Packet) AddAttribute(aType uint8, value []byte) {
	p.Attributes = append(p.Attributes, Attribute{
		Type:  aType,
		Value: value,
	})
}
