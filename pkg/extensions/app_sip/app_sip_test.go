// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package app_sip

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type sipInitContext struct{ router *routing.Router }

func (c sipInitContext) GetDictionary() *dictionary.Dictionary   { return nil }
func (c sipInitContext) GetRouter() *routing.Router              { return c.router }
func (c sipInitContext) GetConfig() *config.Config               { return &config.Config{} }
func (c sipInitContext) GetPeers() []*peer.Peer                  { return nil }
func (c sipInitContext) GetStartTime() time.Time                 { return time.Now() }
func (c sipInitContext) GetExtensionManager() *extension.Manager { return nil }
func (c sipInitContext) GetEdgeRegistry() *edge.Registry         { return nil }

func TestNameAndStop(t *testing.T) {
	ext := &appSIP{}
	if ext.Name() != "app_sip" {
		t.Fatal("unexpected extension name")
	}
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestInitRegistersSIPDispatchHandler(t *testing.T) {
	router := routing.NewRouter()
	ext := &appSIP{}
	if err := ext.Init(sipInitContext{router: router}, nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	msg := message.NewAnswer(message.NewRequest(1, types.AppIDSIP))
	if err := router.Dispatch(nil, msg); err != nil {
		t.Fatalf("registered handler rejected a SIP answer: %v", err)
	}
}

func TestHandleSIPIgnoresAnswers(t *testing.T) {
	if err := handleSIP(nil, message.NewAnswer(message.NewRequest(1, types.AppIDSIP))); err != nil {
		t.Fatalf("answer handling returned error: %v", err)
	}
}

func TestHandleSIPReturnsSendErrors(t *testing.T) {
	peer.SetLocalConfig(peer.LocalConfig{DiameterIdentity: "edge.example.com", Realm: "example.com"})
	p := peer.New(peer.Config{DiameterIdentity: "client.example.com"}, nil)
	msg := message.NewRequest(283, types.AppIDSIP)
	if err := handleSIP(p, msg); err == nil {
		t.Fatal("closed peer send error was swallowed")
	}
}

func TestHandleSIPSendsSuccessAnswer(t *testing.T) {
	peer.SetLocalConfig(peer.LocalConfig{DiameterIdentity: "edge.example.com", Realm: "example.com"})
	p, conn := openPeer(t)
	msg := message.NewRequest(283, types.AppIDSIP)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sip-session"))

	if err := handleSIP(p, msg); err != nil {
		t.Fatalf("handleSIP failed: %v", err)
	}
	ans := conn.waitForMessage(t, 1)
	if code, ok := ans.GetResultCode(); !ok || code != types.ResultSuccess {
		t.Fatalf("Result-Code = %d (present=%v), want success", code, ok)
	}
	if sessionID, ok := ans.GetSessionID(); !ok || sessionID != "sip-session" {
		t.Fatalf("Session-Id = %q (present=%v), want request session", sessionID, ok)
	}
	if host, ok := ans.GetOriginHost(); !ok || host != "edge.example.com" {
		t.Fatalf("Origin-Host = %q (present=%v), want local identity", host, ok)
	}
}

type captureConn struct {
	mu     chan struct{}
	buf    bytes.Buffer
	closed chan struct{}
}

func newCaptureConn() *captureConn {
	return &captureConn{mu: make(chan struct{}, 1), closed: make(chan struct{})}
}
func (c *captureConn) Read(_ []byte) (int, error) { <-c.closed; return 0, net.ErrClosed }
func (c *captureConn) Write(b []byte) (int, error) {
	c.mu <- struct{}{}
	defer func() { <-c.mu }()
	return c.buf.Write(b)
}
func (c *captureConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}
func (c *captureConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 3868}
}
func (c *captureConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 3868}
}
func (c *captureConn) SetDeadline(_ time.Time) error      { return nil }
func (c *captureConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *captureConn) SetWriteDeadline(_ time.Time) error { return nil }

func openPeer(t *testing.T) (*peer.Peer, *captureConn) {
	t.Helper()
	p := peer.New(peer.Config{DiameterIdentity: "client.example.com"}, nil)
	conn := newCaptureConn()
	cer := message.NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	cer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
	cer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "client.example.com"))
	if err := p.StartWithConnection(conn, cer); err != nil {
		t.Fatalf("StartWithConnection failed: %v", err)
	}
	t.Cleanup(p.Stop)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if p.State() == peer.StateROpen {
			return p, conn
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("peer did not reach R-Open")
	return nil, nil
}

func (c *captureConn) waitForMessage(t *testing.T, index int) *message.Message {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu <- struct{}{}
		data := append([]byte(nil), c.buf.Bytes()...)
		<-c.mu
		msgs := decodeMessages(t, data)
		if len(msgs) > index {
			return msgs[index]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for Diameter message %d", index)
	return nil
}

func decodeMessages(t *testing.T, data []byte) []*message.Message {
	t.Helper()
	var msgs []*message.Message
	for len(data) >= 20 {
		length := int(data[1])<<16 | int(data[2])<<8 | int(data[3])
		if length < 20 || length > len(data) {
			return msgs
		}
		msg, err := message.DecodeMessage(data[:length])
		if err != nil {
			t.Fatalf("DecodeMessage failed: %v", err)
		}
		msgs = append(msgs, msg)
		data = data[length:]
	}
	return msgs
}
