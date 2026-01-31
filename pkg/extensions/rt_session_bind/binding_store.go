// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_session_bind provides session-affinity routing for the Diameter Routing Agent.
package rt_session_bind

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/haoli000/godiam/pkg/proto/types"
)

type binding struct {
	PeerIdentity types.DiamID
	CreatedAt    time.Time
	LastUsed     time.Time
	ExpiresAt    time.Time
}

// StoreStats holds binding store statistics.
type StoreStats struct {
	TotalBindings int
	Expired       int
}

type bindingStore struct {
	mu       sync.RWMutex
	bindings map[string]*binding
	maxSize  int
	ttl      time.Duration

	// PM counters (atomic for lock-free reads from Metrics())
	lookups   atomic.Uint64
	hits      atomic.Uint64
	misses    atomic.Uint64
	evictions atomic.Uint64
	cleanups  atomic.Uint64
}

func newBindingStore(ttl time.Duration, maxSize int) *bindingStore {
	return &bindingStore{
		bindings: make(map[string]*binding),
		maxSize:  maxSize,
		ttl:      ttl,
	}
}

// Get returns the bound peer for the given key, if the binding exists and has not expired.
func (s *bindingStore) Get(key string) (types.DiamID, bool) {
	s.lookups.Add(1)
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bindings[key]
	if !ok {
		s.misses.Add(1)
		return "", false
	}
	if time.Now().After(b.ExpiresAt) {
		s.misses.Add(1)
		return "", false
	}
	s.hits.Add(1)
	return b.PeerIdentity, true
}

// Set creates or updates a binding. If the store is at capacity, the oldest binding
// (by LastUsed) is evicted.
func (s *bindingStore) Set(key string, peerIdentity types.DiamID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if existing, ok := s.bindings[key]; ok {
		existing.PeerIdentity = peerIdentity
		existing.LastUsed = now
		existing.ExpiresAt = now.Add(s.ttl)
		return
	}
	if s.maxSize > 0 && len(s.bindings) >= s.maxSize {
		s.evictOldest()
	}
	s.bindings[key] = &binding{
		PeerIdentity: peerIdentity,
		CreatedAt:    now,
		LastUsed:     now,
		ExpiresAt:    now.Add(s.ttl),
	}
}

// Touch updates the LastUsed timestamp and refreshes the TTL for the given key.
func (s *bindingStore) Touch(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.bindings[key]; ok {
		now := time.Now()
		b.LastUsed = now
		b.ExpiresAt = now.Add(s.ttl)
	}
}

// RemoveByPeer removes all bindings for the given peer identity.
func (s *bindingStore) RemoveByPeer(peerIdentity types.DiamID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, b := range s.bindings {
		if b.PeerIdentity == peerIdentity {
			delete(s.bindings, key)
		}
	}
}

// Remove removes a single binding by key.
func (s *bindingStore) Remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bindings, key)
}

// Cleanup removes all expired bindings and returns the number removed.
func (s *bindingStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	removed := 0
	for key, b := range s.bindings {
		if now.After(b.ExpiresAt) {
			delete(s.bindings, key)
			removed++
		}
	}
	s.cleanups.Add(uint64(removed))
	return removed
}

// Len returns the current number of bindings.
func (s *bindingStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.bindings)
}

// evictOldest removes the binding with the oldest LastUsed timestamp.
// Caller must hold the write lock.
func (s *bindingStore) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for key, b := range s.bindings {
		if first || b.LastUsed.Before(oldestTime) {
			oldestKey = key
			oldestTime = b.LastUsed
			first = false
		}
	}
	if !first {
		delete(s.bindings, oldestKey)
		s.evictions.Add(1)
	}
}
