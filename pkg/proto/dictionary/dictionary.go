// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package dictionary provides the Diameter dictionary subsystem.
// This corresponds to libfdproto/dictionary.c from the original freeDiameter.
//
// The dictionary manages all Diameter protocol objects:
// - Vendors: Define vendor-specific extensions
// - Applications: Define Diameter applications
// - Types: Define AVP data types and their encoding/decoding
// - Enums: Define enumerated values for Integer32/Unsigned32 types
// - AVPs: Define AVP structure (code, type, flags, rules)
// - Commands: Define Diameter command structure (code, flags, rules)
// - Rules: Define AVP occurrence rules in commands or grouped AVPs
package dictionary

import (
	"fmt"
	"sync"

	"github.com/haoli000/godiam/pkg/proto/types"
)

// ObjectType represents the type of dictionary object.
type ObjectType int

// ObjectType represents the type of dictionary object.
const (
	// ObjectTypeVendor represents a vendor definition.
	ObjectTypeVendor ObjectType = iota + 1
	// ObjectTypeApplication represents an application definition.
	ObjectTypeApplication
	// ObjectTypeType represents an AVP data type definition.
	ObjectTypeType
	// ObjectTypeEnum represents an enumerated value definition.
	ObjectTypeEnum
	// ObjectTypeAVP represents an AVP definition.
	ObjectTypeAVP
	// ObjectTypeCommand represents a command definition.
	ObjectTypeCommand
	// ObjectTypeRule represents a validation rule definition.
	ObjectTypeRule
)

// String returns the string representation of an ObjectType.
func (t ObjectType) String() string {
	names := map[ObjectType]string{
		ObjectTypeVendor:      "Vendor",
		ObjectTypeApplication: "Application",
		ObjectTypeType:        "Type",
		ObjectTypeEnum:        "Enum",
		ObjectTypeAVP:         "AVP",
		ObjectTypeCommand:     "Command",
		ObjectTypeRule:        "Rule",
	}
	if name, ok := names[t]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(%d)", t)
}

// Vendor represents a Diameter vendor definition.
type Vendor struct {
	ID   types.VendorID
	Name string
}

// Application represents a Diameter application definition.
type Application struct {
	ID       types.ApplicationID
	Name     string
	VendorID types.VendorID
}

// TypeDef represents a Diameter AVP type definition.
// It includes the base type and optional encode/decode callbacks.
type TypeDef struct {
	Name     string
	BaseType types.AVPType
	// Encode converts a Go value to wire format bytes.
	// If nil, default encoding for BaseType is used.
	Encode func(value interface{}) ([]byte, error)
	// Decode converts wire format bytes to a Go value.
	// If nil, default decoding for BaseType is used.
	Decode func(data []byte) (interface{}, error)
}

// EnumValue represents an enumerated value for an Integer32 or Unsigned32 type.
type EnumValue struct {
	Name    string
	Value   int32  // For Integer32/Enumerated
	UValue  uint32 // For Unsigned32
	TypeRef *TypeDef
}

// AVP represents a Diameter AVP definition.
type AVP struct {
	Code     types.AVPCode
	Name     string
	VendorID types.VendorID
	Type     *TypeDef
	Flags    AVPFlags
	Rules    []*Rule // For Grouped AVPs, the child AVP rules
}

// AVPFlags defines the flag requirements for an AVP.
type AVPFlags struct {
	Mandatory     bool // M bit must be set
	MandatoryMust bool // M bit MUST be set (vs MAY)
	Protected     bool // P bit requirements
}

// Command represents a Diameter command definition.
type Command struct {
	Code  types.CommandCode
	Name  string
	Flags CommandFlags
	AppID types.ApplicationID
	Rules []*Rule // AVP rules for this command
}

// CommandFlags defines the flag requirements for a command.
type CommandFlags struct {
	Request   bool // True for Request, false for Answer
	Proxiable bool // P bit
}

// RulePosition defines where an AVP can appear.
type RulePosition int

const (
	// RulePositionFixed indicates a fixed-position AVP.
	RulePositionFixed RulePosition = iota
	// RulePositionRequired indicates a required AVP at any position.
	RulePositionRequired
	// RulePositionOptional indicates an optional AVP.
	RulePositionOptional
)

// Rule defines an AVP occurrence rule in a command or grouped AVP.
type Rule struct {
	AVP      *AVP         // The AVP this rule applies to
	Position RulePosition // Fixed, Required, or Optional
	Min      int          // Minimum occurrences (0 = optional)
	Max      int          // Maximum occurrences (-1 = unlimited)
	Order    int          // Order for fixed-position AVPs
}

// Dictionary is the main dictionary structure.
// It is thread-safe for concurrent access.
type Dictionary struct {
	mu sync.RWMutex

	// Vendors indexed by ID and name
	vendorsByID   map[types.VendorID]*Vendor
	vendorsByName map[string]*Vendor

	// Applications indexed by ID and name
	appsByID   map[types.ApplicationID]*Application
	appsByName map[string]*Application

	// Types indexed by name
	typesByName map[string]*TypeDef

	// Enums indexed by type name, then value or name
	enumsByTypeAndValue map[string]map[int32]*EnumValue
	enumsByTypeAndName  map[string]map[string]*EnumValue

	// AVPs indexed by (vendorID, code) and by name
	avpsByKey  map[avpKey]*AVP
	avpsByName map[string]*AVP

	// Commands indexed by (code, isRequest) and by name
	cmdsByKey  map[cmdKey]*Command
	cmdsByName map[string]*Command
}

type avpKey struct {
	vendorID types.VendorID
	code     types.AVPCode
}

type cmdKey struct {
	code      types.CommandCode
	isRequest bool
}

// New creates a new empty Dictionary.
func New() *Dictionary {
	return &Dictionary{
		vendorsByID:         make(map[types.VendorID]*Vendor),
		vendorsByName:       make(map[string]*Vendor),
		appsByID:            make(map[types.ApplicationID]*Application),
		appsByName:          make(map[string]*Application),
		typesByName:         make(map[string]*TypeDef),
		enumsByTypeAndValue: make(map[string]map[int32]*EnumValue),
		enumsByTypeAndName:  make(map[string]map[string]*EnumValue),
		avpsByKey:           make(map[avpKey]*AVP),
		avpsByName:          make(map[string]*AVP),
		cmdsByKey:           make(map[cmdKey]*Command),
		cmdsByName:          make(map[string]*Command),
	}
}

// AddVendor adds a vendor to the dictionary.
func (d *Dictionary) AddVendor(v *Vendor) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.vendorsByID[v.ID]; exists {
		return fmt.Errorf("vendor with ID %d already exists", v.ID)
	}
	if _, exists := d.vendorsByName[v.Name]; exists {
		return fmt.Errorf("vendor with name %q already exists", v.Name)
	}

	d.vendorsByID[v.ID] = v
	d.vendorsByName[v.Name] = v
	return nil
}

// GetVendorByID returns the vendor with the given ID.
func (d *Dictionary) GetVendorByID(id types.VendorID) (*Vendor, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.vendorsByID[id]
	return v, ok
}

// GetVendorByName returns the vendor with the given name.
func (d *Dictionary) GetVendorByName(name string) (*Vendor, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.vendorsByName[name]
	return v, ok
}

// AddApplication adds an application to the dictionary.
func (d *Dictionary) AddApplication(a *Application) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.appsByID[a.ID]; exists {
		return fmt.Errorf("application with ID %d already exists", a.ID)
	}
	if _, exists := d.appsByName[a.Name]; exists {
		return fmt.Errorf("application with name %q already exists", a.Name)
	}

	d.appsByID[a.ID] = a
	d.appsByName[a.Name] = a
	return nil
}

// GetApplicationByID returns the application with the given ID.
func (d *Dictionary) GetApplicationByID(id types.ApplicationID) (*Application, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	a, ok := d.appsByID[id]
	return a, ok
}

// GetApplicationByName returns the application with the given name.
func (d *Dictionary) GetApplicationByName(name string) (*Application, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	a, ok := d.appsByName[name]
	return a, ok
}

// AddType adds a type definition to the dictionary.
func (d *Dictionary) AddType(t *TypeDef) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.typesByName[t.Name]; exists {
		return fmt.Errorf("type %q already exists", t.Name)
	}

	d.typesByName[t.Name] = t
	return nil
}

// GetTypeByName returns the type with the given name.
func (d *Dictionary) GetTypeByName(name string) (*TypeDef, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	t, ok := d.typesByName[name]
	return t, ok
}

// AddEnum adds an enumerated value to the dictionary.
func (d *Dictionary) AddEnum(typeName string, e *EnumValue) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.enumsByTypeAndValue[typeName] == nil {
		d.enumsByTypeAndValue[typeName] = make(map[int32]*EnumValue)
		d.enumsByTypeAndName[typeName] = make(map[string]*EnumValue)
	}

	if _, exists := d.enumsByTypeAndValue[typeName][e.Value]; exists {
		return fmt.Errorf("enum value %d already exists for type %q", e.Value, typeName)
	}
	if _, exists := d.enumsByTypeAndName[typeName][e.Name]; exists {
		return fmt.Errorf("enum name %q already exists for type %q", e.Name, typeName)
	}

	d.enumsByTypeAndValue[typeName][e.Value] = e
	d.enumsByTypeAndName[typeName][e.Name] = e
	return nil
}

// GetEnumByValue returns the enum with the given value for the given type.
func (d *Dictionary) GetEnumByValue(typeName string, value int32) (*EnumValue, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if m, ok := d.enumsByTypeAndValue[typeName]; ok {
		e, ok := m[value]
		return e, ok
	}
	return nil, false
}

// GetEnumByName returns the enum with the given name for the given type.
func (d *Dictionary) GetEnumByName(typeName, name string) (*EnumValue, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if m, ok := d.enumsByTypeAndName[typeName]; ok {
		e, ok := m[name]
		return e, ok
	}
	return nil, false
}

// AddAVP adds an AVP definition to the dictionary.
func (d *Dictionary) AddAVP(a *AVP) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := avpKey{vendorID: a.VendorID, code: a.Code}
	if _, exists := d.avpsByKey[key]; exists {
		return fmt.Errorf("AVP with code %d and vendor %d already exists", a.Code, a.VendorID)
	}
	if _, exists := d.avpsByName[a.Name]; exists {
		return fmt.Errorf("AVP with name %q already exists", a.Name)
	}

	d.avpsByKey[key] = a
	d.avpsByName[a.Name] = a
	return nil
}

// GetAVPByCode returns the AVP with the given code and vendor ID.
func (d *Dictionary) GetAVPByCode(vendorID types.VendorID, code types.AVPCode) (*AVP, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	a, ok := d.avpsByKey[avpKey{vendorID: vendorID, code: code}]
	return a, ok
}

// GetAVPByName returns the AVP with the given name.
func (d *Dictionary) GetAVPByName(name string) (*AVP, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	a, ok := d.avpsByName[name]
	return a, ok
}

// AddCommand adds a command definition to the dictionary.
func (d *Dictionary) AddCommand(c *Command) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := cmdKey{code: c.Code, isRequest: c.Flags.Request}
	if _, exists := d.cmdsByKey[key]; exists {
		reqType := "Answer"
		if c.Flags.Request {
			reqType = "Request"
		}
		return fmt.Errorf("command %s with code %d already exists", reqType, c.Code)
	}
	if _, exists := d.cmdsByName[c.Name]; exists {
		return fmt.Errorf("command with name %q already exists", c.Name)
	}

	d.cmdsByKey[key] = c
	d.cmdsByName[c.Name] = c
	return nil
}

// GetCommandByCode returns the command with the given code.
func (d *Dictionary) GetCommandByCode(code types.CommandCode, isRequest bool) (*Command, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	c, ok := d.cmdsByKey[cmdKey{code: code, isRequest: isRequest}]
	return c, ok
}

// GetCommandByName returns the command with the given name.
func (d *Dictionary) GetCommandByName(name string) (*Command, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	c, ok := d.cmdsByName[name]
	return c, ok
}

// Stats returns statistics about the dictionary contents.
func (d *Dictionary) Stats() Stats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	enumCount := 0
	for _, m := range d.enumsByTypeAndValue {
		enumCount += len(m)
	}

	return Stats{
		Vendors:      len(d.vendorsByID),
		Applications: len(d.appsByID),
		Types:        len(d.typesByName),
		Enums:        enumCount,
		AVPs:         len(d.avpsByKey),
		Commands:     len(d.cmdsByKey),
	}
}

// Stats contains statistics about dictionary contents.
type Stats struct {
	Vendors      int
	Applications int
	Types        int
	Enums        int
	AVPs         int
	Commands     int
}
