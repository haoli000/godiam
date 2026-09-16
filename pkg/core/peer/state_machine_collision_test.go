// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Tests for the connection-collision half of the RFC 6733 Section 5.6 peer
// state machine: Wait-Conn-Ack, Wait-I-CEA, Wait-Conn-Ack/Elect, Wait-Returns
// and Closing, plus the capabilities exchange that drives them.
//
// These states only occur when both ends initiate to each other at the same
// time. That makes them rare in a healthy network and common exactly when a
// network is flapping, and a mistake here does not fail loudly: it leaves both
// ends holding a different connection, or no connection at all, with no error
// anywhere.

package peer

import (
	"crypto/tls"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// collisionPeer builds a peer parked in a given state with the local identity
// set, so a test can inject one event and assert the transition.
func collisionPeer(t *testing.T, state State) *Peer {
	t.Helper()

	p := New(testConfig("peer.example.com"), testDict())
	p.SetLocalOverride(LocalConfig{
		DiameterIdentity: "local.example.com",
		Realm:            "test.realm",
		ProductName:      "TestProduct",
		OriginStateID:    1,
	})
	p.setState(state)
	t.Cleanup(p.Stop)
	return p
}

// decodeWritten decodes the first Diameter message written to a mock
// connection, so a test can assert what actually went on the wire rather than
// merely that a transition happened.
func decodeWritten(t *testing.T, c *mockConn) *message.Message {
	t.Helper()

	data := c.GetWrittenData()
	if len(data) == 0 {
		t.Fatal("nothing was written to the connection")
	}
	msg, err := message.DecodeMessage(data)
	if err != nil {
		t.Fatalf("decoding what was written failed: %v", err)
	}
	return msg
}

// ============================================================================
// Wait-Conn-Ack
// ============================================================================

// Wait-Conn-Ack + I-Rcv-Conn-Ack -> I-Snd-CER -> Wait-I-CEA.
func TestWaitConnAckSendsCER(t *testing.T) {
	p := collisionPeer(t, StateWaitConnAck)
	conn := newMockConn()
	p.iConn = conn

	if err := p.handleEvent(eventMessage{event: EventIRcvConnAck}); err != nil {
		t.Fatalf("connection ack: %v", err)
	}

	if got := p.State(); got != StateWaitICEA {
		t.Fatalf("state = %v, want Wait-I-CEA", got)
	}
	sent := decodeWritten(t, conn)
	if sent.CommandCode != types.CmdCodeCapabilitiesExchange || !sent.IsRequest() {
		t.Fatalf("sent command %d (request=%v), want a CER", sent.CommandCode, sent.IsRequest())
	}
	if host, ok := sent.GetOriginHost(); !ok || host != "local.example.com" {
		t.Fatalf("CER Origin-Host = %q (present=%v), want the local identity", host, ok)
	}
}

// Wait-Conn-Ack + I-Rcv-Conn-Nack -> Cleanup -> Closed. A non-persistent peer
// has nothing to reconnect for, so it must end up parked rather than retrying.
func TestWaitConnAckConnectFailureRetires(t *testing.T) {
	p := collisionPeer(t, StateWaitConnAck)

	_ = p.handleEvent(eventMessage{event: EventIRcvConnNack})

	if got := p.State(); got != StateZombie {
		t.Fatalf("state = %v, want Zombie for a non-persistent peer", got)
	}
	if p.IsOpen() {
		t.Fatal("peer reports open after its connection attempt failed")
	}
}

// Wait-Conn-Ack + R-Conn-CER -> R-Accept, Process-CER -> Wait-Conn-Ack/Elect.
// This is the collision being detected: the peer dialled us while we were
// dialling it, so the CER is held pending the election rather than answered.
func TestWaitConnAckCollisionEntersElection(t *testing.T) {
	p := collisionPeer(t, StateWaitConnAck)
	rConn := newMockConn()
	cer := createTestCER("remote.example.com", "remote.realm")

	if err := p.handleEvent(eventMessage{event: EventRConnCER, conn: rConn, msg: cer}); err != nil {
		t.Fatalf("inbound CER: %v", err)
	}

	if got := p.State(); got != StateWaitConnAckElect {
		t.Fatalf("state = %v, want Wait-Conn-Ack/Elect", got)
	}
	if p.rConn != rConn {
		t.Fatal("the responder connection was not retained")
	}
	if p.pendingCER == nil {
		t.Fatal("the CER was not held: the election winner needs it to build its CEA")
	}
	if len(rConn.GetWrittenData()) != 0 {
		t.Fatal("a CEA was sent before the election was decided")
	}
}

// A CER the accept policy rejects must be answered with that policy's result
// code and the connection dropped, rather than silently entering an election
// with a peer we are not willing to talk to.
func TestWaitConnAckRejectedCERIsAnsweredAndClosed(t *testing.T) {
	p := collisionPeer(t, StateWaitConnAck)
	p.SetAcceptPolicy(func(types.DiamID, types.DiamID) (bool, types.ResultCode) {
		return false, types.ResultUnknownPeer
	})
	rConn := newMockConn()

	if err := p.handleEvent(eventMessage{
		event: EventRConnCER,
		conn:  rConn,
		msg:   createTestCER("stranger.example.com", "stranger.realm"),
	}); err != nil {
		t.Fatalf("inbound CER: %v", err)
	}

	answer := decodeWritten(t, rConn)
	if code, ok := answer.GetResultCode(); !ok || code != types.ResultUnknownPeer {
		t.Fatalf("CEA Result-Code = %d (present=%v), want DIAMETER_UNKNOWN_PEER", code, ok)
	}
	if !rConn.IsClosed() {
		t.Fatal("the rejected connection was left open")
	}
	if got := p.State(); got == StateWaitConnAckElect {
		t.Fatal("a rejected CER started an election")
	}
}

// ============================================================================
// Wait-I-CEA
// ============================================================================

// Wait-I-CEA + I-Rcv-CEA -> Process-CEA -> I-Open, and the peer's identity is
// taken from the CEA rather than from configuration.
func TestWaitICEAOpensOnCEA(t *testing.T) {
	p := collisionPeer(t, StateWaitICEA)
	p.iConn = newMockConn()
	cer := createTestCER("local.example.com", "test.realm")

	if err := p.handleEvent(eventMessage{
		event: EventIRcvCEA,
		msg:   createTestCEA(cer, "remote.example.com", "remote.realm", types.ResultSuccess),
	}); err != nil {
		t.Fatalf("inbound CEA: %v", err)
	}

	if got := p.State(); got != StateIOpen {
		t.Fatalf("state = %v, want I-Open", got)
	}
	if !p.IsOpen() {
		t.Fatal("peer does not report open after a successful capabilities exchange")
	}
	if got := p.DiameterID(); got != "remote.example.com" {
		t.Fatalf("peer identity = %q, want the CEA Origin-Host", got)
	}
}

// A CEA carrying a failure must not open the connection, however well formed
// it is: the peer has told us it will not serve us.
func TestWaitICEAFailedCEADoesNotOpen(t *testing.T) {
	p := collisionPeer(t, StateWaitICEA)
	p.iConn = newMockConn()
	cer := createTestCER("local.example.com", "test.realm")

	err := p.handleEvent(eventMessage{
		event: EventIRcvCEA,
		msg:   createTestCEA(cer, "remote.example.com", "remote.realm", types.ResultUnknownPeer),
	})

	if err == nil {
		t.Fatal("a CEA rejecting us was accepted")
	}
	if p.IsOpen() {
		t.Fatal("peer reports open after a rejecting CEA")
	}
}

// Wait-I-CEA + I-Rcv-Non-CEA -> Error -> Closed. Anything other than a CEA on
// a connection that has not completed capabilities exchange is a protocol
// error; accepting it would mean serving traffic over an unnegotiated peer.
func TestWaitICEANonCEAIsAProtocolError(t *testing.T) {
	p := collisionPeer(t, StateWaitICEA)
	p.iConn = newMockConn()

	err := p.handleEvent(eventMessage{event: EventIRcvNonCEA, msg: createTestDWR("remote.example.com")})

	if err == nil {
		t.Fatal("a non-CEA before capabilities exchange was accepted")
	}
	if p.IsOpen() {
		t.Fatal("peer reports open after a protocol error")
	}
}

// Wait-I-CEA + R-Conn-CER -> R-Accept, Process-CER, Elect -> Wait-Returns.
func TestWaitICEACollisionElects(t *testing.T) {
	p := collisionPeer(t, StateWaitICEA)
	p.iConn = newMockConn()

	if err := p.handleEvent(eventMessage{
		event: EventRConnCER,
		conn:  newMockConn(),
		msg:   createTestCER("remote.example.com", "remote.realm"),
	}); err != nil {
		t.Fatalf("inbound CER: %v", err)
	}

	if got := p.State(); got != StateWaitReturns {
		t.Fatalf("state = %v, want Wait-Returns", got)
	}
}

// ============================================================================
// Wait-Conn-Ack/Elect
// ============================================================================

// Wait-Conn-Ack/Elect + I-Rcv-Conn-Nack -> R-Snd-CEA -> R-Open. Our own dial
// failed, so there is nothing to elect: the peer's connection wins by default
// and must be completed rather than dropped along with ours.
func TestElectAbandonedDialFallsBackToResponder(t *testing.T) {
	p := collisionPeer(t, StateWaitConnAckElect)
	rConn := newMockConn()
	p.rConn = rConn
	p.pendingCER = createTestCER("remote.example.com", "remote.realm")

	if err := p.handleEvent(eventMessage{event: EventIRcvConnNack}); err != nil {
		t.Fatalf("connection nack: %v", err)
	}

	if got := p.State(); got != StateROpen {
		t.Fatalf("state = %v, want R-Open", got)
	}
	answer := decodeWritten(t, rConn)
	if answer.CommandCode != types.CmdCodeCapabilitiesExchange || answer.IsRequest() {
		t.Fatalf("sent command %d (request=%v), want a CEA", answer.CommandCode, answer.IsRequest())
	}
	if code, ok := answer.GetResultCode(); !ok || code != types.ResultSuccess {
		t.Fatalf("CEA Result-Code = %d (present=%v), want success", code, ok)
	}
	if p.pendingCER != nil {
		t.Fatal("the pending CER was not released after being answered")
	}
}

// Wait-Conn-Ack/Elect + R-Peer-Disc -> R-Disc -> Wait-Conn-Ack. The peer gave
// up its connection, so the collision is over and we go back to waiting for
// our own dial. Holding the stale CER would answer a connection that is gone.
func TestElectPeerDisconnectReturnsToWaitConnAck(t *testing.T) {
	p := collisionPeer(t, StateWaitConnAckElect)
	rConn := newMockConn()
	p.rConn = rConn
	p.pendingCER = createTestCER("remote.example.com", "remote.realm")

	if err := p.handleEvent(eventMessage{event: EventRPeerDisc}); err != nil {
		t.Fatalf("peer disconnect: %v", err)
	}

	if got := p.State(); got != StateWaitConnAck {
		t.Fatalf("state = %v, want Wait-Conn-Ack", got)
	}
	if p.pendingCER != nil {
		t.Fatal("a CER for a closed connection was kept")
	}
	if !rConn.IsClosed() {
		t.Fatal("the abandoned responder connection was not closed")
	}
}

// Wait-Conn-Ack/Elect + R-Conn-CER -> R-Reject. One election at a time: a
// third connection is refused outright, and must not displace the one already
// being elected.
func TestElectRefusesASecondConnection(t *testing.T) {
	p := collisionPeer(t, StateWaitConnAckElect)
	first := newMockConn()
	p.rConn = first
	p.pendingCER = createTestCER("remote.example.com", "remote.realm")

	second := newMockConn()
	if err := p.handleEvent(eventMessage{
		event: EventRConnCER,
		conn:  second,
		msg:   createTestCER("remote.example.com", "remote.realm"),
	}); err != nil {
		t.Fatalf("second CER: %v", err)
	}

	if !second.IsClosed() {
		t.Fatal("the second connection was not refused")
	}
	if p.rConn != first {
		t.Fatal("the second connection displaced the one under election")
	}
	if got := p.State(); got != StateWaitConnAckElect {
		t.Fatalf("state = %v, want Wait-Conn-Ack/Elect unchanged", got)
	}
}

// ============================================================================
// Wait-Returns
// ============================================================================

// Wait-Returns + Win-Election -> I-Disc, R-Snd-CEA -> R-Open. Winning means
// keeping the connection the peer opened and dropping our own; leaving the
// initiator open would leave two connections to one peer.
func TestWaitReturnsWinKeepsResponderAndDropsInitiator(t *testing.T) {
	p := collisionPeer(t, StateWaitReturns)
	iConn, rConn := newMockConn(), newMockConn()
	p.iConn, p.rConn = iConn, rConn
	p.pendingCER = createTestCER("remote.example.com", "remote.realm")

	if err := p.handleEvent(eventMessage{event: EventWinElection}); err != nil {
		t.Fatalf("win election: %v", err)
	}

	if got := p.State(); got != StateROpen {
		t.Fatalf("state = %v, want R-Open", got)
	}
	if !iConn.IsClosed() {
		t.Fatal("the losing initiator connection was left open")
	}
	if rConn.IsClosed() {
		t.Fatal("the winning responder connection was closed")
	}
	if code, ok := decodeWritten(t, rConn).GetResultCode(); !ok || code != types.ResultSuccess {
		t.Fatalf("CEA Result-Code = %d (present=%v), want success", code, ok)
	}
}

// Wait-Returns + I-Rcv-CEA -> R-Disc -> I-Open. The mirror image: the peer won,
// answered our CER, and we drop the connection it opened.
func TestWaitReturnsCEAKeepsInitiatorAndDropsResponder(t *testing.T) {
	p := collisionPeer(t, StateWaitReturns)
	iConn, rConn := newMockConn(), newMockConn()
	p.iConn, p.rConn = iConn, rConn
	p.pendingCER = createTestCER("remote.example.com", "remote.realm")
	cer := createTestCER("local.example.com", "test.realm")

	if err := p.handleEvent(eventMessage{
		event: EventIRcvCEA,
		msg:   createTestCEA(cer, "remote.example.com", "remote.realm", types.ResultSuccess),
	}); err != nil {
		t.Fatalf("inbound CEA: %v", err)
	}

	if got := p.State(); got != StateIOpen {
		t.Fatalf("state = %v, want I-Open", got)
	}
	if !rConn.IsClosed() {
		t.Fatal("the losing responder connection was left open")
	}
	if iConn.IsClosed() {
		t.Fatal("the winning initiator connection was closed")
	}
	if p.pendingCER != nil {
		t.Fatal("the pending CER outlived the connection it belonged to")
	}
}

// Wait-Returns + R-Peer-Disc -> R-Disc -> Wait-I-CEA. The peer withdrew its
// connection mid-election, so our own dial is all that is left and we resume
// waiting for its CEA rather than tearing everything down.
func TestWaitReturnsPeerDisconnectResumesWaitICEA(t *testing.T) {
	p := collisionPeer(t, StateWaitReturns)
	iConn, rConn := newMockConn(), newMockConn()
	p.iConn, p.rConn = iConn, rConn
	p.pendingCER = createTestCER("remote.example.com", "remote.realm")

	if err := p.handleEvent(eventMessage{event: EventRPeerDisc}); err != nil {
		t.Fatalf("peer disconnect: %v", err)
	}

	if got := p.State(); got != StateWaitICEA {
		t.Fatalf("state = %v, want Wait-I-CEA", got)
	}
	if iConn.IsClosed() {
		t.Fatal("the surviving initiator connection was closed")
	}
	if !rConn.IsClosed() {
		t.Fatal("the withdrawn responder connection was not closed")
	}
}

// Every collision state must bound its wait. A timeout that left the peer
// parked would hold a half-open connection indefinitely.
func TestCollisionStatesTimeOut(t *testing.T) {
	for _, state := range []State{
		StateWaitConnAck,
		StateWaitICEA,
		StateWaitConnAckElect,
		StateWaitReturns,
	} {
		t.Run(state.String(), func(t *testing.T) {
			p := collisionPeer(t, state)
			p.iConn, p.rConn = newMockConn(), newMockConn()

			if err := p.handleEvent(eventMessage{event: EventTimeout}); err != nil {
				t.Fatalf("timeout: %v", err)
			}

			if got := p.State(); got != StateZombie && got != StateClosed {
				t.Fatalf("state = %v, want the attempt abandoned", got)
			}
			if p.IsOpen() {
				t.Fatal("peer reports open after timing out mid-handshake")
			}
		})
	}
}

// ============================================================================
// Closing
// ============================================================================

// Closing ends at Closed however the disconnect completes: on the DPA from
// either connection, on the peer dropping the socket, or on the timeout that
// covers a DPA that never arrives.
func TestClosingAlwaysReachesClosed(t *testing.T) {
	for _, ev := range []Event{
		EventIRcvDPA,
		EventRRcvDPA,
		EventTimeout,
		EventIPeerDisc,
		EventRPeerDisc,
	} {
		t.Run(ev.String(), func(t *testing.T) {
			p := collisionPeer(t, StateClosing)
			conn := newMockConn()
			p.iConn = conn

			if err := p.handleEvent(eventMessage{event: ev}); err != nil {
				t.Fatalf("%v: %v", ev, err)
			}

			if got := p.State(); got != StateClosed {
				t.Fatalf("state = %v, want Closed", got)
			}
			if !conn.IsClosed() {
				t.Fatal("the connection survived the disconnect")
			}
		})
	}
}

// ============================================================================
// CEA processing
// ============================================================================

// A CEA is the only evidence that the far end agreed to be our peer, so every
// field the decision rests on must be present. A missing one cannot be assumed
// to be a success.
func TestProcessCEARejectsIncompleteAnswers(t *testing.T) {
	cer := createTestCER("local.example.com", "test.realm")

	tests := []struct {
		name string
		msg  func() *message.Message
	}{
		{
			name: "no Result-Code",
			msg: func() *message.Message {
				m := message.NewAnswer(cer)
				m.AddAVP(message.NewDiameterIdentityAVP(
					types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID("remote.example.com")))
				m.AddAVP(message.NewDiameterIdentityAVP(
					types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID("remote.realm")))
				return m
			},
		},
		{
			name: "no Origin-Host",
			msg: func() *message.Message {
				m := message.NewAnswer(cer)
				m.SetResultCode(types.ResultSuccess)
				m.AddAVP(message.NewDiameterIdentityAVP(
					types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID("remote.realm")))
				return m
			},
		},
		{
			name: "no Origin-Realm",
			msg: func() *message.Message {
				m := message.NewAnswer(cer)
				m.SetResultCode(types.ResultSuccess)
				m.AddAVP(message.NewDiameterIdentityAVP(
					types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID("remote.example.com")))
				return m
			},
		},
		{
			name: "rejected by the peer",
			msg: func() *message.Message {
				return createTestCEA(cer, "remote.example.com", "remote.realm", types.ResultTooBusy)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := New(testConfig("peer.example.com"), testDict())

			if err := p.processCEA(tc.msg()); err == nil {
				t.Fatal("the CEA was accepted")
			}
			if p.DiameterID() == "remote.example.com" {
				t.Fatal("the peer identity was recorded from a CEA that was not accepted")
			}
		})
	}
}

// A peer is identified by what it says in its CEA, not by what was configured:
// the configured address may be a VIP in front of several hosts.
func TestProcessCEARecordsTheAnsweringIdentity(t *testing.T) {
	p := New(testConfig("configured-name.example.com"), testDict())
	cer := createTestCER("local.example.com", "test.realm")

	if err := p.processCEA(createTestCEA(cer, "actual.example.com", "actual.realm", types.ResultSuccess)); err != nil {
		t.Fatalf("processing the CEA failed: %v", err)
	}

	if got := p.DiameterID(); got != "actual.example.com" {
		t.Fatalf("identity = %q, want the identity from the CEA", got)
	}
	if got := p.Realm(); got != "actual.realm" {
		t.Fatalf("realm = %q, want the realm from the CEA", got)
	}
}

// ============================================================================
// TLS
// ============================================================================

// An unrecognised or absent minimum version must fall back to 1.2 rather than
// to Go's zero value, which would let the handshake negotiate TLS 1.0.
func TestTLSMinVersionFallsBackTo12(t *testing.T) {
	tests := map[string]uint16{
		"1.3": tls.VersionTLS13,
		"1.2": tls.VersionTLS12,
		"1.0": tls.VersionTLS12,
		"":    tls.VersionTLS12,
		"1,3": tls.VersionTLS12,
	}

	for in, want := range tests {
		if got := tlsMinVersion(in); got != want {
			t.Errorf("tlsMinVersion(%q) = %#04x, want %#04x", in, got, want)
		}
	}
}

// ============================================================================
// Reconnect policy
// ============================================================================

// A persistent peer must come back on its own after a failed handshake; that
// retry is the only thing that restores a configured upstream without
// operator action.
func TestPersistentPeerRetriesAfterAFailedHandshake(t *testing.T) {
	cfg := testConfig("upstream.example.com")
	cfg.Persistent = true
	cfg.ReconnectInterval = 20 * time.Millisecond

	p := New(cfg, testDict())
	t.Cleanup(p.Stop)
	p.setState(StateWaitICEA)
	p.iConn = newMockConn()

	if err := p.handleEvent(eventMessage{event: EventTimeout}); err != nil {
		t.Fatalf("timeout: %v", err)
	}
	if got := p.State(); got != StateClosed {
		t.Fatalf("state = %v, want Closed pending the retry", got)
	}

	select {
	case ev := <-p.eventChan:
		if ev.event != EventStart {
			t.Fatalf("event = %v, want Start", ev.event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a persistent peer did not retry after its handshake failed")
	}
}
