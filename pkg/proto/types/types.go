// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package types defines core Diameter protocol types.
// This corresponds to the type definitions in libfdproto.h from the original freeDiameter.
package types

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// DiamID represents a Diameter Identity (FQDN in ASCII).
// Used for Origin-Host, Destination-Host, etc.
type DiamID string

// AVPCode represents an AVP Code (32-bit unsigned).
type AVPCode uint32

// VendorID represents a Vendor-ID (32-bit unsigned).
// 0 means IETF (no vendor).
type VendorID uint32

// ApplicationID represents a Diameter Application-ID (32-bit unsigned).
type ApplicationID uint32

// CommandCode represents a Diameter Command-Code (24-bit unsigned, stored in uint32).
type CommandCode uint32

// ResultCode represents a Diameter Result-Code (32-bit unsigned).
type ResultCode uint32

// HopByHopID represents the Hop-by-Hop Identifier (32-bit unsigned).
type HopByHopID uint32

// EndToEndID represents the End-to-End Identifier (32-bit unsigned).
type EndToEndID uint32

// SessionID represents a Diameter Session-ID (UTF8String).
type SessionID string

// Common Vendor IDs
const (
	VendorIDIETF     VendorID = 0
	VendorID3GPP     VendorID = 10415
	VendorIDETSI     VendorID = 13019
	VendorIDVodafone VendorID = 12645
)

// Common Application IDs (RFC 6733 and others)
const (
	AppIDCommon         ApplicationID = 0
	AppIDNASREQ         ApplicationID = 1
	AppIDMobileIPv4     ApplicationID = 2
	AppIDBaseAccounting ApplicationID = 3
	AppIDCreditControl  ApplicationID = 4
	AppIDEAP            ApplicationID = 5
	AppIDSIP            ApplicationID = 6
	AppIDMobileIPv6I    ApplicationID = 7
	AppIDMobileIPv6A    ApplicationID = 8
	AppIDQoS            ApplicationID = 9
	AppIDRelay          ApplicationID = 0xffffffff

	// 3GPP Application IDs
	AppID3GPPCx  ApplicationID = 16777216
	AppID3GPPSh  ApplicationID = 16777217
	AppID3GPPRx  ApplicationID = 16777236
	AppID3GPPGx  ApplicationID = 16777238
	AppID3GPPS6a ApplicationID = 16777251
	AppID3GPPSWx ApplicationID = 16777265
	AppID3GPPS6b ApplicationID = 16777272
)

// AVPType represents the data type of an AVP.
type AVPType int

const (
	// AVPTypeOctetString is an arbitrary data of variable length (RFC 6733 Section 4.2).
	AVPTypeOctetString AVPType = iota
	// AVPTypeInteger32 is a 32-bit signed integer (RFC 6733 Section 4.2).
	AVPTypeInteger32
	// AVPTypeInteger64 is a 64-bit signed integer (RFC 6733 Section 4.2).
	AVPTypeInteger64
	// AVPTypeUnsigned32 is a 32-bit unsigned integer (RFC 6733 Section 4.2).
	AVPTypeUnsigned32
	// AVPTypeUnsigned64 is a 64-bit unsigned integer (RFC 6733 Section 4.2).
	AVPTypeUnsigned64
	// AVPTypeFloat32 is a 32-bit IEEE 754 floating point (RFC 6733 Section 4.2).
	AVPTypeFloat32
	// AVPTypeFloat64 is a 64-bit IEEE 754 floating point (RFC 6733 Section 4.2).
	AVPTypeFloat64

	// AVPTypeAddress is an OctetString with address format (RFC 6733 Section 4.3).
	AVPTypeAddress
	// AVPTypeTime is an Unsigned32 seconds since 1900 (RFC 6733 Section 4.3).
	AVPTypeTime
	// AVPTypeUTF8String is an OctetString with UTF-8 encoding (RFC 6733 Section 4.3).
	AVPTypeUTF8String
	// AVPTypeDiameterIdentity is an OctetString with DiamIdent format (RFC 6733 Section 4.3).
	AVPTypeDiameterIdentity
	// AVPTypeDiameterURI is an OctetString with DiameterURI format (RFC 6733 Section 4.3).
	AVPTypeDiameterURI
	// AVPTypeEnumerated is an Integer32 with predefined values (RFC 6733 Section 4.3).
	AVPTypeEnumerated
	// AVPTypeIPFilterRule is an OctetString with IPFilterRule format (RFC 6733 Section 4.3).
	AVPTypeIPFilterRule
	// AVPTypeGrouped contains other AVPs (RFC 6733 Section 4.3).
	AVPTypeGrouped
)

// String returns the string representation of an AVP type.
func (t AVPType) String() string {
	names := []string{
		"OctetString",
		"Integer32",
		"Integer64",
		"Unsigned32",
		"Unsigned64",
		"Float32",
		"Float64",
		"Address",
		"Time",
		"UTF8String",
		"DiameterIdentity",
		"DiameterURI",
		"Enumerated",
		"IPFilterRule",
		"Grouped",
	}
	if int(t) < len(names) {
		return names[t]
	}
	return fmt.Sprintf("Unknown(%d)", t)
}

// AVP Flags (RFC 6733 Section 4.1)
const (
	AVPFlagVendor    uint8 = 0x80 // V bit - Vendor-specific
	AVPFlagMandatory uint8 = 0x40 // M bit - Mandatory
	AVPFlagProtected uint8 = 0x20 // P bit - Protected (requires encryption)
)

// Command Flags (RFC 6733 Section 3)
const (
	CmdFlagRequest    uint8 = 0x80 // R bit - Request
	CmdFlagProxiable  uint8 = 0x40 // P bit - Proxiable
	CmdFlagError      uint8 = 0x20 // E bit - Error
	CmdFlagRetransmit uint8 = 0x10 // T bit - Retransmission
)

// Diameter Header constants
const (
	DiameterVersion     uint8 = 1
	DiameterHeaderSize  int   = 20 // 20 bytes for the message header
	AVPHeaderSize       int   = 8  // 8 bytes for AVP header without vendor
	AVPHeaderSizeVendor int   = 12 // 12 bytes for AVP header with vendor
)

// Common Result Codes (RFC 6733 Section 7.1)
const (
	// Informational (1xxx)
	ResultMultiRoundAuth ResultCode = 1001

	// Success (2xxx)
	ResultSuccess        ResultCode = 2001
	ResultLimitedSuccess ResultCode = 2002

	// Protocol Errors (3xxx)
	ResultCommandUnsupported     ResultCode = 3001
	ResultUnableToDeliver        ResultCode = 3002
	ResultRealmNotServed         ResultCode = 3003
	ResultTooBusy                ResultCode = 3004
	ResultLoopDetected           ResultCode = 3005
	ResultRedirectIndication     ResultCode = 3006
	ResultApplicationUnsupported ResultCode = 3007
	ResultInvalidHDRBits         ResultCode = 3008
	ResultInvalidAVPBits         ResultCode = 3009
	ResultUnknownPeer            ResultCode = 3010

	// Transient Failures (4xxx)
	ResultAuthenticationRejected ResultCode = 4001
	ResultOutOfSpace             ResultCode = 4002
	ResultElectionLost           ResultCode = 4003

	// Permanent Failures (5xxx)
	ResultAVPUnsupported        ResultCode = 5001
	ResultUnknownSessionID      ResultCode = 5002
	ResultAuthorizationRejected ResultCode = 5003
	ResultInvalidAVPValue       ResultCode = 5004
	ResultMissingAVP            ResultCode = 5005
	ResultResourcesExceeded     ResultCode = 5006
	ResultContradictingAVPs     ResultCode = 5007
	ResultAVPNotAllowed         ResultCode = 5008
	ResultAVPOccursTooManyTimes ResultCode = 5009
	ResultNoCommonApplication   ResultCode = 5010
	ResultUnsupportedVersion    ResultCode = 5011
	ResultUnableToComply        ResultCode = 5012
	ResultInvalidBitInHeader    ResultCode = 5013
	ResultInvalidAVPLength      ResultCode = 5014
	ResultInvalidMessageLength  ResultCode = 5015
	ResultInvalidAVPBitCombo    ResultCode = 5016
	// 3GPP specific result codes
	ResultUnknownUser ResultCode = 5001 // DIAMETER_ERROR_USER_UNKNOWN (TS 29.229)
)

// Address represents a Diameter Address AVP value.
// Supports IPv4 and IPv6 addresses.
type Address struct {
	Type uint16 // Address family (1=IPv4, 2=IPv6)
	Data []byte // Raw address bytes
}

// AddressTypeIPv4 is the address family for IPv4.
const AddressTypeIPv4 uint16 = 1

// AddressTypeIPv6 is the address family for IPv6.
const AddressTypeIPv6 uint16 = 2

// NewAddressFromIP creates an Address from a net.IP.
func NewAddressFromIP(ip net.IP) Address {
	if ip4 := ip.To4(); ip4 != nil {
		return Address{Type: AddressTypeIPv4, Data: ip4}
	}
	return Address{Type: AddressTypeIPv6, Data: ip.To16()}
}

// ToIP converts an Address to net.IP.
func (a Address) ToIP() net.IP {
	return net.IP(a.Data)
}

// Encode encodes the Address to wire format.
func (a Address) Encode() []byte {
	buf := make([]byte, 2+len(a.Data))
	binary.BigEndian.PutUint16(buf[0:2], a.Type)
	copy(buf[2:], a.Data)
	return buf
}

// DecodeAddress decodes an Address from wire format.
func DecodeAddress(data []byte) (Address, error) {
	if len(data) < 2 {
		return Address{}, fmt.Errorf("address too short: %d bytes", len(data))
	}
	addrType := binary.BigEndian.Uint16(data[0:2]) //nolint:gosec // G115: value range is protocol-constrained
	return Address{Type: addrType, Data: data[2:]}, nil
}

// DiameterTime represents a Diameter Time value.
// Diameter time is seconds since 00:00:00 UTC, January 1, 1900.
type DiameterTime uint32

// NTP epoch offset: seconds between 1900-01-01 and 1970-01-01
const ntpEpochOffset = 2208988800

// NewDiameterTime creates a DiameterTime from a Go time.Time.
func NewDiameterTime(t time.Time) DiameterTime {
	return DiameterTime(t.Unix() + ntpEpochOffset) //nolint:gosec // G115: value range is protocol-constrained
}

// ToTime converts a DiameterTime to Go time.Time.
func (dt DiameterTime) ToTime() time.Time {
	return time.Unix(int64(dt)-ntpEpochOffset, 0)
}

// DiameterTimeNow returns the current time as DiameterTime.
func DiameterTimeNow() DiameterTime {
	return NewDiameterTime(time.Now())
}

// DisconnectCause values (RFC 6733 Section 5.4.3)
type DisconnectCause uint32

const (
	// DisconnectRebooting indicates the peer is rebooting.
	DisconnectRebooting DisconnectCause = 0
	// DisconnectBusy indicates the peer is too busy.
	DisconnectBusy DisconnectCause = 1
	// DisconnectDoNotWantToTalk indicates the peer does not wish to communicate.
	DisconnectDoNotWantToTalk DisconnectCause = 2
)

// AuthSessionState values (RFC 6733 Section 8.11)
type AuthSessionState uint32

const (
	// AuthStateIdle indicates no active auth session.
	AuthStateIdle AuthSessionState = 0
	// AuthStatePendingInitial indicates an initial auth request is pending.
	AuthStatePendingInitial AuthSessionState = 1
	// AuthStatePendingUpdates indicates auth update requests are pending.
	AuthStatePendingUpdates AuthSessionState = 2
	// AuthStateIdleDisconnect indicates idle with pending disconnect.
	AuthStateIdleDisconnect AuthSessionState = 3
	// AuthStateNoStateMaintained indicates no state is maintained (special value).
	AuthStateNoStateMaintained AuthSessionState = 1
)

// ReAuthRequestType values (RFC 6733 Section 8.12)
type ReAuthRequestType uint32

const (
	// ReAuthAuthorizeOnly indicates authorize-only re-auth.
	ReAuthAuthorizeOnly ReAuthRequestType = 0
	// ReAuthAuthorizeAuthenticate indicates authorize-and-authenticate re-auth.
	ReAuthAuthorizeAuthenticate ReAuthRequestType = 1
)

// TerminationCause values (RFC 6733 Section 8.15)
type TerminationCause uint32

const (
	// TerminationLogout indicates a user-initiated logout.
	TerminationLogout TerminationCause = 1
	// TerminationServiceNotProvided indicates the service was not provided.
	TerminationServiceNotProvided TerminationCause = 2
	// TerminationBadAnswer indicates a bad answer was received.
	TerminationBadAnswer TerminationCause = 3
	// TerminationAdministrative indicates an administrative termination.
	TerminationAdministrative TerminationCause = 4
	// TerminationLinkBroken indicates the transport link was broken.
	TerminationLinkBroken TerminationCause = 5
	// TerminationAuthExpired indicates the authorization has expired.
	TerminationAuthExpired TerminationCause = 6
	// TerminationSessionTimeout indicates the session timed out.
	TerminationSessionTimeout TerminationCause = 8
	// TerminationUserRequest indicates a user-requested termination.
	TerminationUserRequest TerminationCause = 7
)

// AccountingRecordType represents the Accounting-Record-Type values (RFC 6733 Section 9.8.1).
type AccountingRecordType uint32

const (
	// AccountingEventRecord indicates a one-time accounting event.
	AccountingEventRecord AccountingRecordType = 1
	// AccountingStartRecord indicates the start of an accounting session.
	AccountingStartRecord AccountingRecordType = 2
	// AccountingInterimRecord indicates an interim accounting update.
	AccountingInterimRecord AccountingRecordType = 3
	// AccountingStopRecord indicates the end of an accounting session.
	AccountingStopRecord AccountingRecordType = 4
)
