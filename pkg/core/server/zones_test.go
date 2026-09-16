// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// dialTest opens a short-lived TCP connection to verify a listener is live.
func dialTest(t *testing.T, addr string) (net.Conn, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

func parseServerConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte("identity: \"dea.example.com\"\nrealm: \"example.com\"\n" + body))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	return cfg
}

func zoneByName(t *testing.T, cfg *config.Config, name string) *config.ZoneConfig {
	t.Helper()
	zones := cfg.EffectiveZones()
	for i := range zones {
		if zones[i].Name == name {
			return &zones[i]
		}
	}
	t.Fatalf("zone %q not found", name)
	return nil
}

const admissionConfig = `
zones:
  - name: core
    role: internal
  - name: roaming
    role: external
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com"]
    peers: ["dea1.partnera.com"]
  - name: partner-b
    zone: roaming
    realms: ["partnerb.com"]
    peers: ["dea1.partnerb.com"]
peers:
  - identity: "hss.example.com"
    addresses: ["10.0.0.5"]
    zone: core
  - identity: "mme.example.com"
    addresses: ["10.0.0.6"]
    realm: "example.com"
    zone: core
`

func TestAdmitPeerZoneAndPartner(t *testing.T) {
	cfg := parseServerConfig(t, admissionConfig)
	s := New(cfg, dictionary.New())

	tests := []struct {
		name        string
		zone        string
		originHost  string
		originRealm string
		wantAccept  bool
		wantPartner string
	}{
		{"partner peer on its zone", "roaming", "dea1.partnera.com", "partnera.com", true, "partner-a"},
		{"partner peer on wrong zone", "core", "dea1.partnera.com", "partnera.com", false, ""},
		{"partner peer with foreign realm", "roaming", "dea1.partnera.com", "partnerb.com", false, ""},
		{"unlisted peer of known partner realm", "roaming", "dea9.partnerb.com", "partnerb.com", true, "partner-b"},
		{"unknown external peer", "roaming", "evil.attacker.com", "attacker.com", false, ""},
		{"configured internal peer", "core", "hss.example.com", "example.com", true, ""},
		{"internal peer realm mismatch", "core", "mme.example.com", "evil.com", false, ""},
		{"unknown internal peer", "core", "rogue.example.com", "example.com", false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			zone := zoneByName(t, cfg, tc.zone)
			res := s.admitPeer(zone, nil, types.DiamID(tc.originHost), types.DiamID(tc.originRealm))
			if res.accepted != tc.wantAccept {
				t.Fatalf("accepted = %v (%s), want %v", res.accepted, res.reason, tc.wantAccept)
			}
			if res.accepted && res.partner != tc.wantPartner {
				t.Fatalf("partner = %q, want %q", res.partner, tc.wantPartner)
			}
			if !res.accepted && res.code != types.ResultUnknownPeer {
				t.Fatalf("result code = %d, want %d", res.code, types.ResultUnknownPeer)
			}
		})
	}
}

func TestAdmitPeerZonePolicyAllowUnknown(t *testing.T) {
	// allow_unknown is rejected by validation on an external zone, because an
	// unknown peer there would bypass every edge policy, so it is only
	// meaningful inside the network.
	cfg := parseServerConfig(t, `
zones:
  - name: core
    role: internal
    peers_policy:
      allow_unknown: true
  - name: locked
    role: external
    peers_policy:
      allow_identities: ["known.partner.com"]
`)
	s := New(cfg, dictionary.New())

	if res := s.admitPeer(zoneByName(t, cfg, "core"), nil, "any.host.com", "host.com"); !res.accepted {
		t.Fatalf("allow_unknown zone should accept: %s", res.reason)
	}
	if res := s.admitPeer(zoneByName(t, cfg, "locked"), nil, "known.partner.com", "partner.com"); !res.accepted {
		t.Fatalf("allowlisted identity should be accepted: %s", res.reason)
	}
	if res := s.admitPeer(zoneByName(t, cfg, "locked"), nil, "other.partner.com", "partner.com"); res.accepted {
		t.Fatal("non-allowlisted identity should be rejected")
	}
}

func TestAdmitPeerLegacyConfigUnchanged(t *testing.T) {
	cfg := parseServerConfig(t, `
peers_policy:
  allow_unknown: true
peers:
  - identity: "peer.example.com"
    addresses: ["10.0.0.1"]
`)
	s := New(cfg, dictionary.New())
	zone := zoneByName(t, cfg, config.DefaultZoneName)

	if res := s.admitPeer(zone, nil, "whoever.example.com", "example.com"); !res.accepted {
		t.Fatalf("legacy allow_unknown must still accept: %s", res.reason)
	}
}

// --- TLS client certificate identity binding -------------------------------

func generateIdentityCert(t *testing.T, dir, name string, dnsNames []string) (certPath, keyPath string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}

	certPath = filepath.Join(dir, name+"-cert.pem")
	keyPath = filepath.Join(dir, name+"-key.pem")
	certOut, err := os.Create(certPath) //nolint:gosec // G304: test path
	if err != nil {
		t.Fatalf("creating cert file: %v", err)
	}
	_ = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	_ = certOut.Close()

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshaling key: %v", err)
	}
	keyOut, err := os.Create(keyPath) //nolint:gosec // G304: test path
	if err != nil {
		t.Fatalf("creating key file: %v", err)
	}
	_ = pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	_ = keyOut.Close()
	return certPath, keyPath
}

// tlsPair performs a real TLS handshake over an in-memory pipe and returns the
// server side of the connection, so certificate binding can be tested.
func tlsPair(t *testing.T, serverCert, serverKey, clientCert, clientKey string) net.Conn {
	t.Helper()

	srvCert, err := tls.LoadX509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatalf("loading server keypair: %v", err)
	}
	cliCert, err := tls.LoadX509KeyPair(clientCert, clientKey)
	if err != nil {
		t.Fatalf("loading client keypair: %v", err)
	}

	pool := x509.NewCertPool()
	pem, err := os.ReadFile(clientCert) //nolint:gosec // G304: test path
	if err != nil {
		t.Fatalf("reading client cert: %v", err)
	}
	pool.AppendCertsFromPEM(pem)

	c1, c2 := net.Pipe()
	server := tls.Server(c1, &tls.Config{
		Certificates: []tls.Certificate{srvCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	})
	client := tls.Client(c2, &tls.Config{
		Certificates:       []tls.Certificate{cliCert},
		InsecureSkipVerify: true, //nolint:gosec // G402: test-only client
		MinVersion:         tls.VersionTLS12,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- client.HandshakeContext(ctx) }()
	if err := server.HandshakeContext(ctx); err != nil {
		t.Fatalf("server handshake: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	// Close the raw pipes: closing the TLS conns would block writing close_notify
	// because net.Pipe is unbuffered and has no reader at that point.
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})
	return server
}

func TestAdmitPeerRequiresClientCertIdentity(t *testing.T) {
	dir := t.TempDir()
	srvCertPath, srvKeyPath := generateIdentityCert(t, dir, "dea.example.com", []string{"dea.example.com"})
	cliCertPath, cliKeyPath := generateIdentityCert(t, dir, "dea1.partnera.com", []string{"dea1.partnera.com"})

	cfg := parseServerConfig(t, fmt.Sprintf(`
zones:
  - name: roaming
    role: external
    listen:
      enable_tls: true
    tls:
      enabled: true
      cert_file: %q
      key_file: %q
      ca_file: %q
      verify_peer: true
      require_client_cert_identity: true
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com"]
    peers: ["dea1.partnera.com", "dea2.partnera.com"]
`, srvCertPath, srvKeyPath, cliCertPath))

	s := New(cfg, dictionary.New())
	zone := zoneByName(t, cfg, "roaming")
	conn := tlsPair(t, srvCertPath, srvKeyPath, cliCertPath, cliKeyPath)

	if res := s.admitPeer(zone, conn, "dea1.partnera.com", "partnera.com"); !res.accepted {
		t.Fatalf("matching certificate identity should be accepted: %s", res.reason)
	}
	// Same connection, but the CER claims a different (also valid) partner peer.
	res := s.admitPeer(zone, conn, "dea2.partnera.com", "partnera.com")
	if res.accepted {
		t.Fatal("certificate identity mismatch must be rejected")
	}
	if res.code != types.ResultUnknownPeer {
		t.Fatalf("result code = %d, want %d", res.code, types.ResultUnknownPeer)
	}

	// A non-TLS connection can never satisfy the binding requirement.
	if res := s.admitPeer(zone, nil, "dea1.partnera.com", "partnera.com"); res.accepted {
		t.Fatal("non-TLS connection must be rejected when cert binding is required")
	}
}

func TestBuildServerTLSConfig(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateIdentityCert(t, dir, "dea.example.com", []string{"dea.example.com"})

	cfg, err := buildServerTLSConfig(&config.ZoneTLSConfig{
		TLSConfig: config.TLSConfig{
			Enabled:    true,
			CertFile:   certPath,
			KeyFile:    keyPath,
			CAFile:     certPath,
			VerifyPeer: true,
			MinVersion: "1.3",
		},
	})
	if err != nil {
		t.Fatalf("building TLS config: %v", err)
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("client auth = %v, want RequireAndVerifyClientCert", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Fatal("client CA pool was not loaded")
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("min version = %x, want TLS 1.3", cfg.MinVersion)
	}

	// Configuration validation rejects verify_peer without ca_file on a zone.
	// A configuration predating zones can still reach here, and it has to keep
	// starting: client certificates then verify against the system roots.
	legacy, err := buildServerTLSConfig(&config.ZoneTLSConfig{
		TLSConfig: config.TLSConfig{
			Enabled:    true,
			CertFile:   certPath,
			KeyFile:    keyPath,
			VerifyPeer: true,
		},
	})
	if err != nil {
		t.Fatalf("legacy verify_peer without ca_file must still start: %v", err)
	}
	if legacy.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("client auth = %v, want RequireAndVerifyClientCert", legacy.ClientAuth)
	}
	if legacy.ClientCAs != nil {
		t.Fatal("no ca_file was given, so the system root store must be used")
	}
}

func TestStartListenersPerZone(t *testing.T) {
	cfg := parseServerConfig(t, `
zones:
  - name: core
    role: internal
    listen:
      addresses: ["127.0.0.1"]
      port: 13868
      enable_tcp: true
  - name: roaming
    role: external
    listen:
      addresses: ["127.0.0.1"]
      port: 13869
      enable_tcp: true
`)
	s := New(cfg, dictionary.New())
	if err := s.startListeners(); err != nil {
		t.Fatalf("starting listeners: %v", err)
	}
	defer s.Stop()

	s.mu.RLock()
	got := len(s.listeners)
	s.mu.RUnlock()
	if got != 2 {
		t.Fatalf("expected 2 listeners, got %d", got)
	}

	for _, port := range []int{13868, 13869} {
		conn, err := dialTest(t, fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("dialing port %d: %v", port, err)
		}
		_ = conn.Close()
	}
}

func TestStartListenersNoZonesUsesNodeListen(t *testing.T) {
	cfg := parseServerConfig(t, `
listen:
  addresses: ["127.0.0.1"]
  port: 13870
  enable_tcp: true
`)
	s := New(cfg, dictionary.New())
	if err := s.startListeners(); err != nil {
		t.Fatalf("starting listeners: %v", err)
	}
	defer s.Stop()

	conn, err := dialTest(t, "127.0.0.1:13870")
	if err != nil {
		t.Fatalf("dialing legacy listener: %v", err)
	}
	_ = conn.Close()
}

// --- listener wiring -------------------------------------------------------

// freePort reserves and releases a port so the test can bind it deliberately.
func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// TestZoneListensOnSecurePort covers a partner-facing zone declared with
// enable_tls and secure_port only: it must produce a working TLS listener
// rather than silently binding nothing.
func TestZoneListensOnSecurePort(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateIdentityCert(t, dir, "dea.example.com", []string{"dea.example.com"})
	port := freePort(t)

	cfg := parseServerConfig(t, fmt.Sprintf(`
zones:
  - name: roaming
    role: external
    listen:
      addresses: ["127.0.0.1"]
      secure_port: %d
      enable_tls: true
    tls:
      enabled: true
      cert_file: %q
      key_file: %q
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com"]
    peers: ["dea1.partnera.com"]
`, port, certPath, keyPath))

	s := New(cfg, dictionary.New())
	zone := zoneByName(t, cfg, "roaming")
	if err := s.startZoneListeners(zone); err != nil {
		t.Fatalf("startZoneListeners: %v", err)
	}
	t.Cleanup(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, l := range s.listeners {
			_ = l.Close()
		}
	})

	dialer := &tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // the test only checks that the listener is there
	conn, err := dialer.DialContext(context.Background(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dialing the secure port: %v", err)
	}
	_ = conn.Close()
}

// TestLegacyZoneBindsAllInterfaces pins the pre-edge behaviour where
// listen.addresses steered SCTP only and TCP always bound every interface.
func TestLegacyZoneBindsAllInterfaces(t *testing.T) {
	port := freePort(t)
	cfg := parseServerConfig(t, fmt.Sprintf(`
listen:
  addresses: ["127.0.0.1"]
  port: %d
  enable_tcp: true
`, port))

	s := New(cfg, dictionary.New())
	zone := zoneByName(t, cfg, config.DefaultZoneName)
	if !zone.IsImplicit() {
		t.Fatal("a config without zones should produce an implicit zone")
	}
	if err := s.startZoneListeners(zone); err != nil {
		t.Fatalf("startZoneListeners: %v", err)
	}
	t.Cleanup(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, l := range s.listeners {
			_ = l.Close()
		}
	})

	s.mu.Lock()
	addr := s.listeners[0].Addr().String()
	s.mu.Unlock()
	if strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("listener bound %s, want every interface as before the edge model", addr)
	}
}

// Admission by realm is the one path that produces a partner peer the static
// configuration never named. The binding recorded for it is what lets topology
// hiding recognise the peer later, so losing it silently reopens the leak of
// internal identities toward that partner.
func TestAdmitPeerBindsRealmMatchedPartner(t *testing.T) {
	cfg := parseServerConfig(t, admissionConfig)
	s := New(cfg, dictionary.New())
	reg := s.GetEdgeRegistry()
	zone := zoneByName(t, cfg, "roaming")

	const unlisted = "dea9.partnerb.com"
	if _, ok := reg.PartnerForPeer(unlisted); ok {
		t.Fatal("peer should be unknown before admission")
	}

	res := s.admitPeer(zone, nil, types.DiamID(unlisted), "partnerb.com")
	if !res.accepted {
		t.Fatalf("realm-matched peer rejected: %s", res.reason)
	}
	// admitPeer itself does not bind; the accept-policy closure does, so mirror
	// the one line of it that matters here.
	reg.BindPeer(unlisted, res.partner)

	p, ok := reg.PartnerForPeer(unlisted)
	if !ok || p.Name != "partner-b" {
		t.Fatalf("PartnerForPeer(%q) = %v, %v; want partner-b", unlisted, p, ok)
	}
}
