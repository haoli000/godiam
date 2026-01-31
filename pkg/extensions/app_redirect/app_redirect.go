// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package app_redirect provides the Diameter redirect application extension.
package app_redirect

import (
	"log"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type appRedirect struct {
	redirectHost string
	usage        uint32
	maxCacheTime uint32
	apps         map[types.ApplicationID]bool
}

// Name returns the extension name.
func (e *appRedirect) Name() string { return "app_redirect" }

// Init initializes the extension.
func (e *appRedirect) Init(ctx extension.InitContext, config map[string]interface{}) error {
	log.Printf("Initializing extension: app_redirect")

	if host, ok := config["redirect_host"].(string); ok {
		e.redirectHost = host
	} else {
		log.Printf("app_redirect: No redirect_host configured, extension will be inactive")
		return nil
	}

	e.usage = 2 // ALL_REALM
	if usage, ok := config["redirect_usage"].(float64); ok {
		e.usage = uint32(usage)
	}

	e.maxCacheTime = 3600
	if cache, ok := config["redirect_max_cache_time"].(float64); ok {
		e.maxCacheTime = uint32(cache)
	}

	if apps, ok := config["apps"].([]interface{}); ok {
		e.apps = make(map[types.ApplicationID]bool)
		for _, a := range apps {
			if id, ok := a.(float64); ok {
				e.apps[types.ApplicationID(id)] = true
			}
		}
	}

	// Register an incoming routing handler to redirect messages
	// Priority 50 (medium)
	ctx.GetRouter().RegisterInHandler("app_redirect", 50, e.handleRouting)

	return nil
}

func (e *appRedirect) handleRouting(p *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !msg.IsRequest() || e.redirectHost == "" {
		return candidates, nil
	}

	// Check if this app should be redirected
	if len(e.apps) > 0 {
		if !e.apps[msg.ApplicationID] {
			log.Printf("app_redirect: Skipping redirection for AppID %d (not in configured list)", msg.ApplicationID)
			return candidates, nil
		}
	}

	// Basic logic: if Destination-Host matches a certain pattern or is not us, redirect.
	// For this implementation, if it's configured and we match AppID (or all), we redirect.

	destHost, _ := msg.GetDestinationHost()
	localCfg := peer.GetLocalConfig()

	// If it's explicitly for us, maybe we shouldn't redirect?
	// But as a Redirect Agent, we might still want to redirect.
	// RFC 6733: A Redirect Agent... receives a request and returns an answer.

	if destHost != "" && string(destHost) == string(localCfg.DiameterIdentity) {
		// It's for us, let normal dispatch handle it unless we are strictly a redirect agent for this app
		if len(e.apps) == 0 {
			return candidates, nil
		}
	}

	log.Printf("Redirecting request from %s to %s (AppID=%d)", p.DiameterID(), e.redirectHost, msg.ApplicationID)

	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultRedirectIndication)

	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))
	ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeRedirectHost, types.AVPFlagMandatory, e.redirectHost))

	// Redirect-Host-Usage
	ans.AddAVP(message.NewUnsigned32AVP(types.AVPCodeRedirectHostUsage, types.AVPFlagMandatory, e.usage))
	// Redirect-Max-Cache-Time
	ans.AddAVP(message.NewUnsigned32AVP(types.AVPCodeRedirectMaxCacheTime, types.AVPFlagMandatory, e.maxCacheTime))

	if err := p.Send(ans); err != nil {
		log.Printf("app_redirect: Failed to send answer: %v", err)
		return candidates, err
	}

	return nil, routing.ErrConsumed
}

// Stop implements the Stoppable interface.
func (e *appRedirect) Stop() error {
	return nil
}

func init() {
	extension.Register(&appRedirect{})
}
