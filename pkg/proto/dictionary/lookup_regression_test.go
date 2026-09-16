// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dictionary

import (
	"strings"
	"testing"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func TestLookupsReportMisses(t *testing.T) {
	d := New()
	if err := d.LoadBaseProtocol(); err != nil {
		t.Fatalf("LoadBaseProtocol failed: %v", err)
	}

	tests := []struct {
		name string
		miss func() bool
	}{
		{
			name: "unknown AVP code",
			miss: func() bool {
				_, ok := d.GetAVPByCode(types.VendorIDIETF, 9_999_999)
				return !ok
			},
		},
		{
			name: "unknown AVP name",
			miss: func() bool {
				_, ok := d.GetAVPByName("Sessoin-ID")
				return !ok
			},
		},
		{
			name: "unknown vendor ID",
			miss: func() bool {
				_, ok := d.GetVendorByID(424242)
				return !ok
			},
		},
		{
			name: "unknown vendor name",
			miss: func() bool {
				_, ok := d.GetVendorByName("not-3GPP")
				return !ok
			},
		},
		{
			name: "unknown application name",
			miss: func() bool {
				_, ok := d.GetApplicationByName("Diameter Common Mesages")
				return !ok
			},
		},
		{
			name: "unknown command name",
			miss: func() bool {
				_, ok := d.GetCommandByName("Capabilities-Exchange-Requset")
				return !ok
			},
		},
		{
			name: "unknown type name",
			miss: func() bool {
				_, ok := d.GetTypeByName("Unsigned31")
				return !ok
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mistyping a dictionary key as a match would corrupt Diameter decoding.
			if !tt.miss() {
				t.Fatalf("lookup unexpectedly matched")
			}
		})
	}
}

func TestDuplicateRegistrationLeavesOriginalDefinitionsReachable(t *testing.T) {
	d := New()
	if err := d.LoadBaseProtocol(); err != nil {
		t.Fatalf("LoadBaseProtocol failed: %v", err)
	}

	tests := []struct {
		name      string
		register  func() error
		assertOld func(t *testing.T)
		wantError string
	}{
		{
			name: "vendor ID",
			register: func() error {
				return d.AddVendor(&Vendor{ID: types.VendorID3GPP, Name: "not really 3GPP"})
			},
			assertOld: func(t *testing.T) {
				v, ok := d.GetVendorByID(types.VendorID3GPP)
				if !ok || v.Name != "3GPP" {
					t.Fatalf("duplicate changed vendor: %#v ok=%v", v, ok)
				}
			},
			wantError: "already exists",
		},
		{
			name: "vendor name",
			register: func() error {
				return d.AddVendor(&Vendor{ID: 424242, Name: "3GPP"})
			},
			assertOld: func(t *testing.T) {
				v, ok := d.GetVendorByName("3GPP")
				if !ok || v.ID != types.VendorID3GPP {
					t.Fatalf("duplicate changed vendor name: %#v ok=%v", v, ok)
				}
			},
			wantError: "already exists",
		},
		{
			name: "application ID",
			register: func() error {
				return d.AddApplication(&Application{ID: types.AppIDCommon, Name: "Common but wrong"})
			},
			assertOld: func(t *testing.T) {
				a, ok := d.GetApplicationByID(types.AppIDCommon)
				if !ok || a.Name != "Diameter Common Messages" {
					t.Fatalf("duplicate changed application: %#v ok=%v", a, ok)
				}
			},
			wantError: "already exists",
		},
		{
			name: "application name",
			register: func() error {
				return d.AddApplication(&Application{ID: 424242, Name: "Diameter Common Messages"})
			},
			assertOld: func(t *testing.T) {
				a, ok := d.GetApplicationByName("Diameter Common Messages")
				if !ok || a.ID != types.AppIDCommon {
					t.Fatalf("duplicate changed application name: %#v ok=%v", a, ok)
				}
			},
			wantError: "already exists",
		},
		{
			name: "type name",
			register: func() error {
				return d.AddType(&TypeDef{Name: "UTF8String", BaseType: types.AVPTypeOctetString})
			},
			assertOld: func(t *testing.T) {
				typeDef, ok := d.GetTypeByName("UTF8String")
				if !ok || typeDef.BaseType != types.AVPTypeUTF8String {
					t.Fatalf("duplicate changed type: %#v ok=%v", typeDef, ok)
				}
			},
			wantError: "already exists",
		},
		{
			name: "AVP key",
			register: func() error {
				return d.AddAVP(&AVP{Code: types.AVPCodeSessionID, Name: "Session-ID-Collision", VendorID: types.VendorIDIETF})
			},
			assertOld: func(t *testing.T) {
				avp, ok := d.GetAVPByCode(types.VendorIDIETF, types.AVPCodeSessionID)
				if !ok || avp.Name != "Session-ID" {
					t.Fatalf("duplicate changed AVP: %#v ok=%v", avp, ok)
				}
			},
			wantError: "already exists",
		},
		{
			name: "AVP name",
			register: func() error {
				return d.AddAVP(&AVP{Code: 424242, Name: "Session-ID", VendorID: types.VendorIDIETF})
			},
			assertOld: func(t *testing.T) {
				avp, ok := d.GetAVPByName("Session-ID")
				if !ok || avp.Code != types.AVPCodeSessionID {
					t.Fatalf("duplicate changed AVP name: %#v ok=%v", avp, ok)
				}
			},
			wantError: "already exists",
		},
		{
			name: "command key",
			register: func() error {
				return d.AddCommand(&Command{Code: types.CmdCodeCapabilitiesExchange, Name: "Wrong-CER", Flags: CommandFlags{Request: true}})
			},
			assertOld: func(t *testing.T) {
				cmd, ok := d.GetCommandByCode(types.CmdCodeCapabilitiesExchange, true)
				if !ok || cmd.Name != "Capabilities-Exchange-Request" {
					t.Fatalf("duplicate changed command: %#v ok=%v", cmd, ok)
				}
			},
			wantError: "already exists",
		},
		{
			name: "command name",
			register: func() error {
				return d.AddCommand(&Command{Code: 424242, Name: "Capabilities-Exchange-Request", Flags: CommandFlags{Request: true}})
			},
			assertOld: func(t *testing.T) {
				cmd, ok := d.GetCommandByName("Capabilities-Exchange-Request")
				if !ok || cmd.Code != types.CmdCodeCapabilitiesExchange {
					t.Fatalf("duplicate changed command name: %#v ok=%v", cmd, ok)
				}
			},
			wantError: "already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.register()
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantError)
			}
			tt.assertOld(t)
		})
	}
}

func TestLookupDistinguishesVendorSpecificAVPs(t *testing.T) {
	d := New()
	if err := d.LoadBaseProtocol(); err != nil {
		t.Fatalf("LoadBaseProtocol failed: %v", err)
	}

	vendorSpecific := &AVP{
		Code:     types.AVPCodeSessionID,
		Name:     "Example-Vendor-Session-ID",
		VendorID: types.VendorID3GPP,
		Flags:    AVPFlags{Mandatory: true},
	}
	if err := d.AddAVP(vendorSpecific); err != nil {
		t.Fatalf("AddAVP vendor-specific shadow failed: %v", err)
	}

	base, ok := d.GetAVPByCode(types.VendorIDIETF, types.AVPCodeSessionID)
	if !ok {
		t.Fatal("base Session-ID AVP disappeared")
	}
	vendor, ok := d.GetAVPByCode(types.VendorID3GPP, types.AVPCodeSessionID)
	if !ok {
		t.Fatal("vendor-specific AVP with base code not found")
	}
	if base.Name != "Session-ID" {
		t.Fatalf("base lookup returned %q", base.Name)
	}
	if vendor.Name != "Example-Vendor-Session-ID" {
		t.Fatalf("vendor-specific lookup returned %q", vendor.Name)
	}
	if base == vendor {
		t.Fatal("base and vendor-specific AVPs must not share identity")
	}
}

func TestObjectTypeStringNamesKnownAndUnknownValues(t *testing.T) {
	tests := []struct {
		objectType ObjectType
		want       string
	}{
		{ObjectTypeVendor, "Vendor"},
		{ObjectTypeApplication, "Application"},
		{ObjectTypeType, "Type"},
		{ObjectTypeEnum, "Enum"},
		{ObjectTypeAVP, "AVP"},
		{ObjectTypeCommand, "Command"},
		{ObjectTypeRule, "Rule"},
		{ObjectType(99), "Unknown(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.objectType.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDuplicateEnumRegistrationLeavesOriginalValuesReachable(t *testing.T) {
	d := New()
	if err := d.AddEnum("Disconnect-Cause", &EnumValue{Name: "REBOOTING", Value: 0}); err != nil {
		t.Fatalf("AddEnum failed: %v", err)
	}

	tests := []struct {
		name     string
		register func() error
	}{
		{
			name: "value",
			register: func() error {
				return d.AddEnum("Disconnect-Cause", &EnumValue{Name: "BUSY", Value: 0})
			},
		},
		{
			name: "name",
			register: func() error {
				return d.AddEnum("Disconnect-Cause", &EnumValue{Name: "REBOOTING", Value: 1})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.register(); err == nil || !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("duplicate enum error = %v", err)
			}
			byValue, ok := d.GetEnumByValue("Disconnect-Cause", 0)
			if !ok || byValue.Name != "REBOOTING" {
				t.Fatalf("duplicate changed enum value lookup: %#v ok=%v", byValue, ok)
			}
			byName, ok := d.GetEnumByName("Disconnect-Cause", "REBOOTING")
			if !ok || byName.Value != 0 {
				t.Fatalf("duplicate changed enum name lookup: %#v ok=%v", byName, ok)
			}
		})
	}
}
