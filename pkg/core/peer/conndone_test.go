// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package peer

import (
	"runtime"
	"testing"
	"time"
)

// TestConnDoneSignalsWriteLoopExit verifies that closing connDone causes
// writeLoop to exit — the core fix for the goroutine leak bug where
// writeLoop goroutines stayed blocked forever after peer disconnect.
func TestConnDoneSignalsWriteLoopExit(t *testing.T) {
	dict := testDict()
	cfg := testConfig("conndone.test")
	p := New(cfg, dict)
	defer p.Stop()

	conn := newMockConn()
	p.mu.Lock()
	p.iConn = conn
	p.mu.Unlock()

	// Start writeLoop in a goroutine
	done := make(chan struct{})
	go func() {
		p.writeLoop()
		close(done)
	}()

	// Yield to let writeLoop goroutine start and block on select
	for i := 0; i < 100; i++ {
		runtime.Gosched()
	}

	// Close connDone — this should make writeLoop exit
	close(p.connDone)

	select {
	case <-done:
		// writeLoop exited — pass
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit after connDone was closed (goroutine leak)")
	}
}

// TestConnDoneDoesNotExitOnCtxAlone verifies writeLoop also exits on ctx cancel
// (permanent shutdown via Stop()), as a complementary path to connDone.
func TestWriteLoopExitsOnCtxCancel(t *testing.T) {
	dict := testDict()
	cfg := testConfig("ctx-cancel.test")
	p := New(cfg, dict)

	conn := newMockConn()
	p.mu.Lock()
	p.iConn = conn
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.writeLoop()
		close(done)
	}()

	// Yield to let writeLoop goroutine start
	for i := 0; i < 100; i++ {
		runtime.Gosched()
	}

	// Cancel via Stop (permanent shutdown)
	p.Stop()

	select {
	case <-done:
		// writeLoop exited — pass
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit after ctx cancel")
	}
}

// TestErrorCleanupClosesConnDone verifies errorCleanup() closes connDone,
// which is the mechanism that signals writeLoop to exit on disconnect.
func TestErrorCleanupClosesConnDone(t *testing.T) {
	dict := testDict()
	cfg := testConfig("cleanup.test")
	p := New(cfg, dict)
	defer p.Stop()

	// connDone should be open initially
	select {
	case <-p.connDone:
		t.Fatal("connDone should be open initially")
	default:
		// good
	}

	// errorCleanup should close it
	p.errorCleanup()

	select {
	case <-p.connDone:
		// closed — pass
	default:
		t.Fatal("connDone should be closed after errorCleanup")
	}
}

// TestErrorCleanupIdempotent verifies errorCleanup can be called twice
// without panicking (double-close guard).
func TestErrorCleanupIdempotent(t *testing.T) {
	dict := testDict()
	cfg := testConfig("cleanup-idem.test")
	p := New(cfg, dict)
	defer p.Stop()

	p.errorCleanup()
	p.errorCleanup() // must not panic
}

// TestResetConnDoneAfterCleanup verifies the reconnection flow:
// errorCleanup closes connDone, then resetConnDone recreates it,
// allowing a new writeLoop to start on the next connection cycle.
func TestResetConnDoneAfterCleanup(t *testing.T) {
	dict := testDict()
	cfg := testConfig("reset.test")
	p := New(cfg, dict)
	defer p.Stop()

	// Phase 1: errorCleanup closes connDone
	p.errorCleanup()
	select {
	case <-p.connDone:
		// good, it's closed
	default:
		t.Fatal("connDone should be closed after errorCleanup")
	}

	// Phase 2: resetConnDone creates a fresh channel
	p.resetConnDone()

	// Should be open again
	select {
	case <-p.connDone:
		t.Fatal("connDone should be open after resetConnDone")
	default:
		// good
	}

	// Phase 3: a new writeLoop can be started and will block until connDone closes again
	conn := newMockConn()
	p.mu.Lock()
	p.iConn = conn
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.writeLoop()
		close(done)
	}()

	// Yield to let writeLoop goroutine start
	for i := 0; i < 100; i++ {
		runtime.Gosched()
	}

	// Close the fresh connDone — writeLoop should exit
	p.errorCleanup()

	select {
	case <-done:
		// writeLoop exited — pass
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit after second errorCleanup")
	}
}

// TestResetConnDoneNoOpWhenOpen verifies resetConnDone is a no-op
// when connDone is already open (not closed).
func TestResetConnDoneNoOpWhenOpen(t *testing.T) {
	dict := testDict()
	cfg := testConfig("reset-noop.test")
	p := New(cfg, dict)
	defer p.Stop()

	original := p.connDone
	p.resetConnDone()

	// Should be the same channel (no-op because it was already open)
	select {
	case <-p.connDone:
		t.Fatal("connDone should still be open")
	default:
		// good
	}

	// Verify it's the same channel by closing it and checking both
	close(original)
	select {
	case <-p.connDone:
		// same channel — pass
	default:
		t.Fatal("resetConnDone should not have replaced an open channel")
	}
}
