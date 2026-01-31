// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package session provides Diameter session management.
package session

import (
	"fmt"
	"sync"

	"github.com/haoli000/godiam/pkg/proto/types"
)

// Manager manages all active Diameter sessions.
type Manager struct {
	mu sync.RWMutex

	// Sessions indexed by Session-ID
	sessions map[string]*Session

	// Sessions indexed by Application-ID for quick lookup
	byAppID map[types.ApplicationID]map[string]*Session

	// Callbacks
	onSessionCreate func(*Session)
	onSessionClose  func(*Session)
}

// NewManager creates a new session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		byAppID:  make(map[types.ApplicationID]map[string]*Session),
	}
}

// Create creates a new session and registers it with the manager.
func (m *Manager) Create(originHost types.DiamID, appID types.ApplicationID) *Session {
	session := NewSession(originHost, appID)
	_ = m.Register(session)
	return session
}

// Register adds an existing session to the manager.
func (m *Manager) Register(session *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := session.ID()
	if _, exists := m.sessions[id]; exists {
		return fmt.Errorf("session already exists: %s", id)
	}

	m.sessions[id] = session

	// Index by app ID
	appID := session.AppID()
	if m.byAppID[appID] == nil {
		m.byAppID[appID] = make(map[string]*Session)
	}
	m.byAppID[appID][id] = session

	// Set up termination callback to auto-remove
	origCallback := session.onTerminate
	session.SetOnTerminate(func(s *Session, cause types.TerminationCause) {
		m.Remove(s.ID())
		if origCallback != nil {
			origCallback(s, cause)
		}
	})

	if m.onSessionCreate != nil {
		m.onSessionCreate(session)
	}

	return nil
}

// Get retrieves a session by Session-ID.
func (m *Manager) Get(sessionID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[sessionID]
	return session, ok
}

// GetByAppID retrieves all sessions for an application.
func (m *Manager) GetByAppID(appID types.ApplicationID) []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	appSessions := m.byAppID[appID]
	if appSessions == nil {
		return nil
	}

	result := make([]*Session, 0, len(appSessions))
	for _, session := range appSessions {
		result = append(result, session)
	}
	return result
}

// Remove removes a session from the manager.
func (m *Manager) Remove(sessionID string) {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	if !exists {
		m.mu.Unlock()
		return
	}

	delete(m.sessions, sessionID)

	appID := session.AppID()
	if m.byAppID[appID] != nil {
		delete(m.byAppID[appID], sessionID)
	}

	callback := m.onSessionClose
	m.mu.Unlock()

	if callback != nil {
		callback(session)
	}
}

// Count returns the total number of active sessions.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// CountByAppID returns the number of sessions for an application.
func (m *Manager) CountByAppID(appID types.ApplicationID) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byAppID[appID])
}

// All returns all active sessions.
func (m *Manager) All() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		result = append(result, session)
	}
	return result
}

// CloseAll terminates all sessions with the given cause.
func (m *Manager) CloseAll(cause types.TerminationCause) {
	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.RUnlock()

	for _, session := range sessions {
		session.Terminate(cause)
	}
}

// SetOnSessionCreate sets the callback for session creation.
func (m *Manager) SetOnSessionCreate(cb func(*Session)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSessionCreate = cb
}

// SetOnSessionClose sets the callback for session closure.
func (m *Manager) SetOnSessionClose(cb func(*Session)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSessionClose = cb
}

// Stats returns session statistics.
func (m *Manager) Stats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{
		Total: len(m.sessions),
		ByApp: make(map[types.ApplicationID]int),
	}

	for appID, sessions := range m.byAppID {
		stats.ByApp[appID] = len(sessions)
	}

	return stats
}

// Stats contains session statistics.
type Stats struct {
	Total int
	ByApp map[types.ApplicationID]int
}
