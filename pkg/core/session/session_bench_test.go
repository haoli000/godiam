// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package session

import (
	"testing"

	"github.com/haoli000/godiam/pkg/proto/types"
)

func BenchmarkSessionCreate(b *testing.B) {
	mgr := NewManager()
	originHost := types.DiamID("server.example.com")
	appID := types.AppIDCommon

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.Create(originHost, appID)
	}
}

func BenchmarkSessionLookup(b *testing.B) {
	mgr := NewManager()
	originHost := types.DiamID("server.example.com")
	appID := types.AppIDCommon

	// Pre-create some sessions
	ids := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		s := mgr.Create(originHost, appID)
		ids[i] = s.ID()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mgr.Get(ids[i%1000])
	}
}

func BenchmarkSessionRemove(b *testing.B) {
	mgr := NewManager()
	originHost := types.DiamID("server.example.com")
	appID := types.AppIDCommon

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := mgr.Create(originHost, appID)
		mgr.Remove(s.ID())
	}
}

func BenchmarkSessionScale100k(b *testing.B) {
	mgr := NewManager()
	originHost := types.DiamID("server.example.com")
	appID := types.AppIDCommon

	// Create 100k sessions
	for i := 0; i < 100000; i++ {
		_ = mgr.Create(originHost, appID)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.Count()
	}
}

func BenchmarkSessionParallel(b *testing.B) {
	mgr := NewManager()
	originHost := types.DiamID("server.example.com")
	appID := types.AppIDCommon

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s := mgr.Create(originHost, appID)
			mgr.Get(s.ID())
			mgr.Remove(s.ID())
		}
	})
}
