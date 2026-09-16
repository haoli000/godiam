// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dictionary

import (
	"testing"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func newBaseDictionary(t *testing.T) *Dictionary {
	t.Helper()
	d := New()
	if err := d.LoadBaseProtocol(); err != nil {
		t.Fatalf("LoadBaseProtocol failed: %v", err)
	}
	return d
}

func requireAVP(t *testing.T, d *Dictionary, vendorID types.VendorID, code types.AVPCode, name string) {
	t.Helper()
	avp, ok := d.GetAVPByCode(vendorID, code)
	if !ok {
		t.Fatalf("%s AVP not found by vendor/code", name)
	}
	if avp.Name != name {
		t.Fatalf("AVP name = %q, want %q", avp.Name, name)
	}
}

func requireCommand(t *testing.T, d *Dictionary, code types.CommandCode, request bool, name string, appID types.ApplicationID) {
	t.Helper()
	cmd, ok := d.GetCommandByCode(code, request)
	if !ok {
		t.Fatalf("%s command not found by code/request", name)
	}
	if cmd.Name != name || cmd.AppID != appID {
		t.Fatalf("command = %#v, want name %q app %d", cmd, name, appID)
	}
}

func TestLoadApplicationDictionariesExposeRepresentativeDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		load   func(*Dictionary) error
		assert func(*testing.T, *Dictionary)
	}{
		{
			name: "Credit-Control",
			load: (*Dictionary).LoadCreditControl,
			assert: func(t *testing.T, d *Dictionary) {
				requireAVP(t, d, types.VendorIDIETF, types.AVPCodeCCRequestType, "CC-Request-Type")
				requireCommand(t, d, types.CmdCodeCreditControl, true, "Credit-Control-Request", types.AppIDCreditControl)
			},
		},
		{
			name: "NASREQ",
			load: (*Dictionary).LoadNASREQ,
			assert: func(t *testing.T, d *Dictionary) {
				requireAVP(t, d, types.VendorIDIETF, types.AVPCodeNASPort, "NAS-Port")
				requireCommand(t, d, types.CmdCodeAAReq, true, "AA-Request", types.AppIDNASREQ)
			},
		},
		{
			name: "EAP",
			load: (*Dictionary).LoadEAP,
			assert: func(t *testing.T, d *Dictionary) {
				requireAVP(t, d, types.VendorIDIETF, types.AVPCodeEAPPayload, "EAP-Payload")
				requireCommand(t, d, types.CmdCodeEAP, false, "Diameter-EAP-Answer", types.AppIDEAP)
			},
		},
		{
			name: "SIP",
			load: (*Dictionary).LoadSIP,
			assert: func(t *testing.T, d *Dictionary) {
				app, ok := d.GetApplicationByName("Diameter SIP")
				if !ok || app.ID != types.AppIDSIP {
					t.Fatalf("SIP application = %#v ok=%v", app, ok)
				}
				requireAVP(t, d, types.VendorIDIETF, types.AVPCodeSIPMethod, "SIP-Method")
				requireCommand(t, d, types.CmdCodeSIPLocationInfo, false, "Location-Info-Answer", types.AppIDSIP)
			},
		},
		{
			name: "3GPP Cx",
			load: (*Dictionary).Load3GPPCx,
			assert: func(t *testing.T, d *Dictionary) {
				app, ok := d.GetApplicationByName("3GPP Cx")
				if !ok || app.VendorID != types.VendorID3GPP {
					t.Fatalf("Cx application = %#v ok=%v", app, ok)
				}
				requireAVP(t, d, types.VendorID3GPP, AVPCode3GPPPublicIdentity, "Public-Identity")
				requireCommand(t, d, CmdCode3GPPRegistrationTermination, true, "Registration-Termination-Request", types.AppID3GPPCx)
			},
		},
		{
			name: "3GPP S6a",
			load: (*Dictionary).Load3GPPS6a,
			assert: func(t *testing.T, d *Dictionary) {
				requireAVP(t, d, types.VendorID3GPP, AVPCode3GPPSubscriptionData, "Subscription-Data")
				requireAVP(t, d, types.VendorIDIETF, 1470, "Service-Selection")
				requireCommand(t, d, CmdCode3GPPAuthenticationInformation, false, "Authentication-Information-Answer", types.AppID3GPPS6a)
			},
		},
		{
			name: "3GPP Gx",
			load: (*Dictionary).Load3GPPGx,
			assert: func(t *testing.T, d *Dictionary) {
				requireAVP(t, d, types.VendorID3GPP, AVPCode3GPPChargingRuleInstall, "Charging-Rule-Install")
				app, ok := d.GetApplicationByID(types.AppID3GPPGx)
				if !ok || app.Name != "3GPP Gx" {
					t.Fatalf("Gx application = %#v ok=%v", app, ok)
				}
			},
		},
		{
			name: "3GPP Rx",
			load: (*Dictionary).Load3GPPRx,
			assert: func(t *testing.T, d *Dictionary) {
				requireAVP(t, d, types.VendorID3GPP, AVPCode3GPPFlowDescription, "Flow-Description")
				requireCommand(t, d, CmdCode3GPPAARequest, true, "AA-Request", types.AppID3GPPRx)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newBaseDictionary(t)
			if err := tt.load(d); err != nil {
				t.Fatalf("load failed: %v", err)
			}
			tt.assert(t, d)
		})
	}
}

func TestLoadAll3GPPAndConvenienceLoaderPopulateAllInterfaces(t *testing.T) {
	tests := []struct {
		name string
		load func(*Dictionary) error
	}{
		{name: "LoadAll3GPP", load: (*Dictionary).LoadAll3GPP},
		{name: "Load3GPP", load: (*Dictionary).Load3GPP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newBaseDictionary(t)
			if err := tt.load(d); err != nil {
				t.Fatalf("%s failed: %v", tt.name, err)
			}
			requireAVP(t, d, types.VendorID3GPP, AVPCode3GPPPublicIdentity, "Public-Identity")
			requireAVP(t, d, types.VendorID3GPP, AVPCode3GPPSubscriptionData, "Subscription-Data")
			requireAVP(t, d, types.VendorID3GPP, AVPCode3GPPChargingRuleInstall, "Charging-Rule-Install")
			requireAVP(t, d, types.VendorID3GPP, AVPCode3GPPFlowDescription, "Flow-Description")
		})
	}
}
