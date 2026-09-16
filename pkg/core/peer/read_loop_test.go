// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package peer

import (
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func readLoopPeer(t *testing.T, initiator bool, conn *mockConn) *Peer {
	t.Helper()

	p := New(testConfig("reader.example.com"), testDict())
	t.Cleanup(p.Stop)
	p.mu.Lock()
	if initiator {
		p.iConn = conn
	} else {
		p.rConn = conn
	}
	p.mu.Unlock()
	return p
}

func nextPeerEvent(t *testing.T, p *Peer) eventMessage {
	t.Helper()

	select {
	case ev := <-p.eventChan:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for read-loop event")
		return eventMessage{}
	}
}

func encodeForReadLoop(t *testing.T, msg *message.Message) []byte {
	t.Helper()

	data, err := msg.Encode()
	if err != nil {
		t.Fatalf("encoding staged message: %v", err)
	}
	return data
}

func writeRawReadBytes(t *testing.T, conn *mockConn, data []byte) {
	t.Helper()

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if _, err := conn.readBuf.Write(data); err != nil {
		t.Fatalf("staging raw read bytes: %v", err)
	}
}

func assertReadLoopDisconnect(t *testing.T, ev eventMessage, want Event, errContains error) {
	t.Helper()

	if ev.event != want {
		t.Fatalf("event = %v, want %v", ev.event, want)
	}
	if ev.err == nil {
		t.Fatal("disconnect event carried no read error")
	}
	if errContains != nil && !errors.Is(ev.err, errContains) {
		t.Fatalf("error = %v, want it to wrap %v", ev.err, errContains)
	}
}

func TestReadLoopClassifiesInitiatorMessagesFromWire(t *testing.T) {
	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.HopByHopID = 91
	req.EndToEndID = 92

	cer := createTestCER("remote.example.com", "remote.realm")
	cea := createTestCEA(cer, "remote.example.com", "remote.realm", types.ResultSuccess)
	dwr := createTestDWR("remote.example.com")
	dwa := createTestDWA(dwr, "remote.example.com", "remote.realm")
	dpr := createTestDPR("remote.example.com", "remote.realm", types.DisconnectBusy)
	dpa := message.NewAnswer(dpr)
	dpa.SetResultCode(types.ResultSuccess)

	tests := []struct {
		name string
		msg  *message.Message
		want Event
	}{
		{name: "CEA completes initiator capabilities exchange", msg: cea, want: EventIRcvCEA},
		{name: "CER on initiator is a protocol event", msg: cer, want: EventIRcvCER},
		{name: "DWR asks for watchdog answer", msg: dwr, want: EventIRcvDWR},
		{name: "DWA answers our watchdog", msg: dwa, want: EventIRcvDWA},
		{name: "DPR begins disconnect", msg: dpr, want: EventIRcvDPR},
		{name: "DPA completes disconnect", msg: dpa, want: EventIRcvDPA},
		{name: "application request is delivered to application path", msg: req, want: EventIRcvMessage},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := newMockConn()
			if err := conn.WriteMessage(tc.msg); err != nil {
				t.Fatalf("staging message: %v", err)
			}
			p := readLoopPeer(t, true, conn)

			done := make(chan struct{})
			go func() {
				p.readLoopI()
				close(done)
			}()

			ev := nextPeerEvent(t, p)
			if ev.event != tc.want {
				t.Fatalf("event = %v, want %v", ev.event, tc.want)
			}
			if ev.msg == nil || ev.msg.CommandCode != tc.msg.CommandCode || ev.msg.IsRequest() != tc.msg.IsRequest() {
				t.Fatalf("read-loop delivered the wrong message: %#v", ev.msg)
			}

			stats := p.Stats()
			if stats.MessagesReceived != 1 || stats.BytesReceived != uint64(tc.msg.EncodedLen()) { //nolint:gosec // EncodedLen is a Diameter frame length from test data.
				t.Fatalf("stats = messages %d bytes %d, want one %d-byte message", stats.MessagesReceived, stats.BytesReceived, tc.msg.EncodedLen())
			}
			assertReadLoopDisconnect(t, nextPeerEvent(t, p), EventIPeerDisc, io.EOF)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("initiator read loop did not exit after EOF")
			}
		})
	}
}

func TestReadLoopClassifiesResponderMessagesFromWire(t *testing.T) {
	app := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	app.HopByHopID = 710
	app.EndToEndID = 711

	for _, tc := range []struct {
		name string
		msg  *message.Message
		want Event
	}{
		{name: "CER starts responder capabilities exchange", msg: createTestCER("remote.example.com", "remote.realm"), want: EventRRcvCER},
		{name: "DWR asks responder side for a DWA", msg: createTestDWR("remote.example.com"), want: EventRRcvDWR},
		{name: "DPR asks responder side for a DPA", msg: createTestDPR("remote.example.com", "remote.realm", types.DisconnectDoNotWantToTalk), want: EventRRcvDPR},
		{name: "application request is marked for responder delivery", msg: app, want: EventRRcvMessage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := newMockConn()
			if err := conn.WriteMessage(tc.msg); err != nil {
				t.Fatalf("staging message: %v", err)
			}
			p := readLoopPeer(t, false, conn)

			done := make(chan struct{})
			go func() {
				p.readLoopR()
				close(done)
			}()

			if ev := nextPeerEvent(t, p); ev.event != tc.want {
				t.Fatalf("event = %v, want %v", ev.event, tc.want)
			}
			assertReadLoopDisconnect(t, nextPeerEvent(t, p), EventRPeerDisc, io.EOF)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("responder read loop did not exit after EOF")
			}
		})
	}
}

func TestReadLoopReportsMalformedOrTruncatedFramesAsDisconnects(t *testing.T) {
	valid := encodeForReadLoop(t, createTestDWR("remote.example.com"))

	tests := []struct {
		name string
		data []byte
	}{
		{name: "short header", data: valid[:types.DiameterHeaderSize-1]},
		{name: "peer closes mid-body", data: valid[:types.DiameterHeaderSize+2]},
		{name: "invalid declared length", data: append([]byte{types.DiameterVersion, 0, 0, byte(types.DiameterHeaderSize - 1)}, valid[4:types.DiameterHeaderSize]...)},
		{name: "declared length exceeds available body", data: func() []byte {
			frame := append([]byte(nil), valid[:types.DiameterHeaderSize]...)
			frame[1] = 0
			frame[2] = 0x10
			frame[3] = 0x01
			return frame
		}()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := newMockConn()
			writeRawReadBytes(t, conn, tc.data)
			p := readLoopPeer(t, true, conn)

			done := make(chan struct{})
			go func() {
				p.readLoopI()
				close(done)
			}()

			assertReadLoopDisconnect(t, nextPeerEvent(t, p), EventIPeerDisc, nil)
			if got := p.Stats().MessagesReceived; got != 0 {
				t.Fatalf("malformed frame counted as %d received messages", got)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("read loop did not exit after malformed frame")
			}
		})
	}
}

func TestReadLoopConsumesOneCompleteFrameBeforeReportingTruncation(t *testing.T) {
	conn := newMockConn()
	first := createTestDWR("remote.example.com")
	if err := conn.WriteMessage(first); err != nil {
		t.Fatalf("staging complete message: %v", err)
	}
	truncated := encodeForReadLoop(t, createTestDPR("remote.example.com", "remote.realm", types.DisconnectBusy))
	writeRawReadBytes(t, conn, truncated[:types.DiameterHeaderSize+1])
	p := readLoopPeer(t, false, conn)

	done := make(chan struct{})
	go func() {
		p.readLoopR()
		close(done)
	}()

	if ev := nextPeerEvent(t, p); ev.event != EventRRcvDWR {
		t.Fatalf("first event = %v, want R-Rcv-DWR", ev.event)
	}
	assertReadLoopDisconnect(t, nextPeerEvent(t, p), EventRPeerDisc, nil)
	if got := p.Stats().MessagesReceived; got != 1 {
		t.Fatalf("messages received = %d, want exactly the complete frame", got)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("read loop did not exit after truncated second frame")
	}
}

func TestReadLoopReportsUnsupportedDiameterVersion(t *testing.T) {
	conn := newMockConn()
	frame := make([]byte, types.DiameterHeaderSize)
	frame[0] = 2
	binary.BigEndian.PutUint32(frame[:4], uint32(types.DiameterHeaderSize))
	frame[0] = 2
	writeRawReadBytes(t, conn, frame)
	p := readLoopPeer(t, false, conn)

	done := make(chan struct{})
	go func() {
		p.readLoopR()
		close(done)
	}()

	assertReadLoopDisconnect(t, nextPeerEvent(t, p), EventRPeerDisc, nil)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("read loop did not exit after bad version")
	}
}
