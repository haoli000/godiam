// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package app_gx provides the Diameter Gx application extension for policy and charging control.
package app_gx

import (
	"log"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type appGx struct{}

// Name returns the extension name.
func (e *appGx) Name() string { return "app_gx" }

// Init initializes the extension.
func (e *appGx) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: app_gx (3GPP Gx)")

	// Register dispatch handler for 3GPP Gx Application (16777238)
	ctx.GetRouter().RegisterDispatchHandler(types.AppID3GPPGx, handleGx)

	return nil
}

func handleGx(p *peer.Peer, msg *message.Message) error {
	if !msg.IsRequest() {
		return nil
	}

	if msg.CommandCode != types.CmdCodeCreditControl {
		log.Printf("Unhandled Gx command: %d", msg.CommandCode)
		return nil
	}

	log.Printf("Gx CCR request received from %s", p.DiameterID())

	// Build Gx CCA
	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)

	// Copy mandatory AVPs
	if sessionID, ok := msg.GetSessionID(); ok {
		ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))
	}

	// Add Origin-Host and Origin-Realm
	localCfg := peer.GetLocalConfig()
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))

	// Add Vendor-Specific-Application-ID
	vapp := message.NewGroupedAVP(types.AVPCodeVendorSpecificAppID, types.AVPFlagMandatory, 0)
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(types.VendorID3GPP)))
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppID3GPPGx)))
	ans.AddAVP(vapp)

	// Copy CC-Request-Type and CC-Request-Number
	if ccReqType := msg.FindAVP(types.AVPCodeCCRequestType, 0); ccReqType != nil {
		ans.AddAVP(ccReqType)
	}
	if ccReqNum := msg.FindAVP(types.AVPCodeCCRequestNumber, 0); ccReqNum != nil {
		ans.AddAVP(ccReqNum)
	}

	// In a real PCRF, we would return dynamic rules, event triggers, etc.
	// For now, it's a basic success response.

	if err := p.Send(ans); err != nil {
		log.Printf("Failed to send Gx CCA: %v", err)
		return err
	}

	return nil
}

// Stop implements the Stoppable interface.
func (e *appGx) Stop() error {
	return nil
}

func init() {
	extension.Register(&appGx{})
}
