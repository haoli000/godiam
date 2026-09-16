// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package dea_topohide implements Diameter Edge Agent topology hiding. It masks
// internal node identities before messages leave toward an external roaming
// partner and restores them when the partner references a masked identity.
//
// Two modes are supported per partner:
//
//   - static: every internal identity is replaced by a single edge identity.
//     Cheap and stateless, but a partner cannot address an internal host.
//   - stateful: every internal identity is replaced by an unguessable pseudonym
//     that is remembered for a configurable TTL, so a subsequent request from
//     the partner carrying Destination-Host = <pseudonym> is routed back to the
//     real internal host.
//
// This extension supersedes rt_hide_oh; running both at the same time is not
// supported.
//
//nolint:revive // package name mirrors the extension name used in configuration
package dea_topohide

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// handlerPriority runs topology hiding after screening (8) and rate limiting
// (12) but before the routing extensions (55), matching rt_hide_oh's slot.
const handlerPriority = 30

// defaultSweepInterval controls how often expired pseudonyms are reclaimed.
const defaultSweepInterval = time.Minute

// pseudonymBytes is the amount of entropy in a generated pseudonym label.
const pseudonymBytes = 8

type deaTopoHide struct {
	mu sync.RWMutex

	reg           *edge.Registry
	store         Store
	router        relayOriginer
	localIdentity string
	localRealm    string

	enabled atomic.Bool

	hiddenRequests atomic.Uint64
	hiddenAnswers  atomic.Uint64
	allocated      atomic.Uint64
	restored       atomic.Uint64
	restoreMisses  atomic.Uint64
	scrubbed       atomic.Uint64
	perPartner     sync.Map // string -> *atomic.Uint64

	// randRead supplies pseudonym entropy; overridable in tests.
	randRead func([]byte) (int, error)
	// now supplies the current time; overridable in tests.
	now func() time.Time
}

// Name returns the extension name.
func (e *deaTopoHide) Name() string { return "dea_topohide" }

// Init initializes the extension.
func (e *deaTopoHide) Init(ctx extension.InitContext, cfg map[string]interface{}) error {
	maxEntries := 0
	if v, ok := intOption(cfg, "max_entries"); ok {
		maxEntries = v
	}
	sweep := defaultSweepInterval
	if v, ok := intOption(cfg, "sweep_interval_seconds"); ok && v > 0 {
		sweep = time.Duration(v) * time.Second
	}

	e.mu.Lock()
	e.reg = ctx.GetEdgeRegistry()
	if router := ctx.GetRouter(); router != nil {
		e.router = router
	}
	if c := ctx.GetConfig(); c != nil {
		e.localIdentity = c.Identity
		e.localRealm = c.Realm
		if maxEntries == 0 {
			maxEntries = maxPartnerEntries(c)
		}
	}
	if e.now == nil {
		e.now = time.Now
	}
	if e.randRead == nil {
		e.randRead = rand.Read
	}
	if e.store == nil {
		e.store = newMemoryStore(maxEntries, sweep, e.now)
	}
	if c := ctx.GetConfig(); c != nil {
		for i := range c.Partners {
			limit := c.Partners[i].TopologyHiding.MaxEntries
			if limit < 0 {
				limit = 0 // unlimited
			}
			e.store.SetPartnerLimit(c.Partners[i].Name, limit)
		}
	}
	e.mu.Unlock()

	e.enabled.Store(true)
	e.applyConfig(cfg)

	log.Printf("Initializing extension: dea_topohide (max_entries=%d)", maxEntries)

	router := ctx.GetRouter()
	router.RegisterInHandler("dea_topohide", handlerPriority, e.handleIn)
	router.RegisterPostRouteHandler("dea_topohide", e.handlePostRoute)
	return nil
}

func (e *deaTopoHide) applyConfig(cfg map[string]interface{}) {
	if v, ok := cfg["enabled"].(bool); ok {
		e.enabled.Store(v)
	}
}

func intOption(cfg map[string]interface{}, key string) (int, bool) {
	switch n := cfg[key].(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// maxPartnerEntries derives the store-wide cap from the partner policies. Each
// partner is capped individually as well, so this is the aggregate ceiling.
//
// A partner that explicitly asks for an unlimited store (max_entries: -1)
// makes the aggregate unlimited too.
func maxPartnerEntries(c *config.Config) int {
	total := 0
	for i := range c.Partners {
		if c.Partners[i].TopologyHiding.Mode == config.TopoHideNone {
			continue
		}
		if c.Partners[i].TopologyHiding.MaxEntries < 0 {
			return 0
		}
		total += c.Partners[i].TopologyHiding.MaxEntries
	}
	return total
}

// handleIn processes messages arriving from a peer.
//
// Requests from an external partner may carry a pseudonym in Destination-Host,
// which must be translated back before routing. Answers coming back from an
// internal peer are on their way to the partner that started the session, so
// they are hidden here: the relay path sends answers straight to the previous
// hop without consulting the outgoing handlers.
func (e *deaTopoHide) handleIn(p *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !e.enabled.Load() || p == nil || msg == nil {
		return candidates, nil
	}

	e.mu.RLock()
	reg, store, router := e.reg, e.store, e.router
	e.mu.RUnlock()
	if reg == nil || store == nil || !reg.Configured() {
		return candidates, nil
	}

	if msg.IsRequest() {
		e.restoreDestinationHost(msg, store)
		return candidates, nil
	}

	partner := e.answerPartner(reg, router, string(p.DiameterID()), msg)
	if partner == nil || partner.TopologyHiding.Mode == config.TopoHideNone {
		return candidates, nil
	}
	if e.hide(msg, partner, store) {
		e.hiddenAnswers.Add(1)
		e.countPartner(partner.Name)
	}
	return candidates, nil
}

// handlePostRoute hides internal topology in requests just before they are sent
// to the selected next hop, which is the only point where the destination peer
// is known.
func (e *deaTopoHide) handlePostRoute(msg *message.Message, selectedPeer types.DiamID) {
	if !e.enabled.Load() || msg == nil || !msg.IsRequest() {
		return
	}

	e.mu.RLock()
	reg, store := e.reg, e.store
	e.mu.RUnlock()
	if reg == nil || store == nil || !reg.Configured() {
		return
	}

	partner, ok := reg.PartnerForPeer(string(selectedPeer))
	if !ok || partner.TopologyHiding.Mode == config.TopoHideNone {
		return
	}
	if !reg.IsExternalZone(partner.Zone) {
		return
	}

	// No session mapping is recorded here. This is a request the edge is
	// sending to a partner on behalf of an internal client, so the answer will
	// travel back inward and must not be scrubbed; the relay transaction
	// already tells handleIn that. Recording it would also charge the entry to
	// the destination partner, letting one partner consume another's eviction
	// budget.
	if e.hide(msg, partner, store) {
		e.hiddenRequests.Add(1)
		e.countPartner(partner.Name)
	}
}

// answerPartner resolves the partner an answer is destined for.
//
// The authoritative source is the router's relay transaction: the hop-by-hop ID
// an answer carries was minted by this node when it relayed the request, so it
// identifies the peer the answer is about to be sent back to and a partner can
// neither omit nor forge it. It also tells us when an answer is travelling the
// other way, from a partner back to an internal client, which must not be
// scrubbed.
//
// No Session-Id fallback exists. An answer with no relay transaction is not
// being forwarded to a partner at all, so hiding it would scrub a message that
// never leaves the operator network, and a Session-Id the partner chooses is
// not evidence of where the answer is going.
func (e *deaTopoHide) answerPartner(reg *edge.Registry, router relayOriginer, answeringPeer string, msg *message.Message) *config.PartnerConfig {
	if router == nil {
		return nil
	}
	origin, ok := router.RelayOrigin(answeringPeer, msg.HopByHopID)
	if !ok {
		return nil
	}
	partner, ok := reg.PartnerForPeer(origin)
	if !ok {
		return nil
	}
	return partner
}

// hide masks internal identities in msg according to the partner's policy and
// reports whether anything was changed.
func (e *deaTopoHide) hide(msg *message.Message, partner *config.PartnerConfig, store Store) bool {
	th := &partner.TopologyHiding
	session, _ := msg.GetSessionID()
	changed := false
	originMasked := false

	for i := 0; i < len(msg.AVPs); i++ {
		avp := msg.AVPs[i]
		remove := false

		//nolint:exhaustive // only topology-revealing AVPs are handled
		switch avp.Code {
		case types.AVPCodeOriginHost:
			if host := string(avp.Data); e.mustMask(host, th.EdgeIdentity) {
				avp.Data = []byte(e.mask(host, session, partner, store))
				originMasked = true
				changed = true
			}
		case types.AVPCodeErrorReportingHost:
			if hideFlag(th.HideErrorReportingHost) && e.mustMask(string(avp.Data), th.EdgeIdentity) {
				avp.Data = []byte(th.EdgeIdentity)
				changed = true
			}
		case types.AVPCodeRouteRecord:
			remove = hideFlag(th.HideRouteRecords) && e.mustMask(string(avp.Data), th.EdgeIdentity)
		case types.AVPCodeProxyInfo:
			remove = hideFlag(th.HideProxyInfo) && e.proxyInfoIsInternal(avp, th.EdgeIdentity)
		case types.AVPCodeHostIPAddress:
			remove = hideFlag(th.HideHostIPAddress)
		case types.AVPCodeOriginStateID:
			remove = hideFlag(th.HideOriginStateID)
		}

		if !remove && matchesSelector(avp, th.ScrubAVPs) {
			remove = true
		}
		if remove {
			msg.AVPs = append(msg.AVPs[:i], msg.AVPs[i+1:]...)
			i--
			changed = true
			e.scrubbed.Add(1)
		}
	}

	// A masked Origin-Host must be paired with the outward realm, otherwise the
	// internal realm leaks and the partner sees an inconsistent identity.
	if originMasked && hideFlag(th.HideOriginRealm) {
		if avp := msg.FindAVP(types.AVPCodeOriginRealm, 0); avp != nil {
			if realm := outwardRealm(th); realm != "" && !strings.EqualFold(string(avp.Data), realm) {
				avp.Data = []byte(realm)
				changed = true
			}
		}
	}

	// The edge must remain visible in the Route-Record chain for loop
	// detection even when the internal records were removed.
	if hideFlag(th.HideRouteRecords) && msg.IsRequest() && !hasRouteRecord(msg, th.EdgeIdentity) {
		msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory,
			types.DiamID(th.EdgeIdentity)))
		changed = true
	}
	return changed
}

// outwardRealm is the realm a partner is allowed to see: the pseudonym realm,
// or failing that the realm of the edge identity.
func outwardRealm(th *config.TopologyHidingPolicy) string {
	if th.PseudonymRealm != "" {
		return th.PseudonymRealm
	}
	if i := strings.Index(th.EdgeIdentity, "."); i >= 0 {
		return th.EdgeIdentity[i+1:]
	}
	return ""
}

// mask returns the outward identity for an internal host.
func (e *deaTopoHide) mask(host, session string, partner *config.PartnerConfig, store Store) string {
	th := &partner.TopologyHiding
	if th.Mode != config.TopoHideStateful {
		return th.EdgeIdentity
	}
	if pseudonym, ok := store.Pseudonym(host, session); ok {
		return pseudonym
	}
	pseudonym, err := e.newPseudonym(th)
	if err != nil {
		log.Printf("dea_topohide: pseudonym generation failed, falling back to the edge identity: %v", err)
		return th.EdgeIdentity
	}
	store.PutPseudonym(partner.Name, pseudonym, host, session, e.ttl(partner))
	e.allocated.Add(1)
	return pseudonym
}

// newPseudonym builds an unguessable identity inside the pseudonym realm.
func (e *deaTopoHide) newPseudonym(th *config.TopologyHidingPolicy) (string, error) {
	buf := make([]byte, pseudonymBytes)
	if _, err := e.randRead(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	// The result is normalised so that a mixed-case prefix or realm still
	// produces a key the reverse lookup can find.
	label := normalize(th.PseudonymPrefix) + hex.EncodeToString(buf)
	if th.PseudonymRealm == "" {
		return label, nil
	}
	return label + "." + normalize(th.PseudonymRealm), nil
}

// restoreDestinationHost translates a pseudonym in Destination-Host back to the
// internal host it masks, so ordinary routing can reach it.
func (e *deaTopoHide) restoreDestinationHost(msg *message.Message, store Store) {
	avp := msg.FindAVP(types.AVPCodeDestinationHost, 0)
	if avp == nil {
		return
	}
	host := normalize(string(avp.Data))
	if host == "" {
		return
	}
	realHost, ok := store.RealHost(host)
	if !ok {
		e.restoreMisses.Add(1)
		return
	}
	avp.Data = []byte(realHost)
	e.restored.Add(1)
}

// isInternal reports whether an identity belongs to this node's own realm and
// therefore must not be disclosed to a partner.
func (e *deaTopoHide) isInternal(host string) bool {
	if host == "" {
		return false
	}
	e.mu.RLock()
	identity, realm, reg := e.localIdentity, e.localRealm, e.reg
	e.mu.RUnlock()

	host = normalize(host)
	if host == normalize(identity) {
		return true
	}
	// The edge model knows the whole internal side: peers in internal zones and
	// the realms this agent terminates, not just its own realm.
	if reg != nil && reg.Configured() {
		return reg.IsInternalHost(host)
	}
	return realm != "" && edge.HostInRealm(host, realm)
}

// mustMask reports whether an identity is internal and is not the outward edge
// identity, which is the one value a partner is allowed to see.
func (e *deaTopoHide) mustMask(host, edgeIdentity string) bool {
	if host == "" || normalize(host) == normalize(edgeIdentity) {
		return false
	}
	return e.isInternal(host)
}

// proxyInfoIsInternal reports whether a Proxy-Info names an internal proxy.
func (e *deaTopoHide) proxyInfoIsInternal(avp *message.AVP, edgeIdentity string) bool {
	for _, child := range groupedChildren(avp) {
		if child.Code == types.AVPCodeProxyHost {
			return e.mustMask(string(child.Data), edgeIdentity)
		}
	}
	// A Proxy-Info whose Proxy-Host cannot be read is removed rather than leaked.
	return true
}

// groupedChildren returns the children of a grouped AVP, decoding it on demand.
func groupedChildren(avp *message.AVP) []*message.AVP {
	if avp.IsGrouped() {
		return avp.Children()
	}
	if len(avp.Data) < 8 {
		return nil
	}
	if err := avp.DecodeGrouped(); err != nil {
		return nil
	}
	return avp.Children()
}

func (e *deaTopoHide) ttl(partner *config.PartnerConfig) time.Duration {
	if ttl := partner.TopologyHiding.TTL; ttl > 0 {
		return ttl
	}
	return 30 * time.Minute
}

func (e *deaTopoHide) countPartner(name string) {
	v, ok := e.perPartner.Load(name)
	if !ok {
		v, _ = e.perPartner.LoadOrStore(name, &atomic.Uint64{})
	}
	if counter, ok := v.(*atomic.Uint64); ok {
		counter.Add(1)
	}
}

// hideFlag resolves an optional policy flag; hiding defaults to enabled.
func hideFlag(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}

func hasRouteRecord(msg *message.Message, identity string) bool {
	want := normalize(identity)
	for _, avp := range msg.FindAllAVPs(types.AVPCodeRouteRecord, 0) {
		if normalize(string(avp.Data)) == want {
			return true
		}
	}
	return false
}

func matchesSelector(avp *message.AVP, selectors []config.AVPSelector) bool {
	for _, s := range selectors {
		if uint32(avp.Code) == s.Code && uint32(avp.VendorID) == s.VendorID {
			return true
		}
	}
	return false
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// Stop implements the Stoppable interface.
func (e *deaTopoHide) Stop() error {
	e.enabled.Store(false)
	e.mu.Lock()
	store := e.store
	e.store = nil
	e.mu.Unlock()
	if store != nil {
		store.Close()
	}
	return nil
}

// Reconfigure implements the Reconfigurable interface.
func (e *deaTopoHide) Reconfigure(cfg map[string]interface{}) error {
	e.applyConfig(cfg)
	log.Printf("dea_topohide: reconfigured (enabled=%v)", e.enabled.Load())
	return nil
}

// HealthCheck implements the HealthCheckable interface.
func (e *deaTopoHide) HealthCheck() (string, map[string]interface{}) {
	e.mu.RLock()
	store := e.store
	e.mu.RUnlock()

	details := map[string]interface{}{
		"enabled":          e.enabled.Load(),
		"hidden_requests":  e.hiddenRequests.Load(),
		"hidden_answers":   e.hiddenAnswers.Load(),
		"pseudonyms_added": e.allocated.Load(),
		"restored":         e.restored.Load(),
		"restore_misses":   e.restoreMisses.Load(),
	}
	status := "ok"
	if !e.enabled.Load() || store == nil {
		status = "disabled"
	}
	if store != nil {
		st := store.Stats()
		details["pseudonyms"] = st.Pseudonyms
		details["evictions"] = st.Evictions
		details["expired"] = st.Expired
	}
	return status, details
}

// Metrics implements extension.MetricsProvider.
func (e *deaTopoHide) Metrics() []extension.Metric {
	metrics := []extension.Metric{
		{Name: "hidden_requests_total", Help: "Requests masked before reaching an external partner.", Type: extension.MetricCounter, Value: float64(e.hiddenRequests.Load())},
		{Name: "hidden_answers_total", Help: "Answers masked before reaching an external partner.", Type: extension.MetricCounter, Value: float64(e.hiddenAnswers.Load())},
		{Name: "pseudonyms_allocated_total", Help: "Pseudonyms allocated for internal hosts.", Type: extension.MetricCounter, Value: float64(e.allocated.Load())},
		{Name: "restored_total", Help: "Pseudonyms resolved back to an internal host.", Type: extension.MetricCounter, Value: float64(e.restored.Load())},
		{Name: "restore_misses_total", Help: "Destination-Host values that could not be resolved.", Type: extension.MetricCounter, Value: float64(e.restoreMisses.Load())},
		{Name: "scrubbed_avps_total", Help: "AVPs removed by topology hiding.", Type: extension.MetricCounter, Value: float64(e.scrubbed.Load())},
	}

	e.mu.RLock()
	store := e.store
	e.mu.RUnlock()
	if store != nil {
		st := store.Stats()
		metrics = append(metrics,
			extension.Metric{Name: "store_pseudonyms", Help: "Pseudonym mappings currently held.", Type: extension.MetricGauge, Value: float64(st.Pseudonyms)},
			extension.Metric{Name: "store_evictions_total", Help: "Entries evicted because the store was full.", Type: extension.MetricCounter, Value: float64(st.Evictions)},
			extension.Metric{Name: "store_expired_total", Help: "Entries removed after their TTL elapsed.", Type: extension.MetricCounter, Value: float64(st.Expired)},
		)
	}

	type partnerCount struct {
		name  string
		value uint64
	}
	var counts []partnerCount
	e.perPartner.Range(func(k, v interface{}) bool {
		name, ok1 := k.(string)
		counter, ok2 := v.(*atomic.Uint64)
		if ok1 && ok2 {
			counts = append(counts, partnerCount{name: name, value: counter.Load()})
		}
		return true
	})
	sort.Slice(counts, func(i, j int) bool { return counts[i].name < counts[j].name })
	for _, c := range counts {
		metrics = append(metrics, extension.Metric{
			Name:   "hidden_messages_total",
			Help:   "Messages masked per partner.",
			Type:   extension.MetricCounter,
			Value:  float64(c.value),
			Labels: map[string]string{"partner": c.name},
		})
	}
	return metrics
}

func init() {
	extension.Register(&deaTopoHide{})
}

// relayOriginer reports which peer a relayed request came from, given the peer
// that is answering and the hop-by-hop ID of the answer coming back. Both are
// needed because a hop-by-hop ID is unique only per connection. It is satisfied
// by *routing.Router and kept as an interface so tests can supply their own.
type relayOriginer interface {
	RelayOrigin(outboundPeer string, hbhID types.HopByHopID) (string, bool)
}
