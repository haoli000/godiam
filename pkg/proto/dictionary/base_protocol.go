// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package dictionary provides the Diameter dictionary subsystem.
package dictionary

import (
	"github.com/haoli000/godiam/pkg/proto/types"
)

// LoadBaseProtocol loads the Diameter Base Protocol (RFC 6733) definitions
// into the dictionary.
func (d *Dictionary) LoadBaseProtocol() error {
	// Add IETF vendor (ID 0)
	if err := d.AddVendor(&Vendor{
		ID:   types.VendorIDIETF,
		Name: "IETF",
	}); err != nil {
		return err
	}

	// Add 3GPP vendor
	if err := d.AddVendor(&Vendor{
		ID:   types.VendorID3GPP,
		Name: "3GPP",
	}); err != nil {
		return err
	}

	// Add base applications
	baseApps := []*Application{
		{ID: types.AppIDCommon, Name: "Diameter Common Messages"},
		{ID: types.AppIDNASREQ, Name: "NASREQ"},
		{ID: types.AppIDMobileIPv4, Name: "Mobile IPv4"},
		{ID: types.AppIDBaseAccounting, Name: "Diameter Base Accounting"},
		{ID: types.AppIDCreditControl, Name: "Diameter Credit Control"},
		{ID: types.AppIDEAP, Name: "Diameter EAP"},
		{ID: types.AppIDRelay, Name: "Relay"},
	}
	for _, app := range baseApps {
		if err := d.AddApplication(app); err != nil {
			return err
		}
	}

	// Add base types
	if err := d.loadBaseTypes(); err != nil {
		return err
	}

	// Add base AVPs
	if err := d.loadBaseAVPs(); err != nil {
		return err
	}

	// Add base commands
	if err := d.loadBaseCommands(); err != nil {
		return err
	}

	return nil
}

func (d *Dictionary) loadBaseTypes() error {
	baseTypes := []*TypeDef{
		{Name: "OctetString", BaseType: types.AVPTypeOctetString},
		{Name: "Integer32", BaseType: types.AVPTypeInteger32},
		{Name: "Integer64", BaseType: types.AVPTypeInteger64},
		{Name: "Unsigned32", BaseType: types.AVPTypeUnsigned32},
		{Name: "Unsigned64", BaseType: types.AVPTypeUnsigned64},
		{Name: "Float32", BaseType: types.AVPTypeFloat32},
		{Name: "Float64", BaseType: types.AVPTypeFloat64},
		{Name: "Address", BaseType: types.AVPTypeAddress},
		{Name: "Time", BaseType: types.AVPTypeTime},
		{Name: "UTF8String", BaseType: types.AVPTypeUTF8String},
		{Name: "DiameterIdentity", BaseType: types.AVPTypeDiameterIdentity},
		{Name: "DiameterURI", BaseType: types.AVPTypeDiameterURI},
		{Name: "Enumerated", BaseType: types.AVPTypeEnumerated},
		{Name: "IPFilterRule", BaseType: types.AVPTypeIPFilterRule},
		{Name: "Grouped", BaseType: types.AVPTypeGrouped},
	}
	for _, t := range baseTypes {
		if err := d.AddType(t); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dictionary) loadBaseAVPs() error {
	// Get type references
	octetString, _ := d.GetTypeByName("OctetString")
	utf8String, _ := d.GetTypeByName("UTF8String")
	unsigned32, _ := d.GetTypeByName("Unsigned32")
	unsigned64, _ := d.GetTypeByName("Unsigned64")
	address, _ := d.GetTypeByName("Address")
	diamIdent, _ := d.GetTypeByName("DiameterIdentity")
	diamURI, _ := d.GetTypeByName("DiameterURI")
	timeType, _ := d.GetTypeByName("Time")
	grouped, _ := d.GetTypeByName("Grouped")
	enumerated, _ := d.GetTypeByName("Enumerated")

	// Base Protocol AVPs (RFC 6733)
	baseAVPs := []*AVP{
		// Session AVPs
		{Code: types.AVPCodeSessionID, Name: "Session-ID", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeOriginHost, Name: "Origin-Host", Type: diamIdent, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeOriginRealm, Name: "Origin-Realm", Type: diamIdent, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeDestinationHost, Name: "Destination-Host", Type: diamIdent, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeDestinationRealm, Name: "Destination-Realm", Type: diamIdent, Flags: AVPFlags{Mandatory: true}},

		// Application AVPs
		{Code: types.AVPCodeAuthApplicationID, Name: "Auth-Application-ID", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeAcctApplicationID, Name: "Acct-Application-ID", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeVendorSpecificAppID, Name: "Vendor-Specific-Application-ID", Type: grouped, Flags: AVPFlags{Mandatory: true}},

		// Result AVPs
		{Code: types.AVPCodeResultCode, Name: "Result-Code", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeExperimentalResult, Name: "Experimental-Result", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeExperimentalResultCode, Name: "Experimental-Result-Code", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeErrorMessage, Name: "Error-Message", Type: utf8String},
		{Code: types.AVPCodeErrorReportingHost, Name: "Error-Reporting-Host", Type: diamIdent},
		{Code: types.AVPCodeFailedAVP, Name: "Failed-AVP", Type: grouped, Flags: AVPFlags{Mandatory: true}},

		// Capability Exchange AVPs
		{Code: types.AVPCodeHostIPAddress, Name: "Host-IP-Address", Type: address, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeVendorID, Name: "Vendor-ID", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeProductName, Name: "Product-Name", Type: utf8String},
		{Code: types.AVPCodeOriginStateID, Name: "Origin-State-ID", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSupportedVendorID, Name: "Supported-Vendor-ID", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFirmwareRevision, Name: "Firmware-Revision", Type: unsigned32},
		{Code: types.AVPCodeInbandSecurityID, Name: "Inband-Security-ID", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},

		// Routing AVPs
		{Code: types.AVPCodeRouteRecord, Name: "Route-Record", Type: diamIdent, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeProxyInfo, Name: "Proxy-Info", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeProxyHost, Name: "Proxy-Host", Type: diamIdent, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeProxyState, Name: "Proxy-State", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeRedirectHost, Name: "Redirect-Host", Type: diamURI, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeRedirectHostUsage, Name: "Redirect-Host-Usage", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeRedirectMaxCacheTime, Name: "Redirect-Max-Cache-Time", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},

		// Authentication AVPs
		{Code: types.AVPCodeUserName, Name: "User-Name", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeAuthRequestType, Name: "Auth-Request-Type", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeAuthGracePeriod, Name: "Auth-Grace-Period", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeAuthSessionState, Name: "Auth-Session-State", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeReAuthRequestType, Name: "Re-Auth-Request-Type", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeClass, Name: "Class", Type: octetString, Flags: AVPFlags{Mandatory: true}},

		// Authorization AVPs
		{Code: types.AVPCodeAuthorizationLifetime, Name: "Authorization-Lifetime", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSessionTimeout, Name: "Session-Timeout", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},

		// Accounting AVPs
		{Code: types.AVPCodeAccountingRecordType, Name: "Accounting-Record-Type", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeAccountingRecordNumber, Name: "Accounting-Record-Number", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeAccountingSubSessionID, Name: "Accounting-Sub-Session-ID", Type: unsigned64, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeAccountingSessionID, Name: "Acct-Session-ID", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeAcctMultiSessionID, Name: "Acct-Multi-Session-ID", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeAcctInterimInterval, Name: "Acct-Interim-Interval", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeAccountingRealtimeRequired, Name: "Accounting-Realtime-Required", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeEventTimestamp, Name: "Event-Timestamp", Type: timeType, Flags: AVPFlags{Mandatory: true}},

		// Disconnect/Watchdog AVPs
		{Code: types.AVPCodeDisconnectCause, Name: "Disconnect-Cause", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeTerminationCause, Name: "Termination-Cause", Type: enumerated, Flags: AVPFlags{Mandatory: true}},

		// Other
		{Code: types.AVPCodeMultiRoundTimeOut, Name: "Multi-Round-Time-Out", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
	}

	for _, avp := range baseAVPs {
		avp.VendorID = types.VendorIDIETF // All base AVPs are IETF
		if err := d.AddAVP(avp); err != nil {
			return err
		}
	}

	return nil
}

func (d *Dictionary) loadBaseCommands() error {
	// Base Protocol Commands (RFC 6733)
	baseCommands := []*Command{
		// Capabilities-Exchange
		{
			Code:  types.CmdCodeCapabilitiesExchange,
			Name:  "Capabilities-Exchange-Request",
			Flags: CommandFlags{Request: true, Proxiable: false},
			AppID: types.AppIDCommon,
		},
		{
			Code:  types.CmdCodeCapabilitiesExchange,
			Name:  "Capabilities-Exchange-Answer",
			Flags: CommandFlags{Request: false, Proxiable: false},
			AppID: types.AppIDCommon,
		},

		// Re-Auth
		{
			Code:  types.CmdCodeReAuth,
			Name:  "Re-Auth-Request",
			Flags: CommandFlags{Request: true, Proxiable: true},
			AppID: types.AppIDCommon,
		},
		{
			Code:  types.CmdCodeReAuth,
			Name:  "Re-Auth-Answer",
			Flags: CommandFlags{Request: false, Proxiable: true},
			AppID: types.AppIDCommon,
		},

		// Accounting
		{
			Code:  types.CmdCodeAccounting,
			Name:  "Accounting-Request",
			Flags: CommandFlags{Request: true, Proxiable: true},
			AppID: types.AppIDBaseAccounting,
		},
		{
			Code:  types.CmdCodeAccounting,
			Name:  "Accounting-Answer",
			Flags: CommandFlags{Request: false, Proxiable: true},
			AppID: types.AppIDBaseAccounting,
		},

		// Abort-Session
		{
			Code:  types.CmdCodeAbortSession,
			Name:  "Abort-Session-Request",
			Flags: CommandFlags{Request: true, Proxiable: true},
			AppID: types.AppIDCommon,
		},
		{
			Code:  types.CmdCodeAbortSession,
			Name:  "Abort-Session-Answer",
			Flags: CommandFlags{Request: false, Proxiable: true},
			AppID: types.AppIDCommon,
		},

		// Session-Termination
		{
			Code:  types.CmdCodeSessionTermination,
			Name:  "Session-Termination-Request",
			Flags: CommandFlags{Request: true, Proxiable: true},
			AppID: types.AppIDCommon,
		},
		{
			Code:  types.CmdCodeSessionTermination,
			Name:  "Session-Termination-Answer",
			Flags: CommandFlags{Request: false, Proxiable: true},
			AppID: types.AppIDCommon,
		},

		// Device-Watchdog
		{
			Code:  types.CmdCodeDeviceWatchdog,
			Name:  "Device-Watchdog-Request",
			Flags: CommandFlags{Request: true, Proxiable: false},
			AppID: types.AppIDCommon,
		},
		{
			Code:  types.CmdCodeDeviceWatchdog,
			Name:  "Device-Watchdog-Answer",
			Flags: CommandFlags{Request: false, Proxiable: false},
			AppID: types.AppIDCommon,
		},

		// Disconnect-Peer
		{
			Code:  types.CmdCodeDisconnectPeer,
			Name:  "Disconnect-Peer-Request",
			Flags: CommandFlags{Request: true, Proxiable: false},
			AppID: types.AppIDCommon,
		},
		{
			Code:  types.CmdCodeDisconnectPeer,
			Name:  "Disconnect-Peer-Answer",
			Flags: CommandFlags{Request: false, Proxiable: false},
			AppID: types.AppIDCommon,
		},
	}

	for _, cmd := range baseCommands {
		if err := d.AddCommand(cmd); err != nil {
			return err
		}
	}

	return nil
}

// LoadCreditControl loads the Diameter Credit-Control Application (RFC 4006)
// definitions into the dictionary.
func (d *Dictionary) LoadCreditControl() error {
	unsigned32, _ := d.GetTypeByName("Unsigned32")
	enumerated, _ := d.GetTypeByName("Enumerated")
	utf8String, _ := d.GetTypeByName("UTF8String")
	grouped, _ := d.GetTypeByName("Grouped")
	timeType, _ := d.GetTypeByName("Time")
	octetString, _ := d.GetTypeByName("OctetString")

	// Credit-Control AVPs
	ccAVPs := []*AVP{
		{Code: types.AVPCodeCCRequestType, Name: "CC-Request-Type", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCCRequestNumber, Name: "CC-Request-Number", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCCSessionFailover, Name: "CC-Session-Failover", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCCSubSessionID, Name: "CC-Sub-Session-ID", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeGrantedServiceUnit, Name: "Granted-Service-Unit", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeRequestedServiceUnit, Name: "Requested-Service-Unit", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeUsedServiceUnit, Name: "Used-Service-Unit", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeServiceContextID, Name: "Service-Context-ID", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeServiceIdentifier, Name: "Service-Identifier", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeRatingGroup, Name: "Rating-Group", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSubscriptionID, Name: "Subscription-ID", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSubscriptionIDData, Name: "Subscription-ID-Data", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeSubscriptionIDType, Name: "Subscription-ID-Type", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeMultipleServicesCreditControl, Name: "Multiple-Services-Credit-Control", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeMultipleServicesIndicator, Name: "Multiple-Services-Indicator", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeRequestedAction, Name: "Requested-Action", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeValidityTime, Name: "Validity-Time", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFinalUnitIndication, Name: "Final-Unit-Indication", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeFinalUnitAction, Name: "Final-Unit-Action", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeTariffTimeChange, Name: "Tariff-Time-Change", Type: timeType, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeTariffChangeUsage, Name: "Tariff-Change-Usage", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCostInformation, Name: "Cost-Information", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCostUnit, Name: "Cost-Unit", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeUnitValue, Name: "Unit-Value", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeValueDigits, Name: "Value-Digits", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeExponent, Name: "Exponent", Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCreditControl, Name: "Credit-Control", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCreditControlFailureHandling, Name: "Credit-Control-Failure-Handling", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeDirectDebitingFailureHandling, Name: "Direct-Debiting-Failure-Handling", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeCheckBalanceResult, Name: "Check-Balance-Result", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeRedirectServer, Name: "Redirect-Server", Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeRedirectAddressType, Name: "Redirect-Address-Type", Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeRedirectServerAddress, Name: "Redirect-Server-Address", Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeRestrictionFilterRule, Name: "Restriction-Filter-Rule", Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: types.AVPCodeUserEquipmentInfo, Name: "User-Equipment-Info", Type: grouped},
		{Code: types.AVPCodeUserEquipmentInfoType, Name: "User-Equipment-Info-Type", Type: enumerated},
		{Code: types.AVPCodeUserEquipmentInfoValue, Name: "User-Equipment-Info-Value", Type: octetString},
	}

	for _, avp := range ccAVPs {
		avp.VendorID = types.VendorIDIETF
		if err := d.AddAVP(avp); err != nil {
			return err
		}
	}

	// Credit-Control Commands
	ccCommands := []*Command{
		{
			Code:  types.CmdCodeCreditControl,
			Name:  "Credit-Control-Request",
			Flags: CommandFlags{Request: true, Proxiable: true},
			AppID: types.AppIDCreditControl,
		},
		{
			Code:  types.CmdCodeCreditControl,
			Name:  "Credit-Control-Answer",
			Flags: CommandFlags{Request: false, Proxiable: true},
			AppID: types.AppIDCreditControl,
		},
	}

	for _, cmd := range ccCommands {
		if err := d.AddCommand(cmd); err != nil {
			return err
		}
	}

	return nil
}
