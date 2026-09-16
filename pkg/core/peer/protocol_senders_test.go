// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package peer

import (
	"testing"

	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func senderPeer(t *testing.T) *Peer {
	t.Helper()

	p := New(testConfig("sender.example.com"), testDict())
	p.SetLocalOverride(LocalConfig{
		DiameterIdentity: "local.example.com",
		Realm:            "local.realm",
		ProductName:      "TestProduct",
		OriginStateID:    44,
	})
	t.Cleanup(p.Stop)
	return p
}

func decodeOnlyWrittenMessage(t *testing.T, conn *mockConn) *message.Message {
	t.Helper()

	data := conn.GetWrittenData()
	msg, err := message.DecodeMessage(data)
	if err != nil {
		t.Fatalf("decoding written Diameter frame: %v", err)
	}
	if got, want := len(data), msg.EncodedLen(); got != want {
		t.Fatalf("connection has %d bytes, want exactly one %d-byte message", got, want)
	}
	return msg
}

func assertAnswerCorrelates(t *testing.T, got, req *message.Message) {
	t.Helper()

	if got.IsRequest() {
		t.Fatal("answer sender wrote a request")
	}
	if got.HopByHopID != req.HopByHopID || got.EndToEndID != req.EndToEndID {
		t.Fatalf("answer identifiers = hbh %d e2e %d, want hbh %d e2e %d", got.HopByHopID, got.EndToEndID, req.HopByHopID, req.EndToEndID)
	}
	if code, ok := got.GetResultCode(); !ok || code != types.ResultSuccess {
		t.Fatalf("Result-Code = %d (present=%v), want success", code, ok)
	}
}

func TestDWAOnConnEchoesTheWatchdogRequestIdentifiers(t *testing.T) {
	p := senderPeer(t)
	conn := newMockConn()
	req := createTestDWR("remote.example.com")
	req.HopByHopID = 0x01020304
	req.EndToEndID = 0x05060708

	if err := p.sendDWAOnConn(conn, req); err != nil {
		t.Fatalf("sendDWAOnConn: %v", err)
	}

	got := decodeOnlyWrittenMessage(t, conn)
	assertAnswerCorrelates(t, got, req)
	if got.CommandCode != types.CmdCodeDeviceWatchdog {
		t.Fatalf("command = %d, want DWA", got.CommandCode)
	}
	if host, ok := got.GetOriginHost(); !ok || host != "local.example.com" {
		t.Fatalf("Origin-Host = %q (present=%v), want local.example.com", host, ok)
	}
}

func TestDPAOnConnEchoesTheDisconnectRequestIdentifiers(t *testing.T) {
	p := senderPeer(t)
	conn := newMockConn()
	req := createTestDPR("remote.example.com", "remote.realm", types.DisconnectBusy)
	req.HopByHopID = 0x11121314
	req.EndToEndID = 0x15161718

	if err := p.sendDPAOnConn(conn, req); err != nil {
		t.Fatalf("sendDPAOnConn: %v", err)
	}

	got := decodeOnlyWrittenMessage(t, conn)
	assertAnswerCorrelates(t, got, req)
	if got.CommandCode != types.CmdCodeDisconnectPeer {
		t.Fatalf("command = %d, want DPA", got.CommandCode)
	}
}

func TestDPROnConnCarriesDisconnectCauseAndFreshIdentifiers(t *testing.T) {
	p := senderPeer(t)
	conn := newMockConn()

	if err := p.sendDPROnConn(conn, types.DisconnectDoNotWantToTalk); err != nil {
		t.Fatalf("sendDPROnConn: %v", err)
	}

	got := decodeOnlyWrittenMessage(t, conn)
	if !got.IsRequest() || got.CommandCode != types.CmdCodeDisconnectPeer {
		t.Fatalf("sent command %d request=%v, want DPR", got.CommandCode, got.IsRequest())
	}
	if got.HopByHopID == 0 || got.EndToEndID == 0 {
		t.Fatalf("DPR identifiers were not assigned: hbh %d e2e %d", got.HopByHopID, got.EndToEndID)
	}
	avp := got.FindAVP(types.AVPCodeDisconnectCause, 0)
	if avp == nil {
		t.Fatal("DPR missing Disconnect-Cause")
	}
	cause, err := avp.GetUnsigned32()
	if err != nil {
		t.Fatalf("reading Disconnect-Cause: %v", err)
	}
	if cause != uint32(types.DisconnectDoNotWantToTalk) {
		t.Fatalf("Disconnect-Cause = %d, want %d", cause, types.DisconnectDoNotWantToTalk)
	}
	if p.PendingCount() != 1 {
		t.Fatalf("DPR request was not tracked for DPA correlation, pending=%d", p.PendingCount())
	}
}

func TestProtocolSendersReturnErrorWithoutAConnection(t *testing.T) {
	p := senderPeer(t)
	if err := p.sendDWAOnConn(nil, createTestDWR("remote.example.com")); err == nil {
		t.Fatal("sendDWAOnConn without a connection succeeded")
	}
	if err := p.sendDPROnConn(nil, types.DisconnectRebooting); err == nil {
		t.Fatal("sendDPROnConn without a connection succeeded")
	}
	if err := p.sendDPAOnConn(nil, createTestDPR("remote.example.com", "remote.realm", types.DisconnectBusy)); err == nil {
		t.Fatal("sendDPAOnConn without a connection succeeded")
	}
}
