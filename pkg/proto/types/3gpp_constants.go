// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package types defines core Diameter protocol types.
package types

// 3GPP AVP Codes (from TS 29.272, 29.229, etc)
const (
	// S6a AVPs
	AVPCode3GPPSubscriptionData         AVPCode = 1400
	AVPCode3GPPTerminalInformation      AVPCode = 1401
	AVPCode3GPPIMEI                     AVPCode = 1402
	AVPCode3GPPSoftwareVersion          AVPCode = 1403
	AVPCode3GPPQoSSubscribed            AVPCode = 1404
	AVPCode3GPPULRFlags                 AVPCode = 1405
	AVPCode3GPPULAFlags                 AVPCode = 1406
	AVPCode3GPPVisitedPLMNID            AVPCode = 1407
	AVPCode3GPPRequestedEUTRANAuthInfo  AVPCode = 1408
	AVPCode3GPPNumberOfRequestedVectors AVPCode = 1410
	AVPCode3GPPAuthenticationInfo       AVPCode = 1413
	AVPCode3GPPNetworkAccessMode        AVPCode = 1417
	AVPCode3GPPCancellationType         AVPCode = 1420
	AVPCode3GPPContextIdentifier        AVPCode = 1423
	AVPCode3GPPSubscriberStatus         AVPCode = 1424
	AVPCode3GPPAPNConfigurationProfile  AVPCode = 1429
	AVPCode3GPPAPNConfiguration         AVPCode = 1430
	AVPCode3GPPEPSSubscribedQoSProfile  AVPCode = 1431
	AVPCode3GPPAMBr                     AVPCode = 1435
	AVPCode3GPPPDNType                  AVPCode = 1438
	AVPCode3GPPAllAPNConfigsIncluded    AVPCode = 1428
	AVPCode3GPPMaxRequestedBandwidthDL  AVPCode = 515
	AVPCode3GPPMaxRequestedBandwidthUL  AVPCode = 516
)

// 3GPP Command Codes
const (
	CmdCode3GPPUpdateLocation CommandCode = 316
)
