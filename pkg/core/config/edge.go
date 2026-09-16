// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Zone roles.
const (
	// ZoneRoleInternal marks a zone facing the operator's own network.
	ZoneRoleInternal = "internal"
	// ZoneRoleExternal marks a zone facing untrusted roaming/IPX partners.
	ZoneRoleExternal = "external"
)

// DefaultTopoHideMaxEntries bounds a partner's share of the pseudonym store
// when the configuration does not say otherwise.
const DefaultTopoHideMaxEntries = 100000

// DefaultZoneName is the name of the implicit zone synthesised when no
// zones are declared in the configuration.
const DefaultZoneName = "default"

// Screening and rate-limit actions.
const (
	// ActionReject answers the request with an error Result-Code.
	ActionReject = "reject"
	// ActionDrop silently discards the message.
	ActionDrop = "drop"
	// ActionLog only records the violation and lets the message through.
	ActionLog = "log"
)

// Topology hiding modes.
const (
	// TopoHideNone disables topology hiding.
	TopoHideNone = "none"
	// TopoHideStatic rewrites internal identities with a single edge identity.
	TopoHideStatic = "static"
	// TopoHideStateful rewrites internal identities with reversible pseudonyms.
	TopoHideStateful = "stateful"
)

// ZoneConfig describes a network-facing zone of the edge agent. Each zone owns
// its own listeners, TLS material and inbound peer admission policy.
type ZoneConfig struct {
	// Name uniquely identifies the zone.
	Name string `yaml:"name"`
	// Role is "internal" or "external".
	Role string `yaml:"role"`
	// Realms lists the realms served behind this zone. On an internal zone the
	// entries are the realms the edge agent accepts traffic for, in addition to
	// the local realm and the realms of the zone's own peers. Entries may use a
	// "*.example.com" suffix pattern.
	Realms []string `yaml:"realms,omitempty"`
	// Listen configures the zone's listening endpoints.
	Listen ListenConfig `yaml:"listen"`
	// TLS configures the zone's transport security.
	TLS ZoneTLSConfig `yaml:"tls"`
	// PeersPolicy configures inbound peer acceptance for this zone.
	PeersPolicy PeersPolicy `yaml:"peers_policy"`

	// implicit marks the zone synthesised for a configuration that declares no
	// zones at all. Such a zone reproduces the pre-edge listener behaviour
	// exactly.
	implicit bool `yaml:"-"`
}

// IsImplicit reports whether the zone was synthesised for a configuration that
// predates the edge model.
func (z *ZoneConfig) IsImplicit() bool { return z.implicit }

// IsExternal reports whether the zone faces untrusted networks.
func (z *ZoneConfig) IsExternal() bool { return z.Role == ZoneRoleExternal }

// ZoneTLSConfig extends TLSConfig with edge-specific verification options.
type ZoneTLSConfig struct {
	TLSConfig `yaml:",inline"`
	// RequireClientCertIdentity binds the TLS client certificate identity
	// (CN or DNS SAN) to the Origin-Host presented in the CER.
	RequireClientCertIdentity bool `yaml:"require_client_cert_identity,omitempty"`
}

// PartnerPeer is a peer belonging to a roaming partner, with routing preference.
type PartnerPeer struct {
	// Identity is the peer's Diameter Identity.
	Identity string `yaml:"identity"`
	// Priority groups peers into failover tiers (lower value = preferred).
	Priority int `yaml:"priority,omitempty"`
	// Weight distributes load within a priority tier (default 1).
	Weight int `yaml:"weight,omitempty"`
}

// UnmarshalYAML accepts either a plain identity string or a full mapping.
func (p *PartnerPeer) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		var s string
		if err := value.Decode(&s); err != nil {
			return err
		}
		p.Identity = s
		return nil
	}
	type plain PartnerPeer
	var tmp plain
	if err := value.Decode(&tmp); err != nil {
		return err
	}
	*p = PartnerPeer(tmp)
	return nil
}

// AVPSelector identifies an AVP by code and vendor.
type AVPSelector struct {
	Code     uint32 `yaml:"code"`
	VendorID uint32 `yaml:"vendor_id,omitempty"`
}

// ScreeningPolicy configures GSMA FS.19 style ingress screening for a partner.
type ScreeningPolicy struct {
	// Enabled turns screening on. Defaults to true for external zones.
	Enabled *bool `yaml:"enabled,omitempty"`
	// DefaultAction is applied when a rule has no explicit action.
	DefaultAction string `yaml:"default_action,omitempty"`
	// RejectResultCode is the Result-Code used for rejections (default 5003).
	RejectResultCode uint32 `yaml:"reject_result_code,omitempty"`
	// MaxRouteRecords caps the Route-Record depth (0 = unlimited).
	MaxRouteRecords int `yaml:"max_route_records,omitempty"`
	// CheckOriginRealm validates Origin-Realm against the partner realms.
	CheckOriginRealm *bool `yaml:"check_origin_realm,omitempty"`
	// CheckOriginHost validates that Origin-Host lies within Origin-Realm.
	CheckOriginHost *bool `yaml:"check_origin_host,omitempty"`
	// CheckPeerIdentity validates Origin-Host against the connected peer identity.
	CheckPeerIdentity *bool `yaml:"check_peer_identity,omitempty"`
	// CheckDestinationRealm rejects transit traffic toward foreign realms.
	CheckDestinationRealm *bool `yaml:"check_destination_realm,omitempty"`
	// CheckPLMN validates Visited-PLMN-Id / IMSI against the partner PLMN list.
	CheckPLMN *bool `yaml:"check_plmn,omitempty"`
	// CheckApplications enforces the partner's allowed application list.
	CheckApplications *bool `yaml:"check_applications,omitempty"`
	// CheckCommands enforces the partner's allowed command list.
	CheckCommands *bool `yaml:"check_commands,omitempty"`
	// DenyAVPs rejects messages carrying any of these AVPs.
	DenyAVPs []AVPSelector `yaml:"deny_avps,omitempty"`
	// AllowAVPs, when non-empty, rejects any top-level AVP not listed.
	AllowAVPs []AVPSelector `yaml:"allow_avps,omitempty"`
	// RuleActions overrides the action for a specific rule name.
	RuleActions map[string]string `yaml:"rule_actions,omitempty"`
}

// TopologyHidingPolicy configures how internal topology is masked toward a partner.
type TopologyHidingPolicy struct {
	// Mode is "none", "static" or "stateful".
	Mode string `yaml:"mode,omitempty"`
	// EdgeIdentity is the identity advertised outward (defaults to node identity).
	EdgeIdentity string `yaml:"edge_identity,omitempty"`
	// PseudonymPrefix prefixes generated pseudonym labels (stateful mode).
	PseudonymPrefix string `yaml:"pseudonym_prefix,omitempty"`
	// PseudonymRealm is the realm appended to pseudonyms (defaults to node realm).
	PseudonymRealm string `yaml:"pseudonym_realm,omitempty"`
	// TTL is the lifetime of a pseudonym mapping.
	TTL time.Duration `yaml:"ttl,omitempty"`
	// MaxEntries caps how many entries this partner may hold in the pseudonym
	// store. Unset means DefaultTopoHideMaxEntries; -1 means unlimited, which
	// lets a partner's traffic grow the store without bound and should only be
	// used on a trusted link.
	MaxEntries int `yaml:"max_entries,omitempty"`
	// HideOriginRealm masks Origin-Realm whenever Origin-Host was masked, so a
	// partner never learns the internal realm and the two AVPs stay consistent.
	HideOriginRealm *bool `yaml:"hide_origin_realm,omitempty"`
	// HideRouteRecords strips Route-Record AVPs on egress.
	HideRouteRecords *bool `yaml:"hide_route_records,omitempty"`
	// HideProxyInfo strips Proxy-Info AVPs on egress.
	HideProxyInfo *bool `yaml:"hide_proxy_info,omitempty"`
	// HideErrorReportingHost masks Error-Reporting-Host on egress.
	HideErrorReportingHost *bool `yaml:"hide_error_reporting_host,omitempty"`
	// HideHostIPAddress strips Host-IP-Address AVPs on egress.
	HideHostIPAddress *bool `yaml:"hide_host_ip_address,omitempty"`
	// HideOriginStateID strips Origin-State-Id on egress.
	HideOriginStateID *bool `yaml:"hide_origin_state_id,omitempty"`
	// ScrubAVPs removes additional AVPs on egress.
	ScrubAVPs []AVPSelector `yaml:"scrub_avps,omitempty"`
}

// RateLimitRule is a single token-bucket specification.
type RateLimitRule struct {
	// RequestsPerSecond is the sustained rate (0 = unlimited).
	RequestsPerSecond float64 `yaml:"requests_per_second,omitempty"`
	// Burst is the bucket capacity (defaults to max(1, rate)).
	Burst int `yaml:"burst,omitempty"`
	// Action is "reject" or "drop".
	Action string `yaml:"action,omitempty"`
	// ResultCode is the Result-Code used when rejecting (default 3004).
	ResultCode uint32 `yaml:"result_code,omitempty"`
}

// RateLimitPolicy configures throttling scopes for a partner.
type RateLimitPolicy struct {
	// Enabled turns rate limiting on for this partner.
	Enabled bool `yaml:"enabled,omitempty"`
	// Partner is the aggregate limit across all of the partner's peers.
	Partner RateLimitRule `yaml:",inline"`
	// PerPeer applies the rule to each peer individually.
	PerPeer *RateLimitRule `yaml:"per_peer,omitempty"`
	// PerApplication maps application IDs to rules.
	PerApplication map[uint32]RateLimitRule `yaml:"per_application,omitempty"`
	// PerCommand maps command codes to rules.
	PerCommand map[uint32]RateLimitRule `yaml:"per_command,omitempty"`
}

// PartnerConfig describes a roaming partner reachable through a zone.
type PartnerConfig struct {
	// Name uniquely identifies the partner.
	Name string `yaml:"name"`
	// Zone is the zone this partner connects through.
	Zone string `yaml:"zone"`
	// Realms are the realms owned by the partner. A leading "*." matches subdomains.
	Realms []string `yaml:"realms"`
	// Peers are the partner's Diameter peers, with optional priority/weight.
	Peers []PartnerPeer `yaml:"peers,omitempty"`
	// PLMNIDs are the partner's MCC+MNC identifiers (5 or 6 digits).
	PLMNIDs []string `yaml:"plmn_ids,omitempty"`
	// Applications lists the application IDs the partner may use (empty = any).
	Applications []uint32 `yaml:"applications,omitempty"`
	// Commands lists the command codes the partner may use (empty = any).
	Commands []uint32 `yaml:"commands,omitempty"`
	// AllowTransit permits routing partner traffic toward other external zones.
	AllowTransit bool `yaml:"allow_transit,omitempty"`
	// Screening configures ingress screening.
	Screening ScreeningPolicy `yaml:"screening,omitempty"`
	// TopologyHiding configures outward topology masking.
	TopologyHiding TopologyHidingPolicy `yaml:"topology_hiding,omitempty"`
	// RateLimit configures throttling.
	RateLimit RateLimitPolicy `yaml:"rate_limit,omitempty"`
	// TLS optionally overrides the zone TLS material for this partner.
	TLS *TLSConfig `yaml:"tls,omitempty"`
}

// EffectiveZones returns the configured zones, synthesising a default zone from
// the node-level listen/TLS/peers_policy settings when none are declared.
func (c *Config) EffectiveZones() []ZoneConfig {
	if len(c.Zones) > 0 {
		return c.Zones
	}
	return []ZoneConfig{{
		Name:        DefaultZoneName,
		Role:        ZoneRoleInternal,
		Listen:      c.Listen,
		TLS:         ZoneTLSConfig{TLSConfig: c.TLS},
		PeersPolicy: c.PeersPolicy,
		implicit:    true,
	}}
}

// boolValue dereferences an optional bool with a default.
func boolValue(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// ScreeningEnabled reports whether screening applies to this partner.
func (p *PartnerConfig) ScreeningEnabled(zoneExternal bool) bool {
	return boolValue(p.Screening.Enabled, zoneExternal)
}

// applyEdgeDefaults fills in defaults for zones and partners.
func applyEdgeDefaults(c *Config) {
	for i := range c.Zones {
		z := &c.Zones[i]
		if z.Role == "" {
			z.Role = ZoneRoleInternal
		}
		if z.Listen.Port == 0 {
			z.Listen.Port = 3868
		}
		if z.Listen.SecurePort == 0 {
			z.Listen.SecurePort = 5868
		}
		if z.TLS.MinVersion == "" {
			z.TLS.MinVersion = "1.2"
		}
	}

	for i := range c.Partners {
		p := &c.Partners[i]
		for j := range p.Peers {
			if p.Peers[j].Weight == 0 {
				p.Peers[j].Weight = 1
			}
		}
		if p.Screening.DefaultAction == "" {
			p.Screening.DefaultAction = ActionReject
		}
		if p.Screening.RejectResultCode == 0 {
			p.Screening.RejectResultCode = 5003 // DIAMETER_AUTHORIZATION_REJECTED
		}
		if p.TopologyHiding.Mode == "" {
			p.TopologyHiding.Mode = TopoHideNone
		}
		if p.TopologyHiding.EdgeIdentity == "" {
			p.TopologyHiding.EdgeIdentity = c.Identity
		}
		if p.TopologyHiding.PseudonymRealm == "" {
			p.TopologyHiding.PseudonymRealm = c.Realm
		}
		if p.TopologyHiding.PseudonymPrefix == "" {
			p.TopologyHiding.PseudonymPrefix = "th"
		}
		if p.TopologyHiding.MaxEntries == 0 {
			// Stateful hiding allocates an entry per masked internal host, all
			// of it driven by partner traffic, so it must be bounded by
			// default. -1 stays available as an explicit opt-out.
			p.TopologyHiding.MaxEntries = DefaultTopoHideMaxEntries
		}
		if p.TopologyHiding.TTL == 0 {
			p.TopologyHiding.TTL = 30 * time.Minute
		}
		applyRateLimitDefaults(&p.RateLimit.Partner)
		if p.RateLimit.PerPeer != nil {
			applyRateLimitDefaults(p.RateLimit.PerPeer)
		}
		for k, r := range p.RateLimit.PerApplication {
			applyRateLimitDefaults(&r)
			p.RateLimit.PerApplication[k] = r
		}
		for k, r := range p.RateLimit.PerCommand {
			applyRateLimitDefaults(&r)
			p.RateLimit.PerCommand[k] = r
		}
	}
}

func applyRateLimitDefaults(r *RateLimitRule) {
	if r.Action == "" {
		r.Action = ActionReject
	}
	if r.ResultCode == 0 {
		r.ResultCode = 3004 // DIAMETER_TOO_BUSY
	}
	if r.Burst == 0 && r.RequestsPerSecond > 0 {
		r.Burst = int(r.RequestsPerSecond)
		if r.Burst < 1 {
			r.Burst = 1
		}
	}
}

// validateEdge validates the zone and partner configuration.
func validateEdge(c *Config) error {
	zoneNames := make(map[string]struct{}, len(c.Zones))
	for i := range c.Zones {
		z := &c.Zones[i]
		if z.Name == "" {
			return fmt.Errorf("zones[%d].name is required", i)
		}
		if _, dup := zoneNames[z.Name]; dup {
			return fmt.Errorf("zones[%d]: duplicate name %q", i, z.Name)
		}
		zoneNames[z.Name] = struct{}{}

		if z.Role != ZoneRoleInternal && z.Role != ZoneRoleExternal {
			return fmt.Errorf("zones[%d].role must be %q or %q, got %q", i, ZoneRoleInternal, ZoneRoleExternal, z.Role)
		}
		if z.Listen.Port < 0 || z.Listen.Port > 65535 {
			return fmt.Errorf("zones[%d].listen.port: %d out of range", i, z.Listen.Port)
		}
		for j, addr := range z.Listen.Addresses {
			if !isValidIP(addr) {
				return fmt.Errorf("zones[%d].listen.addresses[%d]: %q is not a valid IP address", i, j, addr)
			}
		}
		if err := validateTLS(&z.TLS.TLSConfig); err != nil {
			return fmt.Errorf("zones[%d].tls: %w", i, err)
		}
		if z.TLS.RequireClientCertIdentity && !z.TLS.Enabled {
			return fmt.Errorf("zones[%d].tls.require_client_cert_identity requires tls.enabled", i)
		}
		if z.TLS.RequireClientCertIdentity && !z.TLS.VerifyPeer {
			return fmt.Errorf("zones[%d].tls.require_client_cert_identity requires tls.verify_peer", i)
		}
		if z.TLS.Enabled && z.TLS.VerifyPeer && z.TLS.CAFile == "" {
			// A listener verifies client certificates against ca_file, so
			// without it the zone cannot start. Reported here rather than at
			// Start(), where it would surface as an opaque startup failure.
			// Client-side TLS blocks are exempt: there, verify_peer with no
			// ca_file correctly means "use the system roots".
			return fmt.Errorf("zones[%d].tls.verify_peer requires ca_file, which supplies the trust anchors "+
				"for client certificates", i)
		}
		if z.Listen.EnableTLS && !z.TLS.Enabled {
			return fmt.Errorf("zones[%d].listen.enable_tls requires tls.enabled", i)
		}
		if z.TLS.Enabled && !z.Listen.EnableTCP && !z.Listen.EnableTLS && !z.Listen.EnableSCTP {
			return fmt.Errorf("zones[%d]: tls.enabled but no transport is enabled; "+
				"set listen.enable_tls to listen on secure_port %d or listen.enable_tcp to listen on port %d",
				i, z.Listen.SecurePort, z.Listen.Port)
		}
		if z.Role == ZoneRoleExternal && z.PeersPolicy.AllowUnknown {
			return fmt.Errorf("zones[%d]: peers_policy.allow_unknown is not permitted on an external zone: "+
				"a peer admitted this way belongs to no partner, so no screening, rate limit or "+
				"topology hiding policy applies to it", i)
		}
	}

	partnerNames := make(map[string]struct{}, len(c.Partners))
	peerOwner := make(map[string]string)
	for i := range c.Partners {
		p := &c.Partners[i]
		if p.Name == "" {
			return fmt.Errorf("partners[%d].name is required", i)
		}
		if _, dup := partnerNames[p.Name]; dup {
			return fmt.Errorf("partners[%d]: duplicate name %q", i, p.Name)
		}
		partnerNames[p.Name] = struct{}{}

		if p.Zone == "" {
			return fmt.Errorf("partners[%d].zone is required", i)
		}
		if _, ok := zoneNames[p.Zone]; !ok {
			return fmt.Errorf("partners[%d].zone: unknown zone %q", i, p.Zone)
		}
		if len(p.Realms) == 0 {
			return fmt.Errorf("partners[%d].realms is required", i)
		}
		for j, realm := range p.Realms {
			bare := strings.TrimPrefix(realm, "*.")
			if bare == "" || !isValidFQDN(bare) {
				return fmt.Errorf("partners[%d].realms[%d]: %q is not a valid realm", i, j, realm)
			}
		}
		for j, pp := range p.Peers {
			if pp.Identity == "" {
				return fmt.Errorf("partners[%d].peers[%d].identity is required", i, j)
			}
			if !isValidFQDN(pp.Identity) {
				return fmt.Errorf("partners[%d].peers[%d].identity: %q is not a valid FQDN", i, j, pp.Identity)
			}
			if pp.Weight < 0 {
				return fmt.Errorf("partners[%d].peers[%d].weight must be >= 0", i, j)
			}
			if owner, dup := peerOwner[pp.Identity]; dup {
				return fmt.Errorf("partners[%d].peers[%d]: peer %q already belongs to partner %q", i, j, pp.Identity, owner)
			}
			peerOwner[pp.Identity] = p.Name
		}
		for j, plmn := range p.PLMNIDs {
			if !isValidPLMN(plmn) {
				return fmt.Errorf("partners[%d].plmn_ids[%d]: %q must be 5 or 6 digits", i, j, plmn)
			}
		}
		if err := validateAction(p.Screening.DefaultAction); err != nil {
			return fmt.Errorf("partners[%d].screening.default_action: %w", i, err)
		}
		for rule, action := range p.Screening.RuleActions {
			if err := validateAction(action); err != nil {
				return fmt.Errorf("partners[%d].screening.rule_actions[%s]: %w", i, rule, err)
			}
		}
		if p.Screening.MaxRouteRecords < 0 {
			return fmt.Errorf("partners[%d].screening.max_route_records must be >= 0", i)
		}
		switch p.TopologyHiding.Mode {
		case TopoHideNone, TopoHideStatic, TopoHideStateful:
		default:
			return fmt.Errorf("partners[%d].topology_hiding.mode must be %q, %q or %q, got %q",
				i, TopoHideNone, TopoHideStatic, TopoHideStateful, p.TopologyHiding.Mode)
		}
		if p.TopologyHiding.MaxEntries < -1 {
			return fmt.Errorf("partners[%d].topology_hiding.max_entries must be >= 0, or -1 for unlimited", i)
		}
		if err := validateRateLimit(&p.RateLimit.Partner); err != nil {
			return fmt.Errorf("partners[%d].rate_limit: %w", i, err)
		}
		if p.RateLimit.PerPeer != nil {
			if err := validateRateLimit(p.RateLimit.PerPeer); err != nil {
				return fmt.Errorf("partners[%d].rate_limit.per_peer: %w", i, err)
			}
		}
		for k, r := range p.RateLimit.PerApplication {
			if err := validateRateLimit(&r); err != nil {
				return fmt.Errorf("partners[%d].rate_limit.per_application[%d]: %w", i, k, err)
			}
		}
		for k, r := range p.RateLimit.PerCommand {
			if err := validateRateLimit(&r); err != nil {
				return fmt.Errorf("partners[%d].rate_limit.per_command[%d]: %w", i, k, err)
			}
		}
		if p.TLS != nil {
			if err := validateTLS(p.TLS); err != nil {
				return fmt.Errorf("partners[%d].tls: %w", i, err)
			}
		}
	}

	externalZones := make(map[string]struct{})
	for i := range c.Zones {
		if c.Zones[i].Role == ZoneRoleExternal {
			externalZones[c.Zones[i].Name] = struct{}{}
		}
	}

	// Peers referencing an unknown zone are a configuration error, and a peer
	// placed on an external zone without a partner would bypass every edge
	// policy, so it is refused as well.
	for i, pc := range c.Peers {
		if pc.Zone == "" {
			continue
		}
		if _, ok := zoneNames[pc.Zone]; !ok {
			return fmt.Errorf("peers[%d].zone: unknown zone %q", i, pc.Zone)
		}
		if _, ok := externalZones[pc.Zone]; !ok {
			continue
		}
		if _, owned := peerOwner[pc.Identity]; !owned {
			return fmt.Errorf("peers[%d]: %q is on external zone %q but belongs to no partner, "+
				"so no screening, rate limit or topology hiding policy would apply to it",
				i, pc.Identity, pc.Zone)
		}
	}

	return nil
}

func validateAction(action string) error {
	switch action {
	case ActionReject, ActionDrop, ActionLog:
		return nil
	default:
		return fmt.Errorf("must be %q, %q or %q, got %q", ActionReject, ActionDrop, ActionLog, action)
	}
}

func validateRateLimit(r *RateLimitRule) error {
	if r.RequestsPerSecond < 0 {
		return fmt.Errorf("requests_per_second must be >= 0")
	}
	if r.Burst < 0 {
		return fmt.Errorf("burst must be >= 0")
	}
	switch r.Action {
	case "", ActionReject, ActionDrop:
		return nil
	default:
		return fmt.Errorf("action must be %q or %q, got %q", ActionReject, ActionDrop, r.Action)
	}
}

func isValidPLMN(s string) bool {
	if len(s) != 5 && len(s) != 6 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
