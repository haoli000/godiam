// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package main implements the diameterc Diameter client for testing.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

var (
	serverHost = flag.String("host", "127.0.0.1", "Server hostname or IP")
	serverPort = flag.Int("port", 3868, "Server port")
	serverID   = flag.String("id", "server.example.com", "Server Diameter Identity")
	realm      = flag.String("realm", "example.com", "Diameter Realm")
	clientID   = flag.String("client-id", "client.example.com", "Client Diameter Identity")
	network    = flag.String("network", "tcp", "Transport network (tcp, sctp)")
	useTLS     = flag.Bool("tls", false, "Enable TLS")
	sendCCR    = flag.Bool("ccr", false, "Send Credit-Control-Request after connection")
	sendACR    = flag.Bool("acr", false, "Send Accounting-Request after connection")
	sendEAP    = flag.Bool("eap", false, "Send Diameter-EAP-Request after connection")
	sendCx     = flag.Bool("cx", false, "Send 3GPP Cx UAR after connection")
	sendGx     = flag.Bool("gx", false, "Send 3GPP Gx CCR after connection")
	sendULR    = flag.Bool("ulr", false, "Send 3GPP S6a ULR after connection")
	sendAIR    = flag.Bool("air", false, "Send 3GPP S6a AIR after connection")
	sendSIP    = flag.Bool("sip", false, "Send Diameter SIP UAR after connection")
	destRealm  = flag.String("dest-realm", "", "Destination Realm for requests (optional)")
	destHost   = flag.String("dest-host", "", "Destination Host for requests (optional)")
)

func main() {
	flag.Parse()

	// Initialize dictionary
	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()
	_ = dict.LoadCreditControl()
	_ = dict.Load3GPP() // Specifically load 3GPP for Cx, Gx, S6a
	_ = dict.LoadSIP()  // Specifically load SIP

	// Client Peer Config
	clientCfg := peer.Config{
		DiameterIdentity:  types.DiamID(*serverID),
		Realm:             types.DiamID(*realm),
		Addresses:         []string{*serverHost},
		Port:              *serverPort,
		Network:           *network,
		UseTLS:            *useTLS,
		ConnectTimeout:    5 * time.Second,
		WatchdogInterval:  30 * time.Second,
		ReconnectInterval: 10 * time.Second,
	}

	if *useTLS {
		clientCfg.TLSConfig = &peer.TLSConfig{
			SkipVerify: true,
		}
	}

	p := peer.New(clientCfg, dict)

	// Set local identity
	p.SetLocalOverride(peer.LocalConfig{
		DiameterIdentity: types.DiamID(*clientID),
		Realm:            types.DiamID(*realm),
		VendorID:         0,
		ProductName:      "go-diameter-client",
		HostIPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		OriginStateID:    uint32(time.Now().Unix()), //nolint:gosec // G115: value range is protocol-constrained
	})

	// Handle messages
	p.SetOnMessage(func(_ *peer.Peer, msg *message.Message) {
		log.Printf("Received message: cmd=%d, app=%d, request=%v",
			msg.CommandCode, msg.ApplicationID, msg.IsRequest())

		if msg.CommandCode == types.CmdCodeCreditControl && !msg.IsRequest() {
			resultCode, _ := msg.GetResultCode()
			log.Printf("CCA received: Result-Code=%d", resultCode)
			if *sendCCR {
				log.Println("Test successful, exiting...")
				os.Exit(0)
			}
		}

		if msg.CommandCode == types.CmdCodeAccounting && !msg.IsRequest() {
			resultCode, _ := msg.GetResultCode()
			log.Printf("ACA received: Result-Code=%d", resultCode)
			if *sendACR {
				log.Println("Test successful, exiting...")
				os.Exit(0)
			}
		}

		if msg.CommandCode == types.CmdCodeEAP && !msg.IsRequest() {
			resultCode, _ := msg.GetResultCode()
			log.Printf("DEA received: Result-Code=%d", resultCode)
			if *sendEAP {
				log.Println("Test successful, exiting...")
				os.Exit(0)
			}
		}

		if !msg.IsRequest() {
			resultCode, _ := msg.GetResultCode()
			if resultCode == types.ResultRedirectIndication {
				log.Printf("REDIRECT INDICATION received (3006)")
				if avp := msg.FindAVP(types.AVPCodeRedirectHost, 0); avp != nil {
					log.Printf("Redirect-Host: %s", avp.GetUTF8String())
				}
				os.Exit(0)
			}
		}

		if msg.CommandCode == dictionary.CmdCode3GPPUserAuthorization && !msg.IsRequest() {
			resultCode, _ := msg.GetResultCode()
			log.Printf("UAA received: Result-Code=%d", resultCode)
			if *sendCx {
				log.Println("Test successful, exiting...")
				os.Exit(0)
			}
		}

		if msg.CommandCode == types.CmdCodeCreditControl && !msg.IsRequest() && msg.ApplicationID == types.AppID3GPPGx {
			resultCode, _ := msg.GetResultCode()
			log.Printf("Gx CCA received: Result-Code=%d", resultCode)
			if *sendGx {
				log.Println("Test successful, exiting...")
				os.Exit(0)
			}
		}

		if msg.CommandCode == dictionary.CmdCode3GPPUpdateLocation && !msg.IsRequest() {
			resultCode, _ := msg.GetResultCode()
			log.Printf("ULA received: Result-Code=%d", resultCode)
			if *sendULR {
				log.Println("Test successful, exiting...")
				os.Exit(0)
			}
		}

		if msg.CommandCode == dictionary.CmdCode3GPPAuthenticationInformation && !msg.IsRequest() {
			resultCode, _ := msg.GetResultCode()
			log.Printf("AIA received: Result-Code=%d", resultCode)
			if *sendAIR {
				log.Println("Test successful, exiting...")
				os.Exit(0)
			}
		}

		if msg.CommandCode == types.CmdCodeSIPUserAuthorization && !msg.IsRequest() {
			resultCode, _ := msg.GetResultCode()
			log.Printf("SIP UAA received: Result-Code=%d", resultCode)
			if *sendSIP {
				log.Println("Test successful, exiting...")
				os.Exit(0)
			}
		}
	})

	// Monitor state changes
	p.SetOnStateChange(func(peerConn *peer.Peer, old peer.State, newState peer.State) {
		log.Printf("State change: %s -> %s", old, newState)
		if peerConn.IsOpen() {
			switch {
			case *sendCCR:
				log.Println("Connection open, sending CCR...")
				sendSampleCCR(peerConn)
			case *sendACR:
				log.Println("Connection open, sending ACR...")
				sendSampleACR(peerConn)
			case *sendEAP:
				log.Println("Connection open, sending DER...")
				sendSampleEAP(peerConn)
			case *sendCx:
				log.Println("Connection open, sending UAR...")
				sendSampleCx(peerConn)
			case *sendGx:
				log.Println("Connection open, sending Gx CCR...")
				sendSampleGx(peerConn)
			case *sendULR:
				log.Println("Connection open, sending ULR...")
				sendSampleULR(peerConn)
			case *sendAIR:
				log.Println("Connection open, sending AIR...")
				sendSampleAIR(peerConn)
			case *sendSIP:
				log.Println("Connection open, sending SIP UAR...")
				sendSampleSIP(peerConn)
			}
		}
	})

	// Start client
	log.Printf("Starting client, connecting to %s:%d...", *serverHost, *serverPort)
	if err := p.Start(); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	// Wait for termination signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	p.Stop()
}

func sendSampleCCR(p *peer.Peer) {
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = 1

	// Session identification
	sessionID := fmt.Sprintf("%s;%d;1", p.Config().DiameterIdentity, time.Now().Unix())
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))

	// Origin identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(*clientID)))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID(*realm)))

	// Destination identification
	dr := *destRealm
	dh := *destHost
	if dr == "" && os.Getenv("DRA_TEST") == "1" {
		dr = "left.side"
	}
	if dh == "" && os.Getenv("DRA_TEST") == "1" {
		dh = "dra1.left.side"
	}
	if dr != "" {
		msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, types.DiamID(dr)))
	} else {
		msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, types.DiamID(*realm)))
	}
	if dh != "" {
		msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, types.DiamID(dh)))
	}

	// Auth-Application-ID
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppIDCreditControl)))

	// CC-Request-Type: INITIAL_REQUEST (1)
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeCCRequestType, types.AVPFlagMandatory, 1))

	// CC-Request-Number
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeCCRequestNumber, types.AVPFlagMandatory, 0))

	if err := p.Send(msg); err != nil {
		log.Printf("Failed to send CCR: %v", err)
	} else {
		log.Println("CCR sent")
	}
}

func sendSampleACR(p *peer.Peer) {
	msg := message.NewRequest(types.CmdCodeAccounting, types.AppIDBaseAccounting)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = 1

	// Session identification
	sessionID := fmt.Sprintf("%s;%d;acct-1", p.Config().DiameterIdentity, time.Now().Unix())
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))

	// Origin identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))

	// Destination identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	// Accounting-Record-Type: START_RECORD (2)
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeAccountingRecordType, types.AVPFlagMandatory, uint32(types.AccountingStartRecord)))

	// Accounting-Record-Number
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeAccountingRecordNumber, types.AVPFlagMandatory, 0))

	if err := p.Send(msg); err != nil {
		log.Printf("Failed to send ACR: %v", err)
	} else {
		log.Println("ACR sent")
	}
}

func sendSampleEAP(p *peer.Peer) {
	msg := message.NewRequest(types.CmdCodeEAP, types.AppIDEAP)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = 1

	// Session identification
	sessionID := fmt.Sprintf("%s;%d;eap-1", p.Config().DiameterIdentity, time.Now().Unix())
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))

	// Origin identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))

	// Destination identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	// Auth-Application-ID
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppIDEAP)))

	// EAP-Payload (dummy)
	msg.AddAVP(message.NewOctetStringAVP(types.AVPCodeEAPPayload, types.AVPFlagMandatory, []byte{0x01, 0x02, 0x03, 0x04}))

	if err := p.Send(msg); err != nil {
		log.Printf("Failed to send DER: %v", err)
	} else {
		log.Println("DER sent")
	}
}

func sendSampleCx(p *peer.Peer) {
	msg := message.NewRequest(dictionary.CmdCode3GPPUserAuthorization, types.AppID3GPPCx)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = 1

	// Session identification
	sessionID := fmt.Sprintf("%s;%d;cx-1", p.Config().DiameterIdentity, time.Now().Unix())
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))

	// Origin identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))

	// Destination identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	// Vendor-Specific-Application-ID
	vapp := message.NewGroupedAVP(types.AVPCodeVendorSpecificAppID, types.AVPFlagMandatory, 0)
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(types.VendorID3GPP)))
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppID3GPPCx)))
	msg.AddAVP(vapp)

	// User-Name
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeUserName, types.AVPFlagMandatory, "user@example.com"))

	// Public-Identity
	msg.AddAVP(message.NewUTF8StringAVP(dictionary.AVPCode3GPPPublicIdentity, types.AVPFlagMandatory|types.AVPFlagVendor, "sip:user@example.com").SetVendorID(types.VendorID3GPP))

	// Visited-Network-Identifier
	msg.AddAVP(message.NewOctetStringAVP(dictionary.AVPCode3GPPVisitedNetworkID, types.AVPFlagMandatory|types.AVPFlagVendor, []byte("example.com")).SetVendorID(types.VendorID3GPP))

	// User-Authorization-Type (REGISTRATION=0)
	msg.AddAVP(message.NewEnumeratedAVP(dictionary.AVPCode3GPPUserAuthType, types.AVPFlagMandatory|types.AVPFlagVendor, 0).SetVendorID(types.VendorID3GPP))

	if err := p.Send(msg); err != nil {
		log.Printf("Failed to send UAR: %v", err)
	} else {
		log.Println("UAR sent")
	}
}

func sendSampleGx(p *peer.Peer) {
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppID3GPPGx)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = 1

	// Session identification
	sessionID := fmt.Sprintf("%s;%d;gx-1", p.Config().DiameterIdentity, time.Now().Unix())
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))

	// Origin identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))

	// Destination identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	// Vendor-Specific-Application-ID
	vapp := message.NewGroupedAVP(types.AVPCodeVendorSpecificAppID, types.AVPFlagMandatory, 0)
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(types.VendorID3GPP)))
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppID3GPPGx)))
	msg.AddAVP(vapp)

	// CC-Request-Type (INITIAL_REQUEST=1)
	msg.AddAVP(message.NewEnumeratedAVP(types.AVPCodeCCRequestType, types.AVPFlagMandatory, 1))

	// CC-Request-Number
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeCCRequestNumber, types.AVPFlagMandatory, 0))

	// IP-CAN-Type (3GPP-EPS=5)
	msg.AddAVP(message.NewEnumeratedAVP(dictionary.AVPCode3GPPIPCANType, types.AVPFlagMandatory|types.AVPFlagVendor, 5).SetVendorID(types.VendorID3GPP))

	if err := p.Send(msg); err != nil {
		log.Printf("Failed to send Gx CCR: %v", err)
	} else {
		log.Println("Gx CCR sent")
	}
}

func sendSampleULR(p *peer.Peer) {
	msg := message.NewRequest(dictionary.CmdCode3GPPUpdateLocation, types.AppID3GPPS6a)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = 1

	// Session identification
	sessionID := fmt.Sprintf("%s;%d;s6a-1", p.Config().DiameterIdentity, time.Now().Unix())
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))

	// Origin identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))

	// Destination identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	// Vendor-Specific-Application-ID
	vapp := message.NewGroupedAVP(types.AVPCodeVendorSpecificAppID, types.AVPFlagMandatory, 0)
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(types.VendorID3GPP)))
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppID3GPPS6a)))
	msg.AddAVP(vapp)

	// User-Name
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeUserName, types.AVPFlagMandatory, "subscriber@example.com"))

	// Visited-PLMN-ID (3 bytes MCC+MNC)
	msg.AddAVP(message.NewOctetStringAVP(dictionary.AVPCode3GPPVisitedPLMNID, types.AVPFlagMandatory|types.AVPFlagVendor, []byte{0x00, 0x01, 0x10}).SetVendorID(types.VendorID3GPP))

	// ULR-Flags (0x02 = Single-Registration-Indication)
	msg.AddAVP(message.NewUnsigned32AVP(dictionary.AVPCode3GPPULRFlags, types.AVPFlagMandatory|types.AVPFlagVendor, 2).SetVendorID(types.VendorID3GPP))

	// RAT-Type (EUTRAN=1004) - AVP Code 1032
	msg.AddAVP(message.NewEnumeratedAVP(1032, types.AVPFlagMandatory|types.AVPFlagVendor, 1004).SetVendorID(types.VendorID3GPP))

	if err := p.Send(msg); err != nil {
		log.Printf("Failed to send ULR: %v", err)
	} else {
		log.Println("ULR sent")
	}
}

func sendSampleAIR(p *peer.Peer) {
	msg := message.NewRequest(dictionary.CmdCode3GPPAuthenticationInformation, types.AppID3GPPS6a)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = 1

	// Session identification
	sessionID := fmt.Sprintf("%s;%d;s6a-air", p.Config().DiameterIdentity, time.Now().Unix())
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))

	// Origin identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))

	// Destination identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	// Vendor-Specific-Application-ID
	vapp := message.NewGroupedAVP(types.AVPCodeVendorSpecificAppID, types.AVPFlagMandatory, 0)
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(types.VendorID3GPP)))
	vapp.AddChild(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppID3GPPS6a)))
	msg.AddAVP(vapp)

	// User-Name
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeUserName, types.AVPFlagMandatory, "subscriber@example.com"))

	// Visited-PLMN-ID
	msg.AddAVP(message.NewOctetStringAVP(dictionary.AVPCode3GPPVisitedPLMNID, types.AVPFlagMandatory|types.AVPFlagVendor, []byte{0x00, 0x01, 0x10}).SetVendorID(types.VendorID3GPP))

	// Requested-EUTRAN-Authentication-Info
	reqAuth := message.NewGroupedAVP(dictionary.AVPCode3GPPRequestedEUTRANAuthInfo, types.AVPFlagMandatory|types.AVPFlagVendor, types.VendorID3GPP)
	reqAuth.AddChild(message.NewUnsigned32AVP(dictionary.AVPCode3GPPNumberOfRequestedVectors, types.AVPFlagMandatory|types.AVPFlagVendor, 1).SetVendorID(types.VendorID3GPP))
	msg.AddAVP(reqAuth)

	if err := p.Send(msg); err != nil {
		log.Printf("Failed to send AIR: %v", err)
	} else {
		log.Println("AIR sent")
	}
}

func sendSampleSIP(p *peer.Peer) {
	msg := message.NewRequest(types.CmdCodeSIPUserAuthorization, types.AppIDSIP)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = 1

	// Session identification
	sessionID := fmt.Sprintf("%s;%d;sip-1", p.Config().DiameterIdentity, time.Now().Unix())
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))

	// Origin identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))

	// Destination identification
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	// Auth-Application-ID
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(types.AppIDSIP)))

	// User-Name
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeUserName, types.AVPFlagMandatory, "sip:user@example.com"))

	// SIP-Server-URI
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSIPServerURI, types.AVPFlagMandatory, "sip:proxy.example.com"))

	// SIP-Method
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSIPMethod, types.AVPFlagMandatory, "REGISTER"))

	if err := p.Send(msg); err != nil {
		log.Printf("Failed to send SIP UAR: %v", err)
	} else {
		log.Println("SIP UAR sent")
	}
}
