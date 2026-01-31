// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package dbg_msg_dumps provides debug message dump hooks for Diameter message tracing.
package dbg_msg_dumps

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/message"
)

// Configuration flags (matching freeDiameter)
const (
	hkErrorsQuiet   = 0x0001 // errors are not dumped
	hkErrorsCompact = 0x0002 // errors in compact mode
	hkErrorsFull    = 0x0004 // errors in full mode
	hkErrorsTree    = 0x0008 // errors in treeview mode

	hkSndrcvQuiet   = 0x0010 // send+rcv are not dumped
	hkSndrcvCompact = 0x0020 // send+rcv in compact mode
	hkSndrcvFull    = 0x0040 // send+rcv in full mode
	hkSndrcvTree    = 0x0080 // send+rcv in tree mode

	hkRoutingQuiet   = 0x0100 // routing decisions are not dumped
	hkRoutingCompact = 0x0200 // routing decisions in compact mode
	hkRoutingFull    = 0x0400 // routing decisions in full mode
	hkRoutingTree    = 0x0800 // routing decisions in tree mode
)

// Ensure routing constants are not flagged as unused; they are part of the
// freeDiameter-compatible flag set and may be used in future handlers.
var _ = [...]uint32{hkRoutingQuiet, hkRoutingCompact, hkRoutingFull, hkRoutingTree}

const (
	hkPeersQuiet   = 0x1000 // peers connections events are not dumped
	hkPeersCompact = 0x2000 // peers connections events in compact mode
	hkPeersFull    = 0x4000 // peers connections events in full mode
	hkPeersTree    = 0x8000 // peers connections events in tree mode

	// Default is hkErrorsTree + hkSndrcvTree + hkPeersTree
	defaultDumpLevel = hkErrorsTree | hkSndrcvTree | hkPeersTree
)

type dbgMsgDumps struct {
	dumpLevel uint32
	router    *routing.Router
}

// Name returns the extension name.
func (e *dbgMsgDumps) Name() string { return "dbg_msg_dumps" }

// Init initializes the extension.
func (e *dbgMsgDumps) Init(ctx extension.InitContext, config map[string]interface{}) error {
	log.Printf("Initializing extension: dbg_msg_dumps")

	// Set default dump level
	e.dumpLevel = defaultDumpLevel

	// Parse configuration parameter if provided
	if configStr, ok := config["dump_level"].(string); ok {
		configStr = strings.TrimSpace(configStr)
		if strings.HasPrefix(configStr, "0x") || strings.HasPrefix(configStr, "0X") {
			level, err := strconv.ParseUint(configStr, 0, 32)
			if err != nil {
				return fmt.Errorf("invalid dump_level format: %w", err)
			}
			e.dumpLevel = uint32(level)
		} else {
			// Try decimal
			level, err := strconv.ParseUint(configStr, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid dump_level format: %w", err)
			}
			e.dumpLevel = uint32(level)
		}
		log.Printf("dbg_msg_dumps: configured dump_level = 0x%04x", e.dumpLevel)
	}

	e.router = ctx.GetRouter()

	// Register incoming routing handler
	e.router.RegisterInHandler("dbg_msg_dumps-in", 100, e.handleIncoming)

	// Register outgoing routing handler
	e.router.RegisterOutHandler("dbg_msg_dumps-out", 100, e.handleOutgoing)

	// Register peer state change callbacks for all current peers
	for _, p := range ctx.GetPeers() {
		p.SetOnStateChange(e.handlePeerStateChange)
	}

	return nil
}

func (e *dbgMsgDumps) getPeerName(p *peer.Peer) string {
	if p == nil {
		return "<unknown peer>"
	}
	return string(p.DiameterID())
}

func (e *dbgMsgDumps) dumpMessage(p *peer.Peer, msg *message.Message, format message.DumpFormat, event string) {
	peerName := e.getPeerName(p)

	switch format {
	case message.DumpSummary:
		log.Printf("DEBUG: %s from %s: %s", event, peerName, msg.DumpSummary())
	case message.DumpFull:
		log.Printf("DEBUG: %s from %s: %s", event, peerName, msg.DumpFull())
	case message.DumpTree:
		log.Printf("DEBUG: %s from %s:\n%s", event, peerName, msg.DumpTree())
	}
}

func (e *dbgMsgDumps) getDumpMode(dumpFlags uint32) message.DumpFormat {
	if dumpFlags&hkErrorsCompact != 0 {
		return message.DumpSummary
	}
	if dumpFlags&hkErrorsFull != 0 {
		return message.DumpFull
	}
	if dumpFlags&hkErrorsTree != 0 {
		return message.DumpTree
	}
	return message.DumpTree // Default
}

func (e *dbgMsgDumps) handleIncoming(p *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if e.dumpLevel&hkSndrcvQuiet != 0 {
		return candidates, nil
	}

	mode := e.getDumpMode(e.dumpLevel & (hkSndrcvCompact | hkSndrcvFull | hkSndrcvTree))
	e.dumpMessage(p, msg, mode, "RCV")

	return candidates, nil
}

func (e *dbgMsgDumps) handleOutgoing(msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if e.dumpLevel&hkSndrcvQuiet != 0 {
		return candidates, nil
	}

	mode := e.getDumpMode(e.dumpLevel & (hkSndrcvCompact | hkSndrcvFull | hkSndrcvTree))
	e.dumpMessage(nil, msg, mode, "SND")

	return candidates, nil
}

func (e *dbgMsgDumps) handlePeerStateChange(p *peer.Peer, _, newState peer.State) {
	if e.dumpLevel&hkPeersQuiet != 0 {
		return
	}

	peerName := e.getPeerName(p)

	switch newState {
	case peer.StateIOpen, peer.StateROpen:
		log.Printf("DEBUG: CONNECTED TO '%s'", peerName)
	case peer.StateClosed:
		log.Printf("DEBUG: DISCONNECTED from '%s'", peerName)
	default:
		// Other states are not logged
	}
}

// Stop implements the Stoppable interface.
func (e *dbgMsgDumps) Stop() error {
	return nil
}

func init() {
	extension.Register(&dbgMsgDumps{})
}
