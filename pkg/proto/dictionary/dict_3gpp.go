// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package dictionary provides 3GPP-specific dictionary extensions.
package dictionary

import (
	"github.com/haoli000/godiam/pkg/proto/types"
)

// 3GPP AVP Codes (TS 29.229, TS 29.272, TS 29.212, etc.)
const (
	// Cx Interface (TS 29.229)
	AVPCode3GPPVisitedNetworkID                        types.AVPCode = 600
	AVPCode3GPPPublicIdentity                          types.AVPCode = 601
	AVPCode3GPPServerName                              types.AVPCode = 602
	AVPCode3GPPServerCapabilities                      types.AVPCode = 603
	AVPCode3GPPMandatoryCapability                     types.AVPCode = 604
	AVPCode3GPPOptionalCapability                      types.AVPCode = 605
	AVPCode3GPPUserData                                types.AVPCode = 606
	AVPCode3GPPSIPNumberAuthItems                      types.AVPCode = 607
	AVPCode3GPPSIPAuthDataItem                         types.AVPCode = 612
	AVPCode3GPPSIPAuthScheme                           types.AVPCode = 608
	AVPCode3GPPSIPAuthenticate                         types.AVPCode = 609
	AVPCode3GPPSIPAuthorization                        types.AVPCode = 610
	AVPCode3GPPSIPAuthContext                          types.AVPCode = 611
	AVPCode3GPPUserAuthType                            types.AVPCode = 623
	AVPCode3GPPServerAssignType                        types.AVPCode = 614
	AVPCode3GPPReasonCode                              types.AVPCode = 616
	AVPCode3GPPReasonInfo                              types.AVPCode = 617
	AVPCode3GPPDeregistrationReason                    types.AVPCode = 615
	AVPCode3GPPChargingInformation                     types.AVPCode = 618
	AVPCode3GPPPrimaryChargingCollectionFunctionName   types.AVPCode = 621
	AVPCode3GPPSecondaryChargingCollectionFunctionName types.AVPCode = 622

	// S6a Interface (TS 29.272)
	AVPCode3GPPSubscriptionData            types.AVPCode = 1400
	AVPCode3GPPTerminalInformation         types.AVPCode = 1401
	AVPCode3GPPIMEI                        types.AVPCode = 1402
	AVPCode3GPPSoftwareVersion             types.AVPCode = 1403
	AVPCode3GPPQoSSubscribed               types.AVPCode = 1404
	AVPCode3GPPULRFlags                    types.AVPCode = 1405
	AVPCode3GPPULAFlags                    types.AVPCode = 1406
	AVPCode3GPPVisitedPLMNID               types.AVPCode = 1407
	AVPCode3GPPRequestedEUTRANAuthInfo     types.AVPCode = 1408
	AVPCode3GPPRequestedUTRANGERANAuthInfo types.AVPCode = 1409
	AVPCode3GPPNumberOfRequestedVectors    types.AVPCode = 1410
	AVPCode3GPPReAuthRequestType           types.AVPCode = 1411
	AVPCode3GPPImmediateResponsePreferred  types.AVPCode = 1412
	AVPCode3GPPAuthenticationInfo          types.AVPCode = 1413
	AVPCode3GPPEUTRANVector                types.AVPCode = 1414
	AVPCode3GPPUTRANVector                 types.AVPCode = 1415
	AVPCode3GPPGERANVector                 types.AVPCode = 1416
	AVPCode3GPPNetworkAccessMode           types.AVPCode = 1417
	AVPCode3GPPHPLMNODBc                   types.AVPCode = 1418
	AVPCode3GPPItemNumber                  types.AVPCode = 1419
	AVPCode3GPPCancellationType            types.AVPCode = 1420
	AVPCode3GPPDSAFlags                    types.AVPCode = 1421
	AVPCode3GPPContextIdentifier           types.AVPCode = 1423
	AVPCode3GPPSubscriberStatus            types.AVPCode = 1424
	AVPCode3GPPOperatorDeterminedBarring   types.AVPCode = 1425
	AVPCode3GPPAccessRestrictionData       types.AVPCode = 1426
	AVPCode3GPPAPNOIReplacement            types.AVPCode = 1427
	AVPCode3GPPAllAPNConfigsIncluded       types.AVPCode = 1428
	AVPCode3GPPAPNConfigurationProfile     types.AVPCode = 1429
	AVPCode3GPPAPNConfiguration            types.AVPCode = 1430
	AVPCode3GPPEPSSubscribedQoSProfile     types.AVPCode = 1431
	AVPCode3GPPVPLMNDynamicAddressAllowed  types.AVPCode = 1432
	AVPCode3GPPSTNSr                       types.AVPCode = 1433
	AVPCode3GPPAlertReason                 types.AVPCode = 1434
	AVPCode3GPPAMBr                        types.AVPCode = 1435
	AVPCode3GPPCSGSubscriptionData         types.AVPCode = 1436
	AVPCode3GPPCSGID                       types.AVPCode = 1437
	AVPCode3GPPPDNType                     types.AVPCode = 1438
	AVPCode3GPPTraceData                   types.AVPCode = 1439
	AVPCode3GPPTraceReference              types.AVPCode = 1459
	AVPCode3GPPTraceDepth                  types.AVPCode = 1462
	AVPCode3GPPTraceNETypeList             types.AVPCode = 1463
	AVPCode3GPPTraceInterfaceList          types.AVPCode = 1464
	AVPCode3GPPTraceEventList              types.AVPCode = 1465
	AVPCode3GPPOMC                         types.AVPCode = 1466
	AVPCode3GPPGPRSSubscriptionData        types.AVPCode = 1467
	AVPCode3GPPCSGIDList                   types.AVPCode = 1469
	AVPCode3GPPExpirationDate              types.AVPCode = 1439
	AVPCode3GPPRand                        types.AVPCode = 1447
	AVPCode3GPPXRES                        types.AVPCode = 1448
	AVPCode3GPPAUTN                        types.AVPCode = 1449
	AVPCode3GPPKASME                       types.AVPCode = 1450

	// Gx Interface (TS 29.212)
	AVPCode3GPPBearerUsage                       types.AVPCode = 1000
	AVPCode3GPPChargingRuleInstall               types.AVPCode = 1001
	AVPCode3GPPChargingRuleRemove                types.AVPCode = 1002
	AVPCode3GPPChargingRuleDefinition            types.AVPCode = 1003
	AVPCode3GPPChargingRuleName                  types.AVPCode = 1005
	AVPCode3GPPChargingRuleBaseName              types.AVPCode = 1004
	AVPCode3GPPEventTrigger                      types.AVPCode = 1006
	AVPCode3GPPMeteringMethod                    types.AVPCode = 1007
	AVPCode3GPPOffline                           types.AVPCode = 1008
	AVPCode3GPPOnline                            types.AVPCode = 1009
	AVPCode3GPPPrecedence                        types.AVPCode = 1010
	AVPCode3GPPReportingLevel                    types.AVPCode = 1011
	AVPCode3GPPChargingKey                       types.AVPCode = 1012
	AVPCode3GPPQCI                               types.AVPCode = 1028
	AVPCode3GPPAllocationRetentionPriority       types.AVPCode = 1034
	AVPCode3GPPPreEmptionCapability              types.AVPCode = 1047
	AVPCode3GPPPreEmptionVulnerability           types.AVPCode = 1048
	AVPCode3GPPDefaultEPSBearerQoS               types.AVPCode = 1049
	AVPCode3GPPANGWAddress                       types.AVPCode = 1050
	AVPCode3GPPBearerIdentifier                  types.AVPCode = 1020
	AVPCode3GPPBearerOperation                   types.AVPCode = 1021
	AVPCode3GPPAccessNetworkChargingIdentifierGx types.AVPCode = 1022
	AVPCode3GPPBearerControlMode                 types.AVPCode = 1023
	AVPCode3GPPNetworkRequestSupport             types.AVPCode = 1024
	AVPCode3GPPGuaranteedBitrateUL               types.AVPCode = 1025
	AVPCode3GPPGuaranteedBitrateDL               types.AVPCode = 1026
	AVPCode3GPPIPCANType                         types.AVPCode = 1027
	AVPCode3GPPMaxRequestedBandwidthDL           types.AVPCode = 515
	AVPCode3GPPMaxRequestedBandwidthUL           types.AVPCode = 516
	AVPCode3GPPDefaultQoSInformation             types.AVPCode = 2816
	AVPCode3GPPANTrusted                         types.AVPCode = 1503

	// Rx Interface (TS 29.214)
	AVPCode3GPPMediaComponentDescription          types.AVPCode = 517
	AVPCode3GPPMediaComponentNumber               types.AVPCode = 518
	AVPCode3GPPMediaSubComponent                  types.AVPCode = 519
	AVPCode3GPPMediaType                          types.AVPCode = 520
	AVPCode3GPPMaxRequestedBandwidth              types.AVPCode = 521
	AVPCode3GPPFlowDescription                    types.AVPCode = 507
	AVPCode3GPPFlowNumber                         types.AVPCode = 509
	AVPCode3GPPFlowStatus                         types.AVPCode = 511
	AVPCode3GPPFlowUsage                          types.AVPCode = 512
	AVPCode3GPPSpecificAction                     types.AVPCode = 513
	AVPCode3GPPAFChargingIdentifier               types.AVPCode = 505
	AVPCode3GPPSponsorIdentity                    types.AVPCode = 531
	AVPCode3GPPApplicationServiceProviderIdentity types.AVPCode = 532
)

// 3GPP Command Codes
const (
	// Cx/Dx Interface (TS 29.229)
	CmdCode3GPPUserAuthorization        types.CommandCode = 300
	CmdCode3GPPServerAssignment         types.CommandCode = 301
	CmdCode3GPPLocationInfo             types.CommandCode = 302
	CmdCode3GPPMultimediaAuthentication types.CommandCode = 303
	CmdCode3GPPRegistrationTermination  types.CommandCode = 304
	CmdCode3GPPPushProfile              types.CommandCode = 305

	// S6a/S6d Interface (TS 29.272)
	CmdCode3GPPUpdateLocation            types.CommandCode = 316
	CmdCode3GPPCancelLocation            types.CommandCode = 317
	CmdCode3GPPAuthenticationInformation types.CommandCode = 318
	CmdCode3GPPInsertSubscriberData      types.CommandCode = 319
	CmdCode3GPPDeleteSubscriberData      types.CommandCode = 320
	CmdCode3GPPPurgeUE                   types.CommandCode = 321
	CmdCode3GPPReset                     types.CommandCode = 322
	CmdCode3GPPNotify                    types.CommandCode = 323

	// Gx Interface (TS 29.212)
	CmdCode3GPPCreditControl types.CommandCode = 272 // Same as RFC 4006

	// Rx Interface (TS 29.214)
	CmdCode3GPPAARequest          types.CommandCode = 265
	CmdCode3GPPSessionTermination types.CommandCode = 275 // Same as base
	CmdCode3GPPAbortSession       types.CommandCode = 274 // Same as base
)

// Load3GPPCx loads the 3GPP Cx/Dx interface dictionary (TS 29.229).
func (d *Dictionary) Load3GPPCx() error {
	// Add Cx application
	if err := d.AddApplication(&Application{
		ID:       types.AppID3GPPCx,
		Name:     "3GPP Cx",
		VendorID: types.VendorID3GPP,
	}); err != nil {
		return err
	}

	// Get types
	utf8String, _ := d.GetTypeByName("UTF8String")
	unsigned32, _ := d.GetTypeByName("Unsigned32")
	grouped, _ := d.GetTypeByName("Grouped")
	octetString, _ := d.GetTypeByName("OctetString")
	enumerated, _ := d.GetTypeByName("Enumerated")

	// Cx AVPs
	cxAVPs := []*AVP{
		{Code: AVPCode3GPPVisitedNetworkID, Name: "Visited-Network-Identifier", VendorID: types.VendorID3GPP, Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPPublicIdentity, Name: "Public-Identity", VendorID: types.VendorID3GPP, Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPServerName, Name: "Server-Name", VendorID: types.VendorID3GPP, Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPServerCapabilities, Name: "Server-Capabilities", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPMandatoryCapability, Name: "Mandatory-Capability", VendorID: types.VendorID3GPP, Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPOptionalCapability, Name: "Optional-Capability", VendorID: types.VendorID3GPP, Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPUserData, Name: "User-Data", VendorID: types.VendorID3GPP, Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPSIPNumberAuthItems, Name: "SIP-Number-Auth-Items", VendorID: types.VendorID3GPP, Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPSIPAuthDataItem, Name: "SIP-Auth-Data-Item", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPSIPAuthScheme, Name: "SIP-Authentication-Scheme", VendorID: types.VendorID3GPP, Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPSIPAuthenticate, Name: "SIP-Authenticate", VendorID: types.VendorID3GPP, Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPSIPAuthorization, Name: "SIP-Authorization", VendorID: types.VendorID3GPP, Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPUserAuthType, Name: "User-Authorization-Type", VendorID: types.VendorID3GPP, Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPServerAssignType, Name: "Server-Assignment-Type", VendorID: types.VendorID3GPP, Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPDeregistrationReason, Name: "Deregistration-Reason", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPReasonCode, Name: "Reason-Code", VendorID: types.VendorID3GPP, Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPReasonInfo, Name: "Reason-Info", VendorID: types.VendorID3GPP, Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPChargingInformation, Name: "Charging-Information", VendorID: types.VendorID3GPP, Type: grouped},
	}

	for _, avp := range cxAVPs {
		if err := d.AddAVP(avp); err != nil {
			return err
		}
	}

	// Cx Commands
	cxCommands := []*Command{
		{Code: CmdCode3GPPUserAuthorization, Name: "User-Authorization-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPUserAuthorization, Name: "User-Authorization-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPServerAssignment, Name: "Server-Assignment-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPServerAssignment, Name: "Server-Assignment-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPLocationInfo, Name: "Location-Info-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPLocationInfo, Name: "Location-Info-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPMultimediaAuthentication, Name: "Multimedia-Auth-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPMultimediaAuthentication, Name: "Multimedia-Auth-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPRegistrationTermination, Name: "Registration-Termination-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPRegistrationTermination, Name: "Registration-Termination-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPPushProfile, Name: "Push-Profile-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPCx},
		{Code: CmdCode3GPPPushProfile, Name: "Push-Profile-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPCx},
	}

	for _, cmd := range cxCommands {
		if err := d.AddCommand(cmd); err != nil {
			return err
		}
	}

	return nil
}

// Load3GPPS6a loads the 3GPP S6a/S6d interface dictionary (TS 29.272).
func (d *Dictionary) Load3GPPS6a() error {
	// Add S6a application
	if err := d.AddApplication(&Application{
		ID:       types.AppID3GPPS6a,
		Name:     "3GPP S6a/S6d",
		VendorID: types.VendorID3GPP,
	}); err != nil {
		return err
	}

	// Get types
	utf8String, _ := d.GetTypeByName("UTF8String")
	unsigned32, _ := d.GetTypeByName("Unsigned32")
	grouped, _ := d.GetTypeByName("Grouped")
	octetString, _ := d.GetTypeByName("OctetString")
	enumerated, _ := d.GetTypeByName("Enumerated")
	address, _ := d.GetTypeByName("Address")

	// S6a AVPs (subset of most commonly used)
	s6aAVPs := []*AVP{
		{Code: AVPCode3GPPSubscriptionData, Name: "Subscription-Data", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPTerminalInformation, Name: "Terminal-Information", VendorID: types.VendorID3GPP, Type: grouped},
		{Code: AVPCode3GPPIMEI, Name: "IMEI", VendorID: types.VendorID3GPP, Type: utf8String},
		{Code: AVPCode3GPPSoftwareVersion, Name: "Software-Version", VendorID: types.VendorID3GPP, Type: utf8String},
		{Code: AVPCode3GPPULRFlags, Name: "ULR-Flags", VendorID: types.VendorID3GPP, Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPULAFlags, Name: "ULA-Flags", VendorID: types.VendorID3GPP, Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPVisitedPLMNID, Name: "Visited-PLMN-ID", VendorID: types.VendorID3GPP, Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPRequestedEUTRANAuthInfo, Name: "Requested-EUTRAN-Authentication-Info", VendorID: types.VendorID3GPP, Type: grouped},
		{Code: AVPCode3GPPNumberOfRequestedVectors, Name: "Number-Of-Requested-Vectors", VendorID: types.VendorID3GPP, Type: unsigned32},
		{Code: AVPCode3GPPAuthenticationInfo, Name: "Authentication-Info", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPEUTRANVector, Name: "E-UTRAN-Vector", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPRand, Name: "RAND", VendorID: types.VendorID3GPP, Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPXRES, Name: "XRES", VendorID: types.VendorID3GPP, Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPAUTN, Name: "AUTN", VendorID: types.VendorID3GPP, Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPKASME, Name: "KASME", VendorID: types.VendorID3GPP, Type: octetString, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPNetworkAccessMode, Name: "Network-Access-Mode", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPCancellationType, Name: "Cancellation-Type", VendorID: types.VendorID3GPP, Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPSubscriberStatus, Name: "Subscriber-Status", VendorID: types.VendorID3GPP, Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPAPNConfigurationProfile, Name: "APN-Configuration-Profile", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPAPNConfiguration, Name: "APN-Configuration", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPContextIdentifier, Name: "Context-Identifier", VendorID: types.VendorID3GPP, Type: unsigned32, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPPDNType, Name: "PDN-Type", VendorID: types.VendorID3GPP, Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPAMBr, Name: "AMBR", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPEPSSubscribedQoSProfile, Name: "EPS-Subscribed-QoS-Profile", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPQCI, Name: "QoS-Class-Identifier", VendorID: types.VendorID3GPP, Type: enumerated, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPAllocationRetentionPriority, Name: "Allocation-Retention-Priority", VendorID: types.VendorID3GPP, Type: grouped, Flags: AVPFlags{Mandatory: true}},
		{Code: AVPCode3GPPTraceData, Name: "Trace-Data", VendorID: types.VendorID3GPP, Type: grouped},
		{Code: 1471, Name: "MIP6-Agent-Info", VendorID: types.VendorID3GPP, Type: grouped},
		{Code: 1470, Name: "Service-Selection", VendorID: types.VendorIDIETF, Type: utf8String, Flags: AVPFlags{Mandatory: true}},
		{Code: 1503, Name: "AN-Trusted", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: 1472, Name: "MIP-Home-Agent-Address", VendorID: types.VendorIDIETF, Type: address, Flags: AVPFlags{Mandatory: true}},
	}

	for _, avp := range s6aAVPs {
		if err := d.AddAVP(avp); err != nil {
			// Ignore duplicate errors for shared AVPs
			continue
		}
	}

	// S6a Commands
	s6aCommands := []*Command{
		{Code: CmdCode3GPPUpdateLocation, Name: "Update-Location-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPUpdateLocation, Name: "Update-Location-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPCancelLocation, Name: "Cancel-Location-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPCancelLocation, Name: "Cancel-Location-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPAuthenticationInformation, Name: "Authentication-Information-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPAuthenticationInformation, Name: "Authentication-Information-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPInsertSubscriberData, Name: "Insert-Subscriber-Data-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPInsertSubscriberData, Name: "Insert-Subscriber-Data-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPDeleteSubscriberData, Name: "Delete-Subscriber-Data-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPDeleteSubscriberData, Name: "Delete-Subscriber-Data-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPPurgeUE, Name: "Purge-UE-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPPurgeUE, Name: "Purge-UE-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPNotify, Name: "Notify-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPS6a},
		{Code: CmdCode3GPPNotify, Name: "Notify-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPS6a},
	}

	for _, cmd := range s6aCommands {
		if err := d.AddCommand(cmd); err != nil {
			return err
		}
	}

	return nil
}

// Load3GPPGx loads the 3GPP Gx interface dictionary (TS 29.212).
func (d *Dictionary) Load3GPPGx() error {
	// Add Gx application
	if err := d.AddApplication(&Application{
		ID:       types.AppID3GPPGx,
		Name:     "3GPP Gx",
		VendorID: types.VendorID3GPP,
	}); err != nil {
		return err
	}

	// Get types
	utf8String, _ := d.GetTypeByName("UTF8String")
	unsigned32, _ := d.GetTypeByName("Unsigned32")
	grouped, _ := d.GetTypeByName("Grouped")
	octetString, _ := d.GetTypeByName("OctetString")
	enumerated, _ := d.GetTypeByName("Enumerated")
	address, _ := d.GetTypeByName("Address")

	// Gx AVPs
	gxAVPs := []*AVP{
		{Code: AVPCode3GPPBearerUsage, Name: "Bearer-Usage", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPChargingRuleInstall, Name: "Charging-Rule-Install", VendorID: types.VendorID3GPP, Type: grouped},
		{Code: AVPCode3GPPChargingRuleRemove, Name: "Charging-Rule-Remove", VendorID: types.VendorID3GPP, Type: grouped},
		{Code: AVPCode3GPPChargingRuleDefinition, Name: "Charging-Rule-Definition", VendorID: types.VendorID3GPP, Type: grouped},
		{Code: AVPCode3GPPChargingRuleName, Name: "Charging-Rule-Name", VendorID: types.VendorID3GPP, Type: octetString},
		{Code: AVPCode3GPPChargingRuleBaseName, Name: "Charging-Rule-Base-Name", VendorID: types.VendorID3GPP, Type: utf8String},
		{Code: AVPCode3GPPEventTrigger, Name: "Event-Trigger", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPMeteringMethod, Name: "Metering-Method", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPOffline, Name: "Offline", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPOnline, Name: "Online", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPPrecedence, Name: "Precedence", VendorID: types.VendorID3GPP, Type: unsigned32},
		{Code: AVPCode3GPPReportingLevel, Name: "Reporting-Level", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPChargingKey, Name: "Charging-Key", VendorID: types.VendorID3GPP, Type: unsigned32},
		{Code: AVPCode3GPPPreEmptionCapability, Name: "Pre-emption-Capability", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPPreEmptionVulnerability, Name: "Pre-emption-Vulnerability", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPDefaultEPSBearerQoS, Name: "Default-EPS-Bearer-QoS", VendorID: types.VendorID3GPP, Type: grouped},
		{Code: AVPCode3GPPANGWAddress, Name: "AN-GW-Address", VendorID: types.VendorID3GPP, Type: address},
		{Code: AVPCode3GPPBearerIdentifier, Name: "Bearer-Identifier", VendorID: types.VendorID3GPP, Type: octetString},
		{Code: AVPCode3GPPBearerOperation, Name: "Bearer-Operation", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPBearerControlMode, Name: "Bearer-Control-Mode", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPNetworkRequestSupport, Name: "Network-Request-Support", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPGuaranteedBitrateUL, Name: "Guaranteed-Bitrate-UL", VendorID: types.VendorID3GPP, Type: unsigned32},
		{Code: AVPCode3GPPGuaranteedBitrateDL, Name: "Guaranteed-Bitrate-DL", VendorID: types.VendorID3GPP, Type: unsigned32},
		{Code: AVPCode3GPPIPCANType, Name: "IP-CAN-Type", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPMaxRequestedBandwidthDL, Name: "Max-Requested-Bandwidth-DL", VendorID: types.VendorID3GPP, Type: unsigned32},
		{Code: AVPCode3GPPMaxRequestedBandwidthUL, Name: "Max-Requested-Bandwidth-UL", VendorID: types.VendorID3GPP, Type: unsigned32},
	}

	for _, avp := range gxAVPs {
		if err := d.AddAVP(avp); err != nil {
			// Ignore duplicates - some AVPs are shared with S6a
			continue
		}
	}

	// Gx uses Credit-Control commands (CCR/CCA) with Gx application ID
	// Commands already added via LoadCreditControl()

	return nil
}

// Load3GPPRx loads the 3GPP Rx interface dictionary (TS 29.214).
func (d *Dictionary) Load3GPPRx() error {
	// Add Rx application
	if err := d.AddApplication(&Application{
		ID:       types.AppID3GPPRx,
		Name:     "3GPP Rx",
		VendorID: types.VendorID3GPP,
	}); err != nil {
		return err
	}

	// Get types
	utf8String, _ := d.GetTypeByName("UTF8String")
	unsigned32, _ := d.GetTypeByName("Unsigned32")
	grouped, _ := d.GetTypeByName("Grouped")
	enumerated, _ := d.GetTypeByName("Enumerated")

	// Rx AVPs
	rxAVPs := []*AVP{
		{Code: AVPCode3GPPMediaComponentDescription, Name: "Media-Component-Description", VendorID: types.VendorID3GPP, Type: grouped},
		{Code: AVPCode3GPPMediaComponentNumber, Name: "Media-Component-Number", VendorID: types.VendorID3GPP, Type: unsigned32},
		{Code: AVPCode3GPPMediaSubComponent, Name: "Media-Sub-Component", VendorID: types.VendorID3GPP, Type: grouped},
		{Code: AVPCode3GPPMediaType, Name: "Media-Type", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPFlowDescription, Name: "Flow-Description", VendorID: types.VendorID3GPP, Type: utf8String},
		{Code: AVPCode3GPPFlowNumber, Name: "Flow-Number", VendorID: types.VendorID3GPP, Type: unsigned32},
		{Code: AVPCode3GPPFlowStatus, Name: "Flow-Status", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPFlowUsage, Name: "Flow-Usage", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPSpecificAction, Name: "Specific-Action", VendorID: types.VendorID3GPP, Type: enumerated},
		{Code: AVPCode3GPPAFChargingIdentifier, Name: "AF-Charging-Identifier", VendorID: types.VendorID3GPP, Type: utf8String},
		{Code: AVPCode3GPPSponsorIdentity, Name: "Sponsor-Identity", VendorID: types.VendorID3GPP, Type: utf8String},
	}

	for _, avp := range rxAVPs {
		if err := d.AddAVP(avp); err != nil {
			continue
		}
	}

	// Rx Commands (AAR/AAA, STR/STA, ASR/ASA are from base protocol with Rx app ID)
	rxCommands := []*Command{
		{Code: CmdCode3GPPAARequest, Name: "AA-Request", Flags: CommandFlags{Request: true, Proxiable: true}, AppID: types.AppID3GPPRx},
		{Code: CmdCode3GPPAARequest, Name: "AA-Answer", Flags: CommandFlags{Request: false, Proxiable: true}, AppID: types.AppID3GPPRx},
	}

	for _, cmd := range rxCommands {
		if err := d.AddCommand(cmd); err != nil {
			continue
		}
	}

	return nil
}

// LoadAll3GPP loads all 3GPP dictionaries.
func (d *Dictionary) LoadAll3GPP() error {
	if err := d.Load3GPPCx(); err != nil {
		return err
	}
	if err := d.Load3GPPS6a(); err != nil {
		return err
	}
	if err := d.Load3GPPGx(); err != nil {
		return err
	}
	if err := d.Load3GPPRx(); err != nil {
		return err
	}
	return nil
}

// Load3GPP is a convenience method that calls LoadAll3GPP.
func (d *Dictionary) Load3GPP() error {
	return d.LoadAll3GPP()
}
