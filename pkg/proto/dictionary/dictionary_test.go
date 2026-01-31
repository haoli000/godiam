// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dictionary

import (
	"testing"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func TestNewDictionary(t *testing.T) {
	d := New()
	if d == nil {
		t.Fatal("New() returned nil")
	}

	stats := d.Stats()
	if stats.Vendors != 0 || stats.Applications != 0 || stats.AVPs != 0 {
		t.Error("new dictionary should be empty")
	}
}

func TestAddVendor(t *testing.T) {
	d := New()

	v := &Vendor{ID: 10415, Name: "3GPP"}
	err := d.AddVendor(v)
	if err != nil {
		t.Fatalf("AddVendor failed: %v", err)
	}

	// Lookup by ID
	found, ok := d.GetVendorByID(10415)
	if !ok {
		t.Error("vendor not found by ID")
	}
	if found.Name != "3GPP" {
		t.Errorf("expected name '3GPP', got %q", found.Name)
	}

	// Lookup by name
	found, ok = d.GetVendorByName("3GPP")
	if !ok {
		t.Error("vendor not found by name")
	}
	if found.ID != 10415 {
		t.Errorf("expected ID 10415, got %d", found.ID)
	}

	// Duplicate ID should fail
	err = d.AddVendor(&Vendor{ID: 10415, Name: "Other"})
	if err == nil {
		t.Error("expected error for duplicate vendor ID")
	}

	// Duplicate name should fail
	err = d.AddVendor(&Vendor{ID: 99999, Name: "3GPP"})
	if err == nil {
		t.Error("expected error for duplicate vendor name")
	}
}

func TestAddAVP(t *testing.T) {
	d := New()

	// Add a type first
	_ = d.AddType(&TypeDef{Name: "UTF8String", BaseType: types.AVPTypeUTF8String})
	typeDef, _ := d.GetTypeByName("UTF8String")

	avp := &AVP{
		Code:     263,
		Name:     "Session-ID",
		VendorID: 0,
		Type:     typeDef,
		Flags:    AVPFlags{Mandatory: true},
	}

	err := d.AddAVP(avp)
	if err != nil {
		t.Fatalf("AddAVP failed: %v", err)
	}

	// Lookup by code
	found, ok := d.GetAVPByCode(0, 263)
	if !ok {
		t.Error("AVP not found by code")
	}
	if found.Name != "Session-ID" {
		t.Errorf("expected name 'Session-ID', got %q", found.Name)
	}

	// Lookup by name
	foundByName, ok := d.GetAVPByName("Session-ID")
	if !ok {
		t.Error("AVP not found by name")
	}
	if foundByName.Code != 263 {
		t.Errorf("expected code 263, got %d", foundByName.Code)
	}
}

func TestAddCommand(t *testing.T) {
	d := New()

	cmd := &Command{
		Code:  257,
		Name:  "Capabilities-Exchange-Request",
		Flags: CommandFlags{Request: true},
		AppID: 0,
	}

	err := d.AddCommand(cmd)
	if err != nil {
		t.Fatalf("AddCommand failed: %v", err)
	}

	// Lookup by code (request)
	found, ok := d.GetCommandByCode(257, true)
	if !ok {
		t.Error("command not found by code (request)")
	}
	if found.Name != "Capabilities-Exchange-Request" {
		t.Errorf("expected name 'Capabilities-Exchange-Request', got %q", found.Name)
	}

	// Lookup by code (answer) should not find it
	_, ok = d.GetCommandByCode(257, false)
	if ok {
		t.Error("answer command should not be found")
	}

	// Lookup by name
	_, ok = d.GetCommandByName("Capabilities-Exchange-Request")
	if !ok {
		t.Error("command not found by name")
	}
}

func TestLoadBaseProtocol(t *testing.T) {
	d := New()
	err := d.LoadBaseProtocol()
	if err != nil {
		t.Fatalf("LoadBaseProtocol failed: %v", err)
	}

	stats := d.Stats()
	t.Logf("Base protocol loaded: %+v", stats)

	// Check IETF vendor
	vendor, ok := d.GetVendorByID(0)
	if !ok {
		t.Error("IETF vendor not found")
	}
	if vendor.Name != "IETF" {
		t.Errorf("expected vendor name 'IETF', got %q", vendor.Name)
	}

	// Check 3GPP vendor
	_, ok = d.GetVendorByID(10415)
	if !ok {
		t.Error("3GPP vendor not found")
	}

	// Check common application
	app, ok := d.GetApplicationByID(0)
	if !ok {
		t.Error("Common Messages application not found")
	}
	if app.Name != "Diameter Common Messages" {
		t.Errorf("expected app name 'Diameter Common Messages', got %q", app.Name)
	}

	// Check base AVPs
	avp, ok := d.GetAVPByCode(0, types.AVPCodeSessionID)
	if !ok {
		t.Error("Session-ID AVP not found")
	}
	if avp.Name != "Session-ID" {
		t.Errorf("expected AVP name 'Session-ID', got %q", avp.Name)
	}

	avp, ok = d.GetAVPByName("Origin-Host")
	if !ok {
		t.Error("Origin-Host AVP not found by name")
	}
	if avp.Code != types.AVPCodeOriginHost {
		t.Errorf("expected AVP code %d, got %d", types.AVPCodeOriginHost, avp.Code)
	}

	// Check base commands
	cmd, ok := d.GetCommandByCode(types.CmdCodeCapabilitiesExchange, true)
	if !ok {
		t.Error("CER command not found")
	}
	if cmd.Name != "Capabilities-Exchange-Request" {
		t.Errorf("expected command name 'Capabilities-Exchange-Request', got %q", cmd.Name)
	}

	cmd, ok = d.GetCommandByCode(types.CmdCodeDeviceWatchdog, false)
	if !ok {
		t.Error("DWA command not found")
	}
	if cmd.Name != "Device-Watchdog-Answer" {
		t.Errorf("expected command name 'Device-Watchdog-Answer', got %q", cmd.Name)
	}
}

func TestLoadCreditControl(t *testing.T) {
	d := New()

	// Load base protocol first (required for types)
	err := d.LoadBaseProtocol()
	if err != nil {
		t.Fatalf("LoadBaseProtocol failed: %v", err)
	}

	// Load credit control
	err = d.LoadCreditControl()
	if err != nil {
		t.Fatalf("LoadCreditControl failed: %v", err)
	}

	// Check CC-Request-Type AVP
	avp, ok := d.GetAVPByCode(0, types.AVPCodeCCRequestType)
	if !ok {
		t.Error("CC-Request-Type AVP not found")
	}
	if avp.Name != "CC-Request-Type" {
		t.Errorf("expected AVP name 'CC-Request-Type', got %q", avp.Name)
	}

	// Check CCR command
	cmd, ok := d.GetCommandByCode(types.CmdCodeCreditControl, true)
	if !ok {
		t.Error("CCR command not found")
	}
	if cmd.Name != "Credit-Control-Request" {
		t.Errorf("expected command name 'Credit-Control-Request', got %q", cmd.Name)
	}
}

func TestEnumValues(t *testing.T) {
	d := New()

	// Add enum values
	err := d.AddEnum("Disconnect-Cause", &EnumValue{Name: "REBOOTING", Value: 0})
	if err != nil {
		t.Fatalf("AddEnum failed: %v", err)
	}
	err = d.AddEnum("Disconnect-Cause", &EnumValue{Name: "BUSY", Value: 1})
	if err != nil {
		t.Fatalf("AddEnum failed: %v", err)
	}

	// Lookup by value
	e, ok := d.GetEnumByValue("Disconnect-Cause", 0)
	if !ok {
		t.Error("enum not found by value")
	}
	if e.Name != "REBOOTING" {
		t.Errorf("expected name 'REBOOTING', got %q", e.Name)
	}

	// Lookup by name
	e, ok = d.GetEnumByName("Disconnect-Cause", "BUSY")
	if !ok {
		t.Error("enum not found by name")
	}
	if e.Value != 1 {
		t.Errorf("expected value 1, got %d", e.Value)
	}

	// Non-existent
	_, ok = d.GetEnumByValue("Disconnect-Cause", 99)
	if ok {
		t.Error("should not find non-existent enum value")
	}
}

func TestDictionaryStats(t *testing.T) {
	d := New()
	_ = d.LoadBaseProtocol()
	_ = d.LoadCreditControl()

	stats := d.Stats()

	if stats.Vendors < 2 {
		t.Errorf("expected at least 2 vendors, got %d", stats.Vendors)
	}
	if stats.Applications < 7 {
		t.Errorf("expected at least 7 applications, got %d", stats.Applications)
	}
	if stats.Types < 15 {
		t.Errorf("expected at least 15 types, got %d", stats.Types)
	}
	if stats.AVPs < 50 {
		t.Errorf("expected at least 50 AVPs, got %d", stats.AVPs)
	}
	if stats.Commands < 14 {
		t.Errorf("expected at least 14 commands, got %d", stats.Commands)
	}
}
