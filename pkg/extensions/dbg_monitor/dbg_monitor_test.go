// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dbg_monitor

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
)

type monitorContext struct {
	router *routing.Router
	cfg    *config.Config
	dict   *dictionary.Dictionary
	peers  []*peer.Peer
	start  time.Time
}

func (c *monitorContext) GetDictionary() *dictionary.Dictionary   { return c.dict }
func (c *monitorContext) GetRouter() *routing.Router              { return c.router }
func (c *monitorContext) GetConfig() *config.Config               { return c.cfg }
func (c *monitorContext) GetPeers() []*peer.Peer                  { return c.peers }
func (c *monitorContext) GetStartTime() time.Time                 { return c.start }
func (c *monitorContext) GetExtensionManager() *extension.Manager { return nil }
func (c *monitorContext) GetEdgeRegistry() *edge.Registry         { return edge.NewRegistry(c.cfg) }

func newMonitorContext(peers ...*peer.Peer) *monitorContext {
	dict := dictionary.New()
	if err := dict.AddVendor(&dictionary.Vendor{ID: 10415, Name: "3GPP"}); err != nil {
		panic(err)
	}
	return &monitorContext{
		router: routing.NewRouter(),
		cfg:    &config.Config{Identity: "local.example.com", Realm: "example.com"},
		dict:   dict,
		peers:  peers,
		start:  time.Now().Add(-2 * time.Second),
	}
}

func decodeResponse[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	var out T
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding JSON response: %v", err)
	}
	return out
}

func TestStatusHandlerReportsCoreIdentityWithoutMutation(t *testing.T) {
	ctx := newMonitorContext()
	ext := &dbgMonitor{ctx: ctx}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()

	ext.handleStatus(rr, req)
	resp := decodeResponse[StatusResponse](t, rr)

	if resp.Identity != "local.example.com" || resp.Realm != "example.com" || resp.Peers != 0 {
		t.Fatalf("status response = %+v", resp)
	}
	if resp.StartTime.IsZero() || resp.Uptime == "" {
		t.Fatalf("status omitted time fields: %+v", resp)
	}
	if ctx.cfg.Identity != "local.example.com" || len(ctx.peers) != 0 {
		t.Fatal("status handler mutated monitor context")
	}
}

func TestPeersHandlerToleratesPartiallyPopulatedPeers(t *testing.T) {
	p := peer.New(peer.Config{DiameterIdentity: "peer.example.net", Realm: "example.net"}, dictionary.New())
	defer p.Stop()
	ext := &dbgMonitor{ctx: newMonitorContext(p)}
	rr := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("peers handler panicked on unopened peer: %v", r)
		}
	}()
	ext.handlePeers(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/peers", nil))
	peers := decodeResponse[[]PeerInfo](t, rr)

	if len(peers) != 1 {
		t.Fatalf("peer count = %d, want 1", len(peers))
	}
	if peers[0].State != peer.StateClosed.String() {
		t.Fatalf("peer state = %q, want Closed", peers[0].State)
	}
}

func TestDictHandlerReturnsDictionaryStats(t *testing.T) {
	ext := &dbgMonitor{ctx: newMonitorContext()}
	rr := httptest.NewRecorder()

	ext.handleDict(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/dict", nil))
	stats := decodeResponse[dictionary.Stats](t, rr)

	if stats.Vendors != 1 {
		t.Fatalf("vendor count = %d, want 1", stats.Vendors)
	}
}

func TestInitServesEndpointsAndStopTerminates(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocating monitor port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("closing monitor port probe: %v", err)
	}

	ext := &dbgMonitor{}
	if err := ext.Init(newMonitorContext(), map[string]interface{}{"port": port}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = ext.Stop() })

	client := &http.Client{Timeout: time.Second}
	url := "http://127.0.0.1:" + ext.server.Addr[1:] + "/status"
	var resp *http.Response
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if reqErr != nil {
			t.Fatalf("creating request: %v", reqErr)
		}
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("monitor did not become reachable: %v", err)
	}
	_ = resp.Body.Close()

	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("creating post-stop request: %v", err)
	}
	if resp, err = client.Do(req); err == nil {
		_ = resp.Body.Close()
		t.Fatal("monitor still accepted HTTP requests after Stop")
	}
}

func TestStopWithoutServerIsNoop(t *testing.T) {
	ext := &dbgMonitor{}
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop without Init failed: %v", err)
	}
}

func TestName(t *testing.T) {
	if (&dbgMonitor{}).Name() != "dbg_monitor" {
		t.Fatal("unexpected extension name")
	}
}
