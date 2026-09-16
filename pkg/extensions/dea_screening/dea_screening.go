// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package dea_screening implements GSMA FS.19 style ingress screening for a
// Diameter Edge Agent. Requests arriving on an external zone are validated
// against the policy of the roaming partner they belong to before any routing,
// rewriting or topology-hiding work is performed.
//
//nolint:revive // package name mirrors the extension name used in configuration
package dea_screening

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// Screening rule names. They are also the keys accepted by the partner
// configuration's screening.rule_actions map.
const (
	RuleOriginRealm      = "origin_realm"
	RuleOriginHost       = "origin_host"
	RulePeerIdentity     = "peer_identity"
	RuleDestinationRealm = "destination_realm"
	RulePLMN             = "plmn"
	RuleApplication      = "application"
	RuleCommand          = "command"
	RuleAVPDenied        = "avp_denied"
	RuleAVPNotAllowed    = "avp_not_allowed"
	RuleRouteRecords     = "route_records"
	RuleUnknownPartner   = "unknown_partner"
)

// handlerPriority places screening ahead of rate limiting (12), topology
// hiding (30) and every routing extension, so rejected traffic is dropped as
// early and as cheaply as possible.
const handlerPriority = 8

// vendor3GPP is the 3GPP vendor ID used by Visited-PLMN-Id.
const vendor3GPP = 10415

type deaScreening struct {
	mu sync.RWMutex

	reg           *edge.Registry
	localIdentity string
	localRealm    string

	enabled  atomic.Bool
	auditLog atomic.Bool

	// sendFn delivers answers to a peer; overridable in tests.
	sendFn func(*peer.Peer, *message.Message) error

	screened  atomic.Uint64
	rejected  atomic.Uint64
	dropped   atomic.Uint64
	logged    atomic.Uint64
	violation sync.Map // violationKey -> *atomic.Uint64
}

type violationKey struct {
	partner string
	rule    string
}

// Name returns the extension name.
func (e *deaScreening) Name() string { return "dea_screening" }

// Init initializes the extension.
func (e *deaScreening) Init(ctx extension.InitContext, cfg map[string]interface{}) error {
	e.mu.Lock()
	e.reg = ctx.GetEdgeRegistry()
	if c := ctx.GetConfig(); c != nil {
		e.localIdentity = c.Identity
		e.localRealm = c.Realm
	}
	e.mu.Unlock()

	e.enabled.Store(true)
	e.auditLog.Store(true)
	e.applyConfig(cfg)

	partners := 0
	if e.reg != nil {
		partners = len(e.reg.Partners())
	}
	log.Printf("Initializing extension: dea_screening (partners=%d)", partners)

	ctx.GetRouter().RegisterInHandler("dea_screening", handlerPriority, e.handleRouting)
	return nil
}

func (e *deaScreening) applyConfig(cfg map[string]interface{}) {
	if v, ok := boolOption(cfg, "enabled"); ok {
		e.enabled.Store(v)
	}
	if v, ok := boolOption(cfg, "audit_log"); ok {
		e.auditLog.Store(v)
	}
}

func boolOption(cfg map[string]interface{}, key string) (bool, bool) {
	v, ok := cfg[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// verdict is the outcome of evaluating the screening rules.
type verdict struct {
	rule    string
	action  string
	detail  string
	partner string
}

// handleRouting screens an incoming request against its partner's policy.
func (e *deaScreening) handleRouting(p *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !e.enabled.Load() || p == nil || msg == nil || !msg.IsRequest() {
		return candidates, nil
	}

	e.mu.RLock()
	reg := e.reg
	localRealm := e.localRealm
	e.mu.RUnlock()
	if reg == nil {
		return candidates, nil
	}

	zoneName := p.Zone()
	if zoneName == "" {
		zoneName = reg.ZoneNameForPeer(string(p.DiameterID()))
	}
	zone, ok := reg.Zone(zoneName)
	if !ok {
		return candidates, nil
	}
	external := zone.IsExternal()

	partner := e.partnerFor(reg, p, msg, external)
	if partner == nil {
		// A peer on an external zone that maps to no partner has no policy to
		// screen it, no rate limit and no topology hiding. Refusing it here is
		// the only thing standing between an unknown peer and the internal
		// network, so this fails closed rather than open.
		if external {
			return e.rejectUnknownPartner(p, msg, zoneName)
		}
		return candidates, nil
	}
	if !partner.ScreeningEnabled(external) {
		return candidates, nil
	}

	e.screened.Add(1)

	v := e.evaluate(reg, partner, string(p.DiameterID()), msg, localRealm)
	if v == nil {
		return candidates, nil
	}

	return e.enforce(p, msg, partner, v)
}

// partnerFor resolves the roaming partner a message belongs to.
//
// On an external zone only the peer identity is trusted. Origin-Realm is
// attacker-controlled, so honouring it would let a peer pick which partner's
// policy, PLMN list and rate limit budget it is judged against simply by
// claiming a different realm. The realm fallback therefore applies to internal
// traffic only, where it is a convenience rather than a trust decision.
func (e *deaScreening) partnerFor(reg *edge.Registry, p *peer.Peer, msg *message.Message, external bool) *config.PartnerConfig {
	if name := p.Partner(); name != "" {
		if partner, ok := reg.Partner(name); ok {
			return partner
		}
	}
	if partner, ok := reg.PartnerForPeer(string(p.DiameterID())); ok {
		return partner
	}
	if external {
		return nil
	}
	if originRealm, ok := msg.GetOriginRealm(); ok {
		if partner, ok := reg.PartnerForRealm(string(originRealm)); ok {
			return partner
		}
	}
	return nil
}

// rejectUnknownPartner refuses a request from a peer that was admitted to an
// external zone but belongs to no configured partner.
func (e *deaScreening) rejectUnknownPartner(p *peer.Peer, msg *message.Message, zoneName string) ([]routing.Route, error) {
	e.screened.Add(1)
	v := &verdict{
		rule:    RuleUnknownPartner,
		action:  config.ActionReject,
		detail:  fmt.Sprintf("peer is not mapped to any partner on external zone %q", zoneName),
		partner: "",
	}
	e.countViolation(v.partner, v.rule)

	if e.auditLog.Load() {
		originHost, _ := msg.GetOriginHost()
		originRealm, _ := msg.GetOriginRealm()
		log.Printf("dea_screening: action=%s rule=%s peer=%s origin_host=%s origin_realm=%s app=%d cmd=%d: %s",
			v.action, v.rule, p.DiameterID(), originHost, originRealm,
			msg.ApplicationID, msg.CommandCode, v.detail)
	}

	e.rejected.Add(1)
	if err := e.send(p, e.errorAnswer(msg, types.ResultAuthorizationRejected, v)); err != nil {
		log.Printf("dea_screening: failed to send rejection answer: %v", err)
	}
	return nil, routing.ErrConsumed
}

// evaluate runs the rule catalogue and returns the first violation, if any.
//
//nolint:gocyclo // the rule catalogue is intentionally a flat sequence of checks
func (e *deaScreening) evaluate(reg *edge.Registry, partner *config.PartnerConfig, peerIdentity string, msg *message.Message, localRealm string) *verdict {
	s := &partner.Screening

	originHost, _ := msg.GetOriginHost()
	originRealm, _ := msg.GetOriginRealm()

	// Cat 1: source validation.
	if enabledCheck(s.CheckOriginRealm) {
		if originRealm == "" {
			return e.violate(partner, RuleOriginRealm, "missing Origin-Realm")
		}
		if !edge.RealmMatches(string(originRealm), partner.Realms) {
			return e.violate(partner, RuleOriginRealm,
				fmt.Sprintf("Origin-Realm %q is not a realm of partner %q", originRealm, partner.Name))
		}
	}
	if enabledCheck(s.CheckOriginHost) {
		if originHost == "" {
			return e.violate(partner, RuleOriginHost, "missing Origin-Host")
		}
		if originRealm != "" && !edge.HostInRealm(string(originHost), string(originRealm)) {
			return e.violate(partner, RuleOriginHost,
				fmt.Sprintf("Origin-Host %q does not belong to Origin-Realm %q", originHost, originRealm))
		}
	}
	if enabledCheck(s.CheckPeerIdentity) {
		if peerID := peerIdentity; peerID != "" && originHost != "" &&
			!strings.EqualFold(peerID, string(originHost)) {
			// A partner acting as a proxy legitimately relays foreign Origin-Hosts,
			// but they must still belong to one of its realms.
			if !edge.RealmMatches(string(originRealm), partner.Realms) {
				return e.violate(partner, RulePeerIdentity,
					fmt.Sprintf("Origin-Host %q does not match connected peer %q", originHost, peerID))
			}
		}
	}

	// Cat 2: permission.
	if enabledCheck(s.CheckDestinationRealm) && !partner.AllowTransit {
		destRealm, ok := msg.GetDestinationRealm()
		if !ok || destRealm == "" {
			return e.violate(partner, RuleDestinationRealm, "missing Destination-Realm")
		}
		servedLocally := strings.EqualFold(string(destRealm), localRealm) ||
			(reg != nil && reg.ServesRealm(string(destRealm)))
		if !servedLocally {
			return e.violate(partner, RuleDestinationRealm,
				fmt.Sprintf("Destination-Realm %q is not served locally and partner %q may not transit", destRealm, partner.Name))
		}
	}
	if enabledCheck(s.CheckApplications) && len(partner.Applications) > 0 {
		if !containsUint32(partner.Applications, uint32(msg.ApplicationID)) {
			return e.violate(partner, RuleApplication,
				fmt.Sprintf("application %d not permitted for partner %q", msg.ApplicationID, partner.Name))
		}
	}
	if enabledCheck(s.CheckCommands) && len(partner.Commands) > 0 {
		if !containsUint32(partner.Commands, uint32(msg.CommandCode)) {
			return e.violate(partner, RuleCommand,
				fmt.Sprintf("command %d not permitted for partner %q", msg.CommandCode, partner.Name))
		}
	}

	// Cat 3: content.
	if enabledCheck(s.CheckPLMN) && len(partner.PLMNIDs) > 0 {
		if detail := checkPLMN(msg, partner.PLMNIDs); detail != "" {
			return e.violate(partner, RulePLMN, detail)
		}
	}
	if len(s.DenyAVPs) > 0 {
		if avp := findDeniedAVP(msg.AVPs, s.DenyAVPs); avp != nil {
			return e.violate(partner, RuleAVPDenied,
				fmt.Sprintf("AVP %d (vendor %d) is denied for partner %q", avp.Code, avp.VendorID, partner.Name))
		}
	}
	if len(s.AllowAVPs) > 0 {
		for _, avp := range msg.AVPs {
			if !avpAllowed(avp, s.AllowAVPs) {
				return e.violate(partner, RuleAVPNotAllowed,
					fmt.Sprintf("AVP %d (vendor %d) is not in the allow list of partner %q", avp.Code, avp.VendorID, partner.Name))
			}
		}
	}
	if s.MaxRouteRecords > 0 {
		if n := len(msg.FindAllAVPs(types.AVPCodeRouteRecord, 0)); n > s.MaxRouteRecords {
			return e.violate(partner, RuleRouteRecords,
				fmt.Sprintf("%d Route-Record AVPs exceed the limit of %d", n, s.MaxRouteRecords))
		}
	}

	return nil
}

// violate builds a verdict, resolving the configured action for the rule.
func (e *deaScreening) violate(partner *config.PartnerConfig, rule, detail string) *verdict {
	action := partner.Screening.DefaultAction
	if a, ok := partner.Screening.RuleActions[rule]; ok && a != "" {
		action = a
	}
	if action == "" {
		action = config.ActionReject
	}
	return &verdict{rule: rule, action: action, detail: detail, partner: partner.Name}
}

// enforce applies the verdict's action to the message.
func (e *deaScreening) enforce(p *peer.Peer, msg *message.Message, partner *config.PartnerConfig, v *verdict) ([]routing.Route, error) {
	e.countViolation(v.partner, v.rule)

	if e.auditLog.Load() {
		originHost, _ := msg.GetOriginHost()
		originRealm, _ := msg.GetOriginRealm()
		log.Printf("dea_screening: action=%s rule=%s partner=%s peer=%s origin_host=%s origin_realm=%s app=%d cmd=%d: %s",
			v.action, v.rule, v.partner, p.DiameterID(), originHost, originRealm,
			msg.ApplicationID, msg.CommandCode, v.detail)
	}

	switch v.action {
	case config.ActionLog:
		e.logged.Add(1)
		return nil, nil
	case config.ActionDrop:
		e.dropped.Add(1)
		return nil, routing.ErrConsumed
	default:
		e.rejected.Add(1)
		code := types.ResultCode(partner.Screening.RejectResultCode)
		if code == 0 {
			code = types.ResultAuthorizationRejected
		}
		if err := e.send(p, e.errorAnswer(msg, code, v)); err != nil {
			log.Printf("dea_screening: failed to send rejection answer: %v", err)
		}
		return nil, routing.ErrConsumed
	}
}

// send delivers a message to a peer through the configured sender.
func (e *deaScreening) send(p *peer.Peer, msg *message.Message) error {
	e.mu.RLock()
	fn := e.sendFn
	e.mu.RUnlock()
	if fn != nil {
		return fn(p, msg)
	}
	return p.Send(msg)
}

// errorAnswer builds the rejection answer. It never discloses internal
// topology: Error-Reporting-Host is deliberately omitted and the Error-Message
// only names the rule that fired.
func (e *deaScreening) errorAnswer(req *message.Message, code types.ResultCode, v *verdict) *message.Message {
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
	ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeErrorMessage, 0, "screening: "+v.rule))
	return ans
}

func (e *deaScreening) countViolation(partner, rule string) {
	key := violationKey{partner: partner, rule: rule}
	if c, ok := e.violation.Load(key); ok {
		c.(*atomic.Uint64).Add(1)
		return
	}
	counter := &atomic.Uint64{}
	actual, _ := e.violation.LoadOrStore(key, counter)
	actual.(*atomic.Uint64).Add(1)
}

// --- rule helpers ----------------------------------------------------------

// enabledCheck reports whether an optional check flag is on; checks default to
// enabled when the partner does not configure them.
func enabledCheck(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}

func containsUint32(list []uint32, v uint32) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func avpAllowed(avp *message.AVP, allow []config.AVPSelector) bool {
	for _, sel := range allow {
		if uint32(avp.Code) == sel.Code && uint32(avp.VendorID) == sel.VendorID {
			return true
		}
	}
	return false
}

// findDeniedAVP walks the AVP tree looking for a denied AVP.
func findDeniedAVP(avps []*message.AVP, deny []config.AVPSelector) *message.AVP {
	for _, avp := range avps {
		for _, sel := range deny {
			if uint32(avp.Code) == sel.Code && uint32(avp.VendorID) == sel.VendorID {
				return avp
			}
		}
		if children := groupedChildren(avp); len(children) > 0 {
			if found := findDeniedAVP(children, deny); found != nil {
				return found
			}
		}
	}
	return nil
}

// groupedChildren returns the children of a grouped AVP, decoding it on demand.
func groupedChildren(avp *message.AVP) []*message.AVP {
	if avp.IsGrouped() {
		return avp.Children()
	}
	// Only attempt a decode for AVPs that can plausibly be grouped.
	if len(avp.Data) < 8 {
		return nil
	}
	if err := avp.DecodeGrouped(); err != nil {
		return nil
	}
	return avp.Children()
}

// checkPLMN validates Visited-PLMN-Id and the IMSI in User-Name against the
// partner's PLMN list. It returns an empty string when no violation is found.
func checkPLMN(msg *message.Message, plmns []string) string {
	if avp := msg.FindAVP(types.AVPCode3GPPVisitedPLMNID, vendor3GPP); avp != nil {
		if plmn, ok := decodeVisitedPLMN(avp.Data); ok && !plmnMatches(plmn, plmns) {
			return fmt.Sprintf("Visited-PLMN-Id %s is not owned by the partner", plmn)
		}
	}
	if userName, ok := msg.GetUserName(); ok && isIMSI(userName) {
		if !imsiMatches(userName, plmns) {
			return "IMSI in User-Name does not belong to a partner PLMN"
		}
	}
	return ""
}

// decodeVisitedPLMN decodes the 3-octet TBCD Visited-PLMN-Id of 3GPP TS 29.272
// into an MCC+MNC string.
func decodeVisitedPLMN(data []byte) (string, bool) {
	if len(data) < 3 {
		return "", false
	}
	mcc1 := data[0] & 0x0f
	mcc2 := data[0] >> 4
	mcc3 := data[1] & 0x0f
	mnc3 := data[1] >> 4
	mnc1 := data[2] & 0x0f
	mnc2 := data[2] >> 4

	digits := []byte{mcc1, mcc2, mcc3, mnc1, mnc2}
	for _, d := range digits {
		if d > 9 {
			return "", false
		}
	}

	var sb strings.Builder
	for _, d := range digits {
		sb.WriteByte('0' + d)
	}
	if mnc3 != 0x0f {
		if mnc3 > 9 {
			return "", false
		}
		sb.WriteByte('0' + mnc3)
	}
	return sb.String(), true
}

// plmnMatches compares a decoded PLMN against the configured list. A 5-digit
// (2-digit MNC) PLMN also matches a 6-digit entry with a leading zero MNC.
func plmnMatches(plmn string, plmns []string) bool {
	for _, p := range plmns {
		if p == plmn {
			return true
		}
		if len(p) == 6 && len(plmn) == 5 && p[:3] == plmn[:3] && p[3] == '0' && p[4:] == plmn[3:] {
			return true
		}
		if len(plmn) == 6 && len(p) == 5 && plmn[:3] == p[:3] && plmn[3] == '0' && plmn[4:] == p[3:] {
			return true
		}
	}
	return false
}

// imsiMatches reports whether an IMSI starts with one of the partner PLMNs.
func imsiMatches(imsi string, plmns []string) bool {
	for _, p := range plmns {
		if strings.HasPrefix(imsi, p) {
			return true
		}
	}
	return false
}

// isIMSI reports whether a User-Name looks like a bare IMSI.
func isIMSI(s string) bool {
	if len(s) < 6 || len(s) > 15 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// --- lifecycle -------------------------------------------------------------

// Stop implements the Stoppable interface.
func (e *deaScreening) Stop() error {
	e.enabled.Store(false)
	return nil
}

// Reconfigure implements the Reconfigurable interface.
func (e *deaScreening) Reconfigure(cfg map[string]interface{}) error {
	e.applyConfig(cfg)
	log.Printf("dea_screening: reconfigured (enabled=%v audit_log=%v)", e.enabled.Load(), e.auditLog.Load())
	return nil
}

// HealthCheck implements the HealthCheckable interface.
func (e *deaScreening) HealthCheck() (string, map[string]interface{}) {
	e.mu.RLock()
	reg := e.reg
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
		"enabled":   e.enabled.Load(),
		"audit_log": e.auditLog.Load(),
		"partners":  partners,
		"screened":  e.screened.Load(),
		"rejected":  e.rejected.Load(),
		"dropped":   e.dropped.Load(),
		"logged":    e.logged.Load(),
	}
}

// Metrics implements extension.MetricsProvider.
func (e *deaScreening) Metrics() []extension.Metric {
	metrics := []extension.Metric{
		{Name: "screened_total", Help: "Requests evaluated by ingress screening.", Type: extension.MetricCounter, Value: float64(e.screened.Load())},
		{Name: "rejected_total", Help: "Requests rejected with an error answer.", Type: extension.MetricCounter, Value: float64(e.rejected.Load())},
		{Name: "dropped_total", Help: "Requests silently dropped.", Type: extension.MetricCounter, Value: float64(e.dropped.Load())},
		{Name: "logged_total", Help: "Screening violations recorded without enforcement.", Type: extension.MetricCounter, Value: float64(e.logged.Load())},
	}

	type entry struct {
		key   violationKey
		value uint64
	}
	var entries []entry
	e.violation.Range(func(k, v interface{}) bool {
		entries = append(entries, entry{key: k.(violationKey), value: v.(*atomic.Uint64).Load()})
		return true
	})
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].key.partner != entries[j].key.partner {
			return entries[i].key.partner < entries[j].key.partner
		}
		return entries[i].key.rule < entries[j].key.rule
	})
	for _, en := range entries {
		metrics = append(metrics, extension.Metric{
			Name:   "violations_total",
			Help:   "Screening violations by partner and rule.",
			Type:   extension.MetricCounter,
			Value:  float64(en.value),
			Labels: map[string]string{"partner": en.key.partner, "rule": en.key.rule},
		})
	}
	return metrics
}

func init() {
	extension.Register(&deaScreening{})
}
