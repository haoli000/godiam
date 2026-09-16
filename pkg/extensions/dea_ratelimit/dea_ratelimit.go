// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package dea_ratelimit throttles traffic arriving from roaming partners on a
// Diameter Edge Agent. Each partner may define an aggregate limit plus finer
// limits per peer, per application and per command; the most specific matching
// rule wins, so a partner-wide ceiling never masks a tighter command limit.
//
// Over-limit requests are either answered with DIAMETER_TOO_BUSY or dropped,
// as configured. Answers are never throttled: discarding an answer would leave
// the originator waiting for a transaction that already completed.
//
//nolint:revive // package name mirrors the extension name used in configuration
package dea_ratelimit

import (
	"log"
	"sort"
	"strconv"
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

// handlerPriority runs rate limiting immediately after screening (8) so that
// throttled traffic never reaches topology hiding (30) or routing (55).
const handlerPriority = 12

// Scope names, also used as metric labels.
const (
	ScopeCommand     = "command"
	ScopeApplication = "application"
	ScopePeer        = "peer"
	ScopePartner     = "partner"
)

const defaultSweepInterval = time.Minute

type deaRateLimit struct {
	mu sync.RWMutex

	reg           *edge.Registry
	limiter       Limiter
	localIdentity string
	localRealm    string
	// keys holds the precomputed bucket keys per partner so the hot path does
	// not build a string for every request.
	keys map[string]*partnerKeys

	enabled  atomic.Bool
	auditLog atomic.Bool

	// sendFn delivers answers to a peer; overridable in tests.
	sendFn func(*peer.Peer, *message.Message) error

	checked  atomic.Uint64
	allowed  atomic.Uint64
	rejected atomic.Uint64
	dropped  atomic.Uint64
	limited  sync.Map // limitKey -> *atomic.Uint64
}

type limitKey struct {
	partner string
	scope   string
}

// decision is the rule selected for a message plus the bucket it consumes.
type decision struct {
	rule  config.RateLimitRule
	scope string
	key   string
}

// partnerKeys caches the bucket keys of one partner. Command, application and
// aggregate keys are fully known from the configuration; per-peer keys are
// memoised on first use because the peer set is dynamic.
type partnerKeys struct {
	partner   string
	aggregate string
	app       map[uint32]string
	cmd       map[uint32]string
	peer      sync.Map // peer identity -> bucket key
}

// peerKey returns the per-peer bucket key, creating it once per peer.
func (k *partnerKeys) peerKey(identity string) string {
	if v, ok := k.peer.Load(identity); ok {
		return v.(string)
	}
	key := k.partner + "|peer|" + identity
	v, _ := k.peer.LoadOrStore(identity, key)
	return v.(string)
}

// commandKey returns the per-command bucket key, falling back to building it
// when a live reload introduced the command after the cache was built.
func (k *partnerKeys) commandKey(code uint32) string {
	if key, ok := k.cmd[code]; ok {
		return key
	}
	return k.partner + "|cmd|" + strconv.FormatUint(uint64(code), 10)
}

// applicationKey returns the per-application bucket key.
func (k *partnerKeys) applicationKey(id uint32) string {
	if key, ok := k.app[id]; ok {
		return key
	}
	return k.partner + "|app|" + strconv.FormatUint(uint64(id), 10)
}

// aggregateKey returns the partner-wide bucket key.
func (k *partnerKeys) aggregateKey() string { return k.aggregate }

// buildKeys precomputes the bucket keys for every configured partner.
func buildKeys(reg *edge.Registry) map[string]*partnerKeys {
	if reg == nil {
		return nil
	}
	partners := reg.Partners()
	keys := make(map[string]*partnerKeys, len(partners))
	for _, p := range partners {
		pk := &partnerKeys{partner: p.Name, aggregate: p.Name + "|partner"}
		if n := len(p.RateLimit.PerApplication); n > 0 {
			pk.app = make(map[uint32]string, n)
			for id := range p.RateLimit.PerApplication {
				pk.app[id] = p.Name + "|app|" + strconv.FormatUint(uint64(id), 10)
			}
		}
		if n := len(p.RateLimit.PerCommand); n > 0 {
			pk.cmd = make(map[uint32]string, n)
			for code := range p.RateLimit.PerCommand {
				pk.cmd[code] = p.Name + "|cmd|" + strconv.FormatUint(uint64(code), 10)
			}
		}
		keys[p.Name] = pk
	}
	return keys
}

// keysFor returns the cached keys of a partner, building them on demand so a
// partner added by a live reload is still covered.
func (e *deaRateLimit) keysFor(name string) *partnerKeys {
	e.mu.RLock()
	pk := e.keys[name]
	e.mu.RUnlock()
	if pk != nil {
		return pk
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if existing := e.keys[name]; existing != nil {
		return existing
	}
	if e.keys == nil {
		e.keys = make(map[string]*partnerKeys)
	}
	pk = &partnerKeys{partner: name, aggregate: name + "|partner"}
	e.keys[name] = pk
	return pk
}

// Name returns the extension name.
func (e *deaRateLimit) Name() string { return "dea_ratelimit" }

// Init initializes the extension.
func (e *deaRateLimit) Init(ctx extension.InitContext, cfg map[string]interface{}) error {
	sweep := defaultSweepInterval
	if v, ok := intOption(cfg, "sweep_interval_seconds"); ok && v > 0 {
		sweep = time.Duration(v) * time.Second
	}

	e.mu.Lock()
	e.reg = ctx.GetEdgeRegistry()
	e.keys = buildKeys(e.reg)
	if c := ctx.GetConfig(); c != nil {
		e.localIdentity = c.Identity
		e.localRealm = c.Realm
	}
	if e.limiter == nil {
		e.limiter = newTokenLimiter(sweep, nil)
	}
	e.mu.Unlock()

	e.enabled.Store(true)
	e.auditLog.Store(true)
	e.applyConfig(cfg)

	log.Printf("Initializing extension: dea_ratelimit")

	ctx.GetRouter().RegisterInHandler("dea_ratelimit", handlerPriority, e.handleRouting)
	return nil
}

func (e *deaRateLimit) applyConfig(cfg map[string]interface{}) {
	if v, ok := cfg["enabled"].(bool); ok {
		e.enabled.Store(v)
	}
	if v, ok := cfg["audit_log"].(bool); ok {
		e.auditLog.Store(v)
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

// handleRouting throttles requests arriving from a rate-limited partner.
func (e *deaRateLimit) handleRouting(p *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !e.enabled.Load() || p == nil || msg == nil || !msg.IsRequest() {
		return candidates, nil
	}

	e.mu.RLock()
	reg, limiter := e.reg, e.limiter
	e.mu.RUnlock()
	if reg == nil || limiter == nil || !reg.Configured() {
		return candidates, nil
	}

	partner := e.partnerFor(reg, p, msg)
	if partner == nil || !partner.RateLimit.Enabled {
		return candidates, nil
	}

	d, ok := selectRule(partner, e.keysFor(partner.Name), string(p.DiameterID()), msg)
	if !ok {
		return candidates, nil
	}

	e.checked.Add(1)
	if limiter.Allow(d.key, d.rule.RequestsPerSecond, d.rule.Burst) {
		e.allowed.Add(1)
		return candidates, nil
	}
	return e.enforce(p, msg, partner, d)
}

// partnerFor resolves the roaming partner a message belongs to.
func (e *deaRateLimit) partnerFor(reg *edge.Registry, p *peer.Peer, msg *message.Message) *config.PartnerConfig {
	if name := p.Partner(); name != "" {
		if partner, ok := reg.Partner(name); ok {
			return partner
		}
	}
	if partner, ok := reg.PartnerForPeer(string(p.DiameterID())); ok {
		return partner
	}
	// Origin-Realm is attacker-controlled, so it must not decide which partner
	// a peer on an external zone is judged against. Peers inside the network
	// may still be matched by realm.
	if reg.IsExternalZone(zoneOf(reg, p)) {
		return nil
	}
	if originRealm, ok := msg.GetOriginRealm(); ok {
		if partner, ok := reg.PartnerForRealm(string(originRealm)); ok {
			return partner
		}
	}
	return nil
}

// zoneOf reports which zone a peer was admitted to.
func zoneOf(reg *edge.Registry, p *peer.Peer) string {
	if name := p.Zone(); name != "" {
		return name
	}
	return reg.ZoneNameForPeer(string(p.DiameterID()))
}

// selectRule returns the most specific rule that applies to the message:
// command, then application, then peer, then the partner aggregate.
func selectRule(partner *config.PartnerConfig, keys *partnerKeys, peerIdentity string, msg *message.Message) (decision, bool) {
	rl := &partner.RateLimit

	if r, ok := rl.PerCommand[uint32(msg.CommandCode)]; ok && r.RequestsPerSecond > 0 {
		return decision{rule: r, scope: ScopeCommand,
			key: keys.commandKey(uint32(msg.CommandCode))}, true
	}
	if r, ok := rl.PerApplication[uint32(msg.ApplicationID)]; ok && r.RequestsPerSecond > 0 {
		return decision{rule: r, scope: ScopeApplication,
			key: keys.applicationKey(uint32(msg.ApplicationID))}, true
	}
	if rl.PerPeer != nil && rl.PerPeer.RequestsPerSecond > 0 {
		return decision{rule: *rl.PerPeer, scope: ScopePeer,
			key: keys.peerKey(peerIdentity)}, true
	}
	if rl.Partner.RequestsPerSecond > 0 {
		return decision{rule: rl.Partner, scope: ScopePartner, key: keys.aggregateKey()}, true
	}
	return decision{}, false
}

// enforce applies the rule's action to an over-limit request.
func (e *deaRateLimit) enforce(p *peer.Peer, msg *message.Message, partner *config.PartnerConfig, d decision) ([]routing.Route, error) {
	e.countLimited(partner.Name, d.scope)

	if e.auditLog.Load() {
		originHost, _ := msg.GetOriginHost()
		log.Printf("dea_ratelimit: action=%s scope=%s partner=%s peer=%s origin_host=%s app=%d cmd=%d rate=%.2f burst=%d",
			d.rule.Action, d.scope, partner.Name, p.DiameterID(), originHost,
			msg.ApplicationID, msg.CommandCode, d.rule.RequestsPerSecond, d.rule.Burst)
	}

	if d.rule.Action == config.ActionDrop {
		e.dropped.Add(1)
		return nil, routing.ErrConsumed
	}

	code := types.ResultCode(d.rule.ResultCode)
	if code == 0 {
		code = types.ResultTooBusy
	}
	if err := e.send(p, e.errorAnswer(msg, code)); err != nil {
		log.Printf("dea_ratelimit: sending the throttling answer failed: %v", err)
	}
	e.rejected.Add(1)
	return nil, routing.ErrConsumed
}

func (e *deaRateLimit) send(p *peer.Peer, msg *message.Message) error {
	e.mu.RLock()
	fn := e.sendFn
	e.mu.RUnlock()
	if fn != nil {
		return fn(p, msg)
	}
	return p.Send(msg)
}

// errorAnswer builds the throttling answer. Like screening rejections it does
// not disclose internal topology.
func (e *deaRateLimit) errorAnswer(req *message.Message, code types.ResultCode) *message.Message {
	e.mu.RLock()
	identity, realm := e.localIdentity, e.localRealm
	e.mu.RUnlock()

	ans := message.NewAnswer(req)
	ans.SetError(true)
	ans.SetResultCode(code)
	if identity != "" {
		ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(identity)))
	}
	if realm != "" {
		ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID(realm)))
	}
	ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeErrorMessage, 0, "rate limit exceeded"))
	return ans
}

func (e *deaRateLimit) countLimited(partner, scope string) {
	key := limitKey{partner: partner, scope: scope}
	if c, ok := e.limited.Load(key); ok {
		if counter, ok := c.(*atomic.Uint64); ok {
			counter.Add(1)
			return
		}
	}
	c, _ := e.limited.LoadOrStore(key, &atomic.Uint64{})
	if counter, ok := c.(*atomic.Uint64); ok {
		counter.Add(1)
	}
}

// Stop implements the Stoppable interface.
func (e *deaRateLimit) Stop() error {
	e.enabled.Store(false)
	e.mu.Lock()
	limiter := e.limiter
	e.limiter = nil
	e.mu.Unlock()
	if limiter != nil {
		limiter.Close()
	}
	return nil
}

// Reconfigure implements the Reconfigurable interface.
func (e *deaRateLimit) Reconfigure(cfg map[string]interface{}) error {
	e.applyConfig(cfg)
	log.Printf("dea_ratelimit: reconfigured (enabled=%v audit_log=%v)", e.enabled.Load(), e.auditLog.Load())
	return nil
}

// HealthCheck implements the HealthCheckable interface.
func (e *deaRateLimit) HealthCheck() (string, map[string]interface{}) {
	e.mu.RLock()
	reg, limiter := e.reg, e.limiter
	e.mu.RUnlock()

	details := map[string]interface{}{
		"enabled":  e.enabled.Load(),
		"checked":  e.checked.Load(),
		"allowed":  e.allowed.Load(),
		"rejected": e.rejected.Load(),
		"dropped":  e.dropped.Load(),
	}
	status := "ok"
	if !e.enabled.Load() || limiter == nil {
		status = "disabled"
	}
	if limiter != nil {
		details["buckets"] = limiter.Len()
	}
	if reg != nil && limiter != nil {
		utilization := make(map[string]float64)
		for _, partner := range reg.Partners() {
			if !partner.RateLimit.Enabled || partner.RateLimit.Partner.RequestsPerSecond <= 0 {
				continue
			}
			if u, ok := limiter.Utilization(partner.Name + "|partner"); ok {
				utilization[partner.Name] = u
			}
		}
		if len(utilization) > 0 {
			details["partner_utilization"] = utilization
		}
	}
	return status, details
}

// Metrics implements extension.MetricsProvider.
func (e *deaRateLimit) Metrics() []extension.Metric {
	metrics := []extension.Metric{
		{Name: "checked_total", Help: "Requests evaluated against a rate limit.", Type: extension.MetricCounter, Value: float64(e.checked.Load())},
		{Name: "allowed_total", Help: "Requests that stayed within their rate limit.", Type: extension.MetricCounter, Value: float64(e.allowed.Load())},
		{Name: "rejected_total", Help: "Requests answered with a throttling Result-Code.", Type: extension.MetricCounter, Value: float64(e.rejected.Load())},
		{Name: "dropped_total", Help: "Requests silently dropped by throttling.", Type: extension.MetricCounter, Value: float64(e.dropped.Load())},
	}

	e.mu.RLock()
	limiter := e.limiter
	e.mu.RUnlock()
	if limiter != nil {
		metrics = append(metrics, extension.Metric{
			Name: "buckets", Help: "Token buckets currently held.",
			Type: extension.MetricGauge, Value: float64(limiter.Len()),
		})
	}

	type entry struct {
		key   limitKey
		value uint64
	}
	var entries []entry
	e.limited.Range(func(k, v interface{}) bool {
		key, ok1 := k.(limitKey)
		counter, ok2 := v.(*atomic.Uint64)
		if ok1 && ok2 {
			entries = append(entries, entry{key: key, value: counter.Load()})
		}
		return true
	})
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].key.partner != entries[j].key.partner {
			return entries[i].key.partner < entries[j].key.partner
		}
		return entries[i].key.scope < entries[j].key.scope
	})
	for _, en := range entries {
		metrics = append(metrics, extension.Metric{
			Name:   "limited_total",
			Help:   "Requests that exceeded a rate limit, by partner and scope.",
			Type:   extension.MetricCounter,
			Value:  float64(en.value),
			Labels: map[string]string{"partner": en.key.partner, "scope": en.key.scope},
		})
	}
	return metrics
}

func init() {
	extension.Register(&deaRateLimit{})
}
