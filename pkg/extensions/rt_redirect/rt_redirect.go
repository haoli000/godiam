// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_redirect provides a routing extension that caches Diameter redirect
// indications (Result-Code 3006) from answers and uses them to influence future
// routing decisions.
// This corresponds to rt_redirect from the original freeDiameter.
//
// When a peer responds with DIAMETER_REDIRECT_INDICATION and one or more
// Redirect-Host AVPs, this extension caches the mapping and boosts the score
// of the redirect target for subsequent requests matching the same realm and
// application.
package rt_redirect

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// redirectKey identifies the scope of a redirect cache entry.
type redirectKey struct {
	appID     types.ApplicationID
	destRealm string
}

// redirectEntry stores cached redirect information.
type redirectEntry struct {
	redirectHosts []types.DiamID
	expiresAt     time.Time
}

// defaultCacheTime is the TTL for redirect entries when Redirect-Max-Cache-Time
// is absent from the answer.
const defaultCacheTime = 3600 // seconds

// defaultScoreBoost is the score added to redirect-target candidates.
const defaultScoreBoost = 100

type rtRedirect struct {
	router     *routing.Router
	scoreBoost int
	maxEntries int
	cacheTTL   time.Duration

	mu    sync.RWMutex
	cache map[redirectKey]*redirectEntry

	// Metrics
	cacheHits        atomic.Uint64
	cacheMisses      atomic.Uint64
	redirectsStored  atomic.Uint64
	redirectsApplied atomic.Uint64
}

// Name returns the extension name.
func (e *rtRedirect) Name() string { return "rt_redirect" }

// Init initializes the extension.
func (e *rtRedirect) Init(ctx extension.InitContext, cfg map[string]interface{}) error {
	e.router = ctx.GetRouter()
	e.cache = make(map[redirectKey]*redirectEntry)
	e.scoreBoost = defaultScoreBoost
	e.maxEntries = 10000
	e.cacheTTL = time.Duration(defaultCacheTime) * time.Second

	if v, ok := cfg["score_boost"]; ok {
		switch n := v.(type) {
		case int:
			e.scoreBoost = n
		case float64:
			e.scoreBoost = int(n)
		}
	}
	if v, ok := cfg["max_entries"]; ok {
		switch n := v.(type) {
		case int:
			e.maxEntries = n
		case float64:
			e.maxEntries = int(n)
		}
	}
	if v, ok := cfg["default_cache_time"]; ok {
		if s, ok := v.(string); ok {
			if d, err := time.ParseDuration(s); err == nil {
				e.cacheTTL = d
			}
		}
	}

	log.Printf("Initializing extension: rt_redirect (score_boost=%d, max_entries=%d, ttl=%v)",
		e.scoreBoost, e.maxEntries, e.cacheTTL)

	// Priority 15: intercept redirect answers early, after fifo_stats (4)
	// and rt_deny_by_size (10), but before busypeers (20).
	ctx.GetRouter().RegisterInHandler("rt_redirect_in", 15, e.handleAnswer)

	// Priority 45: boost scores for cached redirect hosts before final selection.
	ctx.GetRouter().RegisterOutHandler("rt_redirect_out", 45, e.handleRequest)

	return nil
}

// handleAnswer intercepts answers with DIAMETER_REDIRECT_INDICATION and caches the redirect.
func (e *rtRedirect) handleAnswer(_ *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if msg.IsRequest() {
		return candidates, nil
	}

	rc, ok := msg.GetResultCode()
	if !ok || rc != types.ResultRedirectIndication {
		return candidates, nil
	}

	// Extract Redirect-Host AVPs
	redirectHostAVPs := msg.FindAllAVPs(types.AVPCodeRedirectHost, 0)
	if len(redirectHostAVPs) == 0 {
		return candidates, nil
	}

	var hosts []types.DiamID
	for _, avp := range redirectHostAVPs {
		host := avp.GetDiameterIdentity()
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 {
		return candidates, nil
	}

	// Determine cache TTL from Redirect-Max-Cache-Time AVP, or use default
	ttl := e.cacheTTL
	if cacheTimeAVP := msg.FindAVP(types.AVPCodeRedirectMaxCacheTime, 0); cacheTimeAVP != nil {
		if secs, err := cacheTimeAVP.GetUnsigned32(); err == nil && secs > 0 {
			ttl = time.Duration(secs) * time.Second
		}
	}

	// Build cache key from the answer's application and realm context
	destRealm := ""
	if dr, ok := msg.GetDestinationRealm(); ok {
		destRealm = string(dr)
	}

	key := redirectKey{
		appID:     msg.ApplicationID,
		destRealm: destRealm,
	}

	entry := &redirectEntry{
		redirectHosts: hosts,
		expiresAt:     time.Now().Add(ttl),
	}

	e.mu.Lock()
	// Evict if at capacity
	if len(e.cache) >= e.maxEntries {
		e.evictExpired()
	}
	e.cache[key] = entry
	e.mu.Unlock()

	e.redirectsStored.Add(1)
	log.Printf("rt_redirect: cached redirect for app=%d realm=%q → %v (ttl=%v)",
		key.appID, key.destRealm, hosts, ttl)

	return candidates, nil
}

// handleRequest boosts scores for cached redirect targets on outgoing requests.
func (e *rtRedirect) handleRequest(msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !msg.IsRequest() {
		return candidates, nil
	}

	destRealm := ""
	if dr, ok := msg.GetDestinationRealm(); ok {
		destRealm = string(dr)
	}

	key := redirectKey{
		appID:     msg.ApplicationID,
		destRealm: destRealm,
	}

	e.mu.RLock()
	entry, ok := e.cache[key]
	e.mu.RUnlock()

	if !ok {
		e.cacheMisses.Add(1)
		return candidates, nil
	}

	// Check expiry
	if time.Now().After(entry.expiresAt) {
		e.mu.Lock()
		delete(e.cache, key)
		e.mu.Unlock()
		e.cacheMisses.Add(1)
		return candidates, nil
	}

	e.cacheHits.Add(1)

	// Build redirect host lookup
	redirectSet := make(map[types.DiamID]struct{})
	for _, h := range entry.redirectHosts {
		redirectSet[h] = struct{}{}
	}

	// Boost matching candidates
	for i := range candidates {
		if _, match := redirectSet[candidates[i].PeerIdentity]; match {
			candidates[i].Score += e.scoreBoost
			candidates[i].Reason = fmt.Sprintf("rt_redirect: cached redirect (app=%d, realm=%s)",
				key.appID, key.destRealm)
			e.redirectsApplied.Add(1)
		}
	}

	return candidates, nil
}

// evictExpired removes expired entries from the cache. Must be called with mu held.
func (e *rtRedirect) evictExpired() {
	now := time.Now()
	for k, v := range e.cache {
		if now.After(v.expiresAt) {
			delete(e.cache, k)
		}
	}
}

// Stop implements the Stoppable interface.
func (e *rtRedirect) Stop() error {
	e.mu.Lock()
	e.cache = make(map[redirectKey]*redirectEntry)
	e.mu.Unlock()
	return nil
}

// Metrics implements extension.MetricsProvider.
func (e *rtRedirect) Metrics() []extension.Metric {
	e.mu.RLock()
	cacheSize := len(e.cache)
	e.mu.RUnlock()

	return []extension.Metric{
		{
			Name:  "cache_size",
			Help:  "Current number of cached redirect entries.",
			Type:  extension.MetricGauge,
			Value: float64(cacheSize),
		},
		{
			Name:  "cache_hits_total",
			Help:  "Total cache hits for redirect lookups.",
			Type:  extension.MetricCounter,
			Value: float64(e.cacheHits.Load()),
		},
		{
			Name:  "cache_misses_total",
			Help:  "Total cache misses for redirect lookups.",
			Type:  extension.MetricCounter,
			Value: float64(e.cacheMisses.Load()),
		},
		{
			Name:  "redirects_stored_total",
			Help:  "Total redirect entries cached from answers.",
			Type:  extension.MetricCounter,
			Value: float64(e.redirectsStored.Load()),
		},
		{
			Name:  "redirects_applied_total",
			Help:  "Total times a cached redirect boosted a candidate score.",
			Type:  extension.MetricCounter,
			Value: float64(e.redirectsApplied.Load()),
		},
	}
}

func init() {
	extension.Register(&rtRedirect{})
}
