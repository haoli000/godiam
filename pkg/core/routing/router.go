// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package routing provides Diameter message routing.
// This corresponds to routing_dispatch.c from the original freeDiameter.
package routing

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// RouterStats holds atomic counters for routing activity.
type RouterStats struct {
	RequestsRouted     atomic.Uint64 // Total requests entering RouteIn
	RequestsRelayed    atomic.Uint64 // Requests relayed to another peer
	RequestsDispatched atomic.Uint64 // Requests dispatched to local handler
	RoutingErrors      atomic.Uint64 // Routing failures (error answers sent)
	LoopDetections     atomic.Uint64 // Loop detection hits
	AnswersRelayed     atomic.Uint64 // Answers forwarded back via relay
}

// Route represents a routing decision for a message.
type Route struct {
	// PeerIdentity is the peer to route the message to
	PeerIdentity types.DiamID
	// Score is the routing score (higher = better)
	Score int
	// Reason describes why this route was selected
	Reason string
}

// InHandler is a function that processes incoming routing decisions.
// It receives the peer, the message and a list of candidate routes.
// If it handles the message (e.g. by sending an answer), it should return an error
// to stop further processing.
type InHandler func(p *peer.Peer, msg *message.Message, candidates []Route) ([]Route, error)

// OutHandler is a function that processes outgoing routing decisions.
type OutHandler func(msg *message.Message, candidates []Route) ([]Route, error)

// PostRouteHandler is called after a peer has been selected for an outgoing message.
// It receives the routed message and the identity of the selected peer.
type PostRouteHandler func(msg *message.Message, selectedPeer types.DiamID)

// ErrConsumed is returned by a routing handler to indicate it has handled the message.
var ErrConsumed = fmt.Errorf("message consumed by routing handler")

// DispatchHandler is called to dispatch a message to an application.
type DispatchHandler func(p *peer.Peer, msg *message.Message) error

// DispatchError conveys a Diameter result code for dispatch failures.
type DispatchError struct {
	Code types.ResultCode
	Msg  string
}

func (e *DispatchError) Error() string {
	return e.Msg
}

// RoutingError conveys a Diameter routing failure along with an error answer.
//
//nolint:revive // Error() method prevents renaming
type RoutingError struct {
	Code   types.ResultCode
	Answer *message.Message
	Msg    string
	Sent   bool
}

func (e *RoutingError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("routing error %d", e.Code)
}

// relayPeerIdentity returns the identity the router knows a peer by, which is
// the key side of a relay transaction. The negotiated CER identity is
// authoritative once the peer is open; before that the configured identity is
// the only name the routing tables could have used.
func relayPeerIdentity(p *peer.Peer) string {
	if id := p.DiameterID(); id != "" {
		return string(id)
	}
	return string(p.Config().DiameterIdentity)
}

// relayKey identifies a relay transaction.
//
// A hop-by-hop ID is only unique *per connection* (RFC 6733 3), and every peer
// generates its own sequence starting from zero, so two outbound peers hand out
// the same IDs. Keying the transaction map on the ID alone therefore made
// concurrent relays to different peers collide: the second Store evicted the
// first, one answer was dropped with "no routes available", and the other was
// returned to the client carrying the wrong original hop-by-hop ID. Pairing the
// ID with the peer it was minted for restores the uniqueness the map assumes.
type relayKey struct {
	OutboundPeer string
	HBHID        types.HopByHopID
}

// relayTxn stores relay transaction info for routing answers back.
type relayTxn struct {
	OriginalPeer  string           // Original inbound peer identity
	OriginalHBHID types.HopByHopID // Original hop-by-hop ID from inbound request
	OutboundPeer  string           // Outbound peer identity the request was forwarded to
	CreatedAt     time.Time        // When this transaction was created
}

// Router manages message routing and dispatch.
type Router struct {
	mu sync.RWMutex

	// Routing tables
	realmRoutes   map[string][]string // realm -> list of peer identities
	hostRoutes    map[string]string   // host -> peer identity
	appRoutes     map[types.ApplicationID][]string
	localIdentity string
	localRealm    string
	relayEnabled  bool

	// Default realm for messages without Destination-Realm
	defaultRealm string

	// Routing handlers (called in priority order)
	outHandlers []outHandlerEntry // For outgoing messages
	inHandlers  []inHandlerEntry  // For incoming messages

	// Post-route handlers (called after peer selection)
	postRouteHandlers []postRouteHandlerEntry

	// Dispatch handlers by application ID
	dispatchHandlers map[types.ApplicationID]DispatchHandler
	dispatchOwner    map[types.ApplicationID]string // appID -> owner extension name

	// Peer lookup function (provided by server)
	peerLookup func(identity types.DiamID) (interface{}, bool)
	// Peer enumerator to list connected peers (provided by server)
	peerEnumerate     func() []*peer.Peer
	dynamicRealmPeers bool

	// Transaction map: new Hop-by-Hop ID -> relay transaction info
	// Used to relay answers back to the originating peer with correct HBHID
	// Using sync.Map for lock-free concurrent access (optimal for distinct keys per goroutine)
	prevHopByHop sync.Map // relayKey -> relayTxn

	// nextHBH is a counter used to generate unique Hop-by-Hop IDs for relay
	// via non-*peer.Peer senders (the interface-based send path).
	nextHBH atomic.Uint32

	// Stats tracks routing counters (lock-free atomic operations).
	stats RouterStats

	// Relay transaction expiry sweep
	sweepStop chan struct{} // Signal to stop the sweep goroutine
	sweepDone chan struct{} // Closed when sweep goroutine exits
	sweepTTL  time.Duration // Max age for relay transactions (0 = no expiry)
}

// RouteOutWithExclusion determines routing while excluding specified peers.
func (r *Router) RouteOutWithExclusion(msg *message.Message, exclude map[types.DiamID]struct{}) (types.DiamID, error) {
	// Build initial candidate list
	candidates := r.buildCandidates(msg)
	if len(candidates) == 0 {
		return "", fmt.Errorf("no routes available")
	}

	// Apply routing handlers
	for _, entry := range r.outHandlers {
		var err error
		candidates, err = entry.handler(msg, candidates)
		if err != nil {
			return "", err
		}
		if len(candidates) == 0 {
			return "", fmt.Errorf("routing handler %s eliminated all candidates", entry.name)
		}
	}

	// Sort by score (descending)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// Iterate by score tiers, pick first non-excluded in the top tier; if top tier is fully excluded, move to next tier.
	i := 0
	for i < len(candidates) {
		topScore := candidates[i].Score
		start := i
		for i < len(candidates) && candidates[i].Score == topScore {
			i++
		}
		tier := candidates[start:i]
		var tierChoices []Route
		for _, c := range tier {
			if exclude != nil {
				if _, skip := exclude[c.PeerIdentity]; skip {
					continue
				}
			}
			tierChoices = append(tierChoices, c)
		}
		if len(tierChoices) == 0 {
			continue
		}
		if len(tierChoices) == 1 {
			r.firePostRouteHandlers(msg, tierChoices[0].PeerIdentity)
			return tierChoices[0].PeerIdentity, nil
		}
		idx := rand.Intn(len(tierChoices)) //nolint:gosec // G404: crypto randomness not needed for load balancing
		r.firePostRouteHandlers(msg, tierChoices[idx].PeerIdentity)
		return tierChoices[idx].PeerIdentity, nil
	}

	return "", fmt.Errorf("no routes available after exclusions")
}

func containsRouteRecord(msg *message.Message, identity string) bool {
	if identity == "" {
		return false
	}
	for _, avp := range msg.FindAllAVPs(types.AVPCodeRouteRecord, 0) {
		if avp.GetDiameterIdentity() == types.DiamID(identity) {
			return true
		}
	}
	return false
}

func removeLocalProxyInfo(msg *message.Message, identity string) {
	if identity == "" {
		return
	}
	for i := len(msg.AVPs) - 1; i >= 0; i-- {
		avp := msg.AVPs[i]
		if avp.Code != types.AVPCodeProxyInfo {
			continue
		}
		host := avp.FindChild(types.AVPCodeProxyHost, 0)
		if host != nil && host.GetDiameterIdentity() == types.DiamID(identity) {
			msg.AVPs = append(msg.AVPs[:i], msg.AVPs[i+1:]...)
			return
		}
	}
}

func addProxyInfo(msg *message.Message, identity string) {
	if identity == "" {
		return
	}
	state := fmt.Sprintf("%s-%d", identity, time.Now().UnixNano())
	pi := message.NewGroupedAVP(types.AVPCodeProxyInfo, types.AVPFlagMandatory, 0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, types.DiamID(identity)),
		message.NewOctetStringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, []byte(state)),
	)
	msg.AddAVP(pi)
}

func (r *Router) errorAnswer(p *peer.Peer, req *message.Message, code types.ResultCode, reason string) error {
	r.stats.RoutingErrors.Add(1)
	ans := message.NewAnswer(req)
	ans.SetError(true)
	ans.SetResultCode(code)

	if r.localIdentity != "" {
		ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(r.localIdentity)))
	}
	if r.localRealm != "" {
		ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID(r.localRealm)))
	}
	if r.localIdentity != "" {
		ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeErrorReportingHost, types.AVPFlagMandatory, types.DiamID(r.localIdentity)))
	}

	sent := false
	if p != nil {
		if err := p.Send(ans); err == nil {
			sent = true
		}
	}

	return &RoutingError{Code: code, Answer: ans, Msg: reason, Sent: sent}
}

type inHandlerEntry struct {
	priority int
	handler  InHandler
	name     string
	owner    string // extension that registered this handler
}

type outHandlerEntry struct {
	priority int
	handler  OutHandler
	name     string
	owner    string // extension that registered this handler
}

type postRouteHandlerEntry struct {
	name    string
	owner   string
	handler PostRouteHandler
}

// NewRouter creates a new router.
func NewRouter() *Router {
	// Note: As of Go 1.20, rand is auto-seeded, no need to call rand.Seed
	return &Router{
		realmRoutes:      make(map[string][]string),
		hostRoutes:       make(map[string]string),
		appRoutes:        make(map[types.ApplicationID][]string),
		dispatchHandlers: make(map[types.ApplicationID]DispatchHandler),
		dispatchOwner:    make(map[types.ApplicationID]string),
		// prevHopByHop uses sync.Map, no initialization needed
	}
}

// SetDefaultRealm sets the default realm for routing.
func (r *Router) SetDefaultRealm(realm string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultRealm = realm
}

// SetLocalIdentity sets the local Diameter identity.
func (r *Router) SetLocalIdentity(identity, realm string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.localIdentity = identity
	r.localRealm = realm
}

// AddRealmRoute adds a route for a realm.
func (r *Router) AddRealmRoute(realm string, peers ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.realmRoutes[realm] = append(r.realmRoutes[realm], peers...)
}

// AddHostRoute adds a direct route to a specific host.
func (r *Router) AddHostRoute(host, peer string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hostRoutes[host] = peer
}

// AddAppRoute adds a route for a specific application.
func (r *Router) AddAppRoute(appID types.ApplicationID, peers ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appRoutes[appID] = append(r.appRoutes[appID], peers...)
}

// SetRelay enables or disables relaying (forwarding) of messages.
func (r *Router) SetRelay(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.relayEnabled = enabled
}

// SetPeerLookup sets the function to lookup peers.
func (r *Router) SetPeerLookup(fn func(identity types.DiamID) (interface{}, bool)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peerLookup = fn
}

// LookupPeer calls the peer lookup function.
func (r *Router) LookupPeer(identity types.DiamID) (interface{}, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.peerLookup == nil {
		return nil, false
	}
	return r.peerLookup(identity)
}

// SetPeerEnumerator sets the function to enumerate connected peers.
func (r *Router) SetPeerEnumerator(fn func() []*peer.Peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peerEnumerate = fn
}

// SetDynamicRealmPeers enables or disables dynamic realm-based peer discovery.
func (r *Router) SetDynamicRealmPeers(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dynamicRealmPeers = enabled
}

// RegisterOutHandler registers a handler for outgoing message routing.
// The handler name is also used as the owner for deregistration purposes.
func (r *Router) RegisterOutHandler(name string, priority int, handler OutHandler) {
	r.RegisterOutHandlerWithOwner(name, name, priority, handler)
}

// RegisterOutHandlerWithOwner registers a handler with an explicit owner.
func (r *Router) RegisterOutHandlerWithOwner(name, owner string, priority int, handler OutHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outHandlers = append(r.outHandlers, outHandlerEntry{
		priority: priority,
		handler:  handler,
		name:     name,
		owner:    owner,
	})
	sort.Slice(r.outHandlers, func(i, j int) bool {
		return r.outHandlers[i].priority < r.outHandlers[j].priority
	})
}

// RegisterInHandler registers a handler for incoming message routing.
// The handler name is also used as the owner for deregistration purposes.
func (r *Router) RegisterInHandler(name string, priority int, handler InHandler) {
	r.RegisterInHandlerWithOwner(name, name, priority, handler)
}

// RegisterInHandlerWithOwner registers a handler with an explicit owner.
func (r *Router) RegisterInHandlerWithOwner(name, owner string, priority int, handler InHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inHandlers = append(r.inHandlers, inHandlerEntry{
		priority: priority,
		handler:  handler,
		name:     name,
		owner:    owner,
	})
	sort.Slice(r.inHandlers, func(i, j int) bool {
		return r.inHandlers[i].priority < r.inHandlers[j].priority
	})
}

// RegisterPostRouteHandler registers a handler called after peer selection.
// The handler name is also used as the owner for deregistration purposes.
func (r *Router) RegisterPostRouteHandler(name string, handler PostRouteHandler) {
	r.RegisterPostRouteHandlerWithOwner(name, name, handler)
}

// RegisterPostRouteHandlerWithOwner registers a post-route handler with an explicit owner.
func (r *Router) RegisterPostRouteHandlerWithOwner(name, owner string, handler PostRouteHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.postRouteHandlers = append(r.postRouteHandlers, postRouteHandlerEntry{
		name:    name,
		owner:   owner,
		handler: handler,
	})
}

// firePostRouteHandlers notifies all registered post-route handlers of a peer selection.
func (r *Router) firePostRouteHandlers(msg *message.Message, selected types.DiamID) {
	for _, entry := range r.postRouteHandlers {
		entry.handler(msg, selected)
	}
}

// RegisterDispatchHandler registers a dispatch handler for an application.
// The owner defaults to empty string; use RegisterDispatchHandlerWithOwner for explicit ownership.
func (r *Router) RegisterDispatchHandler(appID types.ApplicationID, handler DispatchHandler) {
	r.RegisterDispatchHandlerWithOwner(appID, "", handler)
}

// RegisterDispatchHandlerWithOwner registers a dispatch handler with an explicit owner.
func (r *Router) RegisterDispatchHandlerWithOwner(appID types.ApplicationID, owner string, handler DispatchHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dispatchHandlers[appID] = handler
	r.dispatchOwner[appID] = owner
}

// RouteOut determines the routing for an outgoing message.
// Returns the best peer identity to send the message to.
func (r *Router) RouteOut(msg *message.Message) (types.DiamID, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.RouteOutWithExclusion(msg, nil)
}

// buildCandidates builds the initial list of routing candidates.
func (r *Router) buildCandidates(msg *message.Message) []Route {
	var candidates []Route
	added := make(map[string]struct{})

	// Check for direct host route first
	if destHost, ok := msg.GetDestinationHost(); ok && destHost != "" {
		if peer, ok := r.hostRoutes[string(destHost)]; ok {
			candidates = append(candidates, Route{
				PeerIdentity: types.DiamID(peer),
				Score:        1000, // Highest priority for direct routing
				Reason:       "direct host route",
			})
			added[peer] = struct{}{}
			return candidates
		}
		// If destination host specified but no direct route, still try
		candidates = append(candidates, Route{
			PeerIdentity: destHost,
			Score:        500,
			Reason:       "destination host",
		})
		added[string(destHost)] = struct{}{}
	}

	// Check realm-based routing
	destRealm := ""
	if dr, ok := msg.GetDestinationRealm(); ok {
		destRealm = string(dr)
	} else if r.defaultRealm != "" {
		destRealm = r.defaultRealm
	}

	if destRealm != "" {
		if peers, ok := r.realmRoutes[destRealm]; ok {
			for _, peer := range peers {
				candidates = append(candidates, Route{
					PeerIdentity: types.DiamID(peer),
					Score:        100, // Equal score to allow randomized selection among realm peers
					Reason:       fmt.Sprintf("realm route for %s", destRealm),
				})
				added[peer] = struct{}{}
			}
		}
		// Also include currently connected peers in the target realm (dynamic)
		if r.dynamicRealmPeers && r.peerEnumerate != nil {
			for _, p := range r.peerEnumerate() {
				if string(p.Realm()) == destRealm {
					id := string(p.DiameterID())
					if _, ok := added[id]; !ok {
						candidates = append(candidates, Route{
							PeerIdentity: p.DiameterID(),
							Score:        90, // Slightly lower than static realm routes
							Reason:       fmt.Sprintf("connected peer in realm %s", destRealm),
						})
						added[id] = struct{}{}
					}
				}
			}
		}
	}

	// Check app-based routing
	if peers, ok := r.appRoutes[msg.ApplicationID]; ok {
		for i, peer := range peers {
			candidates = append(candidates, Route{
				PeerIdentity: types.DiamID(peer),
				Score:        200 - i,
				Reason:       fmt.Sprintf("application route for %d", msg.ApplicationID),
			})
			added[peer] = struct{}{}
		}
	}

	return candidates
}

// RouteIn processes an incoming message for local dispatch or forwarding.
func (r *Router) RouteIn(p *peer.Peer, msg *message.Message) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if msg.IsRequest() {
		r.stats.RequestsRouted.Add(1)
	}

	// Apply incoming routing handlers
	candidates := []Route{}
	for _, entry := range r.inHandlers {
		var err error
		candidates, err = entry.handler(p, msg, candidates)
		if err != nil {
			if errors.Is(err, ErrConsumed) {
				return nil // Handled successfully
			}
			return err
		}
	}

	// If this is an answer and we are a relay, try to forward back to previous hop
	if !msg.IsRequest() && r.relayEnabled {
		// Use sync.Map's LoadAndDelete for atomic load+delete (lock-free)
		// An answer's hop-by-hop ID is only meaningful on the connection it
		// arrived on, so the peer is part of the key.
		var txn relayTxn
		var ok bool
		if p != nil {
			key := relayKey{OutboundPeer: relayPeerIdentity(p), HBHID: msg.HopByHopID}
			if val, loaded := r.prevHopByHop.LoadAndDelete(key); loaded {
				txn, ok = val.(relayTxn)
			}
		}

		if ok {
			removeLocalProxyInfo(msg, r.localIdentity)

			// Restore original HopByHopID before forwarding back
			msg.HopByHopID = txn.OriginalHBHID
			// Forward to previous hop
			if r.peerLookup != nil {
				if peerObj, ok := r.peerLookup(types.DiamID(txn.OriginalPeer)); ok {
					if pp, ok := peerObj.(interface{ Send(*message.Message) error }); ok {
						r.stats.AnswersRelayed.Add(1)
						return pp.Send(msg)
					}
				}
			}
		}
	}

	// Determine if the message is for us
	// Behavior depends on relay mode:
	// - If relay is enabled (DRA), only treat as local when Destination-Host equals local identity.
	// - If relay is disabled (application endpoint), treat as local when:
	//     DestHost equals local OR (DestHost absent and DestRealm equals local realm)
	isLocal := false
	if r.relayEnabled {
		if destHost, ok := msg.GetDestinationHost(); ok && destHost != "" {
			if string(destHost) == r.localIdentity {
				isLocal = true
			}
		}
	} else {
		isLocal = true
		if destHost, ok := msg.GetDestinationHost(); ok && destHost != "" {
			if string(destHost) != r.localIdentity {
				isLocal = false
			}
		} else if destRealm, ok := msg.GetDestinationRealm(); ok && destRealm != "" {
			if string(destRealm) != r.localRealm {
				isLocal = false
			}
		}
	}

	if isLocal {
		return r.dispatch(p, msg)
	}

	if msg.IsRequest() && r.relayEnabled && containsRouteRecord(msg, r.localIdentity) {
		r.stats.LoopDetections.Add(1)
		return r.errorAnswer(p, msg, types.ResultLoopDetected, "loop detected: local identity already in Route-Record")
	}

	// If not local, check if relaying is enabled
	if !r.relayEnabled {
		return r.errorAnswer(p, msg, types.ResultUnableToDeliver, fmt.Sprintf("message for remote host but relaying is disabled: cmd=%d", msg.CommandCode))
	}

	exclude := make(map[types.DiamID]struct{})

	// Add Route-Record and Proxy-Info once for this relay attempt
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, types.DiamID(r.localIdentity)))
	if msg.IsRequest() && msg.IsProxiable() {
		addProxyInfo(msg, r.localIdentity)
	}

	// A peer that refuses a message is excluded and the loop retries, so when
	// every candidate is gone the surviving error says only that no route is
	// left. That is misleading when the route existed and the peer simply
	// applied backpressure: reporting the last send failure distinguishes an
	// overloaded next hop from a genuine routing gap.
	var lastSendErr error

	for {
		nextHop, err := r.RouteOutWithExclusion(msg, exclude)
		if err != nil {
			if lastSendErr != nil {
				return r.errorAnswer(p, msg, types.ResultUnableToDeliver,
					fmt.Sprintf("relay failed: %v (last next hop refused the message: %v)", err, lastSendErr))
			}
			return r.errorAnswer(p, msg, types.ResultUnableToDeliver, fmt.Sprintf("relay failed: %v", err))
		}

		if r.peerLookup == nil {
			return r.errorAnswer(p, msg, types.ResultUnableToDeliver, fmt.Sprintf("relay: no peer object found for next hop %s", nextHop))
		}

		peerObj, ok := r.peerLookup(nextHop)
		if !ok {
			exclude[nextHop] = struct{}{}
			continue
		}

		// Try full peer for HBH handling
		if outPeer, ok := peerObj.(*peer.Peer); ok {
			originalHBHID := msg.HopByHopID
			newHBHID := outPeer.NextHopByHopID()
			msg.HopByHopID = newHBHID

			// Key on the identity the answer will arrive under, which is the
			// peer's own, not the routing-table name that selected it.
			outID := relayPeerIdentity(outPeer)
			if p != nil {
				r.prevHopByHop.Store(relayKey{OutboundPeer: outID, HBHID: newHBHID}, relayTxn{
					OriginalPeer:  string(p.DiameterID()),
					OriginalHBHID: originalHBHID,
					OutboundPeer:  string(nextHop),
					CreatedAt:     time.Now(),
				})
			}

			if err := outPeer.Send(msg); err != nil {
				lastSendErr = err
				r.prevHopByHop.Delete(relayKey{OutboundPeer: outID, HBHID: newHBHID})
				msg.HopByHopID = originalHBHID
				exclude[nextHop] = struct{}{}
				continue
			}
			r.stats.RequestsRelayed.Add(1)
			return nil
		}

		if sender, ok := peerObj.(interface{ Send(*message.Message) error }); ok {
			originalHBHID := msg.HopByHopID
			newHBHID := types.HopByHopID(r.nextHBH.Add(1))
			msg.HopByHopID = newHBHID

			if p != nil {
				r.prevHopByHop.Store(relayKey{OutboundPeer: string(nextHop), HBHID: newHBHID}, relayTxn{
					OriginalPeer:  string(p.DiameterID()),
					OriginalHBHID: originalHBHID,
					OutboundPeer:  string(nextHop),
					CreatedAt:     time.Now(),
				})
			}

			if err := sender.Send(msg); err != nil {
				lastSendErr = err
				r.prevHopByHop.Delete(relayKey{OutboundPeer: string(nextHop), HBHID: newHBHID})
				msg.HopByHopID = originalHBHID
				exclude[nextHop] = struct{}{}
				continue
			}
			r.stats.RequestsRelayed.Add(1)
			return nil
		}

		exclude[nextHop] = struct{}{}
	}
}

// dispatch sends the message to the registered application handler.
func (r *Router) dispatch(p *peer.Peer, msg *message.Message) error {
	r.stats.RequestsDispatched.Add(1)
	handler, ok := r.dispatchHandlers[msg.ApplicationID]
	if !ok {
		// Try common messages (application ID 0)
		handler, ok = r.dispatchHandlers[types.AppIDCommon]
	}
	if !ok {
		return &DispatchError{Code: types.ResultApplicationUnsupported, Msg: fmt.Sprintf("no handler for application %d", msg.ApplicationID)}
	}
	return handler(p, msg)
}

// Dispatch dispatches a message to the registered handler.
func (r *Router) Dispatch(p *peer.Peer, msg *message.Message) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dispatch(p, msg)
}

// RoutingTable returns a copy of the current routing table for inspection.
func (r *Router) RoutingTable() TableInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info := TableInfo{
		DefaultRealm: r.defaultRealm,
		RealmRoutes:  make(map[string][]string),
		HostRoutes:   make(map[string]string),
	}

	for realm, peers := range r.realmRoutes {
		info.RealmRoutes[realm] = append([]string{}, peers...)
	}
	for host, peer := range r.hostRoutes {
		info.HostRoutes[host] = peer
	}

	return info
}

// TableInfo contains routing table information.
type TableInfo struct {
	DefaultRealm string
	RealmRoutes  map[string][]string
	HostRoutes   map[string]string
}

// DeregisterInHandler removes an incoming handler by name.
func (r *Router) DeregisterInHandler(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, h := range r.inHandlers {
		if h.name == name {
			r.inHandlers = append(r.inHandlers[:i], r.inHandlers[i+1:]...)
			return true
		}
	}
	return false
}

// DeregisterOutHandler removes an outgoing handler by name.
func (r *Router) DeregisterOutHandler(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, h := range r.outHandlers {
		if h.name == name {
			r.outHandlers = append(r.outHandlers[:i], r.outHandlers[i+1:]...)
			return true
		}
	}
	return false
}

// DeregisterDispatchHandler removes a dispatch handler by application ID.
func (r *Router) DeregisterDispatchHandler(appID types.ApplicationID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.dispatchHandlers[appID]; ok {
		delete(r.dispatchHandlers, appID)
		delete(r.dispatchOwner, appID)
		return true
	}
	return false
}

// DeregisterAllByOwner removes all handlers (in, out, dispatch) registered by the given owner.
func (r *Router) DeregisterAllByOwner(owner string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove in-handlers
	filtered := r.inHandlers[:0]
	for _, h := range r.inHandlers {
		if h.owner != owner {
			filtered = append(filtered, h)
		}
	}
	r.inHandlers = filtered

	// Remove out-handlers
	filteredOut := r.outHandlers[:0]
	for _, h := range r.outHandlers {
		if h.owner != owner {
			filteredOut = append(filteredOut, h)
		}
	}
	r.outHandlers = filteredOut

	// Remove post-route handlers
	filteredPost := r.postRouteHandlers[:0]
	for _, h := range r.postRouteHandlers {
		if h.owner != owner {
			filteredPost = append(filteredPost, h)
		}
	}
	r.postRouteHandlers = filteredPost

	// Remove dispatch handlers
	for appID, o := range r.dispatchOwner {
		if o == owner {
			delete(r.dispatchHandlers, appID)
			delete(r.dispatchOwner, appID)
		}
	}
}

// Stats returns a snapshot of routing counters.
func (r *Router) Stats() (routed, relayed, dispatched, errors, loops, answersRelayed uint64) {
	return r.stats.RequestsRouted.Load(),
		r.stats.RequestsRelayed.Load(),
		r.stats.RequestsDispatched.Load(),
		r.stats.RoutingErrors.Load(),
		r.stats.LoopDetections.Load(),
		r.stats.AnswersRelayed.Load()
}

// RelayOrigin returns the identity of the peer that originated the relayed
// request that the given outbound peer answered with the given hop-by-hop ID,
// if that transaction is still in flight. The entry is left in place, so this
// is safe to call from an incoming handler before RouteIn consumes it.
//
// The hop-by-hop ID is chosen by this node, so an answer can only match a
// transaction if it genuinely belongs to a request this node relayed. Handlers
// use it to discover where an answer is headed, which the answer itself does
// not say. The peer is required because the ID is unique only per connection.
func (r *Router) RelayOrigin(outboundPeer string, hbhID types.HopByHopID) (string, bool) {
	val, loaded := r.prevHopByHop.Load(relayKey{OutboundPeer: outboundPeer, HBHID: hbhID})
	if !loaded {
		return "", false
	}
	txn, ok := val.(relayTxn)
	if !ok {
		return "", false
	}
	return txn.OriginalPeer, true
}

// FailoverPeer removes all relay transactions involving the given peer identity
// (either as inbound originator or outbound target) and returns the number of
// removed entries. This should be called when a peer disconnects to prevent
// unbounded growth of the prevHopByHop map.
func (r *Router) FailoverPeer(identity string) int {
	removed := 0
	r.prevHopByHop.Range(func(key, value any) bool {
		txn, ok := value.(relayTxn)
		if !ok {
			return true
		}
		if txn.OriginalPeer == identity || txn.OutboundPeer == identity {
			r.prevHopByHop.Delete(key)
			removed++
		}
		return true
	})
	return removed
}

// RelayTxnCount returns the number of in-flight relay transactions.
func (r *Router) RelayTxnCount() int {
	count := 0
	r.prevHopByHop.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// StartRelaySweep starts a background goroutine that periodically removes
// relay transactions older than ttl. This prevents unbounded growth of the
// prevHopByHop map when answers never arrive (e.g., remote peer dropped the
// request). The sweep runs every interval. Call StopRelaySweep to stop it.
func (r *Router) StartRelaySweep(interval, ttl time.Duration) {
	r.sweepTTL = ttl
	r.sweepStop = make(chan struct{})
	r.sweepDone = make(chan struct{})

	go func() {
		defer close(r.sweepDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.sweepExpired()
			case <-r.sweepStop:
				return
			}
		}
	}()
}

// StopRelaySweep stops the background sweep goroutine and waits for it to exit.
func (r *Router) StopRelaySweep() {
	if r.sweepStop != nil {
		close(r.sweepStop)
		<-r.sweepDone
	}
}

// sweepExpired removes relay transactions that have exceeded the configured TTL.
// Returns the number of expired entries removed.
func (r *Router) sweepExpired() int {
	if r.sweepTTL <= 0 {
		return 0
	}
	cutoff := time.Now().Add(-r.sweepTTL)
	removed := 0
	r.prevHopByHop.Range(func(key, value any) bool {
		txn, ok := value.(relayTxn)
		if !ok {
			return true
		}
		if txn.CreatedAt.Before(cutoff) {
			r.prevHopByHop.Delete(key)
			removed++
		}
		return true
	})
	return removed
}
