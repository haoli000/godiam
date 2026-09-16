// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package dea_route makes outgoing routing decisions partner aware on a
// Diameter Edge Agent.
//
// It builds candidates from the partner model, so a realm owned by a roaming
// partner is served by that partner's peers in priority tiers with weighted
// selection inside a tier, and it enforces the edge's delivery rules: traffic
// is never handed to an external peer that does not own the destination, and a
// partner may not use the edge to reach another partner unless transit is
// explicitly allowed.
//
//nolint:revive // package name mirrors the extension name used in configuration
package dea_route

import (
	"log"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// handlerPriority runs partner routing after the rewriting extensions and
// before load balancing (60) and randomisation (70), which refine the
// candidate set this handler produces.
const handlerPriority = 55

const (
	// defaultBaseScore ranks partner routes above application (200) and realm
	// (100) routes but below an explicit Destination-Host route (500).
	defaultBaseScore = 300
	// tierStep is the score distance between two priority tiers.
	tierStep = 10
	// winnerBonus lifts the weighted winner above its own tier so the rest of
	// the tier stays available for failover.
	winnerBonus = 1
)

type deaRoute struct {
	mu sync.RWMutex

	reg       *edge.Registry
	baseScore int

	enabled atomic.Bool

	// intn returns a pseudo-random number in [0,n); overridable in tests.
	intn func(int) int

	evaluated  atomic.Uint64
	routed     atomic.Uint64
	transit    atomic.Uint64
	misdeliver atomic.Uint64
	perPartner sync.Map // string -> *atomic.Uint64
}

// Name returns the extension name.
func (e *deaRoute) Name() string { return "dea_route" }

// Init initializes the extension.
func (e *deaRoute) Init(ctx extension.InitContext, cfg map[string]interface{}) error {
	e.mu.Lock()
	e.reg = ctx.GetEdgeRegistry()
	e.baseScore = defaultBaseScore
	if v, ok := intOption(cfg, "base_score"); ok && v > 0 {
		e.baseScore = v
	}
	if e.intn == nil {
		e.intn = randIntn
	}
	e.mu.Unlock()

	e.enabled.Store(true)
	e.applyConfig(cfg)

	log.Printf("Initializing extension: dea_route")

	ctx.GetRouter().RegisterOutHandler("dea_route", handlerPriority, e.handleRouting)
	return nil
}

func (e *deaRoute) applyConfig(cfg map[string]interface{}) {
	if v, ok := cfg["enabled"].(bool); ok {
		e.enabled.Store(v)
	}
}

// randIntn spreads traffic inside a priority tier.
func randIntn(n int) int {
	return rand.Intn(n) //nolint:gosec // G404: routing spread does not need cryptographic randomness
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

// handleRouting refines the candidate set for an outgoing message.
func (e *deaRoute) handleRouting(msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !e.enabled.Load() || msg == nil {
		return candidates, nil
	}

	e.mu.RLock()
	reg := e.reg
	e.mu.RUnlock()
	if reg == nil || !reg.Configured() {
		return candidates, nil
	}

	// Counted before any decision: the direction checks below run on every
	// outgoing message, but in an external-to-internal topology none of the
	// outcome counters ever move, leaving no evidence the control ran at all.
	e.evaluated.Add(1)

	destPartner := destinationPartner(reg, msg)
	originPartner := originPartnerOf(reg, msg)

	// A partner may not reach another partner through the edge unless it is an
	// explicit transit peer.
	if originPartner != nil && destPartner != nil && originPartner.Name != destPartner.Name &&
		!originPartner.AllowTransit {
		// Refusing transit means refusing the message, not merely pruning the
		// partner's own peers: leaving internal candidates in place would
		// deliver a refused request into the network. An empty candidate set
		// makes the router answer DIAMETER_UNABLE_TO_DELIVER.
		e.transit.Add(1)
		log.Printf("dea_route: refusing transit from partner %s toward partner %s", originPartner.Name, destPartner.Name)
		return nil, nil
	}

	// Never hand a message to an external peer that does not own the destination.
	candidates = e.dropForeignExternal(reg, candidates, destPartner)

	if destPartner == nil || len(destPartner.Peers) == 0 {
		return candidates, nil
	}

	candidates = e.applyPartnerPeers(candidates, destPartner)
	e.routed.Add(1)
	e.countPartner(destPartner.Name)
	return candidates, nil
}

// destinationPartner resolves the partner that owns the message destination.
func destinationPartner(reg *edge.Registry, msg *message.Message) *config.PartnerConfig {
	if host, ok := msg.GetDestinationHost(); ok && host != "" {
		if partner, ok := reg.PartnerForPeer(string(host)); ok {
			return partner
		}
	}
	if realm, ok := msg.GetDestinationRealm(); ok && realm != "" {
		if partner, ok := reg.PartnerForRealm(string(realm)); ok {
			return partner
		}
	}
	return nil
}

// originPartnerOf resolves the partner a message originated from.
func originPartnerOf(reg *edge.Registry, msg *message.Message) *config.PartnerConfig {
	if host, ok := msg.GetOriginHost(); ok && host != "" {
		if partner, ok := reg.PartnerForPeer(string(host)); ok {
			return partner
		}
	}
	if realm, ok := msg.GetOriginRealm(); ok && realm != "" {
		if partner, ok := reg.PartnerForRealm(string(realm)); ok {
			return partner
		}
	}
	return nil
}

// dropForeignExternal removes external candidates belonging to a partner other
// than the one that owns the destination.
func (e *deaRoute) dropForeignExternal(reg *edge.Registry, candidates []routing.Route, dest *config.PartnerConfig) []routing.Route {
	kept := make([]routing.Route, 0, len(candidates))
	for _, c := range candidates {
		partner, ok := reg.PartnerForPeer(string(c.PeerIdentity))
		if ok && reg.IsExternalZone(partner.Zone) && (dest == nil || partner.Name != dest.Name) {
			e.misdeliver.Add(1)
			continue
		}
		kept = append(kept, c)
	}
	return kept
}

// applyPartnerPeers merges the partner's peer list into the candidate set,
// scoring it by priority tier and electing a weighted winner per tier.
func (e *deaRoute) applyPartnerPeers(candidates []routing.Route, partner *config.PartnerConfig) []routing.Route {
	e.mu.RLock()
	baseScore, intn := e.baseScore, e.intn
	e.mu.RUnlock()

	index := make(map[types.DiamID]int, len(candidates))
	for i, c := range candidates {
		index[c.PeerIdentity] = i
	}

	tiers := make(map[int][]config.PartnerPeer)
	for _, pp := range partner.Peers {
		tiers[pp.Priority] = append(tiers[pp.Priority], pp)
	}
	priorities := make([]int, 0, len(tiers))
	for priority := range tiers {
		priorities = append(priorities, priority)
	}
	sort.Ints(priorities)

	for _, priority := range priorities {
		tier := tiers[priority]
		score := baseScore - priority*tierStep
		winner := electWinner(tier, intn)
		for _, pp := range tier {
			peerScore := score
			if pp.Identity == winner {
				peerScore += winnerBonus
			}
			route := routing.Route{
				PeerIdentity: types.DiamID(pp.Identity),
				Score:        peerScore,
				Reason:       "partner " + partner.Name,
			}
			if i, ok := index[route.PeerIdentity]; ok {
				if candidates[i].Score < route.Score {
					candidates[i] = route
				}
				continue
			}
			index[route.PeerIdentity] = len(candidates)
			candidates = append(candidates, route)
		}
	}
	return candidates
}

// electWinner picks one peer of a tier proportionally to its weight.
func electWinner(tier []config.PartnerPeer, intn func(int) int) string {
	total := 0
	for _, pp := range tier {
		total += weightOf(pp)
	}
	if total <= 0 || intn == nil {
		return ""
	}
	draw := intn(total)
	for _, pp := range tier {
		draw -= weightOf(pp)
		if draw < 0 {
			return pp.Identity
		}
	}
	return tier[len(tier)-1].Identity
}

func weightOf(pp config.PartnerPeer) int {
	if pp.Weight <= 0 {
		return 1
	}
	return pp.Weight
}

func (e *deaRoute) countPartner(name string) {
	if c, ok := e.perPartner.Load(name); ok {
		if counter, ok := c.(*atomic.Uint64); ok {
			counter.Add(1)
			return
		}
	}
	c, _ := e.perPartner.LoadOrStore(name, &atomic.Uint64{})
	if counter, ok := c.(*atomic.Uint64); ok {
		counter.Add(1)
	}
}

// Stop implements the Stoppable interface.
func (e *deaRoute) Stop() error {
	e.enabled.Store(false)
	return nil
}

// Reconfigure implements the Reconfigurable interface.
func (e *deaRoute) Reconfigure(cfg map[string]interface{}) error {
	e.mu.Lock()
	if v, ok := intOption(cfg, "base_score"); ok && v > 0 {
		e.baseScore = v
	}
	e.mu.Unlock()
	e.applyConfig(cfg)
	log.Printf("dea_route: reconfigured (enabled=%v)", e.enabled.Load())
	return nil
}

// HealthCheck implements the HealthCheckable interface.
func (e *deaRoute) HealthCheck() (string, map[string]interface{}) {
	e.mu.RLock()
	reg, baseScore := e.reg, e.baseScore
	e.mu.RUnlock()

	partners := 0
	if reg != nil {
		partners = len(reg.Partners())
	}
	status := "ok"
	if !e.enabled.Load() {
		status = "disabled"
	}
	return status, map[string]interface{}{
		"enabled":          e.enabled.Load(),
		"partners":         partners,
		"base_score":       baseScore,
		"evaluated":        e.evaluated.Load(),
		"routed":           e.routed.Load(),
		"transit_refused":  e.transit.Load(),
		"foreign_dropped":  e.misdeliver.Load(),
		"handler_priority": handlerPriority,
	}
}

// Metrics implements extension.MetricsProvider.
func (e *deaRoute) Metrics() []extension.Metric {
	metrics := []extension.Metric{
		{Name: "evaluated_total", Help: "Messages the edge routing control evaluated.", Type: extension.MetricCounter, Value: float64(e.evaluated.Load())},
		{Name: "routed_total", Help: "Messages routed using the partner peer list.", Type: extension.MetricCounter, Value: float64(e.routed.Load())},
		{Name: "transit_refused_total", Help: "Messages refused because partner to partner transit is not allowed.", Type: extension.MetricCounter, Value: float64(e.transit.Load())},
		{Name: "foreign_candidates_dropped_total", Help: "Candidates dropped because the peer does not own the destination.", Type: extension.MetricCounter, Value: float64(e.misdeliver.Load())},
	}

	type entry struct {
		name  string
		value uint64
	}
	var entries []entry
	e.perPartner.Range(func(k, v interface{}) bool {
		name, ok1 := k.(string)
		counter, ok2 := v.(*atomic.Uint64)
		if ok1 && ok2 {
			entries = append(entries, entry{name: name, value: counter.Load()})
		}
		return true
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	for _, en := range entries {
		metrics = append(metrics, extension.Metric{
			Name:   "partner_routed_total",
			Help:   "Messages routed per partner.",
			Type:   extension.MetricCounter,
			Value:  float64(en.value),
			Labels: map[string]string{"partner": en.name},
		})
	}
	return metrics
}

func init() {
	extension.Register(&deaRoute{})
}
