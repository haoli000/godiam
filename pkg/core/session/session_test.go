// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package session

import (
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func TestNewSession(t *testing.T) {
	session := NewSession("test.example.com", types.AppIDCreditControl)

	if session.ID() == "" {
		t.Error("session ID should not be empty")
	}
	if session.AppID() != types.AppIDCreditControl {
		t.Errorf("expected app ID %d, got %d", types.AppIDCreditControl, session.AppID())
	}
	if session.State() != StateIdle {
		t.Errorf("expected state IDLE, got %s", session.State())
	}
}

func TestSessionStateTransitions(t *testing.T) {
	session := NewSession("test.example.com", types.AppIDCreditControl)

	// Start idle
	if session.State() != StateIdle {
		t.Errorf("expected IDLE, got %s", session.State())
	}

	// Open session
	session.Open()
	if session.State() != StateOpen {
		t.Errorf("expected OPEN, got %s", session.State())
	}
	if !session.IsOpen() {
		t.Error("IsOpen() should return true")
	}

	// Terminate session
	session.Terminate(types.TerminationLogout)
	if session.State() != StateDisconnected {
		t.Errorf("expected DISCONNECTED, got %s", session.State())
	}
}

func TestSessionData(t *testing.T) {
	session := NewSession("test.example.com", types.AppIDCreditControl)

	// Set data
	session.Set("user", "john")
	session.Set("balance", 100)

	// Get data
	user, ok := session.Get("user")
	if !ok {
		t.Error("user not found")
	}
	if user != "john" {
		t.Errorf("expected 'john', got %v", user)
	}

	balance, ok := session.Get("balance")
	if !ok {
		t.Error("balance not found")
	}
	if balance != 100 {
		t.Errorf("expected 100, got %v", balance)
	}

	// Delete data
	session.Delete("user")
	_, ok = session.Get("user")
	if ok {
		t.Error("user should have been deleted")
	}
}

func TestSessionTimeout(t *testing.T) {
	session := NewSession("test.example.com", types.AppIDCreditControl)
	session.Open()

	timeoutCalled := make(chan bool, 1)
	session.SetOnTimeout(func(_ *Session) {
		timeoutCalled <- true
	})

	// Set short timeout
	session.SetTimeout(50 * time.Millisecond)

	select {
	case <-timeoutCalled:
		// Expected
	case <-time.After(2 * time.Second):
		t.Error("timeout callback not called")
	}

	if session.State() != StateDisconnected {
		t.Errorf("expected DISCONNECTED after timeout, got %s", session.State())
	}
}

func TestSessionManager(t *testing.T) {
	mgr := NewManager()

	// Create sessions
	s1 := mgr.Create("node.example.com", types.AppIDCreditControl)
	s2 := mgr.Create("node.example.com", types.AppIDBaseAccounting)
	_ = mgr.Create("node.example.com", types.AppIDCreditControl)

	if mgr.Count() != 3 {
		t.Errorf("expected 3 sessions, got %d", mgr.Count())
	}

	// Get by ID
	found, ok := mgr.Get(s1.ID())
	if !ok {
		t.Error("session s1 not found")
	}
	if found.ID() != s1.ID() {
		t.Error("wrong session returned")
	}

	// Get by app ID
	ccSessions := mgr.GetByAppID(types.AppIDCreditControl)
	if len(ccSessions) != 2 {
		t.Errorf("expected 2 CC sessions, got %d", len(ccSessions))
	}

	acctSessions := mgr.GetByAppID(types.AppIDBaseAccounting)
	if len(acctSessions) != 1 {
		t.Errorf("expected 1 accounting session, got %d", len(acctSessions))
	}

	// Remove session
	mgr.Remove(s1.ID())
	if mgr.Count() != 2 {
		t.Errorf("expected 2 sessions after removal, got %d", mgr.Count())
	}

	_, ok = mgr.Get(s1.ID())
	if ok {
		t.Error("session s1 should have been removed")
	}

	// Terminate session
	s2.Terminate(types.TerminationLogout)
	// Should auto-remove due to termination callback
	if mgr.Count() != 1 {
		t.Errorf("expected 1 session after termination, got %d", mgr.Count())
	}

	// Close all
	mgr.CloseAll(types.TerminationAdministrative)
	if mgr.Count() != 0 {
		t.Errorf("expected 0 sessions after CloseAll, got %d", mgr.Count())
	}
}

func TestSessionStats(t *testing.T) {
	mgr := NewManager()

	mgr.Create("node.example.com", types.AppIDCreditControl)
	mgr.Create("node.example.com", types.AppIDCreditControl)
	mgr.Create("node.example.com", types.AppIDBaseAccounting)

	stats := mgr.Stats()

	if stats.Total != 3 {
		t.Errorf("expected total 3, got %d", stats.Total)
	}
	if stats.ByApp[types.AppIDCreditControl] != 2 {
		t.Errorf("expected 2 CC sessions, got %d", stats.ByApp[types.AppIDCreditControl])
	}
	if stats.ByApp[types.AppIDBaseAccounting] != 1 {
		t.Errorf("expected 1 acct session, got %d", stats.ByApp[types.AppIDBaseAccounting])
	}
}
