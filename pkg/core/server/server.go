// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package server provides the Diameter server implementation.
package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// Server is a Diameter server that accepts incoming peer connections.
type Server struct {
	mu sync.RWMutex

	config    *config.Config
	dict      *dictionary.Dictionary
	listeners []net.Listener

	// Connected peers
	peers map[string]*peer.Peer

	// Routing and extensions
	router *routing.Router
	extMgr *extension.Manager

	// Edge (DEA) zone/partner registry
	edgeReg *edge.Registry

	// Message handler (legacy, extensions should use router)
	handler MessageHandler

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	startTime     time.Time
	routingErrors atomic.Uint64
}

// MessageHandler is called for each received message.
type MessageHandler func(p *peer.Peer, msg *message.Message)

// New creates a new Diameter server.
func New(cfg *config.Config, dict *dictionary.Dictionary) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	router := routing.NewRouter()
	router.SetLocalIdentity(cfg.Identity, cfg.Realm)

	s := &Server{
		config:    cfg,
		dict:      dict,
		router:    router,
		edgeReg:   edge.NewRegistry(cfg),
		peers:     make(map[string]*peer.Peer),
		ctx:       ctx,
		cancel:    cancel,
		startTime: time.Now(),
	}

	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		p, ok := s.peers[string(identity)]
		return p, ok
	})
	router.SetPeerEnumerator(func() []*peer.Peer {
		return s.Peers()
	})

	return s
}

// SetHandler sets the message handler.
func (s *Server) SetHandler(h MessageHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

// Start starts the server.
func (s *Server) Start() error {
	// Get local IP addresses for Host-IP-Address AVP
	hostIPs := s.getLocalIPAddresses()

	// Configure local peer settings
	authAppIDs := s.config.GetAuthAppIDs()
	// RFC 6733 §2.4: a relay agent MUST advertise the Relay Application-ID
	// (0xffffffff) so that peers know it can handle any application.
	if s.config.Routing.RelayEnabled {
		hasRelay := false
		for _, id := range authAppIDs {
			if id == types.AppIDRelay {
				hasRelay = true
				break
			}
		}
		if !hasRelay {
			authAppIDs = append(authAppIDs, types.AppIDRelay)
		}
	}
	peer.SetLocalConfig(peer.LocalConfig{
		DiameterIdentity: types.DiamID(s.config.Identity),
		Realm:            types.DiamID(s.config.Realm),
		VendorID:         types.VendorID(s.config.Local.VendorID),
		ProductName:      s.config.Local.ProductName,
		FirmwareRevision: s.config.Local.FirmwareRevision,
		HostIPAddresses:  hostIPs,
		AuthAppIDs:       authAppIDs,
		AcctAppIDs:       s.config.GetAcctAppIDs(),
	})

	// Load extensions
	if err := s.startExtensions(); err != nil {
		return fmt.Errorf("starting extensions: %w", err)
	}

	// Setup routing
	s.setupRouting()

	// Start listening
	if err := s.startListeners(); err != nil {
		return fmt.Errorf("starting listeners: %w", err)
	}

	// Connect to configured peers
	for _, peerCfg := range s.config.Peers {
		if peerCfg.ConnectOnStart {
			if err := s.connectToPeer(peerCfg); err != nil {
				// Log error but don't fail startup
				fmt.Printf("failed to connect to peer %s: %v\n", peerCfg.Identity, err)
			}
		}
	}

	return nil
}

// startListeners starts the listeners of every configured edge zone.
// When no zones are configured an implicit internal zone derived from the
// node-level listen/tls settings is used, preserving pre-DEA behaviour.
func (s *Server) startListeners() error {
	zones := s.config.EffectiveZones()
	for i := range zones {
		if err := s.startZoneListeners(&zones[i]); err != nil {
			return fmt.Errorf("zone %q: %w", zones[i].Name, err)
		}
	}

	s.mu.RLock()
	count := len(s.listeners)
	s.mu.RUnlock()
	if count == 0 {
		return fmt.Errorf("no listeners started")
	}

	return nil
}

// acceptLoop accepts incoming connections from a listener.
func (s *Server) acceptLoop(l net.Listener, zone *config.ZoneConfig) {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		conn, err := l.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}

		go s.handleConnection(conn, zone)
	}
}

// handleConnection handles an incoming connection.
func (s *Server) handleConnection(conn net.Conn, zone *config.ZoneConfig) {
	zone = s.zoneOrDefault(zone)
	log.Printf("Handling connection from %v on zone %q", conn.RemoteAddr(), zone.Name)

	// Create a new peer for this connection
	p := peer.New(peer.Config{
		Addresses:        []string{conn.RemoteAddr().String()},
		WatchdogInterval: s.config.Local.WatchdogInterval,
		Persistent:       false,
		Zone:             zone.Name,
	}, s.dict)

	// Set inbound acceptance policy based on the zone and partner model
	p.SetAcceptPolicy(func(originHost, originRealm types.DiamID) (bool, types.ResultCode) {
		res := s.admitPeer(zone, conn, originHost, originRealm)
		if !res.accepted {
			log.Printf("Rejecting inbound peer on zone %q: %s", zone.Name, res.reason)
			return false, res.code
		}
		p.SetZone(zone.Name, res.partner)
		// A peer matched to a partner by realm is not in the static index, so
		// record the binding or the edge extensions cannot tell which policy
		// applies to it.
		if reg := s.GetEdgeRegistry(); reg != nil && res.partner != "" {
			reg.BindPeer(string(originHost), res.partner)
		}
		return true, res.code
	})

	// Set up callbacks
	p.SetOnMessage(func(peerConn *peer.Peer, msg *message.Message) {
		s.mu.RLock()
		handler := s.handler
		router := s.router
		s.mu.RUnlock()

		if handler != nil {
			handler(peerConn, msg)
		}

		if router != nil {
			if err := router.RouteIn(peerConn, msg); err != nil {
				s.handleRoutingError(peerConn, err)
			}
		}
	})

	p.SetOnStateChange(func(peerConn *peer.Peer, _, newState peer.State) {
		if peerConn.IsOpen() {
			peerID := string(peerConn.DiameterID())
			s.mu.Lock()
			_, existed := s.peers[peerID]
			s.peers[peerID] = peerConn
			s.mu.Unlock()

			isConfigured := false
			for _, pc := range s.config.Peers {
				if pc.Identity == peerID {
					isConfigured = true
					break
				}
			}

			if isConfigured {
				if !existed {
					log.Printf("Connected to configured peer: %s realm=%s", peerConn.DiameterID(), peerConn.Realm())
				}
			} else {
				if !existed {
					log.Printf("Dynamically added peer: %s realm=%s", peerConn.DiameterID(), peerConn.Realm())
				}
			}
		} else if newState == peer.StateClosed || newState == peer.StateZombie {
			s.mu.Lock()
			delete(s.peers, string(peerConn.DiameterID()))
			s.mu.Unlock()
			if reg := s.GetEdgeRegistry(); reg != nil {
				reg.UnbindPeer(string(peerConn.DiameterID()))
			}
			// OnStateChange always runs on the peer's own state-machine
			// goroutine, so calling Stop() synchronously here would
			// deadlock (Stop now waits for that goroutine to exit).
			// Nothing here needs to block on it.
			go peerConn.Stop()
		}
	})

	// Start the peer with the existing connection
	// Per RFC 6733, the server waits for CER before starting the peer state machine.
	// Read the first message which should be a CER.
	cer, err := message.ReadMessage(conn)
	if err != nil {
		log.Printf("Failed to read initial CER: %v", err)
		_ = conn.Close()
		return
	}

	// Verify it's a CER
	if cer.CommandCode != types.CmdCodeCapabilitiesExchange || !cer.IsRequest() {
		log.Printf("Expected CER, got %d (req=%v)", cer.CommandCode, cer.IsRequest())
		_ = conn.Close()
		return
	}

	if err := p.StartWithConnection(conn, cer); err != nil {
		fmt.Printf("failed to start peer: %v\n", err)
		_ = conn.Close()
	}
}

// connectToPeer initiates a connection to a configured peer.
func (s *Server) connectToPeer(cfg config.PeerConfig) error {
	pConfig := peer.Config{
		DiameterIdentity:  types.DiamID(cfg.Identity),
		Realm:             types.DiamID(cfg.Realm),
		Addresses:         cfg.Addresses,
		Port:              cfg.Port,
		Network:           cfg.Network,
		UseTLS:            cfg.TLS,
		WatchdogInterval:  s.config.Local.WatchdogInterval,
		ConnectTimeout:    s.config.Local.ConnectTimeout,
		ReconnectInterval: cfg.ReconnectInterval,
		Persistent:        cfg.Persistent,
	}

	// Resolve the edge zone/partner this peer belongs to
	reg := s.GetEdgeRegistry()
	pConfig.Zone = cfg.Zone
	if pConfig.Zone == "" {
		pConfig.Zone = reg.ZoneNameForPeer(cfg.Identity)
	}
	tlsSource := &s.config.TLS
	if partner, ok := reg.PartnerForPeer(cfg.Identity); ok {
		pConfig.Partner = partner.Name
		if partner.TLS != nil {
			tlsSource = partner.TLS
		}
	}
	if zone, ok := reg.Zone(pConfig.Zone); ok && tlsSource == &s.config.TLS && zone.TLS.Enabled {
		tlsSource = &zone.TLS.TLSConfig
	}

	// Configure TLS if enabled
	if cfg.TLS {
		pConfig.TLSConfig = &peer.TLSConfig{
			CertFile:   tlsSource.CertFile,
			KeyFile:    tlsSource.KeyFile,
			CAFile:     tlsSource.CAFile,
			SkipVerify: !tlsSource.VerifyPeer,
			MinVersion: tlsSource.MinVersion,
		}
	}

	p := peer.New(pConfig, s.dict)

	// Set up callbacks
	p.SetOnMessage(func(peerConn *peer.Peer, msg *message.Message) {
		s.mu.RLock()
		handler := s.handler
		router := s.router
		s.mu.RUnlock()

		if handler != nil {
			handler(peerConn, msg)
		}

		if router != nil {
			if err := router.RouteIn(peerConn, msg); err != nil {
				s.handleRoutingError(peerConn, err)
			}
		}
	})

	p.SetOnStateChange(func(peerConn *peer.Peer, _, newState peer.State) {
		switch {
		case peerConn.IsOpen():
			s.mu.Lock()
			_, existed := s.peers[string(peerConn.DiameterID())]
			s.peers[string(peerConn.DiameterID())] = peerConn
			s.mu.Unlock()
			if !existed {
				log.Printf("Connected to configured peer: %s realm=%s", peerConn.DiameterID(), peerConn.Realm())
			}
		case newState == peer.StateClosed || newState == peer.StateZombie:
			s.mu.Lock()
			delete(s.peers, string(peerConn.DiameterID()))
			s.mu.Unlock()
			log.Printf("Peer disconnected: %s (state=%s)", cfg.Identity, newState)
		}
	})

	log.Printf("Initiating connection to peer: %s at %v:%d via %s", cfg.Identity, cfg.Addresses, cfg.Port, cfg.Network)

	// Start the peer
	return p.Start()
}

// Stop stops the server.
func (s *Server) Stop() {
	if n := s.routingErrors.Load(); n > 0 {
		log.Printf("Total routing errors: %d", n)
	}

	// Stop extensions first (they may need active peers/router)
	if s.extMgr != nil {
		_ = s.extMgr.StopAll()
	}

	s.cancel()

	for _, l := range s.listeners {
		_ = l.Close()
	}

	// Disconnect all peers
	s.mu.RLock()
	peers := make([]*peer.Peer, 0, len(s.peers))
	for _, p := range s.peers {
		peers = append(peers, p)
	}
	s.mu.RUnlock()

	for _, p := range peers {
		p.Stop()
	}
}

// GetPeer returns the peer with the given identity.
func (s *Server) GetPeer(identity string) (*peer.Peer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.peers[identity]
	return p, ok
}

// Peers returns all connected peers.
func (s *Server) Peers() []*peer.Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	peers := make([]*peer.Peer, 0, len(s.peers))
	for _, p := range s.peers {
		peers = append(peers, p)
	}
	return peers
}

// Send sends a message to a specific peer.
func (s *Server) Send(peerIdentity string, msg *message.Message) error {
	s.mu.RLock()
	p, ok := s.peers[peerIdentity]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("peer not found: %s", peerIdentity)
	}

	return p.Send(msg)
}

// Router returns the server's router.
func (s *Server) Router() *routing.Router {
	return s.router
}

// Dictionary returns the server's dictionary.
func (s *Server) Dictionary() *dictionary.Dictionary {
	return s.dict
}

// startExtensions initializes the extensions configured for the server using the Manager.
func (s *Server) startExtensions() error {
	s.extMgr = extension.NewManager(s)
	s.extMgr.LoadFromRegistry()
	return s.extMgr.InitFromConfig(s.config.Extensions)
}

// GetDictionary implements extension.InitContext.
func (s *Server) GetDictionary() *dictionary.Dictionary {
	return s.dict
}

// GetRouter implements extension.InitContext.
func (s *Server) GetRouter() *routing.Router {
	return s.router
}

// GetConfig implements extension.InitContext.
func (s *Server) GetConfig() *config.Config {
	return s.config
}

// GetPeers implements extension.InitContext.
func (s *Server) GetPeers() []*peer.Peer {
	return s.Peers()
}

// GetStartTime implements extension.InitContext.
func (s *Server) GetStartTime() time.Time {
	return s.startTime
}

// GetEdgeRegistry returns the server's DEA zone/partner registry.
func (s *Server) GetEdgeRegistry() *edge.Registry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.edgeReg
}

// EdgeRegistry returns the DEA zone/partner registry.
func (s *Server) EdgeRegistry() *edge.Registry {
	return s.GetEdgeRegistry()
}

// GetExtensionManager returns the extension manager.
func (s *Server) GetExtensionManager() *extension.Manager {
	return s.extMgr
}

func (s *Server) handleRoutingError(peerConn *peer.Peer, err error) {
	if err == nil {
		return
	}
	var rerr *routing.RoutingError
	if errors.As(err, &rerr) {
		if rerr != nil && rerr.Answer != nil && peerConn != nil && !rerr.Sent {
			if sendErr := peerConn.Send(rerr.Answer); sendErr == nil {
				rerr.Sent = true
			}
		}
	}
	n := s.routingErrors.Add(1)
	if n == 1 || n == 10 || n == 100 || n == 1000 || n%10000 == 0 {
		log.Printf("Routing error (count=%d): %v", n, err)
	}
}

// StartTime returns the time when the server was created.
func (s *Server) StartTime() time.Time {
	return s.startTime
}

// setupRouting configures the router based on the server configuration.
func (s *Server) setupRouting() {
	s.router.SetDefaultRealm(s.config.Routing.DefaultRealm)
	s.router.SetRelay(s.config.Routing.RelayEnabled)
	s.router.SetDynamicRealmPeers(s.config.Routing.DynamicRealmPeers)

	for realm, peers := range s.config.Routing.RealmRoutes {
		s.router.AddRealmRoute(realm, peers...)
	}
	for host, peerID := range s.config.Routing.HostRoutes {
		s.router.AddHostRoute(host, peerID)
	}

	for _, app := range s.config.Applications {
		if len(app.RouteTo) > 0 {
			s.router.AddAppRoute(types.ApplicationID(app.ID), app.RouteTo...)
		}
	}

	// Auto-populate realm routes from configured peers if not explicitly set
	// This mimics freeDiameter behavior: route requests to any known peer in the target realm.
	realmConfigured := make(map[string]map[string]struct{})
	for realm, peers := range s.config.Routing.RealmRoutes {
		set := make(map[string]struct{})
		for _, id := range peers {
			set[id] = struct{}{}
		}
		realmConfigured[realm] = set
	}
	for _, pc := range s.config.Peers {
		if pc.Realm == "" || pc.Identity == "" {
			continue
		}
		set, ok := realmConfigured[pc.Realm]
		if !ok {
			set = make(map[string]struct{})
			realmConfigured[pc.Realm] = set
		}
		if _, exists := set[pc.Identity]; !exists {
			s.router.AddRealmRoute(pc.Realm, pc.Identity)
			set[pc.Identity] = struct{}{}
		}
	}

	// Host routes are typically added dynamically or via extensions like rt_default
}

// getLocalIPAddresses returns local IP addresses for CER Host-IP-Address AVP.
func (s *Server) getLocalIPAddresses() []net.IP {
	var ips []net.IP

	// First check if listen addresses are configured
	if len(s.config.Listen.Addresses) > 0 {
		for _, addr := range s.config.Listen.Addresses {
			if ip := net.ParseIP(addr); ip != nil {
				ips = append(ips, ip)
			}
		}
		if len(ips) > 0 {
			return ips
		}
	}

	// Get all interface addresses
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		// Fallback to localhost
		return []net.IP{net.ParseIP("127.0.0.1")}
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			// Skip loopback unless no other addresses available
			if !ipnet.IP.IsLoopback() {
				ips = append(ips, ipnet.IP)
			}
		}
	}

	// If no non-loopback addresses, use loopback
	if len(ips) == 0 {
		ips = append(ips, net.ParseIP("127.0.0.1"))
	}

	return ips
}
