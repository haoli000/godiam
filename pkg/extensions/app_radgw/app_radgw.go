// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package app_radgw provides a RADIUS-to-Diameter gateway extension.
package app_radgw

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/radius"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type pendingRequest struct {
	addr          *net.UDPAddr
	id            uint8
	authenticator [16]byte
	expire        time.Time
}

type appRadGW struct {
	radiusPort      int
	sharedSecret    string
	destRealm       string
	router          *routing.Router
	conn            *net.UDPConn
	cancel          context.CancelFunc
	pendingRequests sync.Map // HopByHopID -> pendingRequest
}

// Name returns the extension name.
func (e *appRadGW) Name() string { return "app_radgw" }

// Init initializes the extension.
func (e *appRadGW) Init(ctx extension.InitContext, config map[string]interface{}) error {
	log.Printf("Initializing extension: app_radgw (RADIUS Gateway)")

	if port, ok := config["radius_port"].(float64); ok {
		e.radiusPort = int(port)
	} else {
		e.radiusPort = 1812
	}

	if secret, ok := config["shared_secret"].(string); ok {
		e.sharedSecret = secret
	} else {
		e.sharedSecret = "secret"
	}

	if realm, ok := config["dest_realm"].(string); ok {
		e.destRealm = realm
	} else {
		e.destRealm = string(peer.GetLocalConfig().Realm)
	}

	e.router = ctx.GetRouter()

	// Start RADIUS listener
	addr := &net.UDPAddr{
		Port: e.radiusPort,
		IP:   net.ParseIP("0.0.0.0"),
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on RADIUS port: %w", err)
	}
	e.conn = conn

	// Register dispatch handler for NASREQ (1) to handle answers
	e.router.RegisterDispatchHandler(types.AppIDNASREQ, e.handleDiameterAnswer)

	var bgCtx context.Context
	bgCtx, e.cancel = context.WithCancel(context.Background())

	go e.radiusListenLoop()
	go e.cleanupLoop(bgCtx)

	log.Printf("RADIUS Gateway listening on %s", addr.String())

	return nil
}

func (e *appRadGW) radiusListenLoop() {
	buf := make([]byte, 4096)
	for {
		n, addr, err := e.conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("RADIUS read error: %v", err)
			return
		}

		pkt, err := radius.Parse(buf[:n], e.sharedSecret)
		if err != nil {
			log.Printf("RADIUS parse error from %s: %v", addr, err)
			continue
		}

		e.handleRadiusRequest(addr, pkt)
	}
}

func (e *appRadGW) handleRadiusRequest(addr *net.UDPAddr, pkt *radius.Packet) {
	if pkt.Code != radius.CodeAccessRequest && pkt.Code != radius.CodeAccountingRequest {
		log.Printf("RADIUS Gateway: Ignoring unsupported packet code %d from %s", pkt.Code, addr)
		return
	}

	log.Printf("RADIUS Gateway: Received %v from %s, translating to Diameter", pkt.Code, addr)

	// Create Diameter Request (NASREQ AA-Request or Accounting-Request)
	var cmdCode types.CommandCode
	var appID types.ApplicationID

	if pkt.Code == radius.CodeAccessRequest {
		cmdCode = 265 // AA-Request
		appID = types.AppIDNASREQ
	} else {
		cmdCode = 271 // Accounting-Request
		appID = types.AppIDBaseAccounting
	}

	dr := message.NewRequest(cmdCode, appID)

	// Origin-Host and Origin-Realm
	localCfg := peer.GetLocalConfig()
	dr.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	dr.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))
	dr.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, types.DiamID(e.destRealm)))

	// Map RADIUS Attributes to Diameter AVPs
	for _, attr := range pkt.Attributes {
		// Simplified mapping - many NASREQ AVPs match RADIUS attributes
		// NASREQ uses AVP codes equal to RADIUS attribute types for standard ones
		dr.AddAVP(message.NewAVP(types.AVPCode(attr.Type), types.AVPFlagMandatory, 0, attr.Value))
	}

	// Add Session-ID
	sessionID := fmt.Sprintf("radgw-%s-%d", addr.String(), time.Now().UnixNano())
	dr.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))

	// Find a peer to send to
	destPeer, err := e.router.RouteOut(dr)
	if err != nil {
		log.Printf("RADIUS Gateway: Routing failed: %v", err)
		return
	}

	// Get peer object to get Hop-By-Hop ID
	peerObj, ok := e.router.LookupPeer(destPeer)
	if !ok {
		log.Printf("RADIUS Gateway: Peer %s not found", destPeer)
		return
	}

	p, ok := peerObj.(*peer.Peer)
	if !ok {
		log.Printf("RADIUS Gateway: Invalid peer object type")
		return
	}

	dr.HopByHopID = p.NextHopByHopID()
	dr.EndToEndID = types.EndToEndID(time.Now().UnixNano()) //nolint:gosec // G115: value range is protocol-constrained

	// Store pending request
	e.pendingRequests.Store(dr.HopByHopID, pendingRequest{
		addr:          addr,
		id:            pkt.Identifier,
		authenticator: pkt.Authenticator,
		expire:        time.Now().Add(5 * time.Second),
	})

	if err := p.Send(dr); err != nil {
		log.Printf("RADIUS Gateway: Failed to send Diameter request: %v", err)
		e.pendingRequests.Delete(dr.HopByHopID)
	}
}

func (e *appRadGW) handleDiameterAnswer(_ *peer.Peer, msg *message.Message) error {
	if msg.IsRequest() {
		// We only handle answers here
		return nil
	}

	val, ok := e.pendingRequests.Load(msg.HopByHopID)
	if !ok {
		// Not for us
		return nil
	}
	e.pendingRequests.Delete(msg.HopByHopID)
	req := val.(pendingRequest)

	log.Printf("RADIUS Gateway: Received Diameter answer for %s, translating to RADIUS", req.addr)

	// Result-Code
	resultCode, _ := msg.GetResultCode()

	var radiusCode uint8
	if resultCode == types.ResultSuccess {
		if msg.CommandCode == 265 {
			radiusCode = radius.CodeAccessAccept
		} else {
			radiusCode = radius.CodeAccountingResponse
		}
	} else {
		if msg.CommandCode == 265 {
			radiusCode = radius.CodeAccessReject
		} else {
			// For accounting, we normally don't reject if it's transient?
			// But for now, just don't send anything or send error.
			return nil
		}
	}

	resp := &radius.Packet{
		Code:          radiusCode,
		Identifier:    req.id,
		Authenticator: req.authenticator,
		Secret:        e.sharedSecret,
	}

	// Map AVPs back to RADIUS Attributes
	for _, avp := range msg.AVPs {
		if avp.Code < 256 && avp.VendorID == 0 {
			resp.AddAttribute(uint8(avp.Code), avp.Data)
		}
	}

	data, err := resp.Serialize()
	if err != nil {
		log.Printf("RADIUS Gateway: Serialization error: %v", err)
		return err
	}

	_, err = e.conn.WriteToUDP(data, req.addr)
	return err
}

func (e *appRadGW) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			e.pendingRequests.Range(func(key, value interface{}) bool {
				req := value.(pendingRequest)
				if now.After(req.expire) {
					e.pendingRequests.Delete(key)
				}
				return true
			})
		}
	}
}

// Stop implements the Stoppable interface. It closes the UDP connection and cancels background goroutines.
func (e *appRadGW) Stop() error {
	log.Printf("app_radgw: stopping RADIUS Gateway")
	if e.cancel != nil {
		e.cancel()
	}
	if e.conn != nil {
		return e.conn.Close()
	}
	return nil
}

func init() {
	extension.Register(&appRadGW{})
}
