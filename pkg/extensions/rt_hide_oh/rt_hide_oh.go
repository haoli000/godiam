// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_hide_oh provides a routing extension that hides the Origin-Host AVP.
package rt_hide_oh

import (
	"log"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

const proxyStateMarker = "rt_hide_oh:origin-host"

type rtHideOH struct {
	localIdentity   string
	headersReplaced atomic.Uint64
	markersRemoved  atomic.Uint64
}

// Name returns the extension name.
func (e *rtHideOH) Name() string { return "rt_hide_oh" }

// Init initializes the extension.
func (e *rtHideOH) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: rt_hide_oh")

	e.localIdentity = ctx.GetConfig().Identity
	if e.localIdentity == "" {
		log.Printf("rt_hide_oh: Warning: Local Identity is empty")
	}

	// Register an incoming handler to replace Origin-Host on requests and clean up answers
	// Priority 35 (runs before rt_rewrite at 40, and before app_redirect at 50)
	ctx.GetRouter().RegisterInHandler("rt_hide_oh", 35, e.handleRouting)

	// The outgoing handler is not strictly necessary if we only operate on incoming messages
	// but we leave it registered in case of future use.
	ctx.GetRouter().RegisterOutHandler("rt_hide_oh", 35, e.handleOutgoing)

	return nil
}

func (e *rtHideOH) handleRouting(p *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if msg.IsRequest() {
		// For requests, replace Origin-Host and add a Proxy-Info marker.
		log.Printf("rt_hide_oh: Processing REQUEST from peer %s", p.DiameterID())
		for i, avp := range msg.AVPs {
			if avp.Code == types.AVPCodeOriginHost && avp.VendorID == 0 {
				oldOriginHost := string(avp.Data)

				// Replace OH with local identity (gw)
				msg.AVPs[i].Data = []byte(e.localIdentity)
				log.Printf("rt_hide_oh: Replaced Origin-Host %s with %s", oldOriginHost, e.localIdentity)

				// Add a Proxy-Info marker so we can identify and remove it from the answer.
				// We don't need to store any state; the marker is enough.
				pi := message.NewGroupedAVP(
					types.AVPCodeProxyInfo,
					types.AVPFlagMandatory,
					0,
					// A dummy Proxy-Host is needed as it's a mandatory AVP within Proxy-Info.
					message.NewUTF8StringAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "hidden"),
					message.NewUTF8StringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, proxyStateMarker),
				)
				msg.AddAVP(pi)
				e.headersReplaced.Add(1)
				break
			}
		}
	} else {
		// For answers, find and remove our Proxy-Info marker.
		log.Printf("rt_hide_oh: Processing ANSWER from peer %s", p.DiameterID())
		for i := len(msg.AVPs) - 1; i >= 0; i-- {
			avp := msg.AVPs[i]
			if avp.Code == types.AVPCodeProxyInfo && avp.VendorID == 0 {
				if !avp.IsGrouped() {
					if err := avp.DecodeGrouped(); err != nil {
						log.Printf("rt_hide_oh: Failed to decode Proxy-Info AVP: %v", err)
						continue
					}
				}
				if state := avp.FindChild(types.AVPCodeProxyState, 0); state != nil {
					if state.GetUTF8String() == proxyStateMarker {
						log.Printf("rt_hide_oh: Found and removing our Proxy-Info marker from answer.")
						// Remove the AVP by slicing.
						msg.AVPs = append(msg.AVPs[:i], msg.AVPs[i+1:]...)
						e.markersRemoved.Add(1)
						break // Assuming only one marker will be present
					}
				}
			}
		}
	}
	return candidates, nil
}

// handleOutgoing is currently a no-op but registered for completeness.
func (e *rtHideOH) handleOutgoing(_ *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	return candidates, nil
}

// Stop implements the Stoppable interface.
func (e *rtHideOH) Stop() error {
	return nil
}

// Metrics implements extension.MetricsProvider.
func (e *rtHideOH) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "headers_replaced_total",
			Help:  "Total Origin-Host headers replaced with local identity.",
			Type:  extension.MetricCounter,
			Value: float64(e.headersReplaced.Load()),
		},
		{
			Name:  "markers_removed_total",
			Help:  "Total Proxy-Info markers removed from answers.",
			Type:  extension.MetricCounter,
			Value: float64(e.markersRemoved.Load()),
		},
	}
}

func init() {
	extension.Register(&rtHideOH{})
}
