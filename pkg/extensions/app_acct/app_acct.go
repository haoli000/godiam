// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package app_acct provides the Diameter accounting application extension.
package app_acct

import (
	"log"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type appAcct struct{}

// Name returns the extension name.
func (e *appAcct) Name() string { return "app_acct" }

// Init initializes the extension.
func (e *appAcct) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: app_acct (Accounting)")

	// Register dispatch handler for Base Accounting Application (3)
	ctx.GetRouter().RegisterDispatchHandler(types.AppIDBaseAccounting, handleAccounting)

	return nil
}

func handleAccounting(p *peer.Peer, msg *message.Message) error {
	if !msg.IsRequest() {
		// We are a client receiving an ACA?
		return nil
	}

	log.Printf("ACR request received from %s", p.DiameterID())

	// Build ACR answer (ACA)
	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)

	// Copy mandatory AVPs
	if sessionID, ok := msg.GetSessionID(); ok {
		ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))
	}

	// Add Origin-Host and Origin-Realm from local config
	localCfg := peer.GetLocalConfig()
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))

	// Copy Accounting-Record-Type and Accounting-Record-Number from request
	if recType := msg.FindAVP(types.AVPCodeAccountingRecordType, 0); recType != nil {
		ans.AddAVP(recType)
	}
	if recNum := msg.FindAVP(types.AVPCodeAccountingRecordNumber, 0); recNum != nil {
		ans.AddAVP(recNum)
	}

	// Send answer
	if err := p.Send(ans); err != nil {
		log.Printf("Failed to send ACA: %v", err)
		return err
	}

	// Log the accounting record details
	recordType, _ := msg.GetAccountingRecordType()
	recordNum, _ := msg.GetAccountingRecordNumber()
	userName, _ := msg.GetUserName()

	log.Printf("Accounting Record Processed: Type=%d, Number=%d, User=%s",
		recordType, recordNum, userName)

	return nil
}

// Stop implements the Stoppable interface.
func (e *appAcct) Stop() error {
	return nil
}

func init() {
	extension.Register(&appAcct{})
}
