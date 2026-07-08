// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package message provides Diameter message and AVP manipulation.
// This corresponds to libfdproto/messages.c from the original freeDiameter.
package message

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/haoli000/godiam/pkg/proto/types"
)

// AVP represents a Diameter AVP instance with its data.
type AVP struct {
	Code     types.AVPCode
	Flags    uint8
	VendorID types.VendorID
	Data     []byte
	// For Grouped AVPs, child AVPs
	children []*AVP
}

// Message represents a Diameter message instance.
type Message struct {
	Version       uint8
	Flags         uint8
	CommandCode   types.CommandCode
	ApplicationID types.ApplicationID
	HopByHopID    types.HopByHopID
	EndToEndID    types.EndToEndID
	AVPs          []*AVP
}

// AVP creation functions

// NewAVP creates a new AVP with the given code, flags, and data.
func NewAVP(code types.AVPCode, flags uint8, vendorID types.VendorID, data []byte) *AVP {
	return &AVP{
		Code:     code,
		Flags:    flags,
		VendorID: vendorID,
		Data:     data,
	}
}

// NewGroupedAVP creates a new Grouped AVP with child AVPs.
func NewGroupedAVP(code types.AVPCode, flags uint8, vendorID types.VendorID, children ...*AVP) *AVP {
	return &AVP{
		Code:     code,
		Flags:    flags,
		VendorID: vendorID,
		children: children,
	}
}

// Helper functions for creating typed AVPs

// NewOctetStringAVP creates an AVP with OctetString data.
func NewOctetStringAVP(code types.AVPCode, flags uint8, data []byte) *AVP {
	return NewAVP(code, flags, 0, data)
}

// NewUTF8StringAVP creates an AVP with UTF8String data.
func NewUTF8StringAVP(code types.AVPCode, flags uint8, value string) *AVP {
	return NewAVP(code, flags, 0, []byte(value))
}

// NewInteger32AVP creates an AVP with Integer32 data.
func NewInteger32AVP(code types.AVPCode, flags uint8, value int32) *AVP {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(value)) //nolint:gosec // G115: value range is protocol-constrained
	return NewAVP(code, flags, 0, data)
}

// NewInteger64AVP creates an AVP with Integer64 data.
func NewInteger64AVP(code types.AVPCode, flags uint8, value int64) *AVP {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, uint64(value)) //nolint:gosec // G115: value range is protocol-constrained
	return NewAVP(code, flags, 0, data)
}

// NewUnsigned32AVP creates an AVP with Unsigned32 data.
func NewUnsigned32AVP(code types.AVPCode, flags uint8, value uint32) *AVP {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, value)
	return NewAVP(code, flags, 0, data)
}

// NewUnsigned64AVP creates an AVP with Unsigned64 data.
func NewUnsigned64AVP(code types.AVPCode, flags uint8, value uint64) *AVP {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, value)
	return NewAVP(code, flags, 0, data)
}

// NewEnumeratedAVP creates an AVP with Enumerated data (int32).
func NewEnumeratedAVP(code types.AVPCode, flags uint8, value int32) *AVP {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(value)) //nolint:gosec // G115: value range is protocol-constrained
	return NewAVP(code, flags, 0, data)
}

// NewFloat32AVP creates an AVP with Float32 data.
func NewFloat32AVP(code types.AVPCode, flags uint8, value float32) *AVP {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, math.Float32bits(value))
	return NewAVP(code, flags, 0, data)
}

// NewFloat64AVP creates an AVP with Float64 data.
func NewFloat64AVP(code types.AVPCode, flags uint8, value float64) *AVP {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, math.Float64bits(value))
	return NewAVP(code, flags, 0, data)
}

// NewAddressAVP creates an AVP with Address data.
func NewAddressAVP(code types.AVPCode, flags uint8, addr types.Address) *AVP {
	return NewAVP(code, flags, 0, addr.Encode())
}

// NewTimeAVP creates an AVP with Time data.
func NewTimeAVP(code types.AVPCode, flags uint8, t time.Time) *AVP {
	dt := types.NewDiameterTime(t)
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(dt))
	return NewAVP(code, flags, 0, data)
}

// NewDiameterIdentityAVP creates an AVP with DiameterIdentity data.
func NewDiameterIdentityAVP(code types.AVPCode, flags uint8, identity types.DiamID) *AVP {
	return NewAVP(code, flags, 0, []byte(identity))
}

// NewVendorAVP creates an AVP with vendor ID set.
func NewVendorAVP(code types.AVPCode, flags uint8, vendorID types.VendorID, data []byte) *AVP {
	return NewAVP(code, flags|types.AVPFlagVendor, vendorID, data)
}

// AVP data extraction functions

// IsVendor returns true if the V (vendor) flag is set.
func (a *AVP) IsVendor() bool {
	return a.Flags&types.AVPFlagVendor != 0
}

// IsMandatory returns true if the M (mandatory) flag is set.
func (a *AVP) IsMandatory() bool {
	return a.Flags&types.AVPFlagMandatory != 0
}

// IsProtected returns true if the P (protected) flag is set.
func (a *AVP) IsProtected() bool {
	return a.Flags&types.AVPFlagProtected != 0
}

// IsGrouped returns true if this AVP contains child AVPs.
func (a *AVP) IsGrouped() bool {
	return len(a.children) > 0
}

// SetVendorID sets the Vendor-ID of the AVP and updates the V flag.
func (a *AVP) SetVendorID(vendorID types.VendorID) *AVP {
	a.VendorID = vendorID
	a.Flags |= types.AVPFlagVendor
	return a
}

// Children returns the child AVPs for a Grouped AVP.
func (a *AVP) Children() []*AVP {
	return a.children
}

// GetOctetString returns the AVP data as a byte slice.
func (a *AVP) GetOctetString() []byte {
	return a.Data
}

// GetUTF8String returns the AVP data as a string.
func (a *AVP) GetUTF8String() string {
	return string(a.Data)
}

// GetInteger32 returns the AVP data as int32.
func (a *AVP) GetInteger32() (int32, error) {
	if len(a.Data) < 4 {
		return 0, fmt.Errorf("data too short for Integer32: %d bytes", len(a.Data))
	}
	return int32(binary.BigEndian.Uint32(a.Data)), nil //nolint:gosec // G115: value range is protocol-constrained
}

// GetInteger64 returns the AVP data as int64.
func (a *AVP) GetInteger64() (int64, error) {
	if len(a.Data) < 8 {
		return 0, fmt.Errorf("data too short for Integer64: %d bytes", len(a.Data))
	}
	return int64(binary.BigEndian.Uint64(a.Data)), nil //nolint:gosec // G115: value range is protocol-constrained
}

// GetUnsigned32 returns the AVP data as uint32.
func (a *AVP) GetUnsigned32() (uint32, error) {
	if len(a.Data) < 4 {
		return 0, fmt.Errorf("data too short for Unsigned32: %d bytes", len(a.Data))
	}
	return binary.BigEndian.Uint32(a.Data), nil
}

// GetUnsigned64 returns the AVP data as uint64.
func (a *AVP) GetUnsigned64() (uint64, error) {
	if len(a.Data) < 8 {
		return 0, fmt.Errorf("data too short for Unsigned64: %d bytes", len(a.Data))
	}
	return binary.BigEndian.Uint64(a.Data), nil
}

// GetFloat32 returns the AVP data as float32.
func (a *AVP) GetFloat32() (float32, error) {
	if len(a.Data) < 4 {
		return 0, fmt.Errorf("data too short for Float32: %d bytes", len(a.Data))
	}
	return math.Float32frombits(binary.BigEndian.Uint32(a.Data)), nil
}

// GetFloat64 returns the AVP data as float64.
func (a *AVP) GetFloat64() (float64, error) {
	if len(a.Data) < 8 {
		return 0, fmt.Errorf("data too short for Float64: %d bytes", len(a.Data))
	}
	return math.Float64frombits(binary.BigEndian.Uint64(a.Data)), nil
}

// GetAddress returns the AVP data as an Address.
func (a *AVP) GetAddress() (types.Address, error) {
	return types.DecodeAddress(a.Data)
}

// GetTime returns the AVP data as time.Time.
func (a *AVP) GetTime() (time.Time, error) {
	if len(a.Data) < 4 {
		return time.Time{}, fmt.Errorf("data too short for Time: %d bytes", len(a.Data))
	}
	dt := types.DiameterTime(binary.BigEndian.Uint32(a.Data))
	return dt.ToTime(), nil
}

// GetDiameterIdentity returns the AVP data as DiamID.
func (a *AVP) GetDiameterIdentity() types.DiamID {
	return types.DiamID(a.Data)
}

// Message creation functions

// NewMessage creates a new Diameter message.
func NewMessage(flags uint8, cmdCode types.CommandCode, appID types.ApplicationID) *Message {
	return &Message{
		Version:       types.DiameterVersion,
		Flags:         flags,
		CommandCode:   cmdCode,
		ApplicationID: appID,
	}
}

// NewRequest creates a new Diameter request message.
func NewRequest(cmdCode types.CommandCode, appID types.ApplicationID) *Message {
	return NewMessage(types.CmdFlagRequest, cmdCode, appID)
}

// NewAnswer creates a new Diameter answer message from a request.
func NewAnswer(req *Message) *Message {
	return &Message{
		Version:       types.DiameterVersion,
		Flags:         req.Flags &^ types.CmdFlagRequest, // Clear R bit
		CommandCode:   req.CommandCode,
		ApplicationID: req.ApplicationID,
		HopByHopID:    req.HopByHopID,
		EndToEndID:    req.EndToEndID,
	}
}

// NewAnswerWithResult creates a Diameter answer for req and populates the
// Result-Code AVP. Also sets the E (error) flag when the result is not 2xxx
// (success), matching RFC 6733 §7.1. Handlers must still add Origin-Host and
// Origin-Realm before sending.
func NewAnswerWithResult(req *Message, code types.ResultCode) *Message {
	ans := NewAnswer(req)
	ans.SetResultCode(code)
	if code < 2000 || code >= 3000 {
		ans.Flags |= types.CmdFlagError
	}
	return ans
}

// Message flag helpers

// IsRequest returns true if the R (request) flag is set.
func (m *Message) IsRequest() bool {
	return m.Flags&types.CmdFlagRequest != 0
}

// IsProxiable returns true if the P (proxiable) flag is set.
func (m *Message) IsProxiable() bool {
	return m.Flags&types.CmdFlagProxiable != 0
}

// IsError returns true if the E (error) flag is set.
func (m *Message) IsError() bool {
	return m.Flags&types.CmdFlagError != 0
}

// IsRetransmit returns true if the T (retransmit) flag is set.
func (m *Message) IsRetransmit() bool {
	return m.Flags&types.CmdFlagRetransmit != 0
}

// SetProxiable sets the P (proxiable) flag.
func (m *Message) SetProxiable(proxiable bool) {
	if proxiable {
		m.Flags |= types.CmdFlagProxiable
	} else {
		m.Flags &^= types.CmdFlagProxiable
	}
}

// SetError sets the E (error) flag.
func (m *Message) SetError(isError bool) {
	if isError {
		m.Flags |= types.CmdFlagError
	} else {
		m.Flags &^= types.CmdFlagError
	}
}

// SetRetransmit sets the T (retransmit) flag.
func (m *Message) SetRetransmit(retransmit bool) {
	if retransmit {
		m.Flags |= types.CmdFlagRetransmit
	} else {
		m.Flags &^= types.CmdFlagRetransmit
	}
}

// AVP manipulation

// AddAVP adds an AVP to the message.
func (m *Message) AddAVP(avp *AVP) {
	m.AVPs = append(m.AVPs, avp)
}

// AddAVPs adds multiple AVPs to the message.
func (m *Message) AddAVPs(avps ...*AVP) {
	m.AVPs = append(m.AVPs, avps...)
}

// FindAVP finds the first AVP with the given code (and optionally vendor ID).
// If vendorID is 0, it matches any vendor (including no vendor).
func (m *Message) FindAVP(code types.AVPCode, vendorID types.VendorID) *AVP {
	for _, avp := range m.AVPs {
		if avp.Code == code {
			if vendorID == 0 || avp.VendorID == vendorID {
				return avp
			}
		}
	}
	return nil
}

// FindAllAVPs finds all AVPs with the given code (and optionally vendor ID).
func (m *Message) FindAllAVPs(code types.AVPCode, vendorID types.VendorID) []*AVP {
	var result []*AVP
	for _, avp := range m.AVPs {
		if avp.Code == code {
			if vendorID == 0 || avp.VendorID == vendorID {
				result = append(result, avp)
			}
		}
	}
	return result
}

// AddChild adds a child AVP to a grouped AVP.
func (a *AVP) AddChild(child *AVP) {
	a.children = append(a.children, child)
}

// FindChild finds the first child AVP with the given code.
func (a *AVP) FindChild(code types.AVPCode, vendorID types.VendorID) *AVP {
	for _, child := range a.children {
		if child.Code == code {
			if vendorID == 0 || child.VendorID == vendorID {
				return child
			}
		}
	}
	return nil
}

// RemoveChild removes a child AVP at the specified index.
func (a *AVP) RemoveChild(index int) {
	if index < 0 || index >= len(a.children) {
		return
	}
	a.children = append(a.children[:index], a.children[index+1:]...)
}

// Common AVP accessors for messages

// GetSessionID returns the Session-ID AVP value.
func (m *Message) GetSessionID() (string, bool) {
	avp := m.FindAVP(types.AVPCodeSessionID, 0)
	if avp == nil {
		return "", false
	}
	return avp.GetUTF8String(), true
}

// GetOriginHost returns the Origin-Host AVP value.
func (m *Message) GetOriginHost() (types.DiamID, bool) {
	avp := m.FindAVP(types.AVPCodeOriginHost, 0)
	if avp == nil {
		return "", false
	}
	return avp.GetDiameterIdentity(), true
}

// GetOriginRealm returns the Origin-Realm AVP value.
func (m *Message) GetOriginRealm() (types.DiamID, bool) {
	avp := m.FindAVP(types.AVPCodeOriginRealm, 0)
	if avp == nil {
		return "", false
	}
	return avp.GetDiameterIdentity(), true
}

// GetDestinationHost returns the Destination-Host AVP value.
func (m *Message) GetDestinationHost() (types.DiamID, bool) {
	avp := m.FindAVP(types.AVPCodeDestinationHost, 0)
	if avp == nil {
		return "", false
	}
	return avp.GetDiameterIdentity(), true
}

// GetDestinationRealm returns the Destination-Realm AVP value.
func (m *Message) GetDestinationRealm() (types.DiamID, bool) {
	avp := m.FindAVP(types.AVPCodeDestinationRealm, 0)
	if avp == nil {
		return "", false
	}
	return avp.GetDiameterIdentity(), true
}

// GetResultCode returns the Result-Code AVP value.
func (m *Message) GetResultCode() (types.ResultCode, bool) {
	avp := m.FindAVP(types.AVPCodeResultCode, 0)
	if avp == nil {
		return 0, false
	}
	val, err := avp.GetUnsigned32()
	if err != nil {
		return 0, false
	}
	return types.ResultCode(val), true
}

// GetUserName returns the User-Name AVP value.
func (m *Message) GetUserName() (string, bool) {
	avp := m.FindAVP(types.AVPCodeUserName, 0)
	if avp == nil {
		return "", false
	}
	return avp.GetUTF8String(), true
}

// SetResultCode adds or updates the Result-Code AVP.
func (m *Message) SetResultCode(code types.ResultCode) {
	avp := m.FindAVP(types.AVPCodeResultCode, 0)
	if avp != nil {
		// Update existing
		data := make([]byte, 4)
		binary.BigEndian.PutUint32(data, uint32(code))
		avp.Data = data
	} else {
		// Add new
		m.AddAVP(NewUnsigned32AVP(types.AVPCodeResultCode, types.AVPFlagMandatory, uint32(code)))
	}
}

// GetAccountingRecordType returns the Accounting-Record-Type AVP value.
func (m *Message) GetAccountingRecordType() (types.AccountingRecordType, bool) {
	avp := m.FindAVP(types.AVPCodeAccountingRecordType, 0)
	if avp == nil {
		return 0, false
	}
	val, err := avp.GetUnsigned32()
	if err != nil {
		return 0, false
	}
	return types.AccountingRecordType(val), true
}

// GetAccountingRecordNumber returns the Accounting-Record-Number AVP value.
func (m *Message) GetAccountingRecordNumber() (uint32, bool) {
	avp := m.FindAVP(types.AVPCodeAccountingRecordNumber, 0)
	if avp == nil {
		return 0, false
	}
	val, err := avp.GetUnsigned32()
	if err != nil {
		return 0, false
	}
	return val, true
}

// DumpFormat controls the format of message dumps.
type DumpFormat int

const (
	// DumpSummary prints a compact summary of the message
	DumpSummary DumpFormat = iota
	// DumpFull prints the full message in a single line
	DumpFull
	// DumpTree prints the message in a tree view format
	DumpTree
)

// String returns a string representation of the AVP.
func (a *AVP) String() string {
	return a.dumpString(0)
}

// dumpString returns a string representation with indentation.
func (a *AVP) dumpString(indent int) string {
	prefix := ""
	for i := 0; i < indent; i++ {
		prefix += "  "
	}

	vendorStr := ""
	if a.Flags&types.AVPFlagVendor != 0 {
		vendorStr = fmt.Sprintf("[V:%d] ", a.VendorID)
	}

	flagsStr := ""
	if a.Flags&types.AVPFlagMandatory != 0 {
		flagsStr += "M"
	}
	if a.Flags&types.AVPFlagProtected != 0 {
		flagsStr += "P"
	}
	if a.Flags&types.AVPFlagVendor != 0 {
		flagsStr += "V"
	}

	if len(a.children) > 0 {
		var childrenStr string
		for _, child := range a.children {
			childrenStr += child.dumpString(indent+1) + "\n"
		}
		return fmt.Sprintf("%sAVP[%d] {%s} %s- Grouped AVP\n%s", prefix, a.Code, flagsStr, vendorStr, childrenStr)
	}

	return fmt.Sprintf("%sAVP[%d] {%s} %s- Data: %v", prefix, a.Code, flagsStr, vendorStr, a.Data)
}

// DumpSummary returns a compact summary of the message.
func (m *Message) DumpSummary() string {
	originHost, _ := m.GetOriginHost()
	originRealm, _ := m.GetOriginRealm()
	destRealm, _ := m.GetDestinationRealm()
	sessionID, _ := m.GetSessionID()

	direction := "REQ"
	if !m.IsRequest() {
		direction = "ANS"
	}

	return fmt.Sprintf("%s %s[%d|%d] %s->%s {s:%s}", direction,
		originHost, m.CommandCode, m.ApplicationID, originRealm, destRealm, sessionID)
}

// DumpFull returns the full message dump in a single line.
func (m *Message) DumpFull() string {
	originHost, _ := m.GetOriginHost()
	originRealm, _ := m.GetOriginRealm()
	destHost, _ := m.GetDestinationHost()
	destRealm, _ := m.GetDestinationRealm()

	direction := "REQ"
	if !m.IsRequest() {
		direction = "ANS"
	}

	return fmt.Sprintf("%s %s[%d|%d] %s->%s%s {AVPs:%d, HbHID:%d, EtEID:%d}", direction,
		originHost, m.CommandCode, m.ApplicationID, originRealm, destRealm, destHost,
		len(m.AVPs), m.HopByHopID, m.EndToEndID)
}

// DumpTree returns the message in a tree view format.
func (m *Message) DumpTree() string {
	var buf bytes.Buffer
	m.dumpTree(&buf, 0)
	return buf.String()
}

// dumpTree recursively dumps the message and its AVPs in tree format.
func (m *Message) dumpTree(buf *bytes.Buffer, indent int) {
	prefix := ""
	for i := 0; i < indent; i++ {
		prefix += "  "
	}

	originHost, _ := m.GetOriginHost()
	originRealm, _ := m.GetOriginRealm()
	destRealm, _ := m.GetDestinationRealm()

	direction := "Request"
	if !m.IsRequest() {
		direction = "Answer"
	}

	fmt.Fprintf(buf, "%s%s '%d'\n", prefix, direction, m.CommandCode)
	fmt.Fprintf(buf, "%s  Version: %d, Length: %d\n", prefix, m.Version, m.EncodedLen())
	fmt.Fprintf(buf, "%s  Command: %d, Application: %d\n", prefix, m.CommandCode, m.ApplicationID)
	fmt.Fprintf(buf, "%s  Flags: %08b\n", prefix, m.Flags)
	fmt.Fprintf(buf, "%s  Hop-by-Hop ID: %d, End-to-End ID: %d\n", prefix, m.HopByHopID, m.EndToEndID)
	fmt.Fprintf(buf, "%s  Origin-Host: %s\n", prefix, originHost)
	fmt.Fprintf(buf, "%s  Origin-Realm: %s\n", prefix, originRealm)
	if destHost, ok := m.GetDestinationHost(); ok && destHost != "" {
		fmt.Fprintf(buf, "%s  Destination-Host: %s\n", prefix, destHost)
	}
	fmt.Fprintf(buf, "%s  Destination-Realm: %s\n", prefix, destRealm)

	if len(m.AVPs) > 0 {
		fmt.Fprintf(buf, "%s  AVPs [%d]:\n", prefix, len(m.AVPs))
		for _, avp := range m.AVPs {
			fmt.Fprintf(buf, "%s    %s\n", prefix, avp.dumpString(0))
		}
	}
}
