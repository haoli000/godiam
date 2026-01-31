// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_ignore_dh provides a routing extension that ignores the Destination-Host AVP.
package rt_ignore_dh

import (
	"log"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type rtIgnoreDH struct {
	headersDropped  atomic.Uint64
	headersRestored atomic.Uint64
}

// Name returns the extension name.
func (e *rtIgnoreDH) Name() string { return "rt_ignore_dh" }

// Init initializes the extension.
func (e *rtIgnoreDH) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: rt_ignore_dh")

	// Register an incoming routing handler to drop Destination-Host on requests
	// Priority 35 (runs before rt_rewrite at 40, and before app_redirect at 50)
	ctx.GetRouter().RegisterInHandler("rt_ignore_dh", 35, e.handleRouting)
	// Register an outgoing routing handler to restore Destination-Host before forwarding
	ctx.GetRouter().RegisterOutHandler("rt_ignore_dh", 35, e.handleOutgoing)

	return nil
}

func (e *rtIgnoreDH) handleRouting(_ *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !msg.IsRequest() {
		return candidates, nil
	}

	// Remove top-level Destination-Host AVP if present, but first stash it in Proxy-Info
	for i, avp := range msg.AVPs {
		if avp.Code == types.AVPCodeDestinationHost && avp.VendorID == 0 {
			// Save original Destination-Host in a Proxy-Info entry tagged for rt_ignore_dh
			origDH := types.DiamID(avp.GetDiameterIdentity())
			pi := message.NewGroupedAVP(
				types.AVPCodeProxyInfo,
				types.AVPFlagMandatory,
				0,
				message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, origDH),
				message.NewUTF8StringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, "rt_ignore_dh:dest-host"),
			)
			msg.AddAVP(pi)
			msg.AVPs = append(msg.AVPs[:i], msg.AVPs[i+1:]...)
			e.headersDropped.Add(1)
			log.Printf("rt_ignore_dh: Dropped Destination-Host for routing")
			break
		}
	}

	return candidates, nil
}

// handleOutgoing restores Destination-Host from Proxy-Info if it was stashed by rt_ignore_dh
func (e *rtIgnoreDH) handleOutgoing(msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !msg.IsRequest() {
		return candidates, nil
	}

	// If message already has Destination-Host, nothing to do
	if _, ok := msg.GetDestinationHost(); ok {
		return candidates, nil
	}

	// Scan Proxy-Info entries to find our marker
	for idx, avp := range msg.AVPs {
		if avp.Code == types.AVPCodeProxyInfo && avp.IsGrouped() {
			state := avp.FindChild(types.AVPCodeProxyState, 0)
			if state != nil && state.GetUTF8String() == "rt_ignore_dh:dest-host" {
				host := avp.FindChild(types.AVPCodeProxyHost, 0)
				if host != nil {
					// Restore Destination-Host
					msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, host.GetDiameterIdentity()))
					e.headersRestored.Add(1)
					log.Printf("rt_ignore_dh: Restored Destination-Host from Proxy-Info")
				}
				// Remove our Proxy-Info entry to avoid leaking hop-by-hop state
				msg.AVPs = append(msg.AVPs[:idx], msg.AVPs[idx+1:]...)
				break
			}
		}
	}

	return candidates, nil
}

// Stop implements the Stoppable interface.
func (e *rtIgnoreDH) Stop() error {
	return nil
}

// Metrics implements extension.MetricsProvider.
func (e *rtIgnoreDH) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "headers_dropped_total",
			Help:  "Total Destination-Host headers dropped for routing.",
			Type:  extension.MetricCounter,
			Value: float64(e.headersDropped.Load()),
		},
		{
			Name:  "headers_restored_total",
			Help:  "Total Destination-Host headers restored before forwarding.",
			Type:  extension.MetricCounter,
			Value: float64(e.headersRestored.Load()),
		},
	}
}

func init() {
	extension.Register(&rtIgnoreDH{})
}
