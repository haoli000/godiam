// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package config provides Diameter configuration management.
// This replaces the flex/bison config parser from the original freeDiameter.
package config

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/haoli000/godiam/pkg/proto/types"
	"gopkg.in/yaml.v3"
)

// Config represents the complete Diameter node configuration.
type Config struct {
	// Identity is the Diameter Identity of this node (FQDN)
	Identity string `yaml:"identity"`
	// Realm is the Diameter Realm of this node
	Realm string `yaml:"realm"`

	// Listen configures the listening endpoints
	Listen ListenConfig `yaml:"listen"`

	// TLS configures TLS settings
	TLS TLSConfig `yaml:"tls"`

	// Local configures local node properties
	Local LocalConfig `yaml:"local"`

	// Peers defines the known peers
	Peers []PeerConfig `yaml:"peers"`

	// PeersPolicy configures inbound peer acceptance policy
	PeersPolicy PeersPolicy `yaml:"peers_policy"`

	// Zones defines the edge zones (internal/external network boundaries).
	// When empty, a single implicit internal zone is derived from
	// listen/tls/peers_policy, preserving pre-DEA behaviour.
	Zones []ZoneConfig `yaml:"zones,omitempty"`

	// Partners defines roaming partners reachable through external zones.
	Partners []PartnerConfig `yaml:"partners,omitempty"`

	// Applications lists the supported Diameter applications
	Applications []ApplicationConfig `yaml:"applications"`

	// Routing configures message routing
	Routing RoutingConfig `yaml:"routing"`

	// Extensions lists the extensions to load
	Extensions []ExtensionConfig `yaml:"extensions"`

	// ConfigPath is the path to the source config file (not serialized).
	ConfigPath string `yaml:"-"`

	// saveMu serialises concurrent writes from SaveExtensions.
	saveMu sync.Mutex `yaml:"-"`
}

// ListenConfig configures listening endpoints.
type ListenConfig struct {
	// Addresses to listen on (default all interfaces)
	Addresses []string `yaml:"addresses"`
	// Port to listen on (default 3868)
	Port int `yaml:"port"`
	// SecurePort for TLS connections (default 5868)
	SecurePort int `yaml:"secure_port"`
	// EnableTCP enables TCP transport on Port (default true). When TLS is
	// enabled for the same zone this listener speaks TLS, preserving the
	// historical single-port behaviour.
	EnableTCP bool `yaml:"enable_tcp"`
	// EnableTLS enables a dedicated TLS listener on SecurePort. It requires
	// TLS to be enabled and is independent of EnableTCP, so a zone can offer
	// cleartext and TLS endpoints at once.
	EnableTLS bool `yaml:"enable_tls"`
	// EnableSCTP enables SCTP transport (default false)
	EnableSCTP bool `yaml:"enable_sctp"`
}

// TLSConfig configures TLS settings.
type TLSConfig struct {
	// Enabled enables TLS
	Enabled bool `yaml:"enabled"`
	// CertFile is the path to the certificate file
	CertFile string `yaml:"cert_file"`
	// KeyFile is the path to the private key file
	KeyFile string `yaml:"key_file"`
	// CAFile is the path to the CA certificate file
	CAFile string `yaml:"ca_file"`
	// VerifyPeer enables peer certificate verification
	VerifyPeer bool `yaml:"verify_peer"`
	// MinVersion is the minimum TLS version (1.2 or 1.3)
	MinVersion string `yaml:"min_version"`
}

// LocalConfig configures local node properties.
type LocalConfig struct {
	// VendorID is the vendor ID of this implementation
	VendorID uint32 `yaml:"vendor_id"`
	// ProductName is the product name string
	ProductName string `yaml:"product_name"`
	// FirmwareRevision is the firmware revision number
	FirmwareRevision uint32 `yaml:"firmware_revision"`
	// WatchdogInterval is the DWR interval
	WatchdogInterval time.Duration `yaml:"watchdog_interval"`
	// ConnectTimeout is the peer connection timeout
	ConnectTimeout time.Duration `yaml:"connect_timeout"`
	// RequestTimeout is the default request timeout
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

// PeerConfig configures a Diameter peer.
type PeerConfig struct {
	// Identity is the peer's Diameter Identity
	Identity string `yaml:"identity"`
	// Realm is the peer's realm
	Realm string `yaml:"realm,omitempty"`
	// Addresses are the peer's addresses
	Addresses []string `yaml:"addresses"`
	// Port is the peer's port (default 3868)
	Port int `yaml:"port,omitempty"`
	// Network is the transport protocol (tcp, sctp). Default tcp.
	Network string `yaml:"network,omitempty"`
	// TLS enables TLS for this peer
	TLS bool `yaml:"tls,omitempty"`
	// TLSOptional allows non-TLS if TLS fails
	TLSOptional bool `yaml:"tls_optional,omitempty"`
	// ConnectOnStart automatically connects on startup
	ConnectOnStart bool `yaml:"connect_on_start,omitempty"`
	// Persistent keeps the peer entry after disconnect
	Persistent bool `yaml:"persistent,omitempty"`
	// ReconnectInterval is the time between reconnection attempts
	ReconnectInterval time.Duration `yaml:"reconnect_interval,omitempty"`
	// Zone assigns this peer to an edge zone (defaults to the first internal zone)
	Zone string `yaml:"zone,omitempty"`
}

// ApplicationConfig configures a Diameter application.
type ApplicationConfig struct {
	// ID is the application ID
	ID uint32 `yaml:"id"`
	// VendorID is the vendor ID (0 for IETF)
	VendorID uint32 `yaml:"vendor_id,omitempty"`
	// Type is "auth" or "acct"
	Type string `yaml:"type"`
	// RouteTo are the peers to route this application to (optional)
	RouteTo []string `yaml:"route_to,omitempty"`
}

// RoutingConfig configures message routing.
type RoutingConfig struct {
	// DefaultRealm is the default destination realm
	DefaultRealm string `yaml:"default_realm,omitempty"`
	// RealmRoutes maps realms to peer identities
	RealmRoutes map[string][]string `yaml:"realm_routes,omitempty"`
	// HostRoutes maps destination hosts to direct peer identities
	HostRoutes map[string]string `yaml:"host_routes,omitempty"`
	// RelayEnabled enables relaying of messages (default false)
	RelayEnabled bool `yaml:"relay_enabled,omitempty"`
	// DynamicRealmPeers enables routing to any connected peer within the target realm
	DynamicRealmPeers bool `yaml:"dynamic_realm_peers,omitempty"`
	// DynamicRealmAllowIdentities restricts dynamic realm peers to this allowlist (exact identities)
	DynamicRealmAllowIdentities []string `yaml:"dynamic_realm_allow_identities,omitempty"`
}

// PeersPolicy configures inbound peer acceptance policy.
type PeersPolicy struct {
	// AllowUnknown allows inbound CER from peers not listed in peers[]
	AllowUnknown bool `yaml:"allow_unknown,omitempty"`
	// AllowIdentities allows these exact identities to connect even if unknown
	AllowIdentities []string `yaml:"allow_identities,omitempty"`
}

// ExtensionConfig configures an extension.
type ExtensionConfig struct {
	// Name is the extension name
	Name string `yaml:"name"`
	// Config is extension-specific configuration
	Config map[string]interface{} `yaml:"config,omitempty"`
}

// Load loads configuration from a YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: config file path is user-provided
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg, err := Parse(data)
	if err != nil {
		return nil, err
	}
	cfg.ConfigPath = path
	return cfg, nil
}

// Parse parses configuration from YAML data.
func Parse(data []byte) (*Config, error) {
	config := &Config{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Apply defaults
	applyDefaults(config)

	// Validate
	if err := validate(config); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return config, nil
}

func applyDefaults(c *Config) {
	if c.Listen.Port == 0 {
		c.Listen.Port = 3868
	}
	if c.Listen.SecurePort == 0 {
		c.Listen.SecurePort = 5868
	}
	// EnableTCP defaults to true
	// EnableSCTP defaults to false

	if c.Local.WatchdogInterval == 0 {
		c.Local.WatchdogInterval = 30 * time.Second
	}
	if c.Local.ConnectTimeout == 0 {
		c.Local.ConnectTimeout = 10 * time.Second
	}
	if c.Local.RequestTimeout == 0 {
		c.Local.RequestTimeout = 30 * time.Second
	}
	if c.Local.ProductName == "" {
		c.Local.ProductName = "godiam"
	}

	for i := range c.Peers {
		if c.Peers[i].Port == 0 {
			c.Peers[i].Port = 3868
		}
		if c.Peers[i].Network == "" {
			c.Peers[i].Network = "tcp"
		}
		if c.Peers[i].ReconnectInterval == 0 {
			c.Peers[i].ReconnectInterval = 30 * time.Second
		}
	}

	applyEdgeDefaults(c)
}

// validateTLS validates a TLS configuration block.
func validateTLS(t *TLSConfig) error {
	if !t.Enabled {
		return nil
	}
	if t.CertFile == "" {
		return fmt.Errorf("cert_file is required when TLS is enabled")
	}
	if t.KeyFile == "" {
		return fmt.Errorf("key_file is required when TLS is enabled")
	}
	if err := checkFileReadable(t.CertFile); err != nil {
		return fmt.Errorf("cert_file: %w", err)
	}
	if err := checkFileReadable(t.KeyFile); err != nil {
		return fmt.Errorf("key_file: %w", err)
	}
	if t.CAFile != "" {
		if err := checkFileReadable(t.CAFile); err != nil {
			return fmt.Errorf("ca_file: %w", err)
		}
	}
	switch t.MinVersion {
	case "", "1.2", "1.3":
		return nil
	default:
		return fmt.Errorf("min_version must be \"1.2\" or \"1.3\", got %q", t.MinVersion)
	}
}

// isValidIP reports whether s is a valid textual IP address.
func isValidIP(s string) bool {
	return net.ParseIP(s) != nil
}

func validate(c *Config) error {
	if c.Identity == "" {
		return fmt.Errorf("identity is required")
	}
	if c.Realm == "" {
		return fmt.Errorf("realm is required")
	}

	// Validate identity is a valid FQDN
	if !isValidFQDN(c.Identity) {
		return fmt.Errorf("invalid identity: %s (must be FQDN)", c.Identity)
	}

	// Validate TLS configuration
	if err := validateTLS(&c.TLS); err != nil {
		return fmt.Errorf("tls: %w", err)
	}

	// Validate peers
	for i, peer := range c.Peers {
		if peer.Identity == "" {
			return fmt.Errorf("peers[%d].identity is required", i)
		}
		if !isValidFQDN(peer.Identity) {
			return fmt.Errorf("peers[%d].identity: %q is not a valid FQDN", i, peer.Identity)
		}
		if len(peer.Addresses) == 0 {
			return fmt.Errorf("peers[%d].addresses is required", i)
		}
		for j, addr := range peer.Addresses {
			if net.ParseIP(addr) == nil && !isValidFQDN(addr) {
				return fmt.Errorf("peers[%d].addresses[%d]: %q is not a valid IP or FQDN", i, j, addr)
			}
		}
		if peer.Port < 0 || peer.Port > 65535 {
			return fmt.Errorf("peers[%d].port: %d out of range", i, peer.Port)
		}
		switch peer.Network {
		case "", "tcp", "sctp":
			// ok
		default:
			return fmt.Errorf("peers[%d].network must be \"tcp\" or \"sctp\", got %q", i, peer.Network)
		}
	}

	// Validate applications
	for i, app := range c.Applications {
		if app.ID == 0 {
			return fmt.Errorf("applications[%d].id is required", i)
		}
		if app.Type != "auth" && app.Type != "acct" {
			return fmt.Errorf("applications[%d].type must be 'auth' or 'acct'", i)
		}
	}

	// Validate listen addresses
	if len(c.Listen.Addresses) > 0 {
		for i, addr := range c.Listen.Addresses {
			if !isValidIP(addr) {
				return fmt.Errorf("listen.addresses[%d]: %q is not a valid IP address", i, addr)
			}
		}
	}

	// Validate edge zones and partners
	if err := validateEdge(c); err != nil {
		return err
	}

	// Validate extensions
	seenExt := make(map[string]struct{}, len(c.Extensions))
	for i, ext := range c.Extensions {
		if ext.Name == "" {
			return fmt.Errorf("extensions[%d].name is required", i)
		}
		if _, dup := seenExt[ext.Name]; dup {
			return fmt.Errorf("extensions[%d]: duplicate name %q", i, ext.Name)
		}
		seenExt[ext.Name] = struct{}{}
	}

	return nil
}

// checkFileReadable returns an error if path is empty, missing, a directory, or unreadable.
func checkFileReadable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot stat %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory, not a file", path)
	}
	f, err := os.Open(path) //nolint:gosec // G304: path is admin-supplied config
	if err != nil {
		return fmt.Errorf("cannot open %q: %w", path, err)
	}
	_ = f.Close()
	return nil
}

func isValidFQDN(s string) bool {
	// Allow localhost for testing.
	if s == "localhost" {
		return true
	}
	// Reject IP addresses.
	if net.ParseIP(s) != nil {
		return false
	}
	// RFC 1035 length limits.
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	// Strip a single trailing dot (root label) if present.
	if s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	labels := 0
	labelLen := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '.':
			if labelLen == 0 {
				return false // empty label (leading dot or "..")
			}
			if s[i-1] == '-' {
				return false // label may not end with '-'
			}
			labels++
			labelLen = 0
		case c == '-':
			if labelLen == 0 {
				return false // label may not start with '-'
			}
			labelLen++
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			labelLen++
		default:
			return false
		}
		if labelLen > 63 {
			return false
		}
	}
	if labelLen == 0 || s[len(s)-1] == '-' {
		return false
	}
	labels++
	// A FQDN must have at least two labels (e.g. "example.com").
	return labels >= 2
}

// GetAuthAppIDs returns the configured authentication application IDs.
func (c *Config) GetAuthAppIDs() []types.ApplicationID {
	var ids []types.ApplicationID
	for _, app := range c.Applications {
		if app.Type == "auth" {
			ids = append(ids, types.ApplicationID(app.ID))
		}
	}
	return ids
}

// GetAcctAppIDs returns the configured accounting application IDs.
func (c *Config) GetAcctAppIDs() []types.ApplicationID {
	var ids []types.ApplicationID
	for _, app := range c.Applications {
		if app.Type == "acct" {
			ids = append(ids, types.ApplicationID(app.ID))
		}
	}
	return ids
}

// Example returns an example configuration.
func Example() *Config {
	return &Config{
		Identity: "diameter.example.com",
		Realm:    "example.com",
		Listen: ListenConfig{
			Port:      3868,
			EnableTCP: true,
		},
		TLS: TLSConfig{
			Enabled:    true,
			CertFile:   "/etc/diameter/cert.pem",
			KeyFile:    "/etc/diameter/key.pem",
			CAFile:     "/etc/diameter/ca.pem",
			VerifyPeer: true,
			MinVersion: "1.2",
		},
		Local: LocalConfig{
			VendorID:         0,
			ProductName:      "godiam",
			FirmwareRevision: 1,
			WatchdogInterval: 30 * time.Second,
			ConnectTimeout:   10 * time.Second,
			RequestTimeout:   30 * time.Second,
		},
		Peers: []PeerConfig{
			{
				Identity:       "peer.example.com",
				Realm:          "example.com",
				Addresses:      []string{"192.168.1.100"},
				Port:           3868,
				TLS:            true,
				ConnectOnStart: true,
				Persistent:     true,
			},
		},
		Applications: []ApplicationConfig{
			{ID: uint32(types.AppIDCreditControl), Type: "auth"},
			{ID: uint32(types.AppIDBaseAccounting), Type: "acct"},
		},
	}
}

// SaveExtensions persists extension configuration changes to the YAML config file.
// It reads the existing file, replaces the extensions section, and writes atomically.
// Safe for concurrent callers on the same Config value.
func (c *Config) SaveExtensions(extensions []ExtensionConfig) error {
	if c.ConfigPath == "" {
		return fmt.Errorf("no config file path set; cannot persist")
	}

	c.saveMu.Lock()
	defer c.saveMu.Unlock()

	// Read the full config, update extensions, and rewrite
	data, err := os.ReadFile(c.ConfigPath)
	if err != nil {
		return fmt.Errorf("reading config file for update: %w", err)
	}

	// Parse into a generic map to preserve non-extension fields
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing config for update: %w", err)
	}

	// Convert extensions to the format expected by YAML
	extList := make([]interface{}, 0, len(extensions))
	for _, ext := range extensions {
		entry := map[string]interface{}{
			"name": ext.Name,
		}
		if len(ext.Config) > 0 {
			entry["config"] = ext.Config
		}
		extList = append(extList, entry)
	}
	raw["extensions"] = extList

	// Marshal back to YAML
	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshaling updated config: %w", err)
	}

	// Atomic write: write to temp file, then rename
	tmpPath := c.ConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0600); err != nil {
		return fmt.Errorf("writing temp config: %w", err)
	}
	if err := os.Rename(tmpPath, c.ConfigPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming temp config: %w", err)
	}

	// Update in-memory extensions list
	c.Extensions = extensions
	return nil
}
