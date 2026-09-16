// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package app_redirect

import (
	"bytes"
	"errors"
	"net"
	"strings"
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

type redirectInitContext struct{ router *routing.Router }

func (c redirectInitContext) GetDictionary() *dictionary.Dictionary   { return nil }
func (c redirectInitContext) GetRouter() *routing.Router              { return c.router }
func (c redirectInitContext) GetConfig() *config.Config               { return &config.Config{} }
func (c redirectInitContext) GetPeers() []*peer.Peer                  { return nil }
func (c redirectInitContext) GetStartTime() time.Time                 { return time.Now() }
func (c redirectInitContext) GetExtensionManager() *extension.Manager { return nil }
func (c redirectInitContext) GetEdgeRegistry() *edge.Registry         { return nil }

func TestNameAndStop(t *testing.T) {
	ext := &appRedirect{}
	if ext.Name() != "app_redirect" {
		t.Fatal("unexpected extension name")
	}
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestInitWithoutRedirectHostLeavesExtensionInactive(t *testing.T) {
	router := routing.NewRouter()
	ext := &appRedirect{}
	if err := ext.Init(redirectInitContext{router: router}, nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	msg := message.NewRequest(272, types.AppIDCreditControl)
	if err := router.RouteIn(nil, msg); err == nil {
		t.Fatal("unconfigured redirect extension consumed or dispatched the request")
	}
}

func TestInitRegistersConfiguredRedirectHandler(t *testing.T) {
	router := routing.NewRouter()
	ext := &appRedirect{}
	cfg := map[string]interface{}{
		"redirect_host":           "aaa://redirect.example.com",
		"redirect_usage":          float64(1),
		"redirect_max_cache_time": float64(30),
		"apps":                    []interface{}{float64(types.AppIDCreditControl)},
	}
	if err := ext.Init(redirectInitContext{router: router}, cfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	p := peer.New(peer.Config{DiameterIdentity: "client.example.com"}, nil)
	msg := message.NewRequest(272, types.AppIDCreditControl)
	err := router.RouteIn(p, msg)
	if err == nil || !strings.Contains(err.Error(), "peer not in open state") {
		t.Fatalf("RouteIn error = %v, want redirect send attempt against closed peer", err)
	}
}

func TestHandleRoutingPassThroughCases(t *testing.T) {
	ext := &appRedirect{redirectHost: "aaa://redirect.example.com", apps: map[types.ApplicationID]bool{types.AppIDNASREQ: true}}
	candidates := []routing.Route{{PeerIdentity: "next-hop", Score: 10}}

	answer := message.NewAnswer(message.NewRequest(272, types.AppIDCreditControl))
	got, err := ext.handleRouting(nil, answer, candidates)
	if err != nil || len(got) != 1 {
		t.Fatalf("answer pass-through = (%v, %v), want original candidates", got, err)
	}

	request := message.NewRequest(272, types.AppIDCreditControl)
	got, err = ext.handleRouting(nil, request, candidates)
	if err != nil || len(got) != 1 {
		t.Fatalf("unconfigured app pass-through = (%v, %v), want original candidates", got, err)
	}

	ext.redirectHost = ""
	got, err = ext.handleRouting(nil, request, candidates)
	if err != nil || len(got) != 1 {
		t.Fatalf("inactive pass-through = (%v, %v), want original candidates", got, err)
	}
}

func TestHandleRoutingLeavesRequestsForLocalHostWithoutAppFilter(t *testing.T) {
	peer.SetLocalConfig(peer.LocalConfig{DiameterIdentity: "edge.example.com", Realm: "example.com"})
	ext := &appRedirect{redirectHost: "aaa://redirect.example.com"}
	msg := message.NewRequest(272, types.AppIDCreditControl)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "edge.example.com"))
	candidates := []routing.Route{{PeerIdentity: "local", Score: 1}}

	got, err := ext.handleRouting(nil, msg, candidates)
	if err != nil || len(got) != 1 {
		t.Fatalf("local host pass-through = (%v, %v), want original candidates", got, err)
	}
}

func TestHandleRoutingSendsRedirectAnswer(t *testing.T) {
	peer.SetLocalConfig(peer.LocalConfig{DiameterIdentity: "edge.example.com", Realm: "example.com"})
	p, conn := openPeer(t, "client.example.com")
	ext := &appRedirect{redirectHost: "aaa://redirect.example.com", usage: 1, maxCacheTime: 30}
	msg := message.NewRequest(272, types.AppIDCreditControl)

	got, err := ext.handleRouting(p, msg, nil)
	if !errors.Is(err, routing.ErrConsumed) || got != nil {
		t.Fatalf("redirect result = (%v, %v), want consumed", got, err)
	}
	ans := conn.waitForMessage(t, 1)
	if code, ok := ans.GetResultCode(); !ok || code != types.ResultRedirectIndication {
		t.Fatalf("Result-Code = %d (present=%v), want redirect indication", code, ok)
	}
	if host := ans.FindAVP(types.AVPCodeRedirectHost, 0); host == nil || string(host.Data) != "aaa://redirect.example.com" {
		t.Fatalf("Redirect-Host AVP = %+v, want configured host", host)
	}
	if usage := ans.FindAVP(types.AVPCodeRedirectHostUsage, 0); usage == nil {
		t.Fatal("Redirect-Host-Usage missing")
	} else if got, err := usage.GetUnsigned32(); err != nil || got != 1 {
		t.Fatalf("Redirect-Host-Usage = %d (err=%v), want 1", got, err)
	}
	if cache := ans.FindAVP(types.AVPCodeRedirectMaxCacheTime, 0); cache == nil {
		t.Fatal("Redirect-Max-Cache-Time missing")
	} else if got, err := cache.GetUnsigned32(); err != nil || got != 30 {
		t.Fatalf("Redirect-Max-Cache-Time = %d (err=%v), want 30", got, err)
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

func openPeer(t *testing.T, identity string) (*peer.Peer, *captureConn) {
	t.Helper()
	p := peer.New(peer.Config{DiameterIdentity: types.DiamID(identity)}, nil)
	conn := newCaptureConn()
	cer := message.NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	cer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(identity)))
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
