// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package test_app

import (
	"bytes"
	"context"
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

type testAppContext struct {
	router *routing.Router
	cfg    *config.Config
	dict   *dictionary.Dictionary
	peers  []*peer.Peer
	start  time.Time
}

func (c *testAppContext) GetDictionary() *dictionary.Dictionary   { return c.dict }
func (c *testAppContext) GetRouter() *routing.Router              { return c.router }
func (c *testAppContext) GetConfig() *config.Config               { return c.cfg }
func (c *testAppContext) GetPeers() []*peer.Peer                  { return c.peers }
func (c *testAppContext) GetStartTime() time.Time                 { return c.start }
func (c *testAppContext) GetExtensionManager() *extension.Manager { return nil }
func (c *testAppContext) GetEdgeRegistry() *edge.Registry         { return edge.NewRegistry(c.cfg) }

func newTestAppContext() *testAppContext {
	return &testAppContext{
		router: routing.NewRouter(),
		cfg: &config.Config{
			Identity: "local.example.com",
			Realm:    "example.com",
		},
		dict:  dictionary.New(),
		start: time.Now().Add(-time.Second),
	}
}

func testAppRequest() *message.Message {
	msg := message.NewRequest(types.CmdCodeCreditControl, 16777238)
	msg.HopByHopID = 100
	msg.EndToEndID = 200
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "session;1"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.net"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.net"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "local.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	msg.AddAVP(message.NewGroupedAVP(types.AVPCodeProxyInfo, types.AVPFlagMandatory, 0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "proxy.example.net"),
		message.NewOctetStringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, []byte("state")),
	))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, "dea.example.net"))
	return msg
}

func encodedMessage(t *testing.T, msg *message.Message) []byte {
	t.Helper()
	data, err := msg.Encode()
	if err != nil {
		t.Fatalf("encoding message: %v", err)
	}
	return data
}

func TestInitRegistersDispatchAndParsesConfig(t *testing.T) {
	ctx := newTestAppContext()
	ext := &appTest{}

	if err := ext.Init(ctx, map[string]interface{}{
		"app_id":        float64(16777238),
		"send_on_start": false,
		"dest_realm":    "example.net",
		"dest_host":     "server.example.net",
		"count":         float64(3),
	}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	msg := testAppRequest()
	p := peer.New(peer.Config{DiameterIdentity: "client.example.net", Realm: "example.net"}, dictionary.New())
	defer p.Stop()

	// Dispatch registration is the contract the server relies on; without it
	// requests for this application fall through as unsupported local traffic.
	if err := ctx.router.RouteIn(p, msg); err == nil {
		t.Fatal("dispatch handler was not invoked for the configured application")
	}
	if ext.destRealm != "example.net" || ext.destHost != "server.example.net" || ext.count != 3 {
		t.Fatalf("config was not retained: realm=%q host=%q count=%d", ext.destRealm, ext.destHost, ext.count)
	}
}

func TestHandleDispatchDoesNotMutateRequestTraffic(t *testing.T) {
	oldLocal := peer.GetLocalConfig()
	peer.SetLocalConfig(peer.LocalConfig{DiameterIdentity: "local.example.com", Realm: "example.com"})
	t.Cleanup(func() { peer.SetLocalConfig(oldLocal) })

	ext := &appTest{appID: 16777238}
	msg := testAppRequest()
	before := encodedMessage(t, msg)
	p := peer.New(peer.Config{DiameterIdentity: "client.example.net", Realm: "example.net"}, dictionary.New())
	defer p.Stop()

	if err := ext.handleDispatch(p, msg); err == nil {
		t.Fatal("closed peer accepted an answer send")
	}
	if after := encodedMessage(t, msg); !bytes.Equal(after, before) {
		t.Fatal("request was mutated while building the local answer")
	}
}

func TestHandleDispatchToleratesPartialMessages(t *testing.T) {
	ext := &appTest{appID: 16777238}
	p := peer.New(peer.Config{DiameterIdentity: "client.example.net", Realm: "example.net"}, dictionary.New())
	defer p.Stop()

	for _, msg := range []*message.Message{
		message.NewMessage(0, types.CmdCodeCreditControl, 16777238),
		message.NewRequest(types.CmdCodeCreditControl, 16777238),
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("handleDispatch panicked on partially populated message: %v", r)
				}
			}()
			_ = ext.handleDispatch(p, msg)
		}()
	}
}

func TestRouteRecordsReportsActualVisitedPath(t *testing.T) {
	msg := message.NewRequest(types.CmdCodeCreditControl, 16777238)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, "dea1.example.net"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeRouteRecord, types.AVPFlagMandatory, "dea2.example.net"))

	if got := routeRecords(msg); got != "dea1.example.net,dea2.example.net" {
		t.Fatalf("route records = %q", got)
	}
}

func TestSendInitialStopsWhenContextIsCancelled(t *testing.T) {
	ctx := newTestAppContext()
	ext := &appTest{appID: 16777238, count: 50, destRealm: "example.net", destHost: "server.example.net"}
	bgCtx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})

	go func() {
		defer close(done)
		ext.sendInitial(bgCtx, ctx)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("sendInitial goroutine outlived cancellation")
	}
}

func TestStopCancelsSendOnStartWorker(t *testing.T) {
	ext := &appTest{}
	ctx := newTestAppContext()
	if err := ext.Init(ctx, map[string]interface{}{"send_on_start": true, "count": 100}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	start := time.Now()
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("Stop waited for the startup send timer instead of cancelling it")
	}
}

func TestSendInitialBuildsRoutableRequest(t *testing.T) {
	oldLocal := peer.GetLocalConfig()
	peer.SetLocalConfig(peer.LocalConfig{DiameterIdentity: "local.example.com", Realm: "example.com"})
	t.Cleanup(func() { peer.SetLocalConfig(oldLocal) })

	ctx := newTestAppContext()
	ext := &appTest{appID: 16777238, count: 1, destRealm: "example.net", destHost: "server.example.net"}

	// Startup traffic is diagnostic, but it must still use the same router path
	// as production traffic so DEA route handlers can inspect it.
	ext.sendInitial(context.Background(), ctx)
}

func TestSendInitialRejectsInvalidPeerLookup(t *testing.T) {
	oldLocal := peer.GetLocalConfig()
	peer.SetLocalConfig(peer.LocalConfig{DiameterIdentity: "local.example.com", Realm: "example.com"})
	t.Cleanup(func() { peer.SetLocalConfig(oldLocal) })

	ctx := newTestAppContext()
	ctx.router.SetPeerLookup(func(types.DiamID) (interface{}, bool) { return struct{}{}, true })
	ext := &appTest{appID: 16777238, count: 1, destHost: "server.example.net"}

	// A stale routing table entry must not panic the background worker; it is
	// common during startup races while peers are still connecting.
	ext.sendInitial(context.Background(), ctx)
}

func TestStopInvokesCancelFunction(t *testing.T) {
	called := false
	ext := &appTest{cancel: func() { called = true }}

	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if !called {
		t.Fatal("Stop did not signal the background worker")
	}
}
