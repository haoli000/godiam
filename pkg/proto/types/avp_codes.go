// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package types defines core Diameter protocol types.
package types

// Base Protocol AVP Codes (RFC 6733)
const (
	// Session-related AVPs
	AVPCodeSessionID            AVPCode = 263
	AVPCodeOriginHost           AVPCode = 264
	AVPCodeOriginRealm          AVPCode = 296
	AVPCodeDestinationHost      AVPCode = 293
	AVPCodeDestinationRealm     AVPCode = 283
	AVPCodeAuthApplicationID    AVPCode = 258
	AVPCodeAcctApplicationID    AVPCode = 259
	AVPCodeVendorSpecificAppID  AVPCode = 260
	AVPCodeRedirectHostUsage    AVPCode = 261
	AVPCodeRedirectMaxCacheTime AVPCode = 262

	// Result/Error AVPs
	AVPCodeResultCode             AVPCode = 268
	AVPCodeExperimentalResult     AVPCode = 297
	AVPCodeExperimentalResultCode AVPCode = 298
	AVPCodeErrorMessage           AVPCode = 281
	AVPCodeErrorReportingHost     AVPCode = 294
	AVPCodeFailedAVP              AVPCode = 279

	// Capability Exchange AVPs
	AVPCodeHostIPAddress     AVPCode = 257
	AVPCodeVendorID          AVPCode = 266
	AVPCodeProductName       AVPCode = 269
	AVPCodeOriginStateID     AVPCode = 278
	AVPCodeSupportedVendorID AVPCode = 265
	AVPCodeFirmwareRevision  AVPCode = 267
	AVPCodeInbandSecurityID  AVPCode = 299

	// Routing AVPs
	AVPCodeRouteRecord  AVPCode = 282
	AVPCodeProxyInfo    AVPCode = 284
	AVPCodeProxyHost    AVPCode = 280
	AVPCodeProxyState   AVPCode = 33
	AVPCodeRedirectHost AVPCode = 292

	// Authentication AVPs
	AVPCodeUserName          AVPCode = 1
	AVPCodeAuthRequestType   AVPCode = 274
	AVPCodeAuthGracePeriod   AVPCode = 276
	AVPCodeAuthSessionState  AVPCode = 277
	AVPCodeReAuthRequestType AVPCode = 285
	AVPCodeClass             AVPCode = 25

	// Authorization AVPs
	AVPCodeAuthorizationLifetime AVPCode = 291
	AVPCodeSessionTimeout        AVPCode = 27

	// Accounting AVPs
	AVPCodeAccountingRecordType       AVPCode = 480
	AVPCodeAccountingRecordNumber     AVPCode = 485
	AVPCodeAccountingSubSessionID     AVPCode = 287
	AVPCodeAccountingSessionID        AVPCode = 44
	AVPCodeAcctMultiSessionID         AVPCode = 50
	AVPCodeAcctInterimInterval        AVPCode = 85
	AVPCodeAccountingRealtimeRequired AVPCode = 483
	AVPCodeEventTimestamp             AVPCode = 55

	// Disconnect/Watchdog AVPs
	AVPCodeDisconnectCause  AVPCode = 273
	AVPCodeTerminationCause AVPCode = 295

	// Other Base AVPs
	AVPCodeMultiRoundTimeOut AVPCode = 272
)

// Base Protocol Command Codes (RFC 6733)
const (
	CmdCodeCapabilitiesExchange CommandCode = 257 // CER/CEA
	CmdCodeReAuth               CommandCode = 258 // RAR/RAA
	CmdCodeAccounting           CommandCode = 271 // ACR/ACA
	CmdCodeAbortSession         CommandCode = 274 // ASR/ASA
	CmdCodeSessionTermination   CommandCode = 275 // STR/STA
	CmdCodeDeviceWatchdog       CommandCode = 280 // DWR/DWA
	CmdCodeDisconnectPeer       CommandCode = 282 // DPR/DPA
)

// Credit Control AVP Codes (RFC 4006)
const (
	AVPCodeCCRequestType                 AVPCode = 416
	AVPCodeCCRequestNumber               AVPCode = 415
	AVPCodeCCSessionFailover             AVPCode = 418
	AVPCodeCCSubSessionID                AVPCode = 419
	AVPCodeCheckBalanceResult            AVPCode = 422
	AVPCodeCostInformation               AVPCode = 423
	AVPCodeCostUnit                      AVPCode = 424
	AVPCodeCreditControl                 AVPCode = 426
	AVPCodeCreditControlFailureHandling  AVPCode = 427
	AVPCodeDirectDebitingFailureHandling AVPCode = 428
	AVPCodeExponent                      AVPCode = 429
	AVPCodeFinalUnitAction               AVPCode = 449
	AVPCodeFinalUnitIndication           AVPCode = 430
	AVPCodeGrantedServiceUnit            AVPCode = 431
	AVPCodeMultipleServicesCreditControl AVPCode = 456
	AVPCodeMultipleServicesIndicator     AVPCode = 455
	AVPCodeRatingGroup                   AVPCode = 432
	AVPCodeRedirectAddressType           AVPCode = 433
	AVPCodeRedirectServer                AVPCode = 434
	AVPCodeRedirectServerAddress         AVPCode = 435
	AVPCodeRequestedAction               AVPCode = 436
	AVPCodeRequestedServiceUnit          AVPCode = 437
	AVPCodeRestrictionFilterRule         AVPCode = 438
	AVPCodeServiceContextID              AVPCode = 461
	AVPCodeServiceIdentifier             AVPCode = 439
	AVPCodeSubscriptionID                AVPCode = 443
	AVPCodeSubscriptionIDData            AVPCode = 444
	AVPCodeSubscriptionIDType            AVPCode = 450
	AVPCodeTariffChangeUsage             AVPCode = 452
	AVPCodeTariffTimeChange              AVPCode = 451
	AVPCodeUnitValue                     AVPCode = 445
	AVPCodeUsedServiceUnit               AVPCode = 446
	AVPCodeUserEquipmentInfo             AVPCode = 458
	AVPCodeUserEquipmentInfoType         AVPCode = 459
	AVPCodeUserEquipmentInfoValue        AVPCode = 460
	AVPCodeValueDigits                   AVPCode = 447
	AVPCodeValidityTime                  AVPCode = 448
)

// Credit Control Command Codes (RFC 4006)
const (
	CmdCodeCreditControl CommandCode = 272 // CCR/CCA
)

// NASREQ Command Codes (RFC 7155)
const (
	CmdCodeAAReq CommandCode = 265 // AAR/AAA
)

// EAP AVP Codes (RFC 4072)
const (
	AVPCodeEAPPayload          AVPCode = 462
	AVPCodeEAPReissuedPayload  AVPCode = 463
	AVPCodeEAPMasterSessionKey AVPCode = 464
	AVPCodeEAPKeyName          AVPCode = 102
)

// EAP Command Codes (RFC 4072)
const (
	CmdCodeEAP CommandCode = 268 // DER/DEA
)

// NAS Request AVP Codes (RFC 7155)
const (
	AVPCodeNASPort             AVPCode = 5
	AVPCodeNASPortID           AVPCode = 87
	AVPCodeNASPortType         AVPCode = 61
	AVPCodeCalledStationID     AVPCode = 30
	AVPCodeCallingStationID    AVPCode = 31
	AVPCodeConnectInfo         AVPCode = 77
	AVPCodeOriginatingLineInfo AVPCode = 94
	AVPCodeReplyMessage        AVPCode = 18
	AVPCodeNASIdentifier       AVPCode = 32
	AVPCodeNASIPAddress        AVPCode = 4
	AVPCodeNASIPv6Address      AVPCode = 95
	AVPCodeState               AVPCode = 24
	AVPCodeFramedIPAddress     AVPCode = 8
	AVPCodeFramedIPv6Prefix    AVPCode = 97
	AVPCodeFramedIPv6Pool      AVPCode = 100
	AVPCodeFramedIPNetmask     AVPCode = 9
	AVPCodeFramedMTU           AVPCode = 12
	AVPCodeFramedProtocol      AVPCode = 7
	AVPCodeFramedRouting       AVPCode = 10
	AVPCodeServiceType         AVPCode = 6
	AVPCodeLoginIPHost         AVPCode = 14
	AVPCodeLoginIPv6Host       AVPCode = 98
	AVPCodeLoginService        AVPCode = 15
	AVPCodeLoginTCPPort        AVPCode = 16
	AVPCodeLoginLATService     AVPCode = 34
	AVPCodeLoginLATNode        AVPCode = 35
	AVPCodeLoginLATGroup       AVPCode = 36
	AVPCodeLoginLATPort        AVPCode = 63
	AVPCodeFilterID            AVPCode = 11
	AVPCodeIdleTimeout         AVPCode = 28
	AVPCodePortLimit           AVPCode = 62
	AVPCodeCallbackNumber      AVPCode = 19
	AVPCodeCallbackID          AVPCode = 20
)

// SIP Command Codes (RFC 4740)
const (
	CmdCodeSIPUserAuthorization CommandCode = 283 // UAR/UAA
	CmdCodeSIPServerAssignment  CommandCode = 284 // SAR/SAA
	CmdCodeSIPLocationInfo      CommandCode = 285 // LIR/LIA
	CmdCodeSIPMultimediaAuth    CommandCode = 286 // MAR/MAA
	CmdCodeSIPPushProfile       CommandCode = 288 // PPR/PPA
)

// SIP AVP Codes (RFC 4740)
const (
	AVPCodeSIPAccountingInformation AVPCode = 368
	AVPCodeSIPAccountingServerURI   AVPCode = 369
	AVPCodeSIPAuthDataItem          AVPCode = 376
	AVPCodeSIPAuthenticationScheme  AVPCode = 377
	AVPCodeSIPAuthenticate          AVPCode = 378
	AVPCodeSIPAuthorization         AVPCode = 379
	AVPCodeSIPAuthenticationInfo    AVPCode = 381
	AVPCodeSIPMemberUser            AVPCode = 371
	AVPCodeSIPMethod                AVPCode = 393
	AVPCodeSIPNumberAuthItems       AVPCode = 382
	AVPCodeSIPReasonData            AVPCode = 390
	AVPCodeSIPReasonInfo            AVPCode = 391
	AVPCodeSIPServerAssignmentType  AVPCode = 375
	AVPCodeSIPServerURI             AVPCode = 372
	AVPCodeSIPUserData              AVPCode = 389
	AVPCodeSIPUserAuthorizationType AVPCode = 387
	AVPCodeSIPUserRoutingInfo       AVPCode = 373
)
