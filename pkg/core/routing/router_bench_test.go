// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package routing

import (
	"testing"

	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func BenchmarkRouterRouteOut(b *testing.B) {
	router := NewRouter()
	router.AddRealmRoute("example.com", "peer1.example.com", "peer2.example.com")
	router.AddAppRoute(types.AppIDCreditControl, "ocs1.example.com", "ocs2.example.com")

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = router.RouteOut(msg)
	}
}

func BenchmarkRouterRouteIn(b *testing.B) {
	router := NewRouter()
	router.SetLocalIdentity("server.example.com", "example.com")

	// Register a dummy dispatch handler
	router.RegisterDispatchHandler(types.AppIDCreditControl, func(_ *peer.Peer, _ *message.Message) error {
		return nil
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "server.example.com"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = router.RouteIn(nil, msg)
	}
}
