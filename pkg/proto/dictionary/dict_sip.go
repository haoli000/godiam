// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dictionary

import (
	"github.com/haoli000/godiam/pkg/proto/types"
)

// LoadSIP loads the Diameter SIP Application (RFC 4740) definitions.
func (d *Dictionary) LoadSIP() error {
	// Add SIP application
	if err := d.AddApplication(&Application{
		ID:   types.AppIDSIP,
		Name: "Diameter SIP",
	}); err != nil {
		return err
	}

	unsigned32, _ := d.GetTypeByName("Unsigned32")
	utf8String, _ := d.GetTypeByName("UTF8String")
	octetString, _ := d.GetTypeByName("OctetString")
	enumerated, _ := d.GetTypeByName("Enumerated")
	grouped, _ := d.GetTypeByName("Grouped")

	// SIP AVPs
	sipAVPs := []*AVP{
		{Code: types.AVPCodeSIPAccountingInformation, Name: "SIP-Accounting-Information", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPAccountingServerURI, Name: "SIP-Accounting-Server-URI", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPAuthDataItem, Name: "SIP-Auth-Data-Item", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPAuthenticationScheme, Name: "SIP-Authentication-Scheme", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPAuthenticate, Name: "SIP-Authenticate", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPAuthorization, Name: "SIP-Authorization", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPAuthenticationInfo, Name: "SIP-Authentication-Info", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPMemberUser, Name: "SIP-Member-User", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPMethod, Name: "SIP-Method", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPNumberAuthItems, Name: "SIP-Number-Auth-Items", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPReasonData, Name: "SIP-Reason-Data", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPReasonInfo, Name: "SIP-Reason-Info", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPServerAssignmentType, Name: "SIP-Server-Assignment-Type", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPServerURI, Name: "SIP-Server-URI", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPUserData, Name: "SIP-User-Data", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPUserAuthorizationType, Name: "SIP-User-Authorization-Type", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSIPUserRoutingInfo, Name: "SIP-User-Routing-Info", Type: grouped, Flags: AVPFlags{Mandatory: true}},
	}

	for _, avp := range sipAVPs {
		if err := d.AddAVP(avp); err != nil {
			// Ignore name collisions - some are redefined by 3GPP
			continue
		}
	}

	// SIP Commands
	sipCommands := []*Command{
		{Code: types.CmdCodeSIPUserAuthorization, Name: "User-Authorization-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppIDSIP},
		{Code: types.CmdCodeSIPUserAuthorization, Name: "User-Authorization-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppIDSIP},
		{Code: types.CmdCodeSIPServerAssignment, Name: "Server-Assignment-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppIDSIP},
		{Code: types.CmdCodeSIPServerAssignment, Name: "Server-Assignment-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppIDSIP},
		{Code: types.CmdCodeSIPLocationInfo, Name: "Location-Info-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppIDSIP},
		{Code: types.CmdCodeSIPLocationInfo, Name: "Location-Info-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppIDSIP},
		{Code: types.CmdCodeSIPMultimediaAuth, Name: "Multimedia-Auth-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppIDSIP},
		{Code: types.CmdCodeSIPMultimediaAuth, Name: "Multimedia-Auth-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppIDSIP},
		{Code: types.CmdCodeSIPPushProfile, Name: "Push-Profile-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppIDSIP},
		{Code: types.CmdCodeSIPPushProfile, Name: "Push-Profile-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppIDSIP},
	}

	for _, cmd := range sipCommands {
		if err := d.AddCommand(cmd); err != nil {
			// Ignore name collisions
			continue
		}
	}

	return nil
}
