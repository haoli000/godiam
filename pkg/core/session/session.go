// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package session provides Diameter session management.
// This corresponds to session handling in libfdproto and libfdcore.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/haoli000/godiam/pkg/proto/types"
)

// State represents the state of a Diameter session.
type State int

const (
	// StateIdle - session created but not started
	StateIdle State = iota
	// StateOpen - session is active
	StateOpen
	// StateDisconnected - session terminated
	StateDisconnected
	// StatePending - waiting for authorization
	StatePending
)

// String returns the string representation of a session state.
func (s State) String() string {
	names := []string{"IDLE", "OPEN", "DISCONNECTED", "PENDING"}
	if int(s) < len(names) {
		return names[s]
	}
	return fmt.Sprintf("UNKNOWN(%d)", s)
}

// Session represents a Diameter session.
type Session struct {
	mu sync.RWMutex

	// Session identification
	id        string
	appID     types.ApplicationID
	authState types.AuthSessionState

	// Session state
	state State

	// Peer and realm information
	originHost  types.DiamID
	originRealm types.DiamID
	destHost    types.DiamID
	destRealm   types.DiamID

	// Timing
	createdAt time.Time
	expiresAt time.Time
	timeout   time.Duration

	// Session data (application-specific)
	data map[string]interface{}

	// Callbacks
	onTimeout   func(*Session)
	onTerminate func(*Session, types.TerminationCause)

	// Timer for expiration
	timer *time.Timer
}

// NewSession creates a new session with a generated Session-ID.
func NewSession(originHost types.DiamID, appID types.ApplicationID) *Session {
	return &Session{
		id:         generateSessionID(originHost),
		appID:      appID,
		state:      StateIdle,
		authState:  types.AuthStateIdle,
		originHost: originHost,
		createdAt:  time.Now(),
		data:       make(map[string]interface{}),
	}
}

// NewSessionWithID creates a new session with a specific Session-ID.
func NewSessionWithID(sessionID string, appID types.ApplicationID) *Session {
	return &Session{
		id:        sessionID,
		appID:     appID,
		state:     StateIdle,
		authState: types.AuthStateIdle,
		createdAt: time.Now(),
		data:      make(map[string]interface{}),
	}
}

// generateSessionID generates a Session-ID per RFC 6733 format:
// <DiameterIdentity>;<high 32 bits>;<low 32 bits>[;<optional value>]
func generateSessionID(originHost types.DiamID) string {
	// Generate random bytes for uniqueness
	var randomBytes [8]byte
	_, _ = rand.Read(randomBytes[:])

	timestamp := uint32(time.Now().Unix()) //nolint:gosec // G115: value range is protocol-constrained
	random := hex.EncodeToString(randomBytes[:])

	return fmt.Sprintf("%s;%d;%s", originHost, timestamp, random)
}

// ID returns the Session-ID.
func (s *Session) ID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

// AppID returns the Application-ID.
func (s *Session) AppID() types.ApplicationID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.appID
}

// State returns the current session state.
func (s *Session) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// AuthState returns the Auth-Session-State.
func (s *Session) AuthState() types.AuthSessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authState
}

// SetAuthState sets the Auth-Session-State.
func (s *Session) SetAuthState(state types.AuthSessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authState = state
}

// OriginHost returns the Origin-Host.
func (s *Session) OriginHost() types.DiamID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.originHost
}

// SetOriginHost sets the Origin-Host.
func (s *Session) SetOriginHost(host types.DiamID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.originHost = host
}

// OriginRealm returns the Origin-Realm.
func (s *Session) OriginRealm() types.DiamID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.originRealm
}

// SetOriginRealm sets the Origin-Realm.
func (s *Session) SetOriginRealm(realm types.DiamID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.originRealm = realm
}

// DestHost returns the Destination-Host.
func (s *Session) DestHost() types.DiamID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.destHost
}

// SetDestHost sets the Destination-Host.
func (s *Session) SetDestHost(host types.DiamID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.destHost = host
}

// DestRealm returns the Destination-Realm.
func (s *Session) DestRealm() types.DiamID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.destRealm
}

// SetDestRealm sets the Destination-Realm.
func (s *Session) SetDestRealm(realm types.DiamID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.destRealm = realm
}

// CreatedAt returns when the session was created.
func (s *Session) CreatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.createdAt
}

// ExpiresAt returns when the session expires.
func (s *Session) ExpiresAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.expiresAt
}

// SetTimeout sets the session timeout and schedules expiration.
func (s *Session) SetTimeout(timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.timeout = timeout
	s.expiresAt = time.Now().Add(timeout)

	if s.timer != nil {
		s.timer.Stop()
	}

	s.timer = time.AfterFunc(timeout, func() {
		s.handleTimeout()
	})
}

// RefreshTimeout resets the timeout timer.
func (s *Session) RefreshTimeout() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.timeout > 0 {
		s.expiresAt = time.Now().Add(s.timeout)
		if s.timer != nil {
			s.timer.Reset(s.timeout)
		}
	}
}

// handleTimeout is called when the session times out.
func (s *Session) handleTimeout() {
	s.mu.Lock()
	callback := s.onTimeout
	s.state = StateDisconnected
	s.mu.Unlock()

	if callback != nil {
		callback(s)
	}
}

// Open transitions the session to open state.
func (s *Session) Open() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateOpen
}

// Terminate terminates the session with the given cause.
func (s *Session) Terminate(cause types.TerminationCause) {
	s.mu.Lock()
	callback := s.onTerminate
	s.state = StateDisconnected
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()

	if callback != nil {
		callback(s, cause)
	}
}

// IsOpen returns true if the session is open.
func (s *Session) IsOpen() bool {
	return s.State() == StateOpen
}

// SetOnTimeout sets the timeout callback.
func (s *Session) SetOnTimeout(cb func(*Session)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTimeout = cb
}

// SetOnTerminate sets the termination callback.
func (s *Session) SetOnTerminate(cb func(*Session, types.TerminationCause)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTerminate = cb
}

// Get retrieves application-specific data.
func (s *Session) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	return val, ok
}

// Set stores application-specific data.
func (s *Session) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

// Delete removes application-specific data.
func (s *Session) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
}

// Close cleans up the session resources.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.state = StateDisconnected
}
