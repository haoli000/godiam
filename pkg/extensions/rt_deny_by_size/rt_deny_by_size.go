// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_deny_by_size provides a routing extension that rejects oversized
// Diameter messages before they are routed.
// This corresponds to rt_deny_by_size from the original freeDiameter.
package rt_deny_by_size

import (
	"fmt"
	"log"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
)

const defaultMaxSize = 65536

type rtDenyBySize struct {
	maxSize  int
	rejected atomic.Uint64
}

// Name returns the extension name.
func (e *rtDenyBySize) Name() string { return "rt_deny_by_size" }

// Init initializes the extension.
func (e *rtDenyBySize) Init(ctx extension.InitContext, config map[string]interface{}) error {
	e.maxSize = defaultMaxSize
	if v, ok := config["maximum_size"]; ok {
		switch n := v.(type) {
		case int:
			e.maxSize = n
		case float64:
			e.maxSize = int(n)
		}
	}

	log.Printf("Initializing extension: rt_deny_by_size (max_size=%d)", e.maxSize)

	// Priority 10: runs very early to reject oversized messages before
	// any routing or rewriting work is done.
	ctx.GetRouter().RegisterInHandler("rt_deny_by_size", 10, e.handleRouting)
	return nil
}

// handleRouting checks message size and rejects if it exceeds the limit.
func (e *rtDenyBySize) handleRouting(_ *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !msg.IsRequest() {
		return candidates, nil
	}

	data, err := msg.Encode()
	if err != nil {
		return candidates, nil
	}

	if len(data) > e.maxSize {
		e.rejected.Add(1)
		log.Printf("rt_deny_by_size: rejecting message (size=%d, max=%d, cmd=%d)",
			len(data), e.maxSize, msg.CommandCode)
		return nil, fmt.Errorf("message size %d exceeds maximum %d", len(data), e.maxSize)
	}

	return candidates, nil
}

// Stop implements the Stoppable interface.
func (e *rtDenyBySize) Stop() error { return nil }

// Reconfigure implements the Reconfigurable interface.
func (e *rtDenyBySize) Reconfigure(config map[string]interface{}) error {
	if v, ok := config["maximum_size"]; ok {
		switch n := v.(type) {
		case int:
			e.maxSize = n
		case float64:
			e.maxSize = int(n)
		}
		log.Printf("rt_deny_by_size: reconfigured max_size=%d", e.maxSize)
	}
	return nil
}

// Metrics implements extension.MetricsProvider.
func (e *rtDenyBySize) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "rejected_total",
			Help:  "Total messages rejected due to size limit.",
			Type:  extension.MetricCounter,
			Value: float64(e.rejected.Load()),
		},
	}
}

func init() {
	extension.Register(&rtDenyBySize{})
}
