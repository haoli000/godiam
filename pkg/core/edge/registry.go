// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package edge provides the Diameter Edge Agent (DEA) topology model: the
// zones that separate the internal network from untrusted roaming partners,
// and the partners reachable through those zones.
//
// The registry is a read-mostly index built from the configuration. Routing
// handlers use it on the hot path, so all lookups are map-based.
package edge

import (
	"sort"
	"strings"
	"sync"

	"github.com/haoli000/godiam/pkg/core/config"
)

// Registry indexes zones and partners for fast lookup.
type Registry struct {
	mu sync.RWMutex

	zones       map[string]*config.ZoneConfig
	zoneOrder   []string
	defaultZone string

	partners      map[string]*config.PartnerConfig
	partnerOrder  []string
	partnerByPeer map[string]*config.PartnerConfig
	realmExact    map[string]*config.PartnerConfig
	realmSuffix   []suffixEntry

	peerZone map[string]string

	// boundPeers records peers admitted at runtime whose partner was matched by
	// realm rather than by an entry in partners[].peers. They are not part of
	// the static model, so they are kept separately and survive a reload.
	boundPeers map[string]string

	servedExact  map[string]struct{}
	servedSuffix []string
	servedOrder  []string

	identity   string
	realm      string
	configured bool
}

type suffixEntry struct {
	suffix  string // ".partner.com" form
	partner *config.PartnerConfig
}

// NewRegistry builds a registry from the given configuration.
func NewRegistry(cfg *config.Config) *Registry {
	r := &Registry{}
	r.Reload(cfg)
	return r
}

// Reload rebuilds the registry from a new configuration.
func (r *Registry) Reload(cfg *config.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rebuildLocked(cfg)
}

func (r *Registry) rebuildLocked(cfg *config.Config) {
	r.zones = make(map[string]*config.ZoneConfig)
	r.zoneOrder = nil
	r.partners = make(map[string]*config.PartnerConfig)
	r.partnerOrder = nil
	r.partnerByPeer = make(map[string]*config.PartnerConfig)
	r.realmExact = make(map[string]*config.PartnerConfig)
	r.realmSuffix = nil
	r.peerZone = make(map[string]string)
	r.servedExact = make(map[string]struct{})
	r.servedSuffix = nil
	r.servedOrder = nil
	r.defaultZone = ""
	r.configured = false

	if cfg == nil {
		return
	}

	r.identity = cfg.Identity
	r.realm = cfg.Realm
	r.configured = len(cfg.Zones) > 0 || len(cfg.Partners) > 0

	zones := cfg.EffectiveZones()
	for i := range zones {
		z := zones[i]
		r.zones[z.Name] = &z
		r.zoneOrder = append(r.zoneOrder, z.Name)
		if r.defaultZone == "" && !z.IsExternal() {
			r.defaultZone = z.Name
		}
	}
	if r.defaultZone == "" && len(r.zoneOrder) > 0 {
		r.defaultZone = r.zoneOrder[0]
	}

	for i := range cfg.Partners {
		p := &cfg.Partners[i]
		r.partners[p.Name] = p
		r.partnerOrder = append(r.partnerOrder, p.Name)

		for _, pp := range p.Peers {
			id := normalize(pp.Identity)
			r.partnerByPeer[id] = p
			r.peerZone[id] = p.Zone
		}
		for _, realm := range p.Realms {
			realm = normalize(realm)
			if strings.HasPrefix(realm, "*.") {
				r.realmSuffix = append(r.realmSuffix, suffixEntry{suffix: realm[1:], partner: p})
				continue
			}
			r.realmExact[realm] = p
		}
	}

	// Longest suffix first so the most specific pattern wins.
	sort.SliceStable(r.realmSuffix, func(i, j int) bool {
		return len(r.realmSuffix[i].suffix) > len(r.realmSuffix[j].suffix)
	})

	for _, pc := range cfg.Peers {
		id := normalize(pc.Identity)
		if pc.Zone != "" {
			r.peerZone[id] = pc.Zone
			continue
		}
		if _, ok := r.peerZone[id]; !ok {
			r.peerZone[id] = r.defaultZone
		}
	}

	r.rebuildServedLocked(cfg)
}

// rebuildServedLocked indexes the realms the edge agent serves itself: the
// local realm, the realms declared on internal zones, the realms of peers
// sitting in an internal zone, and realm routes whose destinations are all
// internal. Traffic toward those realms is termination, not transit.
func (r *Registry) rebuildServedLocked(cfg *config.Config) {
	add := func(realm string) {
		realm = normalize(realm)
		if realm == "" {
			return
		}
		if strings.HasPrefix(realm, "*.") {
			suffix := realm[1:]
			for _, s := range r.servedSuffix {
				if s == suffix {
					return
				}
			}
			r.servedSuffix = append(r.servedSuffix, suffix)
			r.servedOrder = append(r.servedOrder, realm)
			return
		}
		if _, ok := r.servedExact[realm]; ok {
			return
		}
		r.servedExact[realm] = struct{}{}
		r.servedOrder = append(r.servedOrder, realm)
	}

	add(cfg.Realm)

	for i := range cfg.Zones {
		z := &cfg.Zones[i]
		if z.IsExternal() {
			continue
		}
		for _, realm := range z.Realms {
			add(realm)
		}
	}

	internalPeer := func(identity string) bool {
		id := normalize(identity)
		if _, ok := r.partnerByPeer[id]; ok {
			return false
		}
		return !r.isExternalZoneLocked(r.peerZone[id])
	}

	for _, pc := range cfg.Peers {
		if internalPeer(pc.Identity) {
			add(pc.Realm)
		}
	}

	for realm, hops := range cfg.Routing.RealmRoutes {
		if len(hops) == 0 {
			continue
		}
		allInternal := true
		for _, hop := range hops {
			if !internalPeer(hop) {
				allInternal = false
				break
			}
		}
		if allInternal {
			add(realm)
		}
	}

	sort.Strings(r.servedOrder)
	// Longest suffix first so the most specific pattern wins.
	sort.SliceStable(r.servedSuffix, func(i, j int) bool {
		return len(r.servedSuffix[i]) > len(r.servedSuffix[j])
	})
}

// ServesRealm reports whether the given realm is terminated by this edge agent
// rather than reached through a roaming partner.
func (r *Registry) ServesRealm(realm string) bool {
	realm = normalize(realm)
	if realm == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.servedExact[realm]; ok {
		return true
	}
	for _, suffix := range r.servedSuffix {
		if strings.HasSuffix(realm, suffix) {
			return true
		}
	}
	return false
}

// IsInternalHost reports whether a Diameter identity belongs to the operator's
// own network rather than to a roaming partner. It is the identity of this
// node, a peer sitting in an internal zone, or a host inside a served realm.
func (r *Registry) IsInternalHost(host string) bool {
	host = normalize(host)
	if host == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if host == normalize(r.identity) {
		return true
	}
	if _, ok := r.partnerByPeer[host]; ok {
		return false
	}
	if zone, ok := r.peerZone[host]; ok {
		return !r.isExternalZoneLocked(zone)
	}
	if _, ok := r.realmExact[hostRealm(host)]; ok {
		return false
	}
	for _, e := range r.realmSuffix {
		if strings.HasSuffix(host, e.suffix) {
			return false
		}
	}
	return r.hostInServedRealmLocked(host)
}

// hostInServedRealmLocked reports whether a host lies inside a served realm.
func (r *Registry) hostInServedRealmLocked(host string) bool {
	// Fast path: the host sits directly in a served realm, which is the usual
	// shape of an internal identity such as "hss1.core.example.com".
	if _, ok := r.servedExact[hostRealm(host)]; ok {
		return true
	}
	for realm := range r.servedExact {
		if HostInRealm(host, realm) {
			return true
		}
	}
	for _, suffix := range r.servedSuffix {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// hostRealm returns the realm part of a Diameter identity.
func hostRealm(host string) string {
	if i := strings.Index(host, "."); i >= 0 {
		return host[i+1:]
	}
	return host
}

// ServedRealms returns the realms terminated by this edge agent, sorted.
func (r *Registry) ServedRealms() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.servedOrder))
	copy(out, r.servedOrder)
	return out
}

func normalize(s string) string { return strings.ToLower(strings.TrimSuffix(s, ".")) }

// Configured reports whether an explicit zone/partner model is in use.
// When false the node behaves exactly as a plain DRA.
func (r *Registry) Configured() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.configured
}

// Identity returns the local Diameter identity.
func (r *Registry) Identity() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.identity
}

// Realm returns the local realm.
func (r *Registry) Realm() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.realm
}

// DefaultZone returns the name of the zone used for peers without an explicit zone.
func (r *Registry) DefaultZone() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultZone
}

// Zones returns the configured zones in declaration order.
func (r *Registry) Zones() []*config.ZoneConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*config.ZoneConfig, 0, len(r.zoneOrder))
	for _, name := range r.zoneOrder {
		out = append(out, r.zones[name])
	}
	return out
}

// Zone returns the zone with the given name.
func (r *Registry) Zone(name string) (*config.ZoneConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	z, ok := r.zones[name]
	return z, ok
}

// Partners returns the configured partners in declaration order.
func (r *Registry) Partners() []*config.PartnerConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*config.PartnerConfig, 0, len(r.partnerOrder))
	for _, name := range r.partnerOrder {
		out = append(out, r.partners[name])
	}
	return out
}

// PartnersForZone returns the partners attached to the given zone.
func (r *Registry) PartnersForZone(zone string) []*config.PartnerConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	zone = normalize(zone)
	out := make([]*config.PartnerConfig, 0)
	for _, name := range r.partnerOrder {
		if p := r.partners[name]; normalize(p.Zone) == zone {
			out = append(out, p)
		}
	}
	return out
}

// Partner returns the partner with the given name.
func (r *Registry) Partner(name string) (*config.PartnerConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.partners[name]
	return p, ok
}

// PartnerForPeer returns the partner owning the given peer identity.
func (r *Registry) PartnerForPeer(identity string) (*config.PartnerConfig, bool) {
	if identity == "" {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	id := normalize(identity)
	if p, ok := r.partnerByPeer[id]; ok {
		return p, true
	}
	if name, ok := r.boundPeers[id]; ok {
		p, ok := r.partners[name]
		return p, ok
	}
	return nil, false
}

// BindPeer records the partner a peer was admitted as. Peers listed under
// partners[].peers are already indexed, but a peer admitted because its realm
// belongs to a partner is only known at connection time; without this binding
// the edge extensions could not tell which policy applies to it.
func (r *Registry) BindPeer(identity, partner string) {
	if identity == "" || partner == "" {
		return
	}
	id := normalize(identity)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.partnerByPeer[id]; ok {
		return // already part of the static model
	}
	if r.boundPeers == nil {
		r.boundPeers = make(map[string]string)
	}
	r.boundPeers[id] = partner
}

// UnbindPeer forgets a runtime peer binding, which keeps the table the size of
// the connected peer set rather than of every peer ever seen.
func (r *Registry) UnbindPeer(identity string) {
	if identity == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.boundPeers, normalize(identity))
}

// PartnerForRealm returns the partner owning the given realm, matching exact
// realms first and then the longest wildcard ("*.example.com") pattern.
func (r *Registry) PartnerForRealm(realm string) (*config.PartnerConfig, bool) {
	if realm == "" {
		return nil, false
	}
	key := normalize(realm)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.realmExact[key]; ok {
		return p, true
	}
	for _, e := range r.realmSuffix {
		if strings.HasSuffix(key, e.suffix) {
			return e.partner, true
		}
	}
	return nil, false
}

// ZoneForPeer returns the zone a peer identity belongs to.
func (r *Registry) ZoneForPeer(identity string) (*config.ZoneConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, ok := r.peerZone[normalize(identity)]
	if !ok {
		name = r.defaultZone
	}
	z, found := r.zones[name]
	return z, found
}

// ZoneNameForPeer returns the zone name a peer identity belongs to.
func (r *Registry) ZoneNameForPeer(identity string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name, ok := r.peerZone[normalize(identity)]; ok {
		return name
	}
	return r.defaultZone
}

// IsExternalZone reports whether the named zone faces untrusted networks.
func (r *Registry) IsExternalZone(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isExternalZoneLocked(name)
}

func (r *Registry) isExternalZoneLocked(name string) bool {
	z, ok := r.zones[name]
	return ok && z.IsExternal()
}

// PeerIdentitiesForZone returns the configured peer identities in a zone.
func (r *Registry) PeerIdentitiesForZone(zone string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for id, z := range r.peerZone {
		if z == zone {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// RealmMatches reports whether realm matches any of the given realm patterns.
// A pattern of the form "*.example.com" matches any strict subdomain.
func RealmMatches(realm string, patterns []string) bool {
	if realm == "" {
		return false
	}
	key := normalize(realm)
	for _, pattern := range patterns {
		p := normalize(pattern)
		if strings.HasPrefix(p, "*.") {
			if strings.HasSuffix(key, p[1:]) {
				return true
			}
			continue
		}
		if key == p {
			return true
		}
	}
	return false
}

// HostInRealm reports whether a Diameter identity belongs to the given realm,
// i.e. the host is the realm itself or a subdomain of it.
func HostInRealm(host, realm string) bool {
	h, rlm := normalize(host), normalize(realm)
	if h == "" || rlm == "" {
		return false
	}
	return h == rlm || strings.HasSuffix(h, "."+rlm)
}
