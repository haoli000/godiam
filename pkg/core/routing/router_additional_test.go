// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package routing

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func requestForRealm(realm string) *message.Message {
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, types.DiamID(realm)))
	return msg
}

func TestRouteOutWithExclusionDoesNotRouteWhenEveryCandidateIsExcluded(t *testing.T) {
	router := NewRouter()
	router.AddRealmRoute("example.com", "peer1.example.com", "peer2.example.com")

	_, err := router.RouteOutWithExclusion(requestForRealm("example.com"), map[types.DiamID]struct{}{
		"peer1.example.com": {},
		"peer2.example.com": {},
	})
	if err == nil || !strings.Contains(err.Error(), "after exclusions") {
		t.Fatalf("error = %v, want no route after all peers are excluded", err)
	}
}

func TestRouteOutWithExclusionFallsBackOnlyAfterPreferredTierIsExhausted(t *testing.T) {
	router := NewRouter()
	router.AddAppRoute(types.AppIDCreditControl, "primary.example.com", "secondary.example.com")
	router.AddRealmRoute("example.com", "realm.example.com")

	msg := requestForRealm("example.com")
	peerID, err := router.RouteOutWithExclusion(msg, map[types.DiamID]struct{}{"primary.example.com": {}})
	if err != nil {
		t.Fatalf("RouteOutWithExclusion failed: %v", err)
	}
	if peerID != "secondary.example.com" {
		t.Fatalf("excluded primary should fail over within the higher app-route tier before using realm route, got %s", peerID)
	}

	peerID, err = router.RouteOutWithExclusion(msg, map[types.DiamID]struct{}{
		"primary.example.com":   {},
		"secondary.example.com": {},
	})
	if err != nil {
		t.Fatalf("RouteOutWithExclusion failed after exhausting app tier: %v", err)
	}
	if peerID != "realm.example.com" {
		t.Fatalf("exhausted app tier should fall back to realm route, got %s", peerID)
	}
}

func TestDynamicRealmPeerConfigurationDoesNotInventRoutesWithoutConnectedRealmState(t *testing.T) {
	router := NewRouter()
	router.SetDynamicRealmPeers(true)
	router.SetPeerEnumerator(func() []*peer.Peer {
		return []*peer.Peer{peer.New(peer.Config{DiameterIdentity: "dynamic.example.com", Realm: "example.com"}, nil)}
	})

	_, err := router.RouteOut(requestForRealm("example.com"))
	if err == nil || !strings.Contains(err.Error(), "no routes available") {
		t.Fatalf("RouteOut error = %v, want no route until a connected peer has learned realm state", err)
	}
}

func TestOutgoingHandlersRunByPriorityAndStopOnError(t *testing.T) {
	router := NewRouter()
	router.AddRealmRoute("example.com", "peer.example.com")
	boom := errors.New("policy refused route")
	var calls []string

	router.RegisterOutHandler("late", 20, func(_ *message.Message, candidates []Route) ([]Route, error) {
		calls = append(calls, "late")
		return candidates, nil
	})
	router.RegisterOutHandler("early", 10, func(_ *message.Message, candidates []Route) ([]Route, error) {
		calls = append(calls, "early")
		return candidates, boom
	})

	_, err := router.RouteOut(requestForRealm("example.com"))
	if !errors.Is(err, boom) {
		t.Fatalf("RouteOut error = %v, want handler error", err)
	}
	if !reflect.DeepEqual(calls, []string{"early"}) {
		t.Fatalf("handler calls = %v, want priority order with stop on error", calls)
	}
}

func TestIncomingHandlerCanConsumeMessageBeforeDispatchOrRelay(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)
	router.AddRealmRoute("remote.example.com", "relay.example.com")
	dispatched := false
	router.RegisterDispatchHandler(types.AppIDCreditControl, func(_ *peer.Peer, _ *message.Message) error {
		dispatched = true
		return nil
	})
	router.RegisterInHandler("screening-drop", 0, func(_ *peer.Peer, _ *message.Message, candidates []Route) ([]Route, error) {
		return candidates, ErrConsumed
	})

	msg := requestForRealm("remote.example.com")
	if err := router.RouteIn(nil, msg); err != nil {
		t.Fatalf("RouteIn returned consumed message as an error: %v", err)
	}
	if dispatched {
		t.Fatal("consumed message reached local dispatch")
	}
	for _, avp := range msg.AVPs {
		if avp.Code == types.AVPCodeRouteRecord {
			t.Fatal("consumed message was prepared for relay after the handler stopped processing")
		}
	}
}

func TestPostRouteHandlersObserveTheSelectedPeerAndDeregisterByOwner(t *testing.T) {
	router := NewRouter()
	router.AddRealmRoute("example.com", "peer.example.com")
	var seen []types.DiamID
	router.RegisterPostRouteHandler("audit", func(_ *message.Message, selected types.DiamID) {
		seen = append(seen, selected)
	})
	router.RegisterPostRouteHandlerWithOwner("owned", "extension-a", func(_ *message.Message, selected types.DiamID) {
		seen = append(seen, "owned:"+selected)
	})

	if _, err := router.RouteOut(requestForRealm("example.com")); err != nil {
		t.Fatalf("RouteOut failed: %v", err)
	}
	if want := []types.DiamID{"peer.example.com", "owned:peer.example.com"}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("post-route observations = %v, want %v", seen, want)
	}

	seen = nil
	router.DeregisterAllByOwner("extension-a")
	if _, err := router.RouteOut(requestForRealm("example.com")); err != nil {
		t.Fatalf("RouteOut failed: %v", err)
	}
	if want := []types.DiamID{"peer.example.com"}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("post-route observations after owner removal = %v, want %v", seen, want)
	}
}

func TestLookupPeerAndRelayOriginReportOnlyConfiguredState(t *testing.T) {
	router := NewRouter()
	if obj, ok := router.LookupPeer("peer.example.com"); ok || obj != nil {
		t.Fatalf("LookupPeer without lookup function = (%v, %v), want nil false", obj, ok)
	}

	sent := &capturingPeer{}
	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		if identity == "peer.example.com" {
			return sent, true
		}
		return nil, false
	})
	if obj, ok := router.LookupPeer("peer.example.com"); !ok || obj != sent {
		t.Fatalf("LookupPeer configured result = (%v, %v), want capturing peer true", obj, ok)
	}

	router.prevHopByHop.Store(relayKey{OutboundPeer: "out.example.com", HBHID: 7}, relayTxn{OriginalPeer: "origin.example.com"})
	router.prevHopByHop.Store(relayKey{OutboundPeer: "out.example.com", HBHID: 8}, "corrupt")
	if got, ok := router.RelayOrigin("out.example.com", 7); !ok || got != "origin.example.com" {
		t.Fatalf("RelayOrigin valid txn = (%q, %v)", got, ok)
	}
	if got, ok := router.RelayOrigin("out.example.com", 8); ok || got != "" {
		t.Fatalf("RelayOrigin corrupt txn = (%q, %v), want empty false", got, ok)
	}
	if got, ok := router.RelayOrigin("out.example.com", 9); ok || got != "" {
		t.Fatalf("RelayOrigin missing txn = (%q, %v), want empty false", got, ok)
	}
	// The same ID on a different connection is a different transaction.
	if got, ok := router.RelayOrigin("other.example.com", 7); ok || got != "" {
		t.Fatalf("RelayOrigin wrong peer = (%q, %v), want empty false", got, ok)
	}
}

func TestWildcardRealmRoutesAreCurrentlyExactKeys(t *testing.T) {
	router := NewRouter()
	router.AddRealmRoute("*.example.com", "wildcard.example.com")

	_, err := router.RouteOut(requestForRealm("ims.example.com"))
	if err == nil {
		t.Fatal("wildcard realm route unexpectedly matched a suffix; update this test when wildcard routing is implemented")
	}
	if !strings.Contains(err.Error(), "no routes available") {
		t.Fatalf("wildcard miss error = %v", err)
	}
}
