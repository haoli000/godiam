// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package peer provides Diameter peer management and state machine.
package peer

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// LocalConfig holds local node configuration for CER/CEA.
type LocalConfig struct {
	DiameterIdentity types.DiamID
	Realm            types.DiamID
	VendorID         types.VendorID
	ProductName      string
	FirmwareRevision uint32
	OriginStateID    uint32
	HostIPAddresses  []net.IP
	AuthAppIDs       []types.ApplicationID
	AcctAppIDs       []types.ApplicationID
}

// localConfig is the local node configuration (should be set before creating peers)
var localConfig LocalConfig

// SetLocalConfig sets the local node configuration.
func SetLocalConfig(config LocalConfig) {
	if config.OriginStateID == 0 {
		// Generate random origin-state-id
		var buf [4]byte
		_, _ = rand.Read(buf[:])
		config.OriginStateID = binary.BigEndian.Uint32(buf[:])
	}
	localConfig = config
}

// GetLocalConfig returns the local node configuration.
func GetLocalConfig() LocalConfig {
	return localConfig
}

func (p *Peer) getEffectiveLocalConfig() LocalConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.localConfig != nil {
		return *p.localConfig
	}
	return localConfig
}

// sendCER sends a Capabilities-Exchange-Request to the peer.
func (p *Peer) sendCER() error {
	cfg := p.getEffectiveLocalConfig()
	msg := message.NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = types.EndToEndID(generateEndToEndID())

	// Add required AVPs
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, cfg.DiameterIdentity))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, cfg.Realm))

	// Add Host-IP-Address AVPs
	for _, ip := range cfg.HostIPAddresses {
		addr := types.NewAddressFromIP(ip)
		msg.AddAVP(message.NewAddressAVP(types.AVPCodeHostIPAddress, types.AVPFlagMandatory, addr))
	}

	// Add Vendor-ID
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(cfg.VendorID)))

	// Add Product-Name
	if cfg.ProductName != "" {
		msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeProductName, 0, cfg.ProductName))
	}

	// Add Origin-State-ID
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeOriginStateID, types.AVPFlagMandatory, cfg.OriginStateID))

	// Add Auth-Application-ID AVPs
	for _, appID := range cfg.AuthAppIDs {
		msg.AddAVP(message.NewUnsigned32AVP(
			types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(appID)))
	}

	// Add Acct-Application-ID AVPs
	for _, appID := range cfg.AcctAppIDs {
		msg.AddAVP(message.NewUnsigned32AVP(
			types.AVPCodeAcctApplicationID, types.AVPFlagMandatory, uint32(appID)))
	}

	// Add Firmware-Revision if set
	if cfg.FirmwareRevision != 0 {
		msg.AddAVP(message.NewUnsigned32AVP(
			types.AVPCodeFirmwareRevision, 0, cfg.FirmwareRevision))
	}

	return p.sendMessage(msg)
}

// processCEA processes a received Capabilities-Exchange-Answer.
func (p *Peer) processCEA(msg *message.Message) error {
	// Check Result-Code
	resultCode, ok := msg.GetResultCode()
	if !ok {
		return fmt.Errorf("CEA missing Result-Code")
	}
	if resultCode != types.ResultSuccess {
		return fmt.Errorf("CEA result code: %d", resultCode)
	}

	// Extract peer identity
	originHost, ok := msg.GetOriginHost()
	if !ok {
		return fmt.Errorf("CEA missing Origin-Host")
	}
	originRealm, ok := msg.GetOriginRealm()
	if !ok {
		return fmt.Errorf("CEA missing Origin-Realm")
	}

	p.mu.Lock()
	p.diameterID = originHost
	p.realm = originRealm
	p.mu.Unlock()

	// TODO: Validate against expected peer identity
	// TODO: Extract supported applications, vendor IDs, etc.

	return nil
}

// processCER processes a received Capabilities-Exchange-Request.
func (p *Peer) processCER(msg *message.Message) error {
	// Extract peer identity
	originHost, ok := msg.GetOriginHost()
	if !ok {
		return fmt.Errorf("CER missing Origin-Host")
	}
	originRealm, ok := msg.GetOriginRealm()
	if !ok {
		return fmt.Errorf("CER missing Origin-Realm")
	}

	p.mu.Lock()
	p.diameterID = originHost
	p.realm = originRealm
	policy := p.acceptPolicy
	p.mu.Unlock()

	// Validate against acceptance policy if provided
	if policy != nil {
		if ok, code := policy(originHost, originRealm); !ok {
			return cerRejectError{code: code}
		}
	}

	return nil
}

// cerRejectError indicates the CER should be rejected with a specific result code.
type cerRejectError struct {
	code types.ResultCode
}

func (e cerRejectError) Error() string {
	return fmt.Sprintf("CER rejected with code %d", e.code)
}

// generateEndToEndID generates a unique End-to-End Identifier.
func generateEndToEndID() uint32 {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	// Combine with timestamp for uniqueness
	ts := uint32(time.Now().UnixNano() & 0xFFFFFFFF)
	id := binary.BigEndian.Uint32(buf[:])
	return id ^ ts
}
