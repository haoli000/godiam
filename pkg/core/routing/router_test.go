// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package routing

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func TestRouterRealmRouting(t *testing.T) {
	router := NewRouter()

	// Add realm routes
	router.AddRealmRoute("example.com", "peer1.example.com", "peer2.example.com")
	router.AddRealmRoute("other.com", "peer.other.com")

	// Create a message with destination realm
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	// Route the message
	peer, err := router.RouteOut(msg)
	if err != nil {
		t.Fatalf("RouteOut failed: %v", err)
	}

	// With equal scoring, selection may be any of the realm peers
	if peer != "peer1.example.com" && peer != "peer2.example.com" {
		t.Errorf("expected one of [peer1.example.com, peer2.example.com], got %s", peer)
	}
}

func TestRouterHostRouting(t *testing.T) {
	router := NewRouter()

	// Add host route
	router.AddHostRoute("specific.example.com", "direct-peer.example.com")
	router.AddRealmRoute("example.com", "realm-peer.example.com")

	// Create a message with destination host
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "specific.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	// Route the message - host route should take precedence
	peer, err := router.RouteOut(msg)
	if err != nil {
		t.Fatalf("RouteOut failed: %v", err)
	}

	if peer != "direct-peer.example.com" {
		t.Errorf("expected direct-peer.example.com (host route), got %s", peer)
	}
}

func TestRouterDefaultRealm(t *testing.T) {
	router := NewRouter()

	router.SetDefaultRealm("default.com")
	router.AddRealmRoute("default.com", "default-peer.example.com")

	// Create a message without destination realm
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)

	// Route should use default realm
	peer, err := router.RouteOut(msg)
	if err != nil {
		t.Fatalf("RouteOut failed: %v", err)
	}

	if peer != "default-peer.example.com" {
		t.Errorf("expected default-peer.example.com, got %s", peer)
	}
}

func TestRouterNoRoute(t *testing.T) {
	router := NewRouter()

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "unknown.com"))

	_, err := router.RouteOut(msg)
	if err == nil {
		t.Error("expected error for no available routes")
	}
}

func TestRouterDispatch(t *testing.T) {
	router := NewRouter()

	var receivedPeer *peer.Peer
	var receivedMsg *message.Message
	router.RegisterDispatchHandler(types.AppIDCreditControl, func(p *peer.Peer, msg *message.Message) error {
		receivedPeer = p
		receivedMsg = msg
		return nil
	})

	testPeer := peer.New(peer.Config{DiameterIdentity: "test.peer"}, nil)
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)

	err := router.Dispatch(testPeer, msg)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if receivedMsg == nil {
		t.Fatal("dispatch handler was not called")
	}
	if receivedPeer != testPeer {
		t.Error("dispatch handler received wrong peer")
	}
	if receivedMsg != msg {
		t.Error("dispatch handler received wrong message")
	}
}

func TestRouteInLocalNoHandlerDoesNotForward(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)

	forwarded := false
	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		if identity == "relay-peer" {
			return &trackingPeer{called: &forwarded}, true
		}
		return nil, false
	})

	// A message explicitly targeted at the local host, but no dispatch handler is registered.
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "local.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	err := router.RouteIn(nil, msg)
	if err == nil {
		t.Fatalf("expected dispatch error, got nil")
	}
	var derr *DispatchError
	if !errors.As(err, &derr) {
		t.Fatalf("expected DispatchError, got %v", err)
	}
	if derr.Code != types.ResultApplicationUnsupported {
		t.Fatalf("expected result code %d, got %d", types.ResultApplicationUnsupported, derr.Code)
	}
	if !strings.Contains(err.Error(), "no handler") {
		t.Fatalf("unexpected error: %v", err)
	}

	if forwarded {
		t.Fatalf("message was forwarded despite being targeted at local host with no handler")
	}

	for _, avp := range msg.AVPs {
		if avp.Code == types.AVPCodeRouteRecord {
			t.Fatalf("Route-Record was added even though message should not have been forwarded")
		}
	}
}

func TestRouterRoutingHandler(t *testing.T) {
	router := NewRouter()

	router.AddRealmRoute("example.com", "peer1.example.com", "peer2.example.com")

	// Register a handler that filters candidates
	router.RegisterOutHandler("test-filter", 10, func(_ *message.Message, candidates []Route) ([]Route, error) {
		// Only keep peer2
		var filtered []Route
		for _, c := range candidates {
			if c.PeerIdentity == "peer2.example.com" {
				filtered = append(filtered, c)
			}
		}
		return filtered, nil
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	peer, err := router.RouteOut(msg)
	if err != nil {
		t.Fatalf("RouteOut failed: %v", err)
	}

	if peer != "peer2.example.com" {
		t.Errorf("expected peer2.example.com (filtered by handler), got %s", peer)
	}
}

func TestRoutingTableInfo(t *testing.T) {
	router := NewRouter()

	router.SetDefaultRealm("default.com")
	router.AddRealmRoute("example.com", "peer1", "peer2")
	router.AddHostRoute("host.example.com", "direct-peer")

	info := router.RoutingTable()

	if info.DefaultRealm != "default.com" {
		t.Errorf("expected default realm 'default.com', got %s", info.DefaultRealm)
	}

	if len(info.RealmRoutes["example.com"]) != 2 {
		t.Errorf("expected 2 peers for example.com, got %d", len(info.RealmRoutes["example.com"]))
	}

	if info.HostRoutes["host.example.com"] != "direct-peer" {
		t.Errorf("expected host route to direct-peer, got %s", info.HostRoutes["host.example.com"])
	}
}
func TestRouterAppRouting(t *testing.T) {
	router := NewRouter()

	router.AddAppRoute(types.AppIDCreditControl, "app-peer.example.com")
	router.AddRealmRoute("example.com", "realm-peer.example.com")

	// Message for an app with a specific route
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	peer, err := router.RouteOut(msg)
	if err != nil {
		t.Fatalf("RouteOut failed: %v", err)
	}

	// App route (score 200) should take precedence over realm route (score 100)
	if peer != "app-peer.example.com" {
		t.Errorf("expected app-peer.example.com, got %s", peer)
	}
}

func TestRouterRelay(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)
	router.AddHostRoute("remote.host.com", "relay-peer")

	// Mock peer lookup
	lookupCalled := false
	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		lookupCalled = true
		if identity == "relay-peer" {
			return &mockPeer{}, true
		}
		return nil, false
	})

	// Message for a remote host
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "remote.host.com"))

	err := router.RouteIn(nil, msg)
	if err != nil {
		t.Fatalf("RouteIn (relay) failed: %v", err)
	}

	if !lookupCalled {
		t.Error("peer lookup was not called for relay")
	}

	// Verify Route-Record was added
	found := false
	for _, avp := range msg.AVPs {
		if avp.Code == types.AVPCodeRouteRecord {
			found = true
			// AVP data is encoded by message.NewDiameterIdentityAVP, simplified check
			break
		}
	}
	if !found {
		t.Error("Route-Record AVP not found in relayed message")
	}
}

func TestRouterRelayAddsProxyInfo(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)
	router.AddHostRoute("remote.host.com", "relay-peer")

	outbound := &capturingPeer{}
	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		if identity == "relay-peer" {
			return outbound, true
		}
		return nil, false
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.SetProxiable(true)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "remote.host.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	// Pre-existing Proxy-Info should be preserved and a new one appended
	msg.AddAVP(message.NewGroupedAVP(types.AVPCodeProxyInfo, types.AVPFlagMandatory, 0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "downstream-proxy"),
		message.NewOctetStringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, []byte("state")),
	))

	if err := router.RouteIn(nil, msg); err != nil {
		t.Fatalf("RouteIn (relay) failed: %v", err)
	}

	if !outbound.called {
		t.Fatalf("expected outbound peer to be invoked")
	}

	proxyInfos := outbound.msg.FindAllAVPs(types.AVPCodeProxyInfo, 0)
	if len(proxyInfos) != 2 {
		t.Fatalf("expected 2 Proxy-Info AVPs (existing + added), got %d", len(proxyInfos))
	}

	added := proxyInfos[1]
	if host := added.FindChild(types.AVPCodeProxyHost, 0); host == nil || host.GetDiameterIdentity() != "local.example.com" {
		t.Fatalf("Proxy-Host missing or unexpected: %v", host)
	}
	if state := added.FindChild(types.AVPCodeProxyState, 0); state == nil || len(state.Data) == 0 {
		t.Fatalf("Proxy-State missing or empty")
	}
}

func TestRouterLoopDetectedReturnsErrorAnswer(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.SetProxiable(true)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "remote.host.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "remote.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, "local.example.com"))

	err := router.RouteIn(nil, msg)
	if err == nil {
		t.Fatalf("expected loop detection error, got nil")
	}

	var rerr *RoutingError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected RoutingError, got %v", err)
	}
	if rerr.Code != types.ResultLoopDetected {
		t.Fatalf("expected result code %d, got %d", types.ResultLoopDetected, rerr.Code)
	}
	if rerr.Answer == nil {
		t.Fatalf("expected error answer to be generated")
	}
	if rerr.Answer.IsRequest() {
		t.Fatalf("generated message is a request, expected answer")
	}
	if rc, ok := rerr.Answer.GetResultCode(); !ok || rc != types.ResultLoopDetected {
		t.Fatalf("answer missing or incorrect Result-Code: %v", rc)
	}
	if !rerr.Answer.IsError() {
		t.Fatalf("answer should have E flag set")
	}

	if rrCount := len(msg.FindAllAVPs(types.AVPCodeRouteRecord, 0)); rrCount != 1 {
		t.Fatalf("expected no additional Route-Record AVPs, got %d", rrCount)
	}
}

func TestRouterUnableToDeliverReturnsErrorAnswer(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)

	lookupCalled := false
	router.SetPeerLookup(func(_ types.DiamID) (interface{}, bool) {
		lookupCalled = true
		return nil, false
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.SetProxiable(true)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "remote.example.com"))

	err := router.RouteIn(nil, msg)
	if err == nil {
		t.Fatalf("expected unable-to-deliver error, got nil")
	}

	var rerr *RoutingError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected RoutingError, got %v", err)
	}
	if rerr.Code != types.ResultUnableToDeliver {
		t.Fatalf("expected result code %d, got %d", types.ResultUnableToDeliver, rerr.Code)
	}
	if rerr.Answer == nil {
		t.Fatalf("expected error answer to be generated")
	}
	if rerr.Answer.IsRequest() {
		t.Fatalf("generated message is a request, expected answer")
	}
	if rc, ok := rerr.Answer.GetResultCode(); !ok || rc != types.ResultUnableToDeliver {
		t.Fatalf("answer missing or incorrect Result-Code: %v", rc)
	}
	if !rerr.Answer.IsError() {
		t.Fatalf("answer should have E flag set")
	}
	if lookupCalled {
		t.Fatalf("peer lookup should not be called when no route candidates exist")
	}
}

func TestRouterSetsErrorReportingHostOnLocalErrors(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)

	lookupCalled := false
	router.SetPeerLookup(func(_ types.DiamID) (interface{}, bool) {
		lookupCalled = true
		return nil, false
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.SetProxiable(true)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "remote.example.com"))

	err := router.RouteIn(nil, msg)
	if err == nil {
		t.Fatalf("expected unable-to-deliver error, got nil")
	}

	var rerr *RoutingError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected RoutingError, got %v", err)
	}

	erh := rerr.Answer.FindAVP(types.AVPCodeErrorReportingHost, 0)
	if erh == nil {
		t.Fatalf("Error-Reporting-Host missing in local error answer")
	}
	if erh.GetDiameterIdentity() != "local.example.com" {
		t.Fatalf("Error-Reporting-Host = %s, want local.example.com", erh.GetDiameterIdentity())
	}
	if !rerr.Answer.IsError() {
		t.Fatalf("answer should have E flag set")
	}
	if lookupCalled {
		t.Fatalf("peer lookup should not be called when no route candidates exist")
	}
}

func TestRouterSendFailureReturnsUnableToDeliver(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)
	router.AddHostRoute("remote.host.com", "relay-peer")

	failing := &failingPeer{}
	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		if identity == "relay-peer" {
			return failing, true
		}
		return nil, false
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.SetProxiable(true)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "remote.host.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	err := router.RouteIn(nil, msg)
	if err == nil {
		t.Fatalf("expected unable-to-deliver error, got nil")
	}

	var rerr *RoutingError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected RoutingError, got %v", err)
	}
	if rerr.Code != types.ResultUnableToDeliver {
		t.Fatalf("expected code %d, got %d", types.ResultUnableToDeliver, rerr.Code)
	}
	if rerr.Answer == nil || rerr.Answer.IsRequest() {
		t.Fatalf("expected error answer")
	}
	if erh := rerr.Answer.FindAVP(types.AVPCodeErrorReportingHost, 0); erh == nil {
		t.Fatalf("Error-Reporting-Host missing")
	}
}

func TestRouterRetryOnSendFailure(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)
	router.AddAppRoute(types.AppIDCreditControl, "fail-peer")
	router.AddRealmRoute("example.com", "ok-peer")

	failing := &failingPeer{}
	success := &capturingPeer{}
	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		switch identity {
		case "fail-peer":
			return failing, true
		case "ok-peer":
			return success, true
		default:
			return nil, false
		}
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.SetProxiable(true)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	if err := router.RouteIn(nil, msg); err != nil {
		t.Fatalf("RouteIn (relay) failed: %v", err)
	}

	if failing.calls != 1 {
		t.Fatalf("expected failing peer to be tried once, got %d", failing.calls)
	}
	if !success.called {
		t.Fatalf("expected message to be sent via alternate peer")
	}
	if success.calls != 1 {
		t.Fatalf("expected alternate peer to be tried once, got %d", success.calls)
	}
}

func TestRouterRetryExhaustsAllPeers(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)
	router.AddAppRoute(types.AppIDCreditControl, "fail-peer-1")
	router.AddRealmRoute("example.com", "fail-peer-2")

	fail1 := &failingPeer{}
	fail2 := &failingPeer{}
	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		switch identity {
		case "fail-peer-1":
			return fail1, true
		case "fail-peer-2":
			return fail2, true
		default:
			return nil, false
		}
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.SetProxiable(true)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))

	err := router.RouteIn(nil, msg)
	if err == nil {
		t.Fatalf("expected unable-to-deliver error")
	}

	var rerr *RoutingError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected RoutingError, got %v", err)
	}
	if rerr.Code != types.ResultUnableToDeliver {
		t.Fatalf("expected code %d, got %d", types.ResultUnableToDeliver, rerr.Code)
	}
	if fail1.calls != 1 || fail2.calls != 1 {
		t.Fatalf("expected both peers to be tried once, got fail1=%d fail2=%d", fail1.calls, fail2.calls)
	}
}

func TestRouterRelayDisabledReturnsErrorAnswer(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(false)

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.SetProxiable(true)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "remote.example.com"))

	err := router.RouteIn(nil, msg)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var rerr *RoutingError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected RoutingError, got %v", err)
	}
	if rerr.Code != types.ResultUnableToDeliver {
		t.Fatalf("expected code %d, got %d", types.ResultUnableToDeliver, rerr.Code)
	}
	if rerr.Answer == nil || rerr.Answer.IsRequest() {
		t.Fatalf("expected error answer")
	}
	if erh := rerr.Answer.FindAVP(types.AVPCodeErrorReportingHost, 0); erh == nil {
		t.Fatalf("Error-Reporting-Host missing")
	}
}

// --- Stats tests ---

func TestStatsDispatchIncrementsCounters(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")

	router.RegisterDispatchHandler(types.AppIDCreditControl, func(_ *peer.Peer, _ *message.Message) error {
		return nil
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "local.example.com"))

	if err := router.RouteIn(nil, msg); err != nil {
		t.Fatalf("RouteIn failed: %v", err)
	}

	routed, _, dispatched, _, _, _ := router.Stats()
	if routed != 1 {
		t.Errorf("expected RequestsRouted=1, got %d", routed)
	}
	if dispatched != 1 {
		t.Errorf("expected RequestsDispatched=1, got %d", dispatched)
	}
}

func TestStatsRelayIncrementsCounters(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)
	router.AddHostRoute("remote.host.com", "relay-peer")

	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		if identity == "relay-peer" {
			return &mockPeer{}, true
		}
		return nil, false
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "remote.host.com"))

	if err := router.RouteIn(nil, msg); err != nil {
		t.Fatalf("RouteIn failed: %v", err)
	}

	routed, relayed, _, _, _, _ := router.Stats()
	if routed != 1 {
		t.Errorf("expected RequestsRouted=1, got %d", routed)
	}
	if relayed != 1 {
		t.Errorf("expected RequestsRelayed=1, got %d", relayed)
	}
}

func TestStatsLoopDetectionIncrementsCounters(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "remote.host.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, "local.example.com"))

	_ = router.RouteIn(nil, msg)

	_, _, _, errors, loops, _ := router.Stats()
	if loops != 1 {
		t.Errorf("expected LoopDetections=1, got %d", loops)
	}
	if errors != 1 {
		t.Errorf("expected RoutingErrors=1, got %d", errors)
	}
}

func TestStatsErrorIncrementsCounters(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)

	router.SetPeerLookup(func(_ types.DiamID) (interface{}, bool) {
		return nil, false
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "remote.example.com"))

	_ = router.RouteIn(nil, msg)

	_, _, _, errors, _, _ := router.Stats()
	if errors != 1 {
		t.Errorf("expected RoutingErrors=1, got %d", errors)
	}
}

func TestStatsAnswerRelayIncrementsCounters(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)
	router.AddHostRoute("remote.host.com", "relay-peer")

	inboundPeer := &capturingPeer{}
	outboundPeer := &mockPeer{}
	// Note: peer.New does not set DiameterID (only set during CER exchange),
	// so DiameterID() returns "" for unstarted peers. The peerLookup must
	// handle "" to find the inbound peer for answer relay.
	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		switch identity {
		case "relay-peer":
			return outboundPeer, true
		case "":
			return inboundPeer, true
		}
		return nil, false
	})

	// Simulate the relay: first relay a request to store the prevHopByHop entry
	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "remote.host.com"))

	// Create a mock inbound peer for relay bookkeeping (DiameterID() returns "")
	inPeer := peer.New(peer.Config{DiameterIdentity: "client.example.com"}, nil)

	if err := router.RouteIn(inPeer, req); err != nil {
		t.Fatalf("RouteIn request failed: %v", err)
	}

	// Now simulate the answer coming back with the new HBH ID. It arrives on
	// the connection the request went out on, which is part of the relay key:
	// a hop-by-hop ID is unique only per connection.
	ans := message.NewAnswer(req)
	ans.HopByHopID = req.HopByHopID // This was rewritten by relay
	answeringPeer := peer.New(peer.Config{DiameterIdentity: "relay-peer"}, nil)

	if err := router.RouteIn(answeringPeer, ans); err != nil {
		t.Fatalf("RouteIn answer failed: %v", err)
	}

	_, _, _, _, _, answersRelayed := router.Stats()
	if answersRelayed != 1 {
		t.Errorf("expected AnswersRelayed=1, got %d", answersRelayed)
	}
}

func TestStatsInitiallyZero(t *testing.T) {
	router := NewRouter()
	routed, relayed, dispatched, errors, loops, answersRelayed := router.Stats()
	if routed != 0 || relayed != 0 || dispatched != 0 || errors != 0 || loops != 0 || answersRelayed != 0 {
		t.Errorf("expected all stats to be 0, got routed=%d relayed=%d dispatched=%d errors=%d loops=%d answers=%d",
			routed, relayed, dispatched, errors, loops, answersRelayed)
	}
}

// --- Mock types ---

type mockPeer struct{}

func (p *mockPeer) Send(_ *message.Message) error {
	return nil
}

type trackingPeer struct {
	called *bool
}

func (p *trackingPeer) Send(_ *message.Message) error {
	if p.called != nil {
		*p.called = true
	}
	return nil
}

type capturingPeer struct {
	called bool
	calls  int
	msg    *message.Message
}

func (p *capturingPeer) Send(msg *message.Message) error {
	p.called = true
	p.calls++
	p.msg = msg
	return nil
}

type failingPeer struct {
	calls int
}

func (p *failingPeer) Send(_ *message.Message) error {
	p.calls++
	return errors.New("send failed")
}

// --- Deregistration tests ---

func TestDeregisterInHandler(t *testing.T) {
	router := NewRouter()

	called := false
	router.RegisterInHandler("test-in", 10, func(_ *peer.Peer, _ *message.Message, candidates []Route) ([]Route, error) {
		called = true
		return candidates, nil
	})

	ok := router.DeregisterInHandler("test-in")
	if !ok {
		t.Fatal("DeregisterInHandler returned false")
	}

	// Verify handler no longer runs by dispatching a local message
	router.SetLocalIdentity("local.example.com", "example.com")
	router.RegisterDispatchHandler(types.AppIDCreditControl, func(_ *peer.Peer, _ *message.Message) error {
		return nil
	})

	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "local.example.com"))
	_ = router.RouteIn(nil, msg)

	if called {
		t.Fatal("deregistered in-handler was still called")
	}
}

func TestDeregisterInHandlerNotFound(t *testing.T) {
	router := NewRouter()
	ok := router.DeregisterInHandler("nonexistent")
	if ok {
		t.Fatal("DeregisterInHandler should return false for unknown handler")
	}
}

func TestDeregisterOutHandler(t *testing.T) {
	router := NewRouter()
	router.AddRealmRoute("example.com", "peer1.example.com")

	router.RegisterOutHandler("test-filter", 10, func(_ *message.Message, _ []Route) ([]Route, error) {
		// Eliminate all candidates
		return nil, nil
	})

	// Before deregister: should fail (handler eliminates all routes)
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	_, err := router.RouteOut(msg)
	if err == nil {
		t.Fatal("expected routing failure due to filtering handler")
	}

	// Deregister
	ok := router.DeregisterOutHandler("test-filter")
	if !ok {
		t.Fatal("DeregisterOutHandler returned false")
	}

	// After deregister: should succeed
	msg2 := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg2.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	p, err := router.RouteOut(msg2)
	if err != nil {
		t.Fatalf("RouteOut should succeed after deregister: %v", err)
	}
	if p != "peer1.example.com" {
		t.Fatalf("expected peer1.example.com, got %s", p)
	}
}

func TestDeregisterDispatchHandler(t *testing.T) {
	router := NewRouter()

	router.RegisterDispatchHandler(types.AppIDCreditControl, func(_ *peer.Peer, _ *message.Message) error {
		return nil
	})

	ok := router.DeregisterDispatchHandler(types.AppIDCreditControl)
	if !ok {
		t.Fatal("DeregisterDispatchHandler returned false")
	}

	// Verify handler is gone
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	err := router.Dispatch(nil, msg)
	if err == nil {
		t.Fatal("expected dispatch error after deregistration")
	}
	var derr *DispatchError
	if !errors.As(err, &derr) {
		t.Fatalf("expected DispatchError, got %v", err)
	}
}

func TestDeregisterDispatchHandlerNotFound(t *testing.T) {
	router := NewRouter()
	ok := router.DeregisterDispatchHandler(types.AppIDCreditControl)
	if ok {
		t.Fatal("DeregisterDispatchHandler should return false for unregistered appID")
	}
}

func TestDeregisterAllByOwner(t *testing.T) {
	router := NewRouter()

	// Register handlers with different owners
	router.RegisterInHandlerWithOwner("ext_a_in", "ext_a", 10, func(_ *peer.Peer, _ *message.Message, candidates []Route) ([]Route, error) {
		return candidates, nil
	})
	router.RegisterOutHandlerWithOwner("ext_a_out", "ext_a", 10, func(_ *message.Message, candidates []Route) ([]Route, error) {
		return candidates, nil
	})
	router.RegisterDispatchHandlerWithOwner(types.AppIDCreditControl, "ext_a", func(_ *peer.Peer, _ *message.Message) error {
		return nil
	})

	// Also register a handler for a different owner that should survive
	surviveCalled := false
	router.RegisterInHandlerWithOwner("ext_b_in", "ext_b", 20, func(_ *peer.Peer, _ *message.Message, candidates []Route) ([]Route, error) {
		surviveCalled = true
		return candidates, nil
	})
	router.RegisterDispatchHandlerWithOwner(types.ApplicationID(3), "ext_b", func(_ *peer.Peer, _ *message.Message) error {
		return nil
	})

	// Deregister all by ext_a
	router.DeregisterAllByOwner("ext_a")

	// Verify ext_a's dispatch handler is gone
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	err := router.Dispatch(nil, msg)
	if err == nil {
		t.Fatal("ext_a's dispatch handler should be deregistered")
	}

	// Verify ext_b's dispatch handler still works
	msg2 := message.NewRequest(265, types.ApplicationID(3)) // Any command code
	msg2.ApplicationID = types.ApplicationID(3)
	err = router.Dispatch(nil, msg2)
	if err != nil {
		t.Fatalf("ext_b's dispatch handler should still work: %v", err)
	}

	// Verify ext_b's in-handler still runs
	router.SetLocalIdentity("local.example.com", "example.com")
	router.RegisterDispatchHandlerWithOwner(types.AppIDCreditControl, "test", func(_ *peer.Peer, _ *message.Message) error {
		return nil
	})
	msg3 := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	msg3.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "local.example.com"))
	_ = router.RouteIn(nil, msg3)
	if !surviveCalled {
		t.Fatal("ext_b's in-handler should have survived deregistration of ext_a")
	}
}

func TestRegisterWithOwnerSetsOwnerField(t *testing.T) {
	router := NewRouter()

	// RegisterInHandler default: owner = name
	router.RegisterInHandler("handler_name", 10, func(_ *peer.Peer, _ *message.Message, candidates []Route) ([]Route, error) {
		return candidates, nil
	})

	router.mu.RLock()
	if len(router.inHandlers) != 1 {
		router.mu.RUnlock()
		t.Fatal("expected 1 in-handler")
	}
	if router.inHandlers[0].owner != "handler_name" {
		router.mu.RUnlock()
		t.Fatalf("expected owner=handler_name, got %s", router.inHandlers[0].owner)
	}
	router.mu.RUnlock()

	// RegisterInHandlerWithOwner: explicit owner
	router.RegisterInHandlerWithOwner("another_handler", "my_extension", 20, func(_ *peer.Peer, _ *message.Message, candidates []Route) ([]Route, error) {
		return candidates, nil
	})

	router.mu.RLock()
	if len(router.inHandlers) != 2 {
		router.mu.RUnlock()
		t.Fatal("expected 2 in-handlers")
	}
	// Handlers are sorted by priority, so index 0 is priority 10, index 1 is priority 20
	if router.inHandlers[1].owner != "my_extension" {
		router.mu.RUnlock()
		t.Fatalf("expected owner=my_extension, got %s", router.inHandlers[1].owner)
	}
	router.mu.RUnlock()
}

func TestDeregisterAllByOwnerNoMatch(t *testing.T) {
	router := NewRouter()
	router.RegisterInHandler("test-in", 10, func(_ *peer.Peer, _ *message.Message, candidates []Route) ([]Route, error) {
		return candidates, nil
	})

	// Deregister a non-existent owner should not remove anything
	router.DeregisterAllByOwner("nonexistent_owner")

	router.mu.RLock()
	count := len(router.inHandlers)
	router.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 in-handler to survive, got %d", count)
	}
}

// ============================================================================
// FailoverPeer Tests
// ============================================================================

func TestFailoverPeerRemovesInboundEntries(t *testing.T) {
	router := NewRouter()

	// Directly populate relay transactions
	for i := 1; i <= 3; i++ {
		router.prevHopByHop.Store(types.HopByHopID(i), relayTxn{
			OriginalPeer:  "client-a",
			OriginalHBHID: types.HopByHopID(i * 100),
			OutboundPeer:  "downstream",
			CreatedAt:     time.Now(),
		})
	}
	for i := 4; i <= 5; i++ {
		router.prevHopByHop.Store(types.HopByHopID(i), relayTxn{
			OriginalPeer:  "client-b",
			OriginalHBHID: types.HopByHopID(i * 100),
			OutboundPeer:  "downstream",
			CreatedAt:     time.Now(),
		})
	}

	if router.RelayTxnCount() != 5 {
		t.Fatalf("expected 5 relay txns, got %d", router.RelayTxnCount())
	}

	// Client-a disconnects
	removed := router.FailoverPeer("client-a")
	if removed != 3 {
		t.Errorf("FailoverPeer returned %d, want 3", removed)
	}
	if router.RelayTxnCount() != 2 {
		t.Errorf("expected 2 relay txns remaining, got %d", router.RelayTxnCount())
	}
}

func TestFailoverPeerRemovesOutboundEntries(t *testing.T) {
	router := NewRouter()

	// 2 entries forwarded to peer-a, 1 to peer-b
	for i := 1; i <= 2; i++ {
		router.prevHopByHop.Store(types.HopByHopID(i), relayTxn{
			OriginalPeer:  "client",
			OriginalHBHID: types.HopByHopID(i * 100),
			OutboundPeer:  "peer-a",
			CreatedAt:     time.Now(),
		})
	}
	router.prevHopByHop.Store(types.HopByHopID(3), relayTxn{
		OriginalPeer:  "client",
		OriginalHBHID: 300,
		OutboundPeer:  "peer-b",
		CreatedAt:     time.Now(),
	})

	if router.RelayTxnCount() != 3 {
		t.Fatalf("expected 3 relay txns, got %d", router.RelayTxnCount())
	}

	// peer-a disconnects — removes entries forwarded to peer-a
	removed := router.FailoverPeer("peer-a")
	if removed != 2 {
		t.Errorf("FailoverPeer returned %d, want 2", removed)
	}
	if router.RelayTxnCount() != 1 {
		t.Errorf("expected 1 relay txn remaining, got %d", router.RelayTxnCount())
	}
}

func TestFailoverPeerNoMatch(t *testing.T) {
	router := NewRouter()

	router.prevHopByHop.Store(types.HopByHopID(1), relayTxn{
		OriginalPeer:  "client",
		OriginalHBHID: 100,
		OutboundPeer:  "downstream",
		CreatedAt:     time.Now(),
	})

	removed := router.FailoverPeer("unknown-peer")
	if removed != 0 {
		t.Errorf("FailoverPeer returned %d for unknown peer, want 0", removed)
	}
	if router.RelayTxnCount() != 1 {
		t.Errorf("expected 1 relay txn, got %d", router.RelayTxnCount())
	}
}

// ============================================================================
// Relay Transaction Expiry/Sweep Tests
// ============================================================================

func TestSweepExpiredRemovesOldEntries(t *testing.T) {
	router := NewRouter()
	router.sweepTTL = 100 * time.Millisecond

	// Add entries with different ages
	router.prevHopByHop.Store(types.HopByHopID(1), relayTxn{
		OriginalPeer:  "client",
		OriginalHBHID: 100,
		OutboundPeer:  "downstream",
		CreatedAt:     time.Now().Add(-200 * time.Millisecond), // expired
	})
	router.prevHopByHop.Store(types.HopByHopID(2), relayTxn{
		OriginalPeer:  "client",
		OriginalHBHID: 200,
		OutboundPeer:  "downstream",
		CreatedAt:     time.Now().Add(-200 * time.Millisecond), // expired
	})
	router.prevHopByHop.Store(types.HopByHopID(3), relayTxn{
		OriginalPeer:  "client",
		OriginalHBHID: 300,
		OutboundPeer:  "downstream",
		CreatedAt:     time.Now(), // fresh
	})

	removed := router.sweepExpired()
	if removed != 2 {
		t.Errorf("sweepExpired returned %d, want 2", removed)
	}
	if router.RelayTxnCount() != 1 {
		t.Errorf("expected 1 relay txn remaining, got %d", router.RelayTxnCount())
	}
}

func TestSweepExpiredNoTTL(t *testing.T) {
	router := NewRouter()
	// sweepTTL is 0 (default) — sweep should be a no-op

	router.prevHopByHop.Store(types.HopByHopID(1), relayTxn{
		OriginalPeer: "client",
		OutboundPeer: "downstream",
		CreatedAt:    time.Now().Add(-24 * time.Hour),
	})

	removed := router.sweepExpired()
	if removed != 0 {
		t.Errorf("sweepExpired with no TTL returned %d, want 0", removed)
	}
	if router.RelayTxnCount() != 1 {
		t.Errorf("expected 1 relay txn, got %d", router.RelayTxnCount())
	}
}

func TestStartStopRelaySweep(t *testing.T) {
	router := NewRouter()

	// Add an entry that will expire quickly
	router.prevHopByHop.Store(types.HopByHopID(1), relayTxn{
		OriginalPeer: "client",
		OutboundPeer: "downstream",
		CreatedAt:    time.Now().Add(-50 * time.Millisecond),
	})

	// Start sweep with 20ms interval and 30ms TTL
	router.StartRelaySweep(20*time.Millisecond, 30*time.Millisecond)

	// Wait for at least one sweep cycle
	time.Sleep(80 * time.Millisecond)

	if router.RelayTxnCount() != 0 {
		t.Errorf("expected 0 relay txns after sweep, got %d", router.RelayTxnCount())
	}

	router.StopRelaySweep()
}

func TestRelaySweepPreservesFreshEntries(t *testing.T) {
	router := NewRouter()

	router.prevHopByHop.Store(types.HopByHopID(1), relayTxn{
		OriginalPeer: "client",
		OutboundPeer: "downstream",
		CreatedAt:    time.Now(),
	})

	router.StartRelaySweep(20*time.Millisecond, 1*time.Second)
	time.Sleep(60 * time.Millisecond) // 3 sweep cycles, but TTL is 1s

	if router.RelayTxnCount() != 1 {
		t.Errorf("expected 1 relay txn (not yet expired), got %d", router.RelayTxnCount())
	}

	router.StopRelaySweep()
}

// TestAnswersFromDifferentPeersWithTheSameHopByHopID reproduces a defect found
// by the rt_load_balance integration suite: every peer generates its own
// hop-by-hop ID sequence starting at zero, so relaying to two backends produced
// two transactions with the same ID. Keyed on the ID alone, the second evicted
// the first, half the answers were dropped as "no routes available", and the
// survivors could be returned against another client's request.
func TestAnswersFromDifferentPeersWithTheSameHopByHopID(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)

	clientA := &capturingPeer{}
	clientB := &capturingPeer{}
	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		switch identity {
		case "client-a":
			return clientA, true
		case "client-b":
			return clientB, true
		}
		return nil, false
	})

	// Both backends minted the same outbound hop-by-hop ID, which is legal:
	// the ID only has to be unique on its own connection.
	const shared = types.HopByHopID(5)
	router.prevHopByHop.Store(relayKey{OutboundPeer: "backend-a", HBHID: shared}, relayTxn{
		OriginalPeer: "client-a", OriginalHBHID: 111, OutboundPeer: "backend-a", CreatedAt: time.Now(),
	})
	router.prevHopByHop.Store(relayKey{OutboundPeer: "backend-b", HBHID: shared}, relayTxn{
		OriginalPeer: "client-b", OriginalHBHID: 222, OutboundPeer: "backend-b", CreatedAt: time.Now(),
	})

	answerFrom := func(backend string) {
		req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
		ans := message.NewAnswer(req)
		ans.HopByHopID = shared
		if err := router.RouteIn(peer.New(peer.Config{DiameterIdentity: types.DiamID(backend)}, nil), ans); err != nil {
			t.Fatalf("RouteIn answer from %s failed: %v", backend, err)
		}
	}

	answerFrom("backend-a")
	answerFrom("backend-b")

	if !clientA.called || !clientB.called {
		t.Fatalf("both clients must receive their answer, got a=%v b=%v", clientA.called, clientB.called)
	}
	if clientA.msg.HopByHopID != 111 {
		t.Errorf("client-a answer HBH = %d, want 111 (its own request's ID)", clientA.msg.HopByHopID)
	}
	if clientB.msg.HopByHopID != 222 {
		t.Errorf("client-b answer HBH = %d, want 222 (its own request's ID)", clientB.msg.HopByHopID)
	}
}

// TestRelayReportsWhyTheNextHopRefused pins the diagnostic that the dea_soak
// burst made necessary: when the only candidate rejects the message because its
// send queue is full, the peer is healthy and the route exists, but the error
// used to claim there was no route at all, sending operators after a routing
// misconfiguration that does not exist.
func TestRelayReportsWhyTheNextHopRefused(t *testing.T) {
	router := NewRouter()
	router.SetLocalIdentity("local.example.com", "example.com")
	router.SetRelay(true)
	router.AddHostRoute("remote.host.com", "busy-peer")

	router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		if identity == "busy-peer" {
			return &failingPeer{}, true
		}
		return nil, false
	})

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "remote.host.com"))

	err := router.RouteIn(peer.New(peer.Config{DiameterIdentity: "client.example.com"}, nil), req)
	if err == nil {
		t.Fatal("relaying to a peer that refuses every message must fail")
	}
	if !strings.Contains(err.Error(), "send failed") {
		t.Errorf("error %q does not say why the next hop refused the message", err)
	}
}
