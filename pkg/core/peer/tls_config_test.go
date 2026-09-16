// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package peer

import (
	"crypto/tls"
	"path/filepath"
	"testing"
)

func TestLoadTLSConfigWithoutExplicitConfigKeepsCompatibilityMode(t *testing.T) {
	p := New(testConfig("tls-default.example.com"), testDict())
	t.Cleanup(p.Stop)

	cfg, err := p.loadTLSConfig()
	if err != nil {
		t.Fatalf("loadTLSConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("loadTLSConfig returned nil config")
	}
	if !cfg.InsecureSkipVerify {
		t.Fatal("default TLS config no longer preserves compatibility skip-verify mode")
	}
}

func TestLoadTLSConfigReportsMissingCertificateFiles(t *testing.T) {
	cfg := testConfig("tls-cert.example.com")
	cfg.TLSConfig = &TLSConfig{
		CertFile: filepath.Join("pkg", "core", "peer", "testdata", "missing-client.crt"),
		KeyFile:  filepath.Join("pkg", "core", "peer", "testdata", "missing-client.key"),
	}
	p := New(cfg, testDict())
	t.Cleanup(p.Stop)

	if _, err := p.loadTLSConfig(); err == nil {
		t.Fatal("loadTLSConfig succeeded with missing certificate files")
	}
}

func TestLoadTLSConfigReportsMissingCAFile(t *testing.T) {
	cfg := testConfig("tls-ca.example.com")
	cfg.TLSConfig = &TLSConfig{
		CAFile: filepath.Join("pkg", "core", "peer", "testdata", "missing-ca.pem"),
	}
	p := New(cfg, testDict())
	t.Cleanup(p.Stop)

	if _, err := p.loadTLSConfig(); err == nil {
		t.Fatal("loadTLSConfig succeeded with a missing CA file")
	}
}

func TestLoadTLSConfigMapsMinimumVersionAndServerName(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want uint16
	}{
		{name: "TLS 1.3", in: "1.3", want: tls.VersionTLS13},
		{name: "TLS 1.2", in: "1.2", want: tls.VersionTLS12},
		{name: "unknown values fall back to TLS 1.2", in: "1.1", want: tls.VersionTLS12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig("tls-version.example.com")
			cfg.TLSConfig = &TLSConfig{MinVersion: tc.in, SkipVerify: true}
			p := New(cfg, testDict())
			t.Cleanup(p.Stop)

			got, err := p.loadTLSConfig()
			if err != nil {
				t.Fatalf("loadTLSConfig: %v", err)
			}
			if got.MinVersion != tc.want {
				t.Fatalf("MinVersion = %#04x, want %#04x", got.MinVersion, tc.want)
			}
			if got.ServerName != "tls-version.example.com" {
				t.Fatalf("ServerName = %q, want peer DiameterIdentity", got.ServerName)
			}
			if !got.InsecureSkipVerify {
				t.Fatal("SkipVerify setting was not copied")
			}
		})
	}
}
