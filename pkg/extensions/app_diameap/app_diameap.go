// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package app_diameap provides the Diameter EAP application extension.
package app_diameap

import (
	"log"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type appDiamEAP struct{}

// Name returns the extension name.
func (e *appDiamEAP) Name() string { return "app_diameap" }

// Init initializes the extension.
func (e *appDiamEAP) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: app_diameap (Diameter EAP)")

	// Register dispatch handler for EAP Application (5)
	ctx.GetRouter().RegisterDispatchHandler(types.AppIDEAP, handleEAP)

	return nil
}

func handleEAP(p *peer.Peer, msg *message.Message) error {
	if !msg.IsRequest() {
		return nil
	}

	log.Printf("DER (EAP Request) received from %s", p.DiameterID())

	// Build DEA (EAP Answer)
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

	// For a real EAP implementation, we would extract the EAP-Payload,
	// pass it to an EAP state machine (or backend like RADIUS),
	// and inclue the EAP-Payload in the response.

	if eapPayload := msg.FindAVP(types.AVPCodeEAPPayload, 0); eapPayload != nil {
		log.Printf("EAP payload found (len=%d)", len(eapPayload.Data))
		// Pseudo-response: just echo or move forward
		ans.AddAVP(eapPayload)
	}

	// Add Auth-Application-ID
	ans.AddAVP(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppIDEAP)))

	if err := p.Send(ans); err != nil {
		log.Printf("Failed to send DEA: %v", err)
		return err
	}

	return nil
}

// Stop implements the Stoppable interface.
func (e *appDiamEAP) Stop() error {
	return nil
}

func init() {
	extension.Register(&appDiamEAP{})
}
