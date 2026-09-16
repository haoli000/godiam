// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dea_ratelimit

import (
	"hash/maphash"
	"sync"
	"time"
)

// Limiter decides whether a message may pass for a given bucket key.
//
// Implementations must be safe for concurrent use. The sharded in-memory
// implementation below is the default; a distributed limiter can be
// substituted without touching the extension logic.
type Limiter interface {
	// Allow consumes one token from the bucket identified by key, creating it
	// with the given rate and burst when it does not exist yet.
	Allow(key string, rate float64, burst int) bool
	// Utilization reports the fraction of the burst currently consumed for key,
	// in the range [0,1].
	Utilization(key string) (float64, bool)
	// Len reports how many buckets are held.
	Len() int
	// Close releases any resources held by the limiter.
	Close()
}

const (
	// shardCount keeps lock contention low on the hot path. It must be a power
	// of two so the shard index can be masked.
	shardCount = 64
	shardMask  = shardCount - 1
	// idleBucketTTL is how long an unused bucket is kept before reclaiming it.
	idleBucketTTL = 10 * time.Minute
)

type bucket struct {
	tokens   float64
	burst    float64
	rate     float64
	lastFill time.Time
	lastUsed time.Time
}

type shard struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

// tokenLimiter is a sharded token-bucket Limiter.
type tokenLimiter struct {
	shards [shardCount]shard
	seed   maphash.Seed
	now    func() time.Time

	stopCh chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
}

// newTokenLimiter creates a limiter. When sweepInterval is positive, idle
// buckets are reclaimed periodically.
func newTokenLimiter(sweepInterval time.Duration, now func() time.Time) *tokenLimiter {
	if now == nil {
		now = time.Now
	}
	l := &tokenLimiter{seed: maphash.MakeSeed(), now: now, stopCh: make(chan struct{})}
	for i := range l.shards {
		l.shards[i].buckets = make(map[string]*bucket)
	}
	if sweepInterval > 0 {
		l.wg.Add(1)
		go l.sweepLoop(sweepInterval)
	}
	return l
}

func (l *tokenLimiter) sweepLoop(interval time.Duration) {
	defer l.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.sweep()
		}
	}
}

func (l *tokenLimiter) sweep() {
	cutoff := l.now().Add(-idleBucketTTL)
	for i := range l.shards {
		s := &l.shards[i]
		s.mu.Lock()
		for key, b := range s.buckets {
			if b.lastUsed.Before(cutoff) {
				delete(s.buckets, key)
			}
		}
		s.mu.Unlock()
	}
}

func (l *tokenLimiter) shardFor(key string) *shard {
	var h maphash.Hash
	h.SetSeed(l.seed)
	_, _ = h.WriteString(key)
	return &l.shards[h.Sum64()&shardMask]
}

// Allow implements Limiter.
func (l *tokenLimiter) Allow(key string, rate float64, burst int) bool {
	if rate <= 0 {
		return true // an unset rate means unlimited
	}
	capacity := float64(burst)
	if capacity <= 0 {
		capacity = rate
	}

	s := l.shardFor(key)
	now := l.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[key]
	if !ok {
		b = &bucket{tokens: capacity, burst: capacity, rate: rate, lastFill: now}
		s.buckets[key] = b
	}
	if b.rate != rate || b.burst != capacity {
		// The policy was reconfigured; rescale the bucket.
		if capacity < b.tokens {
			b.tokens = capacity
		}
		b.rate, b.burst = rate, capacity
	}

	if elapsed := now.Sub(b.lastFill); elapsed > 0 {
		b.tokens += elapsed.Seconds() * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.lastFill = now
	}
	b.lastUsed = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Utilization implements Limiter.
func (l *tokenLimiter) Utilization(key string) (float64, bool) {
	s := l.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[key]
	if !ok || b.burst <= 0 {
		return 0, false
	}
	used := (b.burst - b.tokens) / b.burst
	if used < 0 {
		used = 0
	}
	if used > 1 {
		used = 1
	}
	return used, true
}

// Len implements Limiter.
func (l *tokenLimiter) Len() int {
	total := 0
	for i := range l.shards {
		l.shards[i].mu.Lock()
		total += len(l.shards[i].buckets)
		l.shards[i].mu.Unlock()
	}
	return total
}

// Close implements Limiter.
func (l *tokenLimiter) Close() {
	l.once.Do(func() { close(l.stopCh) })
	l.wg.Wait()
}
