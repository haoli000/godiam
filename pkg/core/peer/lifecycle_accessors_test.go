// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package peer

import (
	"net"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func waitForPeerState(t *testing.T, p *Peer, want State) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := p.State(); got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state = %v, want %v", p.State(), want)
}

func waitForClosedConn(t *testing.T, conn *mockConn) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if conn.IsClosed() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("connection was not closed")
}

func TestPeerAccessorsExposeConfiguredRoutingMetadataAndLocalOverride(t *testing.T) {
	cfg := testConfig("accessor.example.com")
	cfg.Zone = "edge-a"
	cfg.Partner = "partner-a"
	p := New(cfg, testDict())
	t.Cleanup(p.Stop)

	if got := p.Config(); got.DiameterIdentity != cfg.DiameterIdentity || got.Zone != "edge-a" || got.Partner != "partner-a" {
		t.Fatalf("Config() = %#v, want configured identity and routing metadata", got)
	}
	if p.Zone() != "edge-a" || p.Partner() != "partner-a" {
		t.Fatalf("Zone/Partner = %q/%q, want edge-a/partner-a", p.Zone(), p.Partner())
	}

	p.SetZone("edge-b", "partner-b")
	if p.Zone() != "edge-b" || p.Partner() != "partner-b" {
		t.Fatalf("SetZone did not update metadata: %q/%q", p.Zone(), p.Partner())
	}

	override := LocalConfig{DiameterIdentity: "local.accessor.example.com", Realm: "local.realm", OriginStateID: 9}
	p.SetLocalOverride(override)
	if got := p.LocalConfig(); got.DiameterIdentity != override.DiameterIdentity || got.Realm != override.Realm || got.OriginStateID != override.OriginStateID {
		t.Fatalf("LocalConfig() = %#v, want override %#v", got, override)
	}
}

func TestTrackPendingAndReceiveExposeCorrelationAndDelivery(t *testing.T) {
	p := New(testConfig("pending.example.com"), testDict())
	t.Cleanup(p.Stop)

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.HopByHopID = 4242
	req.EndToEndID = 9898
	p.TrackPending(req)
	if got := p.PendingCount(); got != 1 {
		t.Fatalf("pending count = %d, want 1", got)
	}

	ans := message.NewAnswer(req)
	ans.SetResultCode(types.ResultSuccess)
	if !p.processInboundMessage(ans) {
		t.Fatal("matching answer was not delivered")
	}
	select {
	case got := <-p.Receive():
		if got.HopByHopID != req.HopByHopID || got.EndToEndID != req.EndToEndID {
			t.Fatalf("received answer identifiers = %d/%d, want %d/%d", got.HopByHopID, got.EndToEndID, req.HopByHopID, req.EndToEndID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delivered answer was not available on Receive()")
	}
	if got := p.PendingCount(); got != 0 {
		t.Fatalf("pending count = %d, want answer to consume request", got)
	}
}

func TestDisconnectQueuesAStopEvent(t *testing.T) {
	p := New(testConfig("disconnect.example.com"), testDict())
	t.Cleanup(p.Stop)

	p.Disconnect(types.DisconnectRebooting)
	select {
	case ev := <-p.eventChan:
		if ev.event != EventStop {
			t.Fatalf("event = %v, want Stop", ev.event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Disconnect did not queue a Stop event")
	}
}

func TestStartReportsFailedOutboundConnectionAndStops(t *testing.T) {
	cfg := testConfig("start-failure.example.com")
	cfg.Addresses = []string{"127.0.0.1"}
	cfg.Port = 1
	cfg.ConnectTimeout = 20 * time.Millisecond
	p := New(cfg, testDict())
	t.Cleanup(p.Stop)

	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForPeerState(t, p, StateZombie)
	if p.IsOpen() {
		t.Fatal("peer opened despite failed outbound connection")
	}
}

func TestStartWithConnectionAcceptsCERAndServesResponderLifecycle(t *testing.T) {
	serverSide, peerSide := net.Pipe()
	defer func() { _ = peerSide.Close() }()
	p := New(testConfig("start-with-connection.example.com"), testDict())
	p.SetLocalOverride(LocalConfig{DiameterIdentity: "local.lifecycle.example.com", Realm: "local.realm", ProductName: "TestProduct", OriginStateID: 7})
	t.Cleanup(p.Stop)

	readCEA := make(chan *message.Message, 1)
	readErr := make(chan error, 1)
	go func() {
		msg, err := message.ReadMessage(peerSide)
		if err != nil {
			readErr <- err
			return
		}
		readCEA <- msg
	}()

	cer := createTestCER("remote.lifecycle.example.com", "remote.realm")
	if err := p.StartWithConnection(serverSide, cer); err != nil {
		t.Fatalf("StartWithConnection: %v", err)
	}

	select {
	case cea := <-readCEA:
		if cea.CommandCode != types.CmdCodeCapabilitiesExchange || cea.IsRequest() {
			t.Fatalf("first outbound message command=%d request=%v, want CEA", cea.CommandCode, cea.IsRequest())
		}
		if cea.HopByHopID != cer.HopByHopID || cea.EndToEndID != cer.EndToEndID {
			t.Fatalf("CEA identifiers = %d/%d, want CER identifiers %d/%d", cea.HopByHopID, cea.EndToEndID, cer.HopByHopID, cer.EndToEndID)
		}
	case err := <-readErr:
		t.Fatalf("reading CEA: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for CEA")
	}
	waitForPeerState(t, p, StateROpen)

	second := newMockConn()
	p.InjectRConnCER(second, createTestCER("remote.lifecycle.example.com", "remote.realm"))
	waitForClosedConn(t, second)
	if got := p.State(); got != StateROpen {
		t.Fatalf("state after rejecting duplicate CER = %v, want R-Open", got)
	}
}
