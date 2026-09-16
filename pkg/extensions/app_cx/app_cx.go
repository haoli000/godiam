// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package app_cx provides the Diameter Cx/Dx application extension for IMS.
package app_cx

import (
	"log"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type appCx struct{}

// Name returns the extension name.
func (e *appCx) Name() string { return "app_cx" }

// Init initializes the extension.
func (e *appCx) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: app_cx (3GPP Cx)")

	// Register dispatch handler for 3GPP Cx Application (16777216)
	ctx.GetRouter().RegisterDispatchHandler(types.AppID3GPPCx, handleCx)
	// Register dispatch handler for NASREQ (1)
	ctx.GetRouter().RegisterDispatchHandler(types.AppIDNASREQ, handleNASREQ)

	return nil
}

func handleNASREQ(p *peer.Peer, msg *message.Message) error {
	if !msg.IsRequest() {
		return nil
	}

	log.Printf("NASREQ request received: Code=%d from %s", msg.CommandCode, p.DiameterID())

	// Build AA-Answer
	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)

	// Copy Session-ID
	if sessionID, ok := msg.GetSessionID(); ok {
		ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))
	}

	// Add Origin-Host and Origin-Realm
	localCfg := peer.GetLocalConfig()
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))

	if err := p.Send(ans); err != nil {
		log.Printf("Failed to send NASREQ answer: %v", err)
		return err
	}

	return nil
}

func handleCx(p *peer.Peer, msg *message.Message) error {
	if !msg.IsRequest() {
		return nil
	}

	switch msg.CommandCode {
	case dictionary.CmdCode3GPPUserAuthorization:
		return handleUAR(p, msg)
	case dictionary.CmdCode3GPPMultimediaAuthentication:
		return handleMAR(p, msg)
	case dictionary.CmdCode3GPPServerAssignment:
		return handleSAR(p, msg)
	case dictionary.CmdCode3GPPLocationInfo:
		return handleLIR(p, msg)
	default:
		log.Printf("Unhandled Cx command: %d", msg.CommandCode)
		return nil
	}
}

// Every Diameter answer must carry Origin-Host and Origin-Realm (RFC 6733
// 6.2), and a strict peer rejects one that does not. NewAnswer deliberately
// leaves them to the handler, so the bare answers below have to stamp them.
func addAnswerIdentity(ans, req *message.Message) {
	if sessionID, ok := req.GetSessionID(); ok {
		ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))
	}
	localCfg := peer.GetLocalConfig()
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))
}

func handleUAR(p *peer.Peer, msg *message.Message) error {
	log.Printf("UAR received from %s", p.DiameterID())

	// Build UAA
	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)

	// Copy Session-ID
	if sessionID, ok := msg.GetSessionID(); ok {
		ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))
	}

	// Add Origin-Host and Origin-Realm
	localCfg := peer.GetLocalConfig()
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))

	// TS 29.229: User-Authorization-Answer may contain Experimental-Result-Code
	// or Result-Code. For success, we use Result-Code=2001.

	// Add Vendor-Specific-Application-ID
	vapp := message.NewGroupedAVP(types.AVPCodeVendorSpecificAppID, types.AVPFlagMandatory, 0)
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(types.VendorID3GPP)))
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppID3GPPCx)))
	ans.AddAVP(vapp)

	if err := p.Send(ans); err != nil {
		log.Printf("Failed to send UAA: %v", err)
		return err
	}

	return nil
}

func handleMAR(p *peer.Peer, msg *message.Message) error {
	log.Printf("MAR received from %s", p.DiameterID())
	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)
	addAnswerIdentity(ans, msg)
	return p.Send(ans)
}

func handleSAR(p *peer.Peer, msg *message.Message) error {
	log.Printf("SAR received from %s", p.DiameterID())
	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)
	addAnswerIdentity(ans, msg)
	return p.Send(ans)
}

func handleLIR(p *peer.Peer, msg *message.Message) error {
	log.Printf("LIR received from %s", p.DiameterID())
	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)
	addAnswerIdentity(ans, msg)
	return p.Send(ans)
}

// Stop implements the Stoppable interface.
func (e *appCx) Stop() error {
	return nil
}

func init() {
	extension.Register(&appCx{})
}
