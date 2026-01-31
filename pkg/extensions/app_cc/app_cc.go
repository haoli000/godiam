// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package app_cc provides the Diameter credit-control application extension.
package app_cc

import (
	"log"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type appCC struct {
	requestsHandled atomic.Uint64
}

// Name returns the extension name.
func (e *appCC) Name() string { return "app_cc" }

// Init initializes the extension.
func (e *appCC) Init(ctx extension.InitContext, _ map[string]interface{}) error {
	log.Printf("Initializing extension: app_cc (Credit Control)")

	// Register dispatch handler for Credit Control Application (4)
	ctx.GetRouter().RegisterDispatchHandler(types.AppIDCreditControl, e.handleCreditControl)

	return nil
}

func (e *appCC) handleCreditControl(p *peer.Peer, msg *message.Message) error {
	if !msg.IsRequest() {
		return nil
	}

	e.requestsHandled.Add(1)

	// Build CCR answer
	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)

	// Copy mandatory AVPs
	if sessionID, ok := msg.GetSessionID(); ok {
		ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))
	}

	// Add Origin-Host and Origin-Realm from local config
	localCfg := peer.GetLocalConfig()
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))

	// Add CC-Request-Type and CC-Request-Number from request
	if ccReqType := msg.FindAVP(types.AVPCodeCCRequestType, 0); ccReqType != nil {
		ans.AddAVP(ccReqType)
	}
	if ccReqNum := msg.FindAVP(types.AVPCodeCCRequestNumber, 0); ccReqNum != nil {
		ans.AddAVP(ccReqNum)
	}

	// Send answer
	if err := p.Send(ans); err != nil {
		log.Printf("Failed to send CCA: %v", err)
		return err
	}

	return nil
}

// Stop implements the Stoppable interface.
func (e *appCC) Stop() error {
	log.Printf("app_cc: total requests handled: %d", e.requestsHandled.Load())
	return nil
}

// Metrics implements extension.MetricsProvider.
func (e *appCC) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "requests_handled_total",
			Help:  "Total Credit Control requests handled.",
			Type:  extension.MetricCounter,
			Value: float64(e.requestsHandled.Load()),
		},
	}
}

func init() {
	extension.Register(&appCC{})
}
