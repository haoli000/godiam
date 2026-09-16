// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package peer

import (
	"testing"
	"time"
)

// A watchdog timeout moves the peer to Suspect and sends a DWR. The timer has
// to be rearmed at that point, because it is the only thing bounding the wait
// for the DWA: without it a peer whose DWA is lost stays in Suspect forever,
// which reports it as not open, removes it from routing, and never triggers a
// reconnect. The symptom is an upstream peer that silently disappears after an
// idle period and never comes back.
func TestSuspectRearmsTheWatchdog(t *testing.T) {
	cfg := testConfig("upstream.example.com")
	cfg.WatchdogInterval = 30 * time.Millisecond

	p := New(cfg, testDict())
	p.setState(StateIOpen)
	p.iConn = newMockConn()

	if err := p.handleEvent(eventMessage{event: EventTimeout}); err != nil {
		t.Fatalf("handling the watchdog timeout failed: %v", err)
	}
	if got := p.State(); got != StateSuspect {
		t.Fatalf("state = %v, want Suspect", got)
	}

	select {
	case ev := <-p.eventChan:
		if ev.event != EventTimeout {
			t.Fatalf("event = %v, want a second EventTimeout", ev.event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no second watchdog timeout: a lost DWA would strand the peer in Suspect")
	}
}

// The rearmed timeout must actually retire the connection, so that a persistent
// peer schedules a reconnect instead of lingering unusable.
func TestSuspectTimeoutClosesTheConnection(t *testing.T) {
	cfg := testConfig("upstream.example.com")
	cfg.WatchdogInterval = 30 * time.Millisecond

	p := New(cfg, testDict())
	p.setState(StateSuspect)
	p.iConn = newMockConn()

	if err := p.handleEvent(eventMessage{event: EventTimeout}); err != nil {
		t.Fatalf("handling the suspect timeout failed: %v", err)
	}
	if got := p.State(); got != StateZombie && got != StateClosed {
		t.Fatalf("state = %v, want the connection retired", got)
	}
}
