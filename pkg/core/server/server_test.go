// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func generateTestCert(t *testing.T, dir string) (string, string) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Co"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPath := filepath.Join(dir, "cert.pem")
	certOut, err := os.Create(certPath) //nolint:gosec // G304: test file path
	if err != nil {
		t.Fatalf("Failed to open cert.pem for writing: %v", err)
	}
	_ = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	_ = certOut.Close()

	keyPath := filepath.Join(dir, "key.pem")
	keyOut, err := os.Create(keyPath) //nolint:gosec // G304: test file path
	if err != nil {
		t.Fatalf("Failed to open key.pem for writing: %v", err)
	}
	b, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("Unable to marshal ECDSA private key: %v", err)
	}
	_ = pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
	_ = keyOut.Close()

	return certPath, keyPath
}

func TestServerTLS(t *testing.T) {
	// Create temp dir for certs
	tmpDir, err := os.MkdirTemp("", "diameter-tls-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	certFile, keyFile := generateTestCert(t, tmpDir)

	// Free port
	l, _ := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test code does not propagate context
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	// Server Config
	cfg := &config.Config{
		Identity: "server.test.com",
		Realm:    "test.com",
		Listen: config.ListenConfig{
			Port:      port, // Use same port for TLS in this simplified test
			EnableTCP: true, // We will override with TLS enabled
		},
		TLS: config.TLSConfig{
			Enabled:  true,
			CertFile: certFile,
			KeyFile:  keyFile,
		},
		Local: config.LocalConfig{
			VendorID:    10415,
			ProductName: "TestServer",
		},
		PeersPolicy: config.PeersPolicy{
			AllowUnknown: true, // Accept unknown clients for testing
		},
		Applications: []config.ApplicationConfig{
			{ID: 0, Type: "auth"}, // Common Messages (required for CER/CEA)
		},
	}

	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()

	srv := New(cfg, dict)
	if err := srv.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Stop()

	// Wait for server to listen
	time.Sleep(200 * time.Millisecond)

	// Create Client Peer
	clientCfg := peer.Config{
		DiameterIdentity: "server.test.com", // Expect server identity
		Realm:            "test.com",
		Addresses:        []string{"127.0.0.1"},
		Port:             port,
		UseTLS:           true,
		TLSConfig: &peer.TLSConfig{
			SkipVerify: true, // Self-signed cert
			CertFile:   certFile,
			KeyFile:    keyFile,
		},
		ConnectTimeout:    time.Second,
		WatchdogInterval:  time.Second,
		ReconnectInterval: time.Second,
	}

	client := peer.New(clientCfg, dict)
	// Set Client's local identity so it differs from Server's
	client.SetLocalOverride(peer.LocalConfig{
		DiameterIdentity: "client.test.com",
		Realm:            "test.com",
		VendorID:         10415,
		ProductName:      "TestClient",
		HostIPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		OriginStateID:    1,
	})

	client.SetOnStateChange(func(_ *peer.Peer, oldState, newState peer.State) {
		t.Logf("Client State Change: %s -> %s", oldState, newState)
	})

	_ = client.Start()
	defer client.Stop()

	// Wait for connection
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for connection")
		case <-ticker.C:
			if client.IsOpen() {
				t.Log("Connection established successfully over TLS")
				return
			}
		}
	}
}

func TestRelayAppIDAdvertised(t *testing.T) {
	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()

	cfg := &config.Config{
		Identity: "relay.example.com",
		Realm:    "example.com",
		Listen: config.ListenConfig{
			Port:      0,
			EnableTCP: true,
		},
		Routing: config.RoutingConfig{
			RelayEnabled: true,
		},
		Applications: []config.ApplicationConfig{
			{ID: 4, Type: "auth"},
		},
	}

	srv := New(cfg, dict)
	if err := srv.Start(); err != nil {
		t.Fatalf("Server start failed: %v", err)
	}
	defer srv.Stop()

	lc := peer.GetLocalConfig()
	found := false
	for _, id := range lc.AuthAppIDs {
		if id == types.AppIDRelay {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Relay Application-ID (0x%x) not found in AuthAppIDs: %v", types.AppIDRelay, lc.AuthAppIDs)
	}
}

func TestRelayAppIDNotAdvertisedWhenDisabled(t *testing.T) {
	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()

	cfg := &config.Config{
		Identity: "server.example.com",
		Realm:    "example.com",
		Listen: config.ListenConfig{
			Port:      0,
			EnableTCP: true,
		},
		Routing: config.RoutingConfig{
			RelayEnabled: false,
		},
		Applications: []config.ApplicationConfig{
			{ID: 4, Type: "auth"},
		},
	}

	srv := New(cfg, dict)
	if err := srv.Start(); err != nil {
		t.Fatalf("Server start failed: %v", err)
	}
	defer srv.Stop()

	lc := peer.GetLocalConfig()
	for _, id := range lc.AuthAppIDs {
		if id == types.AppIDRelay {
			t.Errorf("Relay Application-ID should not be advertised when relay is disabled")
		}
	}
}

func TestRelayAppIDDeduplication(t *testing.T) {
	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()

	cfg := &config.Config{
		Identity: "relay.example.com",
		Realm:    "example.com",
		Listen: config.ListenConfig{
			Port:      0,
			EnableTCP: true,
		},
		Routing: config.RoutingConfig{
			RelayEnabled: true,
		},
		Applications: []config.ApplicationConfig{
			// User explicitly configures the relay app-id
			{ID: uint32(types.AppIDRelay), Type: "auth"},
			{ID: 4, Type: "auth"},
		},
	}

	srv := New(cfg, dict)
	if err := srv.Start(); err != nil {
		t.Fatalf("Server start failed: %v", err)
	}
	defer srv.Stop()

	lc := peer.GetLocalConfig()
	count := 0
	for _, id := range lc.AuthAppIDs {
		if id == types.AppIDRelay {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected exactly 1 Relay Application-ID, got %d in: %v", count, lc.AuthAppIDs)
	}
}
