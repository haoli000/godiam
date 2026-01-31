// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate_ListenAddresses_Valid(t *testing.T) {
	cfg := &Config{
		Identity: "server.example.com",
		Realm:    "example.com",
		Listen: ListenConfig{
			Port:      3868,
			EnableTCP: true,
			Addresses: []string{"127.0.0.1", "192.168.1.1"},
		},
	}
	if err := validate(cfg); err != nil {
		t.Errorf("Expected valid config, got error: %v", err)
	}
}

func TestValidate_ListenAddresses_IPv6(t *testing.T) {
	cfg := &Config{
		Identity: "server.example.com",
		Realm:    "example.com",
		Listen: ListenConfig{
			Port:      3868,
			EnableTCP: true,
			Addresses: []string{"::1", "fe80::1"},
		},
	}
	if err := validate(cfg); err != nil {
		t.Errorf("Expected valid config with IPv6, got error: %v", err)
	}
}

func TestValidate_ListenAddresses_Invalid(t *testing.T) {
	cfg := &Config{
		Identity: "server.example.com",
		Realm:    "example.com",
		Listen: ListenConfig{
			Port:      3868,
			EnableTCP: true,
			Addresses: []string{"127.0.0.1", "not-an-ip"},
		},
	}
	err := validate(cfg)
	if err == nil {
		t.Fatal("Expected error for invalid IP address")
	}
	expected := `listen.addresses[1]: "not-an-ip" is not a valid IP address`
	if err.Error() != expected {
		t.Errorf("Expected error %q, got %q", expected, err.Error())
	}
}

func TestValidate_ListenAddresses_Empty(t *testing.T) {
	cfg := &Config{
		Identity: "server.example.com",
		Realm:    "example.com",
		Listen: ListenConfig{
			Port:      3868,
			EnableTCP: true,
		},
	}
	if err := validate(cfg); err != nil {
		t.Errorf("Expected valid config with no addresses, got error: %v", err)
	}
}

func TestValidate_ListenAddresses_Hostname(t *testing.T) {
	cfg := &Config{
		Identity: "server.example.com",
		Realm:    "example.com",
		Listen: ListenConfig{
			Port:      3868,
			EnableTCP: true,
			Addresses: []string{"my-host.local"},
		},
	}
	err := validate(cfg)
	if err == nil {
		t.Fatal("Expected error for hostname (not IP) in listen.addresses")
	}
}

func TestLoadConfig_WithAddresses(t *testing.T) {
	yaml := `
identity: server.example.com
realm: example.com
listen:
  port: 3868
  enable_tcp: true
  addresses:
    - "172.20.11.1"
    - "172.20.12.1"
`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(cfgFile, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Listen.Addresses) != 2 {
		t.Fatalf("Expected 2 addresses, got %d", len(cfg.Listen.Addresses))
	}
	if cfg.Listen.Addresses[0] != "172.20.11.1" || cfg.Listen.Addresses[1] != "172.20.12.1" {
		t.Errorf("Unexpected addresses: %v", cfg.Listen.Addresses)
	}
}
