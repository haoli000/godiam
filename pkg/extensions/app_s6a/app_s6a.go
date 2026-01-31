// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package app_s6a provides the Diameter S6a application extension for LTE subscriber management.
package app_s6a

import (
	"log"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type appS6a struct{}

// Name returns the extension name.
func (e *appS6a) Name() string { return "app_s6a" }

// Init initializes the extension.
func (e *appS6a) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: app_s6a (3GPP S6a/S6d)")

	// Register dispatch handler for 3GPP S6a Application (16777251)
	ctx.GetRouter().RegisterDispatchHandler(types.AppID3GPPS6a, handleS6a)

	return nil
}

func handleS6a(p *peer.Peer, msg *message.Message) error {
	if !msg.IsRequest() {
		return nil
	}

	switch msg.CommandCode {
	case dictionary.CmdCode3GPPUpdateLocation:
		return handleULR(p, msg)
	case dictionary.CmdCode3GPPAuthenticationInformation:
		return handleAIR(p, msg)
	case dictionary.CmdCode3GPPPurgeUE:
		return handlePurge(p, msg)
	default:
		log.Printf("Unhandled S6a command: %d", msg.CommandCode)
		return nil
	}
}

func handleULR(p *peer.Peer, msg *message.Message) error {
	log.Printf("ULR received from %s", p.DiameterID())

	// Build ULA
	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)

	// Session-ID
	if sessionID, ok := msg.GetSessionID(); ok {
		ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))
	}

	// Origin
	localCfg := peer.GetLocalConfig()
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))

	// Vendor-Specific-Application-ID
	vapp := message.NewGroupedAVP(types.AVPCodeVendorSpecificAppID, types.AVPFlagMandatory, 0)
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(types.VendorID3GPP)))
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppID3GPPS6a)))
	ans.AddAVP(vapp)

	// ULA-Flags (all zero for now)
	ans.AddAVP(message.NewUnsigned32AVP(dictionary.AVPCode3GPPULAFlags, types.AVPFlagMandatory|types.AVPFlagVendor, 0).SetVendorID(types.VendorID3GPP))

	// Mock Subscription-Data
	sub := message.NewGroupedAVP(dictionary.AVPCode3GPPSubscriptionData, types.AVPFlagMandatory|types.AVPFlagVendor, types.VendorID3GPP)
	sub.AddChild(message.NewEnumeratedAVP(dictionary.AVPCode3GPPSubscriberStatus, types.AVPFlagMandatory|types.AVPFlagVendor, 0).SetVendorID(types.VendorID3GPP))  // SERVICE_GRANTED
	sub.AddChild(message.NewUnsigned32AVP(dictionary.AVPCode3GPPNetworkAccessMode, types.AVPFlagMandatory|types.AVPFlagVendor, 2).SetVendorID(types.VendorID3GPP)) // ONLY_EUTRAN
	ans.AddAVP(sub)

	if err := p.Send(ans); err != nil {
		log.Printf("Failed to send ULA: %v", err)
		return err
	}

	return nil
}

func handleAIR(p *peer.Peer, msg *message.Message) error {
	log.Printf("AIR received from %s", p.DiameterID())

	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)

	// Session-ID
	if sessionID, ok := msg.GetSessionID(); ok {
		ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))
	}

	// Origin
	localCfg := peer.GetLocalConfig()
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))

	// Vendor-Specific-Application-ID
	vapp := message.NewGroupedAVP(types.AVPCodeVendorSpecificAppID, types.AVPFlagMandatory, 0)
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(types.VendorID3GPP)))
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppID3GPPS6a)))
	ans.AddAVP(vapp)

	// Mock Authentication-Info (E-UTRAN Vector)
	authInfo := message.NewGroupedAVP(dictionary.AVPCode3GPPAuthenticationInfo, types.AVPFlagMandatory|types.AVPFlagVendor, types.VendorID3GPP)
	vector := message.NewGroupedAVP(dictionary.AVPCode3GPPEUTRANVector, types.AVPFlagMandatory|types.AVPFlagVendor, types.VendorID3GPP)
	vector.AddChild(message.NewOctetStringAVP(dictionary.AVPCode3GPPRand, types.AVPFlagMandatory|types.AVPFlagVendor, []byte("dummy-rand-16bytes-")).SetVendorID(types.VendorID3GPP))
	vector.AddChild(message.NewOctetStringAVP(dictionary.AVPCode3GPPAUTN, types.AVPFlagMandatory|types.AVPFlagVendor, []byte("dummy-autn-16bytes-")).SetVendorID(types.VendorID3GPP))
	vector.AddChild(message.NewOctetStringAVP(dictionary.AVPCode3GPPXRES, types.AVPFlagMandatory|types.AVPFlagVendor, []byte("dummy-xres-8bytes")).SetVendorID(types.VendorID3GPP))
	vector.AddChild(message.NewOctetStringAVP(dictionary.AVPCode3GPPKASME, types.AVPFlagMandatory|types.AVPFlagVendor, []byte("dummy-kasme-32bytes-long-secret")).SetVendorID(types.VendorID3GPP))
	authInfo.AddChild(vector)
	ans.AddAVP(authInfo)

	if err := p.Send(ans); err != nil {
		log.Printf("Failed to send AIA: %v", err)
		return err
	}

	return nil
}

func handlePurge(p *peer.Peer, msg *message.Message) error {
	log.Printf("Purge-UE received from %s", p.DiameterID())
	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)
	return p.Send(ans)
}

// Stop implements the Stoppable interface.
func (e *appS6a) Stop() error {
	return nil
}

func init() {
	extension.Register(&appS6a{})
}
