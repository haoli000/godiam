// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dea_topohide

import (
	"container/list"
	"sync"
	"time"
)

// StoreStats reports the observable state of a Store.
type StoreStats struct {
	Pseudonyms int64
	Hits       int64
	Misses     int64
	Evictions  int64
	Expired    int64
}

// Store keeps the reversible state required by stateful topology hiding:
// the pseudonym to real-host mapping and the session to partner mapping used
// to hide answers travelling back toward an external partner.
//
// Implementations must be safe for concurrent use. The in-memory
// implementation below is the default; an external (e.g. Redis) backend can be
// substituted without touching the extension logic.
type Store interface {
	// SetPartnerLimit caps how many entries a single partner may hold. A limit
	// of 0 leaves that partner bounded only by the store-wide cap.
	SetPartnerLimit(partner string, limit int)
	// PutPseudonym records a pseudonym for realHost within a session scope.
	PutPseudonym(partner, pseudonym, realHost, session string, ttl time.Duration)
	// Pseudonym returns the pseudonym previously assigned to realHost in the
	// given session scope, if it is still valid.
	Pseudonym(realHost, session string) (string, bool)
	// RealHost resolves a pseudonym back to the internal host it masks.
	RealHost(pseudonym string) (string, bool)
	// Stats reports counters for metrics and health reporting.
	Stats() StoreStats
	// Close releases any resources held by the store.
	Close()
}

type entry struct {
	key       string
	value     string
	alias     string // pseudonym key mirrored into the forward index
	partner   string // partner the entry was created for
	expiresAt time.Time
	elem      *list.Element
}

// bucket holds one partner's entries in least-recently-used order. Accounting
// per partner keeps a noisy or hostile partner from evicting the live
// pseudonyms of a quiet one, which would break routing for the victim.
type bucket struct {
	lru   *list.List
	limit int
}

// memoryStore is an in-memory Store with TTL expiry and LRU eviction.
type memoryStore struct {
	mu sync.Mutex

	// pseudonyms maps pseudonym -> entry (value = real host).
	pseudonyms map[string]*entry
	// forward maps "realHost|session" -> pseudonym.
	forward map[string]string

	// buckets holds one LRU list per partner.
	buckets    map[string]*bucket
	maxEntries int
	total      int

	stats  StoreStats
	now    func() time.Time
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// newMemoryStore creates an in-memory store. maxEntries of 0 means unlimited.
// A sweeper removes expired entries every sweepInterval when it is positive.
func newMemoryStore(maxEntries int, sweepInterval time.Duration, now func() time.Time) *memoryStore {
	if now == nil {
		now = time.Now
	}
	s := &memoryStore{
		pseudonyms: make(map[string]*entry),
		forward:    make(map[string]string),
		buckets:    make(map[string]*bucket),
		maxEntries: maxEntries,
		now:        now,
		stopCh:     make(chan struct{}),
	}
	if sweepInterval > 0 {
		s.wg.Add(1)
		go s.sweepLoop(sweepInterval)
	}
	return s
}

func (s *memoryStore) sweepLoop(interval time.Duration) {
	defer s.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.sweep()
		}
	}
}

func (s *memoryStore) sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, b := range s.buckets {
		for elem := b.lru.Front(); elem != nil; {
			next := elem.Next()
			e, ok := elem.Value.(*entry)
			if ok && !e.expiresAt.After(now) {
				s.removeLocked(e)
				s.stats.Expired++
			}
			elem = next
		}
	}
}

// SetPartnerLimit implements Store.
func (s *memoryStore) SetPartnerLimit(partner string, limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bucketLocked(partner).limit = limit
}

// bucketLocked returns the partner's bucket, creating it on first use.
func (s *memoryStore) bucketLocked(partner string) *bucket {
	b, ok := s.buckets[partner]
	if !ok {
		b = &bucket{lru: list.New()}
		s.buckets[partner] = b
	}
	return b
}

// touchLocked marks an entry as recently used within its own partition.
func (s *memoryStore) touchLocked(e *entry) {
	if e.elem == nil {
		return
	}
	if b, ok := s.buckets[e.partner]; ok {
		b.lru.MoveToBack(e.elem)
	}
}

// insertLocked adds an entry to its partner partition and enforces both the
// partner cap and the store-wide cap.
func (s *memoryStore) insertLocked(e *entry) {
	b := s.bucketLocked(e.partner)
	e.elem = b.lru.PushBack(e)
	s.total++
	s.evictPartnerLocked(b)
	s.evictGlobalLocked()
}

func forwardKey(realHost, session string) string {
	return normalize(realHost) + "|" + session
}

// PutPseudonym implements Store.
func (s *memoryStore) PutPseudonym(partner, pseudonym, realHost, session string, ttl time.Duration) {
	// Diameter identities are case-insensitive, so every key is normalised on
	// the way in to match the lookup side.
	pseudonym = normalize(pseudonym)

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.pseudonyms[pseudonym]; ok {
		s.removeLocked(existing)
	}
	e := &entry{
		key:       pseudonym,
		value:     realHost,
		alias:     forwardKey(realHost, session),
		partner:   partner,
		expiresAt: s.now().Add(ttl),
	}
	s.pseudonyms[pseudonym] = e
	s.forward[e.alias] = pseudonym
	s.insertLocked(e)
}

// Pseudonym implements Store.
func (s *memoryStore) Pseudonym(realHost, session string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pseudonym, ok := s.forward[forwardKey(realHost, session)]
	if !ok {
		return "", false
	}
	e, ok := s.pseudonyms[pseudonym]
	if !ok || !e.expiresAt.After(s.now()) {
		if ok {
			s.removeLocked(e)
			s.stats.Expired++
		}
		return "", false
	}
	s.touchLocked(e)
	return pseudonym, true
}

// RealHost implements Store.
func (s *memoryStore) RealHost(pseudonym string) (string, bool) {
	pseudonym = normalize(pseudonym)

	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.pseudonyms[pseudonym]
	if !ok {
		s.stats.Misses++
		return "", false
	}
	if !e.expiresAt.After(s.now()) {
		s.removeLocked(e)
		s.stats.Expired++
		s.stats.Misses++
		return "", false
	}
	s.touchLocked(e)
	s.stats.Hits++
	return e.value, true
}

// Stats implements Store.
func (s *memoryStore) Stats() StoreStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats
	st.Pseudonyms = int64(len(s.pseudonyms))
	return st
}

// Close implements Store.
func (s *memoryStore) Close() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	s.wg.Wait()
}

// evictPartnerLocked enforces one partner's own cap. The caller must hold the
// mutex.
func (s *memoryStore) evictPartnerLocked(b *bucket) {
	if b.limit <= 0 {
		return
	}
	for b.lru.Len() > b.limit {
		if !s.evictOldestLocked(b) {
			return
		}
	}
}

// evictGlobalLocked enforces the store-wide cap. It always takes from the
// largest partner, so pressure falls on whoever is consuming the most rather
// than on whoever happens to be idle. The caller must hold the mutex.
func (s *memoryStore) evictGlobalLocked() {
	if s.maxEntries <= 0 {
		return
	}
	for s.total > s.maxEntries {
		var largest *bucket
		for _, b := range s.buckets {
			if largest == nil || b.lru.Len() > largest.lru.Len() {
				largest = b
			}
		}
		if largest == nil || !s.evictOldestLocked(largest) {
			return
		}
	}
}

// evictOldestLocked drops the least recently used entry of one partner and
// reports whether anything was removed. The caller must hold the mutex.
func (s *memoryStore) evictOldestLocked(b *bucket) bool {
	elem := b.lru.Front()
	if elem == nil {
		return false
	}
	e, ok := elem.Value.(*entry)
	if !ok {
		b.lru.Remove(elem)
		return true
	}
	s.removeLocked(e)
	s.stats.Evictions++
	return true
}

// detachLocked removes an entry from its partner partition without touching
// the lookup indexes. The caller must hold the mutex.
func (s *memoryStore) detachLocked(e *entry) {
	if e.elem == nil {
		return
	}
	if b, ok := s.buckets[e.partner]; ok {
		b.lru.Remove(e.elem)
	}
	e.elem = nil
	s.total--
}

// removeLocked deletes an entry from every index. The caller must hold the mutex.
func (s *memoryStore) removeLocked(e *entry) {
	s.detachLocked(e)
	if p, ok := s.forward[e.alias]; ok && p == e.key {
		delete(s.forward, e.alias)
	}
	delete(s.pseudonyms, e.key)
}
