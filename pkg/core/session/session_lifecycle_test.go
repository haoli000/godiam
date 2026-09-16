// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package session

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func TestSessionAccessorsPreserveDiameterMetadata(t *testing.T) {
	const sessionID = "client.example.com;123;abc"
	s := NewSessionWithID(sessionID, types.AppIDCommon)

	if s.ID() != sessionID {
		t.Fatalf("registered peers must be able to correlate the exact Session-Id, got %q", s.ID())
	}
	if s.AuthState() != types.AuthStateIdle {
		t.Fatalf("new auth sessions should start idle, got %v", s.AuthState())
	}
	if s.CreatedAt().IsZero() {
		t.Fatal("session creation time is needed to reason about leaks and expiry")
	}

	s.SetAuthState(types.AuthStateNoStateMaintained)
	s.SetOriginHost("origin.example.com")
	s.SetOriginRealm("example.com")
	s.SetDestHost("server.example.com")
	s.SetDestRealm("server.example.com")

	if s.AuthState() != types.AuthStateNoStateMaintained {
		t.Fatalf("auth state changed incorrectly: %v", s.AuthState())
	}
	if s.OriginHost() != "origin.example.com" || s.OriginRealm() != "example.com" {
		t.Fatalf("origin metadata was not preserved: host=%q realm=%q", s.OriginHost(), s.OriginRealm())
	}
	if s.DestHost() != "server.example.com" || s.DestRealm() != "server.example.com" {
		t.Fatalf("destination metadata was not preserved: host=%q realm=%q", s.DestHost(), s.DestRealm())
	}
	if State(99).String() != "UNKNOWN(99)" {
		t.Fatalf("unknown states should remain diagnosable, got %q", State(99).String())
	}
}

func TestTimeoutDisconnectsSessionButManagerStillReturnsIt(t *testing.T) {
	mgr := NewManager()
	s := mgr.Create("origin.example.com", types.AppIDCommon)
	s.Open()

	timedOut := make(chan struct{}, 1)
	s.SetOnTimeout(func(*Session) {
		timedOut <- struct{}{}
	})
	s.SetTimeout(10 * time.Millisecond)

	select {
	case <-timedOut:
	case <-time.After(time.Second):
		t.Fatal("timeout callback did not fire; leaked live sessions would never be reclaimed")
	}

	found, ok := mgr.Get(s.ID())
	if !ok {
		t.Fatal("current manager behaviour changed: timed-out sessions are no longer retained")
	}
	if found.State() != StateDisconnected {
		t.Fatalf("timed-out session must not remain open, got %s", found.State())
	}
	if mgr.Count() != 1 {
		t.Fatalf("current manager retains timed-out sessions; got count %d", mgr.Count())
	}
}

func TestTerminatingRegisteredSessionEvictsItOnce(t *testing.T) {
	mgr := NewManager()

	var originalCallbackCalls atomic.Int32
	s := NewSessionWithID("origin.example.com;1;double-release", types.AppIDCreditControl)
	s.SetOnTerminate(func(*Session, types.TerminationCause) {
		originalCallbackCalls.Add(1)
	})

	var closeCallbackCalls atomic.Int32
	mgr.SetOnSessionClose(func(closed *Session) {
		if closed.ID() != s.ID() {
			t.Errorf("closed wrong session: %s", closed.ID())
		}
		closeCallbackCalls.Add(1)
	})

	if err := mgr.Register(s); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	s.Terminate(types.TerminationLogout)
	s.Terminate(types.TerminationLogout)
	mgr.Remove(s.ID())

	if _, ok := mgr.Get(s.ID()); ok {
		t.Fatal("terminated session remained addressable")
	}
	if mgr.Count() != 0 {
		t.Fatalf("double release left sessions behind: %d", mgr.Count())
	}
	if closeCallbackCalls.Load() != 1 {
		t.Fatalf("eviction callback should describe one map removal, got %d", closeCallbackCalls.Load())
	}
	if originalCallbackCalls.Load() != 2 {
		t.Fatalf("session termination callback should still reflect both termination attempts, got %d", originalCallbackCalls.Load())
	}
}

func TestRegisterRejectsDuplicateSessionID(t *testing.T) {
	mgr := NewManager()
	first := NewSessionWithID("origin.example.com;1;duplicate", types.AppIDCommon)
	second := NewSessionWithID(first.ID(), types.AppIDBaseAccounting)

	if err := mgr.Register(first); err != nil {
		t.Fatalf("first register failed: %v", err)
	}
	if err := mgr.Register(second); err == nil {
		t.Fatal("duplicate Session-Id replaced an existing session")
	}

	sessions := mgr.All()
	if len(sessions) != 1 || sessions[0].ID() != first.ID() {
		t.Fatalf("duplicate registration changed active sessions: %#v", sessions)
	}
	if mgr.CountByAppID(types.AppIDCommon) != 1 {
		t.Fatalf("original app index was damaged: %d", mgr.CountByAppID(types.AppIDCommon))
	}
	if mgr.CountByAppID(types.AppIDBaseAccounting) != 0 {
		t.Fatalf("duplicate session leaked into app index: %d", mgr.CountByAppID(types.AppIDBaseAccounting))
	}
}

func TestManagerCallbacksAndLookupsReflectEviction(t *testing.T) {
	mgr := NewManager()

	created := make(chan string, 2)
	closed := make(chan string, 2)
	mgr.SetOnSessionCreate(func(s *Session) { created <- s.ID() })
	mgr.SetOnSessionClose(func(s *Session) { closed <- s.ID() })

	first := mgr.Create("origin.example.com", types.AppIDCommon)
	second := mgr.Create("origin.example.com", types.AppIDCreditControl)

	for _, want := range []string{first.ID(), second.ID()} {
		select {
		case got := <-created:
			if got != want {
				t.Fatalf("create callback order changed: got %s want %s", got, want)
			}
		default:
			t.Fatalf("missing create callback for %s", want)
		}
	}

	if _, ok := mgr.Get("missing-session"); ok {
		t.Fatal("missing Session-Id was reported as active")
	}
	if got := mgr.GetByAppID(types.AppIDBaseAccounting); got != nil {
		t.Fatalf("unknown application lookup should not allocate false sessions: %#v", got)
	}

	mgr.Remove(first.ID())
	select {
	case got := <-closed:
		if got != first.ID() {
			t.Fatalf("close callback reported %s, want %s", got, first.ID())
		}
	default:
		t.Fatal("missing close callback")
	}

	remaining := mgr.All()
	if len(remaining) != 1 || remaining[0].ID() != second.ID() {
		t.Fatalf("eviction removed the wrong session: %#v", remaining)
	}
}

func TestCloseStopsSessionWithoutChangingStoredData(t *testing.T) {
	s := NewSession("origin.example.com", types.AppIDCommon)
	s.Set("subscriber", "alice")
	s.SetTimeout(time.Hour)
	if !s.ExpiresAt().After(time.Now()) {
		t.Fatalf("timeout did not establish a future expiry: %v", s.ExpiresAt())
	}

	s.Close()

	if s.State() != StateDisconnected {
		t.Fatalf("closed sessions must not look live, got %s", s.State())
	}
	if got, ok := s.Get("subscriber"); !ok || got != "alice" {
		t.Fatalf("closing transport state should not erase application context, got %v ok=%v", got, ok)
	}
}

func TestConcurrentCreateLookupAndDeleteIsRaceFree(t *testing.T) {
	mgr := NewManager()
	const workers = 8
	const iterations = 100

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				s := NewSessionWithID(
					fmt.Sprintf("origin.example.com;%d;%d", worker, i),
					types.AppIDCommon,
				)
				if err := mgr.Register(s); err != nil {
					t.Errorf("register failed: %v", err)
					return
				}
				if found, ok := mgr.Get(s.ID()); !ok || found.ID() != s.ID() {
					t.Errorf("lookup lost active session: found=%v ok=%v", found, ok)
					return
				}
				_ = mgr.All()
				mgr.Remove(s.ID())
				if _, ok := mgr.Get(s.ID()); ok {
					t.Errorf("removed session remained visible: %s", s.ID())
					return
				}
			}
		}()
	}
	wg.Wait()

	if mgr.Count() != 0 {
		t.Fatalf("concurrent create/delete leaked %d sessions", mgr.Count())
	}
}
