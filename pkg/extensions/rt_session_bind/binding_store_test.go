// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_session_bind

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBindingStore_SetGet(t *testing.T) {
	s := newBindingStore(time.Hour, 100)
	s.Set("key1", "peer1.example.com")

	peer, ok := s.Get("key1")
	if !ok {
		t.Fatal("expected binding to exist")
	}
	if peer != "peer1.example.com" {
		t.Fatalf("expected peer1.example.com, got %s", peer)
	}
}

func TestBindingStore_GetMissing(t *testing.T) {
	s := newBindingStore(time.Hour, 100)

	_, ok := s.Get("nonexistent")
	if ok {
		t.Fatal("expected no binding for nonexistent key")
	}
}

func TestBindingStore_Update(t *testing.T) {
	s := newBindingStore(time.Hour, 100)
	s.Set("key1", "peer1.example.com")
	s.Set("key1", "peer2.example.com")

	peer, ok := s.Get("key1")
	if !ok {
		t.Fatal("expected binding to exist")
	}
	if peer != "peer2.example.com" {
		t.Fatalf("expected peer2.example.com, got %s", peer)
	}
	if s.Len() != 1 {
		t.Fatalf("expected 1 binding, got %d", s.Len())
	}
}

func TestBindingStore_Expiration(t *testing.T) {
	s := newBindingStore(50*time.Millisecond, 100)
	s.Set("key1", "peer1.example.com")

	// Should exist immediately
	if _, ok := s.Get("key1"); !ok {
		t.Fatal("expected binding to exist before expiration")
	}

	// Wait well past expiration (4x TTL)
	time.Sleep(200 * time.Millisecond)

	// Should be expired
	if _, ok := s.Get("key1"); ok {
		t.Fatal("expected binding to be expired")
	}
}

func TestBindingStore_Touch(t *testing.T) {
	s := newBindingStore(200*time.Millisecond, 100)
	s.Set("key1", "peer1.example.com")

	time.Sleep(120 * time.Millisecond)
	s.Touch("key1")

	time.Sleep(120 * time.Millisecond)
	// Should still be alive because Touch refreshed the TTL
	if _, ok := s.Get("key1"); !ok {
		t.Fatal("expected binding to still exist after Touch")
	}

	time.Sleep(200 * time.Millisecond)
	// Now it should be expired
	if _, ok := s.Get("key1"); ok {
		t.Fatal("expected binding to be expired after full TTL")
	}
}

func TestBindingStore_MaxSize(t *testing.T) {
	s := newBindingStore(time.Hour, 3)
	s.Set("key1", "peer1")
	s.Set("key2", "peer2")
	s.Set("key3", "peer3")

	if s.Len() != 3 {
		t.Fatalf("expected 3 bindings, got %d", s.Len())
	}

	// Adding a 4th should evict the oldest (key1)
	s.Set("key4", "peer4")
	if s.Len() != 3 {
		t.Fatalf("expected 3 bindings after eviction, got %d", s.Len())
	}
	if _, ok := s.Get("key1"); ok {
		t.Fatal("expected key1 to be evicted")
	}
	if _, ok := s.Get("key4"); !ok {
		t.Fatal("expected key4 to exist")
	}
}

func TestBindingStore_RemoveByPeer(t *testing.T) {
	s := newBindingStore(time.Hour, 100)
	s.Set("key1", "peer1")
	s.Set("key2", "peer1")
	s.Set("key3", "peer2")

	s.RemoveByPeer("peer1")

	if s.Len() != 1 {
		t.Fatalf("expected 1 binding after RemoveByPeer, got %d", s.Len())
	}
	if _, ok := s.Get("key1"); ok {
		t.Fatal("expected key1 to be removed")
	}
	if _, ok := s.Get("key2"); ok {
		t.Fatal("expected key2 to be removed")
	}
	if _, ok := s.Get("key3"); !ok {
		t.Fatal("expected key3 to still exist")
	}
}

func TestBindingStore_Remove(t *testing.T) {
	s := newBindingStore(time.Hour, 100)
	s.Set("key1", "peer1")
	s.Remove("key1")

	if _, ok := s.Get("key1"); ok {
		t.Fatal("expected key1 to be removed")
	}
	if s.Len() != 0 {
		t.Fatalf("expected 0 bindings, got %d", s.Len())
	}
}

func TestBindingStore_Cleanup(t *testing.T) {
	s := newBindingStore(50*time.Millisecond, 100)
	s.Set("key1", "peer1")
	s.Set("key2", "peer2")
	s.Set("key3", "peer3")

	// Wait well past expiration (4x TTL)
	time.Sleep(200 * time.Millisecond)

	removed := s.Cleanup()
	if removed != 3 {
		t.Fatalf("expected 3 removed, got %d", removed)
	}
	if s.Len() != 0 {
		t.Fatalf("expected 0 bindings after cleanup, got %d", s.Len())
	}
}

func TestBindingStore_CleanupPartial(t *testing.T) {
	s := newBindingStore(50*time.Millisecond, 100)
	s.Set("key1", "peer1")

	// Wait well past expiration
	time.Sleep(200 * time.Millisecond)
	// key1 is now expired; add key2 which is fresh
	s.Set("key2", "peer2")

	removed := s.Cleanup()
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	if s.Len() != 1 {
		t.Fatalf("expected 1 binding remaining, got %d", s.Len())
	}
	if _, ok := s.Get("key2"); !ok {
		t.Fatal("expected key2 to still exist")
	}
}

func TestBindingStore_Concurrent(t *testing.T) {
	s := newBindingStore(time.Hour, 1000)
	var wg sync.WaitGroup

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				s.Set(key, "peer1")
			}
		}(i)
	}

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				s.Get(key)
			}
		}(i)
	}

	// Cleaners
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Cleanup()
		}()
	}

	wg.Wait()

	// After all writers complete, verify store is consistent
	if s.Len() == 0 {
		t.Error("expected some bindings to exist after concurrent writes")
	}
}

func TestBindingStoreLookupCounters(t *testing.T) {
	s := newBindingStore(time.Hour, 100)
	s.Set("key1", "peer1.example.com")

	// Hit
	_, found := s.Get("key1")
	if !found {
		t.Fatal("expected to find key1")
	}
	// Miss
	_, found = s.Get("key2")
	if found {
		t.Fatal("expected key2 not found")
	}

	if s.lookups.Load() != 2 {
		t.Errorf("expected 2 lookups, got %d", s.lookups.Load())
	}
	if s.hits.Load() != 1 {
		t.Errorf("expected 1 hit, got %d", s.hits.Load())
	}
	if s.misses.Load() != 1 {
		t.Errorf("expected 1 miss, got %d", s.misses.Load())
	}
}

func TestBindingStoreEvictionCounter(t *testing.T) {
	s := newBindingStore(time.Hour, 2)
	s.Set("key1", "peer1")
	s.Set("key2", "peer2")
	s.Set("key3", "peer3") // triggers eviction

	if s.evictions.Load() != 1 {
		t.Errorf("expected 1 eviction, got %d", s.evictions.Load())
	}
}

func TestBindingStoreCleanupCounter(t *testing.T) {
	s := newBindingStore(time.Millisecond, 100)
	s.Set("key1", "peer1")
	s.Set("key2", "peer2")

	// Wait well past expiration (10x TTL)
	time.Sleep(10 * time.Millisecond)
	removed := s.Cleanup()

	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}
	if s.cleanups.Load() != 2 {
		t.Errorf("expected cleanups counter=2, got %d", s.cleanups.Load())
	}
}

func TestBindingStoreExpiredMissCounter(t *testing.T) {
	s := newBindingStore(time.Millisecond, 100)
	s.Set("key1", "peer1")
	// Wait well past expiration (10x TTL)
	time.Sleep(10 * time.Millisecond)

	_, found := s.Get("key1")
	if found {
		t.Fatal("expected expired key to be not found")
	}
	if s.lookups.Load() != 1 {
		t.Errorf("expected 1 lookup, got %d", s.lookups.Load())
	}
	if s.misses.Load() != 1 {
		t.Errorf("expected 1 miss for expired key, got %d", s.misses.Load())
	}
}
