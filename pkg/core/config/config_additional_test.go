// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func packageScratchFile(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join("testdata", "generated")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

func TestLoadReportsFileAndSyntaxProblems(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "generated", "missing.yaml"))
	if err == nil || !strings.Contains(err.Error(), "reading config file") {
		t.Fatalf("missing config error = %v, want file-reading context", err)
	}

	path := packageScratchFile(t, "malformed.yaml")
	if err := os.WriteFile(path, []byte("identity: [unterminated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(path)
	if err == nil || !strings.Contains(err.Error(), "parsing config") {
		t.Fatalf("malformed config error = %v, want YAML parsing context", err)
	}
}

func TestParseAppliesOperationalDefaults(t *testing.T) {
	cfg, err := Parse([]byte(`
identity: server.example.com
realm: example.com
peers:
  - identity: peer.example.com
    addresses: [192.0.2.10]
`))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if cfg.Listen.Port != 3868 || cfg.Listen.SecurePort != 5868 {
		t.Fatalf("unexpected listener defaults: %+v", cfg.Listen)
	}
	if cfg.Local.ProductName != "godiam" || cfg.Local.WatchdogInterval != 30*time.Second || cfg.Local.ConnectTimeout != 10*time.Second || cfg.Local.RequestTimeout != 30*time.Second {
		t.Fatalf("local liveness defaults not applied: %+v", cfg.Local)
	}
	peer := cfg.Peers[0]
	if peer.Port != 3868 || peer.Network != "tcp" || peer.ReconnectInterval != 30*time.Second {
		t.Fatalf("peer reconnect defaults not applied: %+v", peer)
	}
	if zones := cfg.EffectiveZones(); len(zones) != 1 || !zones[0].IsImplicit() {
		t.Fatalf("pre-edge config should synthesize one implicit zone, got %+v", zones)
	}
}

func TestConfiguredApplicationIDsAreSplitByType(t *testing.T) {
	cfg := &Config{Applications: []ApplicationConfig{
		{ID: 4, Type: "acct"},
		{ID: uint32(types.AppIDCreditControl), Type: "auth"},
		{ID: 16777251, Type: "auth"},
	}}

	if got, want := cfg.GetAuthAppIDs(), []types.ApplicationID{types.AppIDCreditControl, 16777251}; !reflect.DeepEqual(got, want) {
		t.Fatalf("auth app IDs = %v, want %v", got, want)
	}
	if got, want := cfg.GetAcctAppIDs(), []types.ApplicationID{4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("acct app IDs = %v, want %v", got, want)
	}
}

func TestExampleDocumentsAValidateableShapeExceptLocalFilePaths(t *testing.T) {
	cfg := Example()
	if cfg.Identity == "" || cfg.Realm == "" {
		t.Fatalf("example omits Diameter identity: %+v", cfg)
	}
	if cfg.Listen.Port != 3868 || !cfg.Listen.EnableTCP {
		t.Fatalf("example listener is not a usable TCP Diameter listener: %+v", cfg.Listen)
	}
	if got := cfg.GetAuthAppIDs(); len(got) != 1 || got[0] != types.AppIDCreditControl {
		t.Fatalf("example auth applications = %v", got)
	}
	if got := cfg.GetAcctAppIDs(); len(got) != 1 || got[0] != types.AppIDBaseAccounting {
		t.Fatalf("example accounting applications = %v", got)
	}
}

func TestSaveExtensionsRewritesOnlyTheExtensionSection(t *testing.T) {
	path := packageScratchFile(t, "save-extensions.yaml")
	original := []byte(`identity: server.example.com
realm: example.com
listen:
  port: 3868
extensions:
  - name: old
    config:
      enabled: false
`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	newExt := []ExtensionConfig{{Name: "screening", Config: map[string]interface{}{"mode": "strict"}}, {Name: "metrics"}}
	if err := cfg.SaveExtensions(newExt); err != nil {
		t.Fatalf("SaveExtensions failed: %v", err)
	}
	if !reflect.DeepEqual(cfg.Extensions, newExt) {
		t.Fatalf("in-memory extensions = %#v, want %#v", cfg.Extensions, newExt)
	}

	written, err := os.ReadFile(path) //nolint:gosec // path is created by this test under package testdata
	if err != nil {
		t.Fatal(err)
	}
	text := string(written)
	for _, want := range []string{"identity: server.example.com", "realm: example.com", "name: screening", "mode: strict", "name: metrics"} {
		if !strings.Contains(text, want) {
			t.Fatalf("updated config missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "name: old") {
		t.Fatalf("old extension survived rewrite:\n%s", text)
	}
}

func TestSaveExtensionsRequiresLoadedConfigPath(t *testing.T) {
	if err := (&Config{}).SaveExtensions(nil); err == nil || !strings.Contains(err.Error(), "no config file path") {
		t.Fatalf("SaveExtensions without ConfigPath error = %v", err)
	}
}
