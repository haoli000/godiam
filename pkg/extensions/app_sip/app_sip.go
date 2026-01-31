// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package app_sip provides the Diameter SIP application extension.
package app_sip

import (
	"log"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type appSIP struct{}

// Name returns the extension name.
func (e *appSIP) Name() string { return "app_sip" }

// Init initializes the extension.
func (e *appSIP) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: app_sip (Diameter SIP)")

	// Register dispatch handler for Diameter SIP Application (6)
	ctx.GetRouter().RegisterDispatchHandler(types.AppIDSIP, handleSIP)

	return nil
}

func handleSIP(p *peer.Peer, msg *message.Message) error {
	if !msg.IsRequest() {
		return nil
	}

	log.Printf("SIP request received: Code=%d from %s", msg.CommandCode, p.DiameterID())

	// Build SIP Answer
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

	// Base SIP answer logic:
	// In a real application, we would check SIP-Method, Auth-Data-Item, etc.
	// For now, we return a simple success.

	if err := p.Send(ans); err != nil {
		log.Printf("Failed to send SIP answer: %v", err)
		return err
	}

	return nil
}

// Stop implements the Stoppable interface.
func (e *appSIP) Stop() error {
	return nil
}

func init() {
	extension.Register(&appSIP{})
}
