// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package peer

import (
	"testing"

	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func openStatePeer(t *testing.T, state State) (*Peer, *mockConn) {
	t.Helper()

	p := New(testConfig("open-state.example.com"), testDict())
	p.SetLocalOverride(LocalConfig{DiameterIdentity: "local.open.example.com", Realm: "local.realm", ProductName: "TestProduct", OriginStateID: 12})
	conn := newMockConn()
	p.setState(state)
	p.mu.Lock()
	if state == StateIOpen || state == StateSuspect {
		p.iConn = conn
	} else {
		p.rConn = conn
	}
	p.mu.Unlock()
	t.Cleanup(p.Stop)
	return p, conn
}

func TestOpenStatesAnswerWatchdogRequestsOnTheirActiveConnection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state State
		event Event
	}{
		{name: "initiator", state: StateIOpen, event: EventIRcvDWR},
		{name: "responder", state: StateROpen, event: EventRRcvDWR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, conn := openStatePeer(t, tc.state)
			req := createTestDWR("remote.example.com")
			req.HopByHopID = 31337
			req.EndToEndID = 31338

			if err := p.handleEvent(eventMessage{event: tc.event, msg: req}); err != nil {
				t.Fatalf("watchdog event: %v", err)
			}

			got := decodeOnlyWrittenMessage(t, conn)
			assertAnswerCorrelates(t, got, req)
			if got.CommandCode != types.CmdCodeDeviceWatchdog {
				t.Fatalf("command = %d, want DWA", got.CommandCode)
			}
			if gotState := p.State(); gotState != tc.state {
				t.Fatalf("state = %v, want %v", gotState, tc.state)
			}
		})
	}
}

func TestOpenStatesEnterClosingForLocalAndRemoteDisconnect(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   State
		event   Event
		request *message.Message
		wantCmd types.CommandCode
		wantReq bool
	}{
		{name: "initiator local Stop sends DPR", state: StateIOpen, event: EventStop, wantCmd: types.CmdCodeDisconnectPeer, wantReq: true},
		{name: "responder local Stop sends DPR", state: StateROpen, event: EventStop, wantCmd: types.CmdCodeDisconnectPeer, wantReq: true},
		{name: "initiator remote DPR sends DPA", state: StateIOpen, event: EventIRcvDPR, request: createTestDPR("remote.example.com", "remote.realm", types.DisconnectBusy), wantCmd: types.CmdCodeDisconnectPeer, wantReq: false},
		{name: "responder remote DPR sends DPA", state: StateROpen, event: EventRRcvDPR, request: createTestDPR("remote.example.com", "remote.realm", types.DisconnectBusy), wantCmd: types.CmdCodeDisconnectPeer, wantReq: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, conn := openStatePeer(t, tc.state)
			ev := eventMessage{event: tc.event, msg: tc.request}
			if err := p.handleEvent(ev); err != nil {
				t.Fatalf("disconnect event: %v", err)
			}

			got := decodeOnlyWrittenMessage(t, conn)
			if got.CommandCode != tc.wantCmd || got.IsRequest() != tc.wantReq {
				t.Fatalf("written command=%d request=%v, want command=%d request=%v", got.CommandCode, got.IsRequest(), tc.wantCmd, tc.wantReq)
			}
			if tc.request != nil {
				assertAnswerCorrelates(t, got, tc.request)
			}
			if gotState := p.State(); gotState != StateClosing {
				t.Fatalf("state = %v, want Closing", gotState)
			}
		})
	}
}

func TestOpenStatesRejectDuplicateResponderConnections(t *testing.T) {
	for _, state := range []State{StateIOpen, StateROpen} {
		t.Run(state.String(), func(t *testing.T) {
			p, _ := openStatePeer(t, state)
			duplicate := newMockConn()

			if err := p.handleEvent(eventMessage{event: EventRConnCER, conn: duplicate, msg: createTestCER("remote.example.com", "remote.realm")}); err != nil {
				t.Fatalf("duplicate CER: %v", err)
			}

			if !duplicate.IsClosed() {
				t.Fatal("duplicate responder connection was not closed")
			}
			if got := p.State(); got != state {
				t.Fatalf("state = %v, want %v", got, state)
			}
		})
	}
}

func TestOpenAndSuspectStatesDeliverTrafficAndRecoverLiveness(t *testing.T) {
	app := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	app.HopByHopID = 9191
	app.EndToEndID = 9292

	for _, tc := range []struct {
		name      string
		state     State
		event     Event
		wantState State
	}{
		{name: "I-Open application request", state: StateIOpen, event: EventIRcvMessage, wantState: StateIOpen},
		{name: "R-Open application request", state: StateROpen, event: EventRRcvMessage, wantState: StateROpen},
		{name: "Suspect initiator application request", state: StateSuspect, event: EventIRcvMessage, wantState: StateIOpen},
		{name: "Suspect responder application request", state: StateSuspect, event: EventRRcvMessage, wantState: StateROpen},
		{name: "Suspect initiator DWA", state: StateSuspect, event: EventIRcvDWA, wantState: StateIOpen},
		{name: "Suspect responder DWA", state: StateSuspect, event: EventRRcvDWA, wantState: StateROpen},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := openStatePeer(t, tc.state)
			if tc.event == EventRRcvMessage || tc.event == EventRRcvDWA {
				p.mu.Lock()
				p.iConn = nil
				p.rConn = newMockConn()
				p.mu.Unlock()
			}
			var delivered int
			p.SetOnMessage(func(_ *Peer, _ *message.Message) { delivered++ })

			if err := p.handleEvent(eventMessage{event: tc.event, msg: app}); err != nil {
				t.Fatalf("event: %v", err)
			}
			if got := p.State(); got != tc.wantState {
				t.Fatalf("state = %v, want %v", got, tc.wantState)
			}
			if (tc.event == EventIRcvMessage || tc.event == EventRRcvMessage) && delivered != 1 {
				t.Fatalf("delivered messages = %d, want 1", delivered)
			}
		})
	}
}

func TestSuspectDisconnectEventsRetireTheConnection(t *testing.T) {
	for _, ev := range []Event{EventIPeerDisc, EventRPeerDisc} {
		t.Run(ev.String(), func(t *testing.T) {
			p, conn := openStatePeer(t, StateSuspect)
			if ev == EventRPeerDisc {
				p.mu.Lock()
				p.iConn = nil
				p.rConn = conn
				p.mu.Unlock()
			}

			if err := p.handleEvent(eventMessage{event: ev}); err != nil {
				t.Fatalf("suspect disconnect: %v", err)
			}
			if got := p.State(); got != StateZombie && got != StateClosed {
				t.Fatalf("state = %v, want retired peer", got)
			}
			if !conn.IsClosed() {
				t.Fatal("suspect disconnect did not close the connection")
			}
		})
	}
}
