// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package peer provides Diameter peer management and state machine.
package peer

import (
	"time"
)

// startWatchdog starts the watchdog timer.
func (p *Peer) startWatchdog() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.watchdogTimer != nil {
		p.watchdogTimer.Stop()
	}

	p.watchdogTimer = time.AfterFunc(p.config.WatchdogInterval, func() {
		p.eventChan <- eventMessage{event: EventTimeout}
	})
}

// resetWatchdog resets the watchdog timer.
func (p *Peer) resetWatchdog() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.watchdogTimer != nil {
		p.watchdogTimer.Reset(p.config.WatchdogInterval)
	}
}

// stopWatchdog stops the watchdog timer.
func (p *Peer) stopWatchdog() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.watchdogTimer != nil {
		p.watchdogTimer.Stop()
		p.watchdogTimer = nil
	}
}
