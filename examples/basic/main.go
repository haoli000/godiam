// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package main demonstrates basic usage of the godiam library.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func main() {
	fmt.Println("godiam Example")
	fmt.Println("===================")

	// Create and initialize dictionary
	dict := dictionary.New()
	if err := dict.LoadBaseProtocol(); err != nil {
		log.Fatalf("Failed to load base protocol: %v", err)
	}
	if err := dict.LoadCreditControl(); err != nil {
		log.Fatalf("Failed to load credit control: %v", err)
	}

	stats := dict.Stats()
	fmt.Printf("\nDictionary loaded:\n")
	fmt.Printf("  Vendors: %d\n", stats.Vendors)
	fmt.Printf("  Applications: %d\n", stats.Applications)
	fmt.Printf("  Types: %d\n", stats.Types)
	fmt.Printf("  AVPs: %d\n", stats.AVPs)
	fmt.Printf("  Commands: %d\n", stats.Commands)

	// Look up some AVPs
	fmt.Println("\nAVP Lookups:")
	if avp, ok := dict.GetAVPByName("Session-ID"); ok {
		fmt.Printf("  Session-ID: code=%d, type=%s\n", avp.Code, avp.Type.BaseType)
	}
	if avp, ok := dict.GetAVPByName("Result-Code"); ok {
		fmt.Printf("  Result-Code: code=%d, type=%s\n", avp.Code, avp.Type.BaseType)
	}
	if avp, ok := dict.GetAVPByName("CC-Request-Type"); ok {
		fmt.Printf("  CC-Request-Type: code=%d, type=%s\n", avp.Code, avp.Type.BaseType)
	}

	// Look up commands
	fmt.Println("\nCommand Lookups:")
	if cmd, ok := dict.GetCommandByCode(types.CmdCodeCapabilitiesExchange, true); ok {
		fmt.Printf("  CER: code=%d, name=%s\n", cmd.Code, cmd.Name)
	}
	if cmd, ok := dict.GetCommandByCode(types.CmdCodeCreditControl, true); ok {
		fmt.Printf("  CCR: code=%d, name=%s\n", cmd.Code, cmd.Name)
	}

	// Create a sample message
	fmt.Println("\nCreating sample CCR message:")
	msg := createSampleCCR()

	// Encode the message
	data, err := msg.Encode()
	if err != nil {
		log.Fatalf("Failed to encode message: %v", err)
	}
	fmt.Printf("  Encoded message: %d bytes\n", len(data))
	fmt.Printf("  Command Code: %d\n", msg.CommandCode)
	fmt.Printf("  Is Request: %v\n", msg.IsRequest())
	fmt.Printf("  Application ID: %d\n", msg.ApplicationID)

	// Decode the message
	decoded, err := message.DecodeMessage(data)
	if err != nil {
		log.Fatalf("Failed to decode message: %v", err)
	}
	fmt.Println("\nDecoded message:")
	fmt.Printf("  Command Code: %d\n", decoded.CommandCode)
	fmt.Printf("  AVP count: %d\n", len(decoded.AVPs))

	// Extract some AVP values
	if sessionID, ok := decoded.GetSessionID(); ok {
		fmt.Printf("  Session-ID: %s\n", sessionID)
	}
	if originHost, ok := decoded.GetOriginHost(); ok {
		fmt.Printf("  Origin-Host: %s\n", originHost)
	}
	if originRealm, ok := decoded.GetOriginRealm(); ok {
		fmt.Printf("  Origin-Realm: %s\n", originRealm)
	}

	// Create answer
	fmt.Println("\nCreating CCA (answer):")
	ans := message.NewAnswer(decoded)
	ans.SetResultCode(types.ResultSuccess)
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "server.example.com"))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))

	ansData, err := ans.Encode()
	if err != nil {
		log.Fatalf("Failed to encode answer: %v", err)
	}
	fmt.Printf("  Encoded answer: %d bytes\n", len(ansData))
	fmt.Printf("  Is Request: %v\n", ans.IsRequest())

	if resultCode, ok := ans.GetResultCode(); ok {
		fmt.Printf("  Result-Code: %d\n", resultCode)
	}

	fmt.Println("\nExample complete!")
}

// createSampleCCR creates a sample Credit-Control-Request message.
func createSampleCCR() *message.Message {
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.HopByHopID = 1
	msg.EndToEndID = 1

	// Session identification
	sessionID := fmt.Sprintf("client.example.com;%d;1", time.Now().Unix())
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))

	// Origin identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))

	// Destination identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	// Auth-Application-ID
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppIDCreditControl)))

	// CC-Request-Type: INITIAL_REQUEST (1)
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeCCRequestType, types.AVPFlagMandatory, 1))

	// CC-Request-Number
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeCCRequestNumber, types.AVPFlagMandatory, 0))

	// Service-Context-ID
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeServiceContextID, types.AVPFlagMandatory, "example@example.com"))

	// Requested-Service-Unit (grouped AVP with empty children to request unlimited)
	msg.AddAVP(message.NewGroupedAVP(types.AVPCodeRequestedServiceUnit, types.AVPFlagMandatory, 0))

	// Subscription-ID (grouped AVP)
	subscriptionID := message.NewGroupedAVP(types.AVPCodeSubscriptionID, types.AVPFlagMandatory, 0,
		message.NewUnsigned32AVP(types.AVPCodeSubscriptionIDType, types.AVPFlagMandatory, 0), // END_USER_E164
		message.NewUTF8StringAVP(types.AVPCodeSubscriptionIDData, types.AVPFlagMandatory, "12025551234"),
	)
	msg.AddAVP(subscriptionID)

	return msg
}
