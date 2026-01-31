// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package main implements HSS-Lite, a simple HSS simulator handling S6a Update-Location-Requests.
package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	logger "github.com/haoli000/godiam/pkg/core/log"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/server"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// Subscriber represents a user profile
type Subscriber struct {
	IMSI    string
	AuthKey string // K value
	OPc     string // Operator Key
	APN     string
	MaxUL   uint32
	MaxDL   uint32
}

var (
	subscribers = map[string]Subscriber{
		"208930000000001": {
			IMSI:    "208930000000001",
			AuthKey: "00112233445566778899AABBCCDDEEFF",
			OPc:     "00000000000000000000000000000000",
			APN:     "internet",
			MaxUL:   50000000,
			MaxDL:   100000000,
		},
	}
)

func main() {
	configPath := flag.String("config", "hss.yaml", "Path to config file")
	flag.Parse()

	// Initialize logging
	logger.SetLevel(logger.LevelDebug)
	logger.SetPrefix("HSS")

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		// Create default config if not exists
		logger.Info("Config file not found or invalid, utilizing default HSS configuration")
		cfg = createDefaultHSSConfig()
	}

	// Initialize dictionary
	dict := dictionary.New()
	if err := dict.LoadBaseProtocol(); err != nil {
		logger.Fatal("Failed to load base protocol: %v", err)
	}
	if err := dict.Load3GPPS6a(); err != nil {
		logger.Fatal("Failed to load S6a dictionary: %v", err)
	}

	// Create server
	srv := server.New(cfg, dict)

	// Register S6a App Handler
	srv.SetHandler(func(p *peer.Peer, msg *message.Message) {
		handleMessage(p, msg)
	})

	// Start server
	logger.Info("Starting HSS-Lite on port %d...", cfg.Listen.Port)
	if err := srv.Start(); err != nil {
		logger.Fatal("Failed to start server: %v", err)
	}

	// Wait for termination
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down HSS-Lite...")
	srv.Stop()
}

func handleMessage(p *peer.Peer, msg *message.Message) {
	// Dispatch based on Application ID
	switch msg.ApplicationID {
	case types.AppID3GPPS6a:
		handleS6aMessage(p, msg)
	default:
		// Default handler for base protocol (CER/DWR are handled by peer state machine,
		// but we might get other messages)
		logger.Warn("Received message for unknown app %d from %s", msg.ApplicationID, p.DiameterID())
		sendError(p, msg, types.ResultApplicationUnsupported)
	}
}

func handleS6aMessage(p *peer.Peer, msg *message.Message) {
	switch msg.CommandCode {
	case types.CmdCode3GPPUpdateLocation: // ULR
		handleULR(p, msg)
	default:
		logger.Warn("Received unknown S6a command %d from %s", msg.CommandCode, p.DiameterID())
		sendError(p, msg, types.ResultCommandUnsupported)
	}
}

func handleULR(p *peer.Peer, msg *message.Message) {
	if !msg.IsRequest() {
		return // Ignore answers
	}

	logger.Info("Received Update-Location-Request from %s", p.DiameterID())

	// Extract User-Name (IMSI)
	userName, ok := msg.GetUserName()
	if !ok {
		logger.Error("ULR missing User-Name")
		sendError(p, msg, types.ResultMissingAVP)
		return
	}

	logger.Debug("Processing ULR for subscriber: %s", userName)

	// Lookup subscriber
	sub, exists := subscribers[userName]
	if !exists {
		logger.Warn("Subscriber not found: %s", userName)
		sendError(p, msg, types.ResultUnknownUser) // DIAMETER_ERROR_USER_UNKNOWN (5001)
		return
	}

	// Send ULA (Success)
	sendULA(p, msg, &sub)
}

func sendULA(p *peer.Peer, req *message.Message, sub *Subscriber) {
	ans := message.NewAnswer(req)
	ans.SetResultCode(types.ResultSuccess)

	// Add mandatory AVPs
	addOriginAVPs(ans)

	// Add ULA-Flags (Bit 1: ULA-Separation-Indication)
	ans.AddAVP(message.NewUnsigned32AVP(types.AVPCode3GPPULAFlags, types.AVPFlagMandatory|types.AVPFlagVendor, 1).SetVendorID(types.VendorID3GPP))

	// Add Subscription-Data (Grouped)
	// This is verified complex structure, we'll implement a minimal version
	// Subscription-Data ::= <AVP Header: 1400 10415>
	//   [ Subscriber-Status ] (0 - SERVICE_GRANTED)
	//   [ Access-Restriction-Data ]
	//   [ APN-Configuration-Profile ]
	//   ...

	subStatus := message.NewEnumeratedAVP(types.AVPCode3GPPSubscriberStatus, types.AVPFlagMandatory|types.AVPFlagVendor, 0).SetVendorID(types.VendorID3GPP)

	// APN-Configuration
	apnConf := message.NewGroupedAVP(types.AVPCode3GPPAPNConfiguration, types.AVPFlagMandatory|types.AVPFlagVendor, types.VendorID3GPP,
		message.NewUnsigned32AVP(types.AVPCode3GPPContextIdentifier, types.AVPFlagMandatory|types.AVPFlagVendor, 1).SetVendorID(types.VendorID3GPP),
		message.NewEnumeratedAVP(types.AVPCode3GPPPDNType, types.AVPFlagMandatory|types.AVPFlagVendor, 0).SetVendorID(types.VendorID3GPP), // IPv4
		message.NewUTF8StringAVP(1470, types.AVPFlagMandatory, sub.APN),                                                                   // Service-Selection
		// AMBR
		message.NewGroupedAVP(types.AVPCode3GPPAMBr, types.AVPFlagMandatory|types.AVPFlagVendor, types.VendorID3GPP,
			message.NewUnsigned32AVP(types.AVPCode3GPPMaxRequestedBandwidthUL, types.AVPFlagVendor, sub.MaxUL).SetVendorID(types.VendorID3GPP),
			message.NewUnsigned32AVP(types.AVPCode3GPPMaxRequestedBandwidthDL, types.AVPFlagVendor, sub.MaxDL).SetVendorID(types.VendorID3GPP),
		),
	)

	// APN-Configuration-Profile
	apnProfile := message.NewGroupedAVP(types.AVPCode3GPPAPNConfigurationProfile, types.AVPFlagMandatory|types.AVPFlagVendor, types.VendorID3GPP,
		message.NewUnsigned32AVP(types.AVPCode3GPPContextIdentifier, types.AVPFlagMandatory|types.AVPFlagVendor, 1).SetVendorID(types.VendorID3GPP),
		message.NewEnumeratedAVP(types.AVPCode3GPPAllAPNConfigsIncluded, types.AVPFlagMandatory|types.AVPFlagVendor, 0).SetVendorID(types.VendorID3GPP),
		apnConf,
	)

	subData := message.NewGroupedAVP(types.AVPCode3GPPSubscriptionData, types.AVPFlagMandatory|types.AVPFlagVendor, types.VendorID3GPP,
		subStatus,
		apnProfile,
	)

	ans.AddAVP(subData)

	logger.Info("Sending Update-Location-Answer Success for %s", sub.IMSI)
	if err := p.Send(ans); err != nil {
		logger.Error("Failed to send ULA: %v", err)
	}
}

func sendError(p *peer.Peer, req *message.Message, resultCode types.ResultCode) {
	ans := message.NewAnswer(req)
	ans.SetError(true)
	ans.SetResultCode(resultCode)
	addOriginAVPs(ans)
	_ = p.Send(ans)
}

func addOriginAVPs(msg *message.Message) {
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "hss.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))
}

func createDefaultHSSConfig() *config.Config {
	return &config.Config{
		Identity: "hss.example.com",
		Realm:    "example.com",
		Listen: config.ListenConfig{
			Port:      3868,
			EnableTCP: true,
		},
		Local: config.LocalConfig{
			VendorID:         uint32(types.VendorID3GPP),
			ProductName:      "godiam HSS-Lite",
			WatchdogInterval: 30 * time.Second,
		},
		Applications: []config.ApplicationConfig{
			{ID: uint32(types.AppID3GPPS6a), Type: "auth"},
		},
	}
}
