// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package prom_metrics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// --- Mock InitContext ---

type mockInitContext struct {
	router    *routing.Router
	dict      *dictionary.Dictionary
	mgr       *extension.Manager
	startTime time.Time
	cfg       *config.Config
	peers     []*peer.Peer
}

func (m *mockInitContext) GetDictionary() *dictionary.Dictionary { return m.dict }
func (m *mockInitContext) GetRouter() *routing.Router            { return m.router }
func (m *mockInitContext) GetConfig() *config.Config             { return m.cfg }
func (m *mockInitContext) GetPeers() []*peer.Peer                { return m.peers }
func (m *mockInitContext) GetStartTime() time.Time               { return m.startTime }
func (m *mockInitContext) GetExtensionManager() *extension.Manager {
	return m.mgr
}

func newTestContext() *mockInitContext {
	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()

	router := routing.NewRouter()
	mgr := extension.NewManager(nil)

	return &mockInitContext{
		router:    router,
		dict:      dict,
		mgr:       mgr,
		startTime: time.Now().Add(-60 * time.Second), // started 60s ago
		cfg: &config.Config{
			Identity: "test.example.com",
			Realm:    "example.com",
		},
	}
}

func TestCollectorDescribe(t *testing.T) {
	ctx := newTestContext()
	collector := newDiameterCollector(ctx)

	descCh := make(chan *prometheus.Desc, 100)
	collector.Describe(descCh)
	close(descCh)

	var descs []*prometheus.Desc
	for d := range descCh {
		descs = append(descs, d)
	}

	// We expect 23 metric descriptors
	if len(descs) != 23 {
		t.Errorf("expected 23 descriptors, got %d", len(descs))
	}
}

func TestCollectorCollect(t *testing.T) {
	ctx := newTestContext()
	collector := newDiameterCollector(ctx)

	metricCh := make(chan prometheus.Metric, 100)
	collector.Collect(metricCh)
	close(metricCh)

	var metrics []prometheus.Metric
	for m := range metricCh {
		metrics = append(metrics, m)
	}

	// With 0 peers we should get:
	// 2 server metrics + 1 peers_total + 6 routing + 4 dictionary = 13
	// Extension metrics depend on manager state.
	if len(metrics) < 13 {
		t.Errorf("expected at least 13 metrics, got %d", len(metrics))
	}
}

func TestMetricsHTTPEndpoint(t *testing.T) {
	ctx := newTestContext()

	registry := prometheus.NewRegistry()
	registry.MustRegister(newDiameterCollector(ctx))

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	text := string(body)

	// Verify key metric families appear in the output
	expectedMetrics := []string{
		"diameter_server_info",
		"diameter_server_uptime_seconds",
		"diameter_peers_total",
		"diameter_routing_requests_total",
		"diameter_routing_relayed_total",
		"diameter_routing_dispatched_total",
		"diameter_routing_errors_total",
		"diameter_routing_loops_detected_total",
		"diameter_routing_answers_relayed_total",
		"diameter_dictionary_vendors",
		"diameter_dictionary_applications",
		"diameter_dictionary_avps",
		"diameter_dictionary_commands",
	}

	for _, name := range expectedMetrics {
		if !strings.Contains(text, name) {
			t.Errorf("expected metric %q not found in output", name)
		}
	}

	// Verify server info label values
	if !strings.Contains(text, `identity="test.example.com"`) {
		t.Error("expected identity label in server_info metric")
	}
	if !strings.Contains(text, `realm="example.com"`) {
		t.Error("expected realm label in server_info metric")
	}
}

func TestCollectorServerUptime(t *testing.T) {
	ctx := newTestContext()
	collector := newDiameterCollector(ctx)

	metricCh := make(chan prometheus.Metric, 100)
	collector.collectServer(metricCh)
	close(metricCh)

	var count int
	for range metricCh {
		count++
	}

	// Should have server_info and server_uptime
	if count != 2 {
		t.Errorf("expected 2 server metrics, got %d", count)
	}
}

func TestCollectorDictionary(t *testing.T) {
	ctx := newTestContext()
	collector := newDiameterCollector(ctx)

	metricCh := make(chan prometheus.Metric, 100)
	collector.collectDictionary(metricCh)
	close(metricCh)

	var count int
	for range metricCh {
		count++
	}

	// Should have vendors, applications, avps, commands
	if count != 4 {
		t.Errorf("expected 4 dictionary metrics, got %d", count)
	}
}

func TestCollectorRouting(t *testing.T) {
	ctx := newTestContext()
	collector := newDiameterCollector(ctx)

	metricCh := make(chan prometheus.Metric, 100)
	collector.collectRouting(metricCh)
	close(metricCh)

	var count int
	for range metricCh {
		count++
	}

	// Should have 6 routing metrics
	if count != 6 {
		t.Errorf("expected 6 routing metrics, got %d", count)
	}
}

// --- Mock extension for testing ---

type mockExt struct {
	name string
}

func (e *mockExt) Name() string { return e.name }
func (e *mockExt) Init(_ extension.InitContext, _ map[string]interface{}) error {
	return nil
}
func (e *mockExt) Stop() error { return nil }

// --- Tests with peer data ---

func TestCollectorWithPeers(t *testing.T) {
	ctx := newTestContext()
	// Create a peer (will be in Closed state since we don't start it)
	p := peer.New(peer.Config{
		DiameterIdentity: "peer1.example.com",
		Realm:            "example.com",
	}, ctx.dict)
	ctx.peers = []*peer.Peer{p}

	collector := newDiameterCollector(ctx)

	metricCh := make(chan prometheus.Metric, 100)
	collector.collectPeers(metricCh)
	close(metricCh)

	var count int
	for range metricCh {
		count++
	}

	// peers_total (1) + per-peer: up, msgs_sent, msgs_recv, bytes_sent, bytes_recv = 6
	// No connection_duration since ConnectedSince is zero
	if count != 6 {
		t.Errorf("expected 6 peer metrics, got %d", count)
	}
}

func TestCollectorPeerLabelsInHTTPOutput(t *testing.T) {
	ctx := newTestContext()
	p := peer.New(peer.Config{
		DiameterIdentity: "peer1.example.com",
		Realm:            "example.com",
	}, ctx.dict)
	ctx.peers = []*peer.Peer{p}

	registry := prometheus.NewRegistry()
	registry.MustRegister(newDiameterCollector(ctx))

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	text := string(body)

	// Peer is not started, so DiameterID() returns "" (set during CER exchange)
	// and IsOpen() returns false making up=0
	if !strings.Contains(text, `diameter_peer_up{partner="",peer="",realm="",zone=""} 0`) {
		t.Error("expected peer_up=0 for unstarted peer")
	}
	if !strings.Contains(text, "diameter_peers_total 1") {
		t.Error("expected peers_total=1")
	}
	if !strings.Contains(text, "diameter_peer_messages_sent_total") {
		t.Error("expected peer_messages_sent_total metric")
	}
}

func TestCollectorMultiplePeers(t *testing.T) {
	ctx := newTestContext()
	p1 := peer.New(peer.Config{DiameterIdentity: "peer1.example.com", Realm: "example.com"}, ctx.dict)
	p2 := peer.New(peer.Config{DiameterIdentity: "peer2.other.com", Realm: "other.com"}, ctx.dict)
	ctx.peers = []*peer.Peer{p1, p2}

	collector := newDiameterCollector(ctx)

	metricCh := make(chan prometheus.Metric, 100)
	collector.collectPeers(metricCh)
	close(metricCh)

	var count int
	for range metricCh {
		count++
	}

	// peers_total (1) + 2 peers * 5 metrics each = 11
	if count != 11 {
		t.Errorf("expected 11 peer metrics for 2 peers, got %d", count)
	}
}

// --- Routing stat values via HTTP ---

func TestRoutingStatsReflectedInMetrics(t *testing.T) {
	ctx := newTestContext()
	router := ctx.router
	router.SetLocalIdentity("local.example.com", "example.com")

	// Register a dispatch handler so RouteIn can dispatch locally
	router.RegisterDispatchHandler(types.AppIDCreditControl, func(_ *peer.Peer, _ *message.Message) error {
		return nil
	})

	// Route 3 requests through dispatch
	for i := 0; i < 3; i++ {
		msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
		msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "local.example.com"))
		_ = router.RouteIn(nil, msg)
	}

	// Verify via HTTP metrics output
	registry := prometheus.NewRegistry()
	registry.MustRegister(newDiameterCollector(ctx))

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	text := string(body)

	if !strings.Contains(text, "diameter_routing_requests_total 3") {
		t.Errorf("expected routing_requests_total=3 in output, got:\n%s", filterLines(text, "routing_requests"))
	}
	if !strings.Contains(text, "diameter_routing_dispatched_total 3") {
		t.Errorf("expected routing_dispatched_total=3 in output, got:\n%s", filterLines(text, "routing_dispatched"))
	}
	if !strings.Contains(text, "diameter_routing_errors_total 0") {
		t.Errorf("expected routing_errors_total=0 in output, got:\n%s", filterLines(text, "routing_errors"))
	}
}

// --- Extension state metrics ---

func TestCollectorExtensionStates(t *testing.T) {
	ctx := newTestContext()
	mgr := ctx.mgr

	// Add mock extensions in different states
	ext1 := &mockExt{name: "ext_a"}
	ext2 := &mockExt{name: "ext_b"}
	ext3 := &mockExt{name: "ext_c"}
	mgr.AddExtension(ext1)
	mgr.AddExtension(ext2)
	mgr.AddExtension(ext3)

	// Enable two of them (they move to "active" state)
	_ = mgr.Enable("ext_a", nil)
	_ = mgr.Enable("ext_b", nil)
	// ext_c stays in "registered" state

	collector := newDiameterCollector(ctx)

	metricCh := make(chan prometheus.Metric, 100)
	collector.collectExtensions(metricCh)
	close(metricCh)

	var count int
	for range metricCh {
		count++
	}

	// Should have 2 state entries: "active" and "registered"
	if count != 2 {
		t.Errorf("expected 2 extension state metrics, got %d", count)
	}
}

func TestCollectorExtensionStatesInHTTPOutput(t *testing.T) {
	ctx := newTestContext()
	mgr := ctx.mgr

	ext1 := &mockExt{name: "ext_a"}
	ext2 := &mockExt{name: "ext_b"}
	mgr.AddExtension(ext1)
	mgr.AddExtension(ext2)
	_ = mgr.Enable("ext_a", nil)

	registry := prometheus.NewRegistry()
	registry.MustRegister(newDiameterCollector(ctx))

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	text := string(body)

	if !strings.Contains(text, `diameter_extensions_total{state="active"} 1`) {
		t.Error("expected extensions_total active=1")
	}
	if !strings.Contains(text, `diameter_extensions_total{state="registered"} 1`) {
		t.Error("expected extensions_total registered=1")
	}
}

// --- Nil manager edge case ---

func TestCollectorNilManager(t *testing.T) {
	ctx := newTestContext()
	ctx.mgr = nil

	collector := newDiameterCollector(ctx)

	metricCh := make(chan prometheus.Metric, 100)
	collector.collectExtensions(metricCh)
	close(metricCh)

	var count int
	for range metricCh {
		count++
	}

	if count != 0 {
		t.Errorf("expected 0 extension metrics with nil manager, got %d", count)
	}
}

// --- Dictionary values ---

func TestCollectorDictionaryValues(t *testing.T) {
	ctx := newTestContext()

	registry := prometheus.NewRegistry()
	registry.MustRegister(newDiameterCollector(ctx))

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	text := string(body)

	// After LoadBaseProtocol, dictionary should have non-zero counts
	stats := ctx.dict.Stats()
	expectedVendors := fmt.Sprintf("diameter_dictionary_vendors %d", stats.Vendors)
	if !strings.Contains(text, expectedVendors) {
		t.Errorf("expected %q in output", expectedVendors)
	}
	expectedAVPs := fmt.Sprintf("diameter_dictionary_avps %d", stats.AVPs)
	if !strings.Contains(text, expectedAVPs) {
		t.Errorf("expected %q in output", expectedAVPs)
	}
}

// --- Server uptime ---

func TestCollectorUptimePositive(t *testing.T) {
	ctx := newTestContext()

	registry := prometheus.NewRegistry()
	registry.MustRegister(newDiameterCollector(ctx))

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	text := string(body)

	// Start time is 60s ago, so uptime should be >= 59 seconds
	if !strings.Contains(text, "diameter_server_uptime_seconds") {
		t.Error("expected server_uptime_seconds metric")
	}
	// Verify the uptime is a positive number (rough check)
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "diameter_server_uptime_seconds ") {
			val := strings.TrimPrefix(line, "diameter_server_uptime_seconds ")
			if val == "0" || strings.HasPrefix(val, "-") {
				t.Errorf("expected positive uptime, got %s", val)
			}
		}
	}
}

// filterLines returns lines from text containing substr.
func filterLines(text, substr string) string {
	var result []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, substr) {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

// --- Extension MetricsProvider tests ---

// mockMetricsExt implements Extension + Stoppable + MetricsProvider.
type mockMetricsExt struct {
	name string
}

func (e *mockMetricsExt) Name() string { return e.name }
func (e *mockMetricsExt) Init(_ extension.InitContext, _ map[string]interface{}) error {
	return nil
}
func (e *mockMetricsExt) Stop() error { return nil }
func (e *mockMetricsExt) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "requests_total",
			Help:  "Total requests.",
			Type:  extension.MetricCounter,
			Value: 42,
		},
		{
			Name:   "active_sessions",
			Help:   "Current active sessions.",
			Type:   extension.MetricGauge,
			Value:  7,
			Labels: map[string]string{"type": "voice"},
		},
	}
}

func TestExtensionMetricsCollector(t *testing.T) {
	ctx := newTestContext()
	mgr := ctx.mgr

	ext := &mockMetricsExt{name: "test_ext"}
	mgr.AddExtension(ext)
	_ = mgr.Enable("test_ext", nil)

	registry := prometheus.NewRegistry()
	registry.MustRegister(&extensionMetricsCollector{ctx: ctx})

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	text := string(body)

	if !strings.Contains(text, "diameter_ext_test_ext_requests_total") {
		t.Errorf("expected diameter_ext_test_ext_requests_total in output:\n%s", text)
	}
	if !strings.Contains(text, "42") {
		t.Errorf("expected value 42 in output:\n%s", text)
	}
	if !strings.Contains(text, `diameter_ext_test_ext_active_sessions{type="voice"} 7`) {
		t.Errorf("expected diameter_ext_test_ext_active_sessions with label in output:\n%s", text)
	}
}

func TestExtensionMetricsCollectorNilManager(t *testing.T) {
	ctx := newTestContext()
	ctx.mgr = nil

	collector := &extensionMetricsCollector{ctx: ctx}

	metricCh := make(chan prometheus.Metric, 100)
	collector.Collect(metricCh)
	close(metricCh)

	var count int
	for range metricCh {
		count++
	}

	if count != 0 {
		t.Errorf("expected 0 metrics with nil manager, got %d", count)
	}
}

func TestExtensionMetricsNotCollectedWhenInactive(t *testing.T) {
	ctx := newTestContext()
	mgr := ctx.mgr

	ext := &mockMetricsExt{name: "inactive_ext"}
	mgr.AddExtension(ext)
	// Do not enable — extension stays in "registered" state

	collector := &extensionMetricsCollector{ctx: ctx}

	metricCh := make(chan prometheus.Metric, 100)
	collector.Collect(metricCh)
	close(metricCh)

	var count int
	for range metricCh {
		count++
	}

	if count != 0 {
		t.Errorf("expected 0 metrics for inactive extension, got %d", count)
	}
}

func (m *mockInitContext) GetEdgeRegistry() *edge.Registry {
	return edge.NewRegistry(m.GetConfig())
}
