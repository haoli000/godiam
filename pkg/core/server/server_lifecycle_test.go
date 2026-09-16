// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func testDictionary(t *testing.T) *dictionary.Dictionary {
	t.Helper()
	dict := dictionary.New()
	if err := dict.LoadBaseProtocol(); err != nil {
		t.Fatalf("loading base dictionary: %v", err)
	}
	return dict
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before deadline")
}

func listenerAddr(t *testing.T, s *Server, index int) string {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if index >= len(s.listeners) {
		t.Fatalf("listener %d missing; have %d", index, len(s.listeners))
	}
	return s.listeners[index].Addr().String()
}

func listenerPort(t *testing.T, s *Server, index int) int {
	t.Helper()
	addr, err := net.ResolveTCPAddr("tcp", listenerAddr(t, s, index))
	if err != nil {
		t.Fatalf("resolving listener address: %v", err)
	}
	return addr.Port
}

func startClient(t *testing.T, dict *dictionary.Dictionary, addr, originHost, originRealm string) *peer.Peer {
	t.Helper()
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		t.Fatalf("resolving server address: %v", err)
	}
	client := peer.New(peer.Config{
		DiameterIdentity:  types.DiamID("dea.example.com"),
		Realm:             "example.com",
		Addresses:         []string{tcpAddr.IP.String()},
		Port:              tcpAddr.Port,
		ConnectTimeout:    time.Second,
		WatchdogInterval:  time.Hour,
		ReconnectInterval: time.Hour,
	}, dict)
	client.SetLocalOverride(peer.LocalConfig{
		DiameterIdentity: types.DiamID(originHost),
		Realm:            types.DiamID(originRealm),
		VendorID:         10415,
		ProductName:      "zone-test-client",
		HostIPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		OriginStateID:    1,
	})
	if err := client.Start(); err != nil {
		t.Fatalf("starting client: %v", err)
	}
	t.Cleanup(client.Stop)
	return client
}

func TestZoneAwareListenersTagAcceptedPeersWithTheirZone(t *testing.T) {
	dict := testDictionary(t)
	cfg := &config.Config{
		Identity: "dea.example.com",
		Realm:    "example.com",
		Zones: []config.ZoneConfig{
			{
				Name: "core",
				Role: config.ZoneRoleInternal,
				Listen: config.ListenConfig{
					Addresses: []string{"127.0.0.1"},
					Port:      0,
					EnableTCP: true,
				},
				PeersPolicy: config.PeersPolicy{AllowUnknown: true},
			},
			{
				Name: "roaming",
				Role: config.ZoneRoleExternal,
				Listen: config.ListenConfig{
					Addresses: []string{"127.0.0.1"},
					Port:      0,
					EnableTCP: true,
				},
			},
		},
		Partners: []config.PartnerConfig{
			{
				Name:   "partner-a",
				Zone:   "roaming",
				Realms: []string{"partnera.com"},
			},
		},
		Local: config.LocalConfig{
			VendorID:         10415,
			ProductName:      "zone-test-server",
			WatchdogInterval: time.Hour,
		},
		Applications: []config.ApplicationConfig{{ID: 0, Type: "auth"}},
	}
	s := New(cfg, dict)
	if err := s.Start(); err != nil {
		t.Fatalf("starting server: %v", err)
	}
	t.Cleanup(s.Stop)

	coreClient := startClient(t, dict, listenerAddr(t, s, 0), "mme.example.com", "example.com")
	roamingClient := startClient(t, dict, listenerAddr(t, s, 1), "dea9.partnera.com", "partnera.com")

	waitFor(t, coreClient.IsOpen)
	waitFor(t, roamingClient.IsOpen)
	waitFor(t, func() bool {
		_, coreOK := s.GetPeer("mme.example.com")
		_, roamingOK := s.GetPeer("dea9.partnera.com")
		return coreOK && roamingOK
	})

	corePeer, _ := s.GetPeer("mme.example.com")
	if corePeer.Zone() != "core" || corePeer.Partner() != "" {
		t.Fatalf("core peer zone/partner = %q/%q, want core/<empty>", corePeer.Zone(), corePeer.Partner())
	}
	roamingPeer, _ := s.GetPeer("dea9.partnera.com")
	if roamingPeer.Zone() != "roaming" || roamingPeer.Partner() != "partner-a" {
		t.Fatalf("roaming peer zone/partner = %q/%q, want roaming/partner-a", roamingPeer.Zone(), roamingPeer.Partner())
	}
	if partner, ok := s.GetEdgeRegistry().PartnerForPeer("dea9.partnera.com"); !ok || partner.Name != "partner-a" {
		t.Fatalf("realm-admitted peer was not bound to partner-a: %v, %v", partner, ok)
	}
}

func TestLegacyListenerStartStopReleasesPortAndStopIsIdempotent(t *testing.T) {
	dict := testDictionary(t)
	cfg := &config.Config{
		Identity: "dea.example.com",
		Realm:    "example.com",
		Listen: config.ListenConfig{
			Addresses: []string{"127.0.0.1"},
			Port:      0,
			EnableTCP: true,
		},
		Local: config.LocalConfig{
			VendorID:         10415,
			ProductName:      "lifecycle-test-server",
			WatchdogInterval: time.Hour,
		},
		PeersPolicy:  config.PeersPolicy{AllowUnknown: true},
		Applications: []config.ApplicationConfig{{ID: 0, Type: "auth"}},
	}
	first := New(cfg, dict)
	if err := first.Start(); err != nil {
		t.Fatalf("starting first server: %v", err)
	}
	port := listenerPort(t, first, 0)
	first.Stop()
	first.Stop()

	secondCfg := &config.Config{
		Identity: cfg.Identity,
		Realm:    cfg.Realm,
		Listen: config.ListenConfig{
			Addresses: cfg.Listen.Addresses,
			Port:      port,
			EnableTCP: cfg.Listen.EnableTCP,
		},
		Local:        cfg.Local,
		PeersPolicy:  cfg.PeersPolicy,
		Applications: cfg.Applications,
	}
	second := New(secondCfg, dict)
	if err := second.Start(); err != nil {
		t.Fatalf("second server could not bind released port %d: %v", port, err)
	}
	t.Cleanup(second.Stop)

	conn, err := dialTest(t, listenerAddr(t, second, 0))
	if err != nil {
		t.Fatalf("dialing rebound listener: %v", err)
	}
	_ = conn.Close()
}

func TestConnectToPeerUsesConfiguredZoneAndPartnerTLSSource(t *testing.T) {
	dict := testDictionary(t)
	acceptorCfg := &config.Config{
		Identity: "acceptor.example.com",
		Realm:    "example.com",
		Listen: config.ListenConfig{
			Addresses: []string{"127.0.0.1"},
			Port:      0,
			EnableTCP: true,
		},
		PeersPolicy: config.PeersPolicy{AllowUnknown: true},
		Local: config.LocalConfig{
			VendorID:         10415,
			ProductName:      "acceptor",
			WatchdogInterval: time.Hour,
		},
		Applications: []config.ApplicationConfig{{ID: 0, Type: "auth"}},
	}
	acceptor := New(acceptorCfg, dict)
	if err := acceptor.Start(); err != nil {
		t.Fatalf("starting acceptor: %v", err)
	}
	t.Cleanup(acceptor.Stop)

	dialerCfg := &config.Config{
		Identity: "dialer.example.com",
		Realm:    "example.com",
		Zones: []config.ZoneConfig{{
			Name: "core",
			Role: config.ZoneRoleInternal,
		}},
		Local: config.LocalConfig{
			VendorID:         10415,
			ProductName:      "dialer",
			WatchdogInterval: time.Hour,
			ConnectTimeout:   time.Second,
		},
		Applications: []config.ApplicationConfig{{ID: 0, Type: "auth"}},
	}
	dialer := New(dialerCfg, dict)
	t.Cleanup(dialer.Stop)

	if err := dialer.connectToPeer(config.PeerConfig{
		Identity: "acceptor.example.com",
		Realm:    "example.com",
		Addresses: []string{
			"127.0.0.1",
		},
		Port: listenerPort(t, acceptor, 0),
		Zone: "core",
	}); err != nil {
		t.Fatalf("connectToPeer: %v", err)
	}

	waitFor(t, func() bool {
		_, ok := dialer.GetPeer("acceptor.example.com")
		return ok
	})
	connected, _ := dialer.GetPeer("acceptor.example.com")
	if connected.Zone() != "core" {
		t.Fatalf("outbound peer zone = %q, want core", connected.Zone())
	}
}

func TestServerAccessorsReflectConfiguredState(t *testing.T) {
	dict := dictionary.New()
	cfg := &config.Config{
		Identity: "dea.example.com",
		Realm:    "example.com",
		Routing: config.RoutingConfig{
			DefaultRealm:      "default.example.com",
			RelayEnabled:      true,
			DynamicRealmPeers: true,
			RealmRoutes: map[string][]string{
				"configured.example.com": {"peer-a.example.com"},
			},
			HostRoutes: map[string]string{
				"hss.example.com": "peer-a.example.com",
			},
		},
		Peers: []config.PeerConfig{{
			Identity: "auto.example.com",
			Realm:    "auto.example.com",
		}},
		Applications: []config.ApplicationConfig{{
			ID:      4,
			Type:    "auth",
			RouteTo: []string{"peer-a.example.com"},
		}},
	}
	s := New(cfg, dict)
	s.SetHandler(func(*peer.Peer, *message.Message) {})
	s.setupRouting()

	p := peer.New(peer.Config{DiameterIdentity: "peer-a.example.com"}, dict)
	s.mu.Lock()
	s.peers["peer-a.example.com"] = p
	s.mu.Unlock()

	if s.Router() == nil || s.GetRouter() != s.Router() {
		t.Fatal("router accessors did not return the configured router")
	}
	if s.Dictionary() != dict || s.GetDictionary() != dict {
		t.Fatal("dictionary accessors did not return the configured dictionary")
	}
	if s.GetConfig() != cfg {
		t.Fatal("config accessor did not return the server config")
	}
	if s.StartTime().IsZero() || !s.GetStartTime().Equal(s.StartTime()) {
		t.Fatal("start time accessors did not return the creation time")
	}
	if s.EdgeRegistry() != s.GetEdgeRegistry() {
		t.Fatal("edge registry accessors disagree")
	}
	if got, ok := s.Router().LookupPeer("peer-a.example.com"); !ok || got != p {
		t.Fatalf("router peer lookup = %v, %v; want inserted peer", got, ok)
	}
	if got := s.Peers(); len(got) != 1 || got[0] != p {
		t.Fatalf("Peers() = %v, want inserted peer", got)
	}
	if got := s.GetPeers(); len(got) != 1 || got[0] != p {
		t.Fatalf("GetPeers() = %v, want inserted peer", got)
	}
	if _, ok := s.GetPeer("missing.example.com"); ok {
		t.Fatal("GetPeer found a peer that was never registered")
	}
	if err := s.Send("missing.example.com", message.NewRequest(types.CmdCodeDeviceWatchdog, 0)); err == nil {
		t.Fatal("Send to an unknown peer succeeded")
	}
	table := s.Router().RoutingTable()
	if table.DefaultRealm != "default.example.com" {
		t.Fatalf("default realm = %q, want default.example.com", table.DefaultRealm)
	}
	if fmt.Sprint(table.RealmRoutes["auto.example.com"]) != "[auto.example.com]" {
		t.Fatalf("auto-populated realm route missing: %v", table.RealmRoutes)
	}
	if table.HostRoutes["hss.example.com"] != "peer-a.example.com" {
		t.Fatalf("host route missing: %v", table.HostRoutes)
	}

	if err := s.startExtensions(); err != nil {
		t.Fatalf("starting extensions: %v", err)
	}
	if s.GetExtensionManager() == nil {
		t.Fatal("extension manager was not exposed after startup")
	}
}

func TestRoutingErrorsAreCountedWithoutDoubleSending(t *testing.T) {
	s := New(&config.Config{Identity: "dea.example.com", Realm: "example.com"}, dictionary.New())

	s.handleRoutingError(nil, nil)
	if got := s.routingErrors.Load(); got != 0 {
		t.Fatalf("nil routing error changed count to %d", got)
	}

	s.handleRoutingError(nil, errors.New("no route"))
	if got := s.routingErrors.Load(); got != 1 {
		t.Fatalf("plain routing error count = %d, want 1", got)
	}

	answer := message.NewRequest(types.CmdCodeDeviceWatchdog, 0)
	rerr := &routing.RoutingError{
		Code:   types.ResultUnableToDeliver,
		Answer: answer,
	}
	s.handleRoutingError(nil, rerr)
	if got := s.routingErrors.Load(); got != 2 {
		t.Fatalf("routing error count = %d, want 2", got)
	}
	if rerr.Sent {
		t.Fatal("routing error was marked sent even though there was no peer to send it to")
	}
}

func TestServerTLSConfigurationErrorsAreRejectedBeforeListening(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateIdentityCert(t, dir, "dea.example.com", []string{"dea.example.com"})

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "tls without transport",
			body: fmt.Sprintf(`
zones:
  - name: roaming
    role: external
    tls:
      enabled: true
      cert_file: %q
      key_file: %q
`, certPath, keyPath),
			want: "tls.enabled but no transport is enabled",
		},
		{
			name: "verify peer without ca file",
			body: fmt.Sprintf(`
zones:
  - name: roaming
    role: external
    listen:
      enable_tls: true
    tls:
      enabled: true
      cert_file: %q
      key_file: %q
      verify_peer: true
`, certPath, keyPath),
			want: "tls.verify_peer requires ca_file",
		},
		{
			name: "secure listener without tls",
			body: `
zones:
  - name: roaming
    role: external
    listen:
      enable_tls: true
`,
			want: "listen.enable_tls requires tls.enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse([]byte("identity: \"dea.example.com\"\nrealm: \"example.com\"\n" + tc.body))
			if err == nil {
				t.Fatal("configuration parsed successfully")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestLoadCertPoolRejectsUnreadableOrNonCertificateFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pem")
	if _, err := loadCertPool(missing); err == nil {
		t.Fatal("missing CA file loaded successfully")
	}

	notPEM := filepath.Join(t.TempDir(), "not-a-cert.pem")
	if err := os.WriteFile(notPEM, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("writing invalid CA file: %v", err)
	}
	if _, err := loadCertPool(notPEM); err == nil {
		t.Fatal("invalid CA file loaded successfully")
	}
}

func TestZoneOrDefaultUsesImplicitZoneWhenAcceptLoopHasNoTag(t *testing.T) {
	cfg := parseServerConfig(t, `
listen:
  port: 0
  enable_tcp: true
`)
	s := New(cfg, dictionary.New())
	zone := s.zoneOrDefault(nil)
	if zone.Name != config.DefaultZoneName || !zone.IsImplicit() {
		t.Fatalf("zoneOrDefault(nil) = %q implicit=%v, want implicit default", zone.Name, zone.IsImplicit())
	}
}
