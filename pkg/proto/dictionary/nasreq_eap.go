// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dictionary

import (
	"github.com/haoli000/godiam/pkg/proto/types"
)

// LoadNASREQ loads the Diameter NASREQ Application (RFC 7155) definitions.
func (d *Dictionary) LoadNASREQ() error {
	unsigned32, _ := d.GetTypeByName("Unsigned32")
	enumerated, _ := d.GetTypeByName("Enumerated")
	utf8String, _ := d.GetTypeByName("UTF8String")
	octetString, _ := d.GetTypeByName("OctetString")
	address, _ := d.GetTypeByName("Address")

	// NASREQ AVPs
	nasAVPs := []*AVP{
		{Code: types.AVPCodeNASPort, Name: "NAS-Port", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeNASPortID, Name: "NAS-Port-ID", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeNASPortType, Name: "NAS-Port-Type", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCalledStationID, Name: "Called-Station-ID", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCallingStationID, Name: "Calling-Station-ID", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeConnectInfo, Name: "Connect-Info", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeOriginatingLineInfo, Name: "Originating-Line-Info", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeReplyMessage, Name: "Reply-Message", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeNASIdentifier, Name: "NAS-Identifier", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeNASIPAddress, Name: "NAS-IP-Address", Type: address, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeNASIPv6Address, Name: "NAS-IPv6-Address", Type: address, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeState, Name: "State", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFramedIPAddress, Name: "Framed-IP-Address", Type: address, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFramedIPv6Prefix, Name: "Framed-IPv6-Prefix", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFramedIPv6Pool, Name: "Framed-IPv6-Pool", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFramedIPNetmask, Name: "Framed-IP-Netmask", Type: address, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFramedMTU, Name: "Framed-MTU", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFramedProtocol, Name: "Framed-Protocol", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFramedRouting, Name: "Framed-Routing", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeServiceType, Name: "Service-Type", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeLoginIPHost, Name: "Login-IP-Host", Type: address, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeLoginIPv6Host, Name: "Login-IPv6-Host", Type: address, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeLoginService, Name: "Login-Service", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeLoginTCPPort, Name: "Login-TCP-Port", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeLoginLATService, Name: "Login-LAT-Service", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeLoginLATNode, Name: "Login-LAT-Node", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeLoginLATGroup, Name: "Login-LAT-Group", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeLoginLATPort, Name: "Login-LAT-Port", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFilterID, Name: "Filter-ID", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeIdleTimeout, Name: "Idle-Timeout", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodePortLimit, Name: "Port-Limit", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCallbackNumber, Name: "Callback-Number", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCallbackID, Name: "Callback-ID", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
	}

	for _, avp := range nasAVPs {
		avp.VendorID = types.VendorIDIETF
		if err := d.AddAVP(avp); err != nil {
			return err
		}
	}

	// NASREQ Commands
	nasCommands := []*Command{
		{
			Code:  types.CmdCodeAAReq,
			Name:  "AA-Request",
			Flags: CommandFlags{Request: true, Proxiable: true},
			AppID: types.AppIDNASREQ,
		},
		{
			Code:  types.CmdCodeAAReq,
			Name:  "AA-Answer",
			Flags: CommandFlags{Request: false, Proxiable: true},
			AppID: types.AppIDNASREQ,
		},
	}

	for _, cmd := range nasCommands {
		if err := d.AddCommand(cmd); err != nil {
			return err
		}
	}

	return nil
}

// LoadEAP loads the Diameter EAP Application (RFC 4072) definitions.
func (d *Dictionary) LoadEAP() error {
	octetString, _ := d.GetTypeByName("OctetString")

	// EAP AVPs
	eapAVPs := []*AVP{
		{Code: types.AVPCodeEAPPayload, Name: "EAP-Payload", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeEAPReissuedPayload, Name: "EAP-Reissued-Payload", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeEAPMasterSessionKey, Name: "EAP-Master-Session-Key", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeEAPKeyName, Name: "EAP-Key-Name", Type: octetString, Flags: AVPFlags{Mandatory: true}},
	}

	for _, avp := range eapAVPs {
		avp.VendorID = types.VendorIDIETF
		if err := d.AddAVP(avp); err != nil {
			return err
		}
	}

	// EAP Commands
	eapCommands := []*Command{
		{
			Code:  types.CmdCodeEAP,
			Name:  "Diameter-EAP-Request",
			Flags: CommandFlags{Request: true, Proxiable: true},
			AppID: types.AppIDEAP,
		},
		{
			Code:  types.CmdCodeEAP,
			Name:  "Diameter-EAP-Answer",
			Flags: CommandFlags{Request: false, Proxiable: true},
			AppID: types.AppIDEAP,
		},
	}

	for _, cmd := range eapCommands {
		if err := d.AddCommand(cmd); err != nil {
			return err
		}
	}

	return nil
}
