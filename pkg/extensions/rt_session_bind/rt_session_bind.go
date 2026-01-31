// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_session_bind provides session-affinity routing for the Diameter Routing Agent.
// It maintains a binding table that maps a configurable key (Session-ID or Subscription-ID)
// to a peer identity, ensuring subsequent requests in the same session or for the same
// subscriber are routed to the same backend peer.
package rt_session_bind

import (
	"fmt"
	"log"
	"time"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

const (
	defaultScoreBoost      = 80
	defaultTTL             = 24 * time.Hour
	defaultMaxBindings     = 100000
	defaultCleanupInterval = 5 * time.Minute
	defaultBindingKey      = "session_id"
)

type rtSessionBind struct {
	store         *bindingStore
	bindingKey    string
	scoreBoost    int
	cleanupTicker *time.Ticker
	stopCh        chan struct{}
}

// Name returns the extension name.
func (e *rtSessionBind) Name() string { return "rt_session_bind" }

// Init initializes the extension.
func (e *rtSessionBind) Init(ctx extension.InitContext, config map[string]interface{}) error {
	bindingKey := defaultBindingKey
	if v, ok := config["binding_key"].(string); ok {
		bindingKey = v
	}
	switch bindingKey {
	case "session_id", "subscription_id":
	default:
		return fmt.Errorf("rt_session_bind: unsupported binding_key %q (use session_id or subscription_id)", bindingKey)
	}
	e.bindingKey = bindingKey

	e.scoreBoost = defaultScoreBoost
	if v, ok := config["score_boost"]; ok {
		switch n := v.(type) {
		case int:
			e.scoreBoost = n
		case float64:
			e.scoreBoost = int(n)
		}
	}

	ttl := defaultTTL
	if v, ok := config["ttl"].(string); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("rt_session_bind: invalid ttl %q: %w", v, err)
		}
		ttl = d
	}

	maxBindings := defaultMaxBindings
	if v, ok := config["max_bindings"]; ok {
		switch n := v.(type) {
		case int:
			maxBindings = n
		case float64:
			maxBindings = int(n)
		}
	}

	cleanupInterval := defaultCleanupInterval
	if v, ok := config["cleanup_interval"].(string); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("rt_session_bind: invalid cleanup_interval %q: %w", v, err)
		}
		cleanupInterval = d
	}

	e.store = newBindingStore(ttl, maxBindings)
	e.stopCh = make(chan struct{})

	// Register OutHandler at priority 80 (after rt_rewrite at 40, before final selection)
	ctx.GetRouter().RegisterOutHandler("rt_session_bind", 80, e.handleOutgoing)

	// Register PostRouteHandler to learn which peer was selected
	ctx.GetRouter().RegisterPostRouteHandler("rt_session_bind", e.handlePostRoute)

	// Start background cleanup
	e.cleanupTicker = time.NewTicker(cleanupInterval)
	go e.cleanupLoop()

	log.Printf("rt_session_bind: initialized (key=%s, boost=%d, ttl=%s, max=%d)",
		e.bindingKey, e.scoreBoost, ttl, maxBindings)
	return nil
}

// Stop shuts down the extension.
func (e *rtSessionBind) Stop() error {
	close(e.stopCh)
	e.cleanupTicker.Stop()
	return nil
}

// Metrics implements extension.MetricsProvider.
func (e *rtSessionBind) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "bindings_total",
			Help:  "Current number of active session bindings.",
			Type:  extension.MetricGauge,
			Value: float64(e.store.Len()),
		},
		{
			Name:  "lookups_total",
			Help:  "Total binding store lookups.",
			Type:  extension.MetricCounter,
			Value: float64(e.store.lookups.Load()),
		},
		{
			Name:  "hits_total",
			Help:  "Total binding store lookups that found an active binding.",
			Type:  extension.MetricCounter,
			Value: float64(e.store.hits.Load()),
		},
		{
			Name:  "misses_total",
			Help:  "Total binding store lookups that found no binding.",
			Type:  extension.MetricCounter,
			Value: float64(e.store.misses.Load()),
		},
		{
			Name:  "evictions_total",
			Help:  "Total bindings evicted due to store capacity.",
			Type:  extension.MetricCounter,
			Value: float64(e.store.evictions.Load()),
		},
		{
			Name:  "cleanups_total",
			Help:  "Total expired bindings removed by background cleanup.",
			Type:  extension.MetricCounter,
			Value: float64(e.store.cleanups.Load()),
		},
	}
}

func (e *rtSessionBind) cleanupLoop() {
	for {
		select {
		case <-e.stopCh:
			return
		case <-e.cleanupTicker.C:
			removed := e.store.Cleanup()
			if removed > 0 {
				log.Printf("rt_session_bind: cleaned up %d expired bindings (%d remaining)", removed, e.store.Len())
			}
		}
	}
}

// extractKey extracts the binding key from a Diameter message.
func (e *rtSessionBind) extractKey(msg *message.Message) (string, bool) {
	switch e.bindingKey {
	case "session_id":
		return msg.GetSessionID()
	case "subscription_id":
		subIDAVP := msg.FindAVP(types.AVPCodeSubscriptionID, 0)
		if subIDAVP == nil {
			return "", false
		}
		if err := subIDAVP.DecodeGrouped(); err != nil {
			return "", false
		}
		dataAVP := subIDAVP.FindChild(types.AVPCodeSubscriptionIDData, 0)
		if dataAVP == nil {
			return "", false
		}
		v := dataAVP.GetUTF8String()
		if v == "" {
			return "", false
		}
		return v, true
	}
	return "", false
}

// handleOutgoing is the OutHandler that boosts the score of the bound peer.
func (e *rtSessionBind) handleOutgoing(msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !msg.IsRequest() {
		return candidates, nil
	}
	key, ok := e.extractKey(msg)
	if !ok {
		return candidates, nil
	}
	boundPeer, found := e.store.Get(key)
	if !found {
		return candidates, nil
	}
	for i := range candidates {
		if candidates[i].PeerIdentity == boundPeer {
			candidates[i].Score += e.scoreBoost
			candidates[i].Reason = "session-bind"
			e.store.Touch(key)
			return candidates, nil
		}
	}
	// Bound peer not in candidates (down/removed). Let normal routing proceed.
	// PostRouteHandler will record a new binding for the newly selected peer.
	return candidates, nil
}

// handlePostRoute records or updates the binding after peer selection.
func (e *rtSessionBind) handlePostRoute(msg *message.Message, selectedPeer types.DiamID) {
	if !msg.IsRequest() {
		return
	}
	key, ok := e.extractKey(msg)
	if !ok {
		return
	}
	e.store.Set(key, selectedPeer)
}

func init() {
	extension.Register(&rtSessionBind{})
}
