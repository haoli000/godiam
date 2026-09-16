// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package prom_metrics

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
)

func TestPromMetricsNameIsStable(t *testing.T) {
	ext := &promMetrics{}
	if ext.Name() != "prom_metrics" {
		t.Fatalf("Name() = %q, want prom_metrics", ext.Name())
	}
}

func TestInitRegistersCollectorsAndInstallsMetricsHandler(t *testing.T) {
	ctx := newTestContext()
	ext := &promMetrics{}
	if err := ext.Init(ctx, map[string]interface{}{"port": 0, "bind_address": "127.0.0.1"}); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() {
		if err := ext.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	})

	families, err := ext.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() failed: %v", err)
	}
	names := map[string]bool{}
	for _, family := range families {
		names[family.GetName()] = true
	}
	for _, want := range []string{"diameter_server_info", "go_goroutines", "process_cpu_seconds_total"} {
		if !names[want] {
			t.Fatalf("registered metric families missing %q in %v", want, names)
		}
	}

	req := httptestRequest(t, http.MethodGet, "/metrics")
	rec := httptestRecorder()
	ext.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "diameter_server_info") {
		t.Fatalf("metrics handler did not expose diameter metrics:\n%s", rec.Body.String())
	}
}

func TestInitSupportsFloatPortAndServesHTTP(t *testing.T) {
	ctx := newTestContext()
	port := freeLocalPort(t)
	ext := &promMetrics{}
	if err := ext.Init(ctx, map[string]interface{}{"port": float64(port), "bind_address": "127.0.0.1"}); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() {
		if err := ext.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	})

	url := fmt.Sprintf("http://127.0.0.1:%d/metrics", port)
	body := getEventually(t, url)
	if !strings.Contains(body, "diameter_server_info") {
		t.Fatalf("live endpoint missing diameter_server_info:\n%s", body)
	}
}

func TestInitDoesNotReportPortAlreadyInUse(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port

	ext := &promMetrics{}
	if err := ext.Init(newTestContext(), map[string]interface{}{"port": port, "bind_address": "127.0.0.1"}); err != nil {
		t.Fatalf("Init() currently returns nil even when ListenAndServe later fails; got %v", err)
	}
	t.Cleanup(func() { _ = ext.Stop() })

	req := httptestRequest(t, http.MethodGet, "/metrics")
	rec := httptestRecorder()
	ext.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handler status = %d, want 200", rec.Code)
	}
}

func TestPeerMetricLabelsPreserveZoneAndPartner(t *testing.T) {
	ctx := newTestContext()
	p := peer.New(peer.Config{
		DiameterIdentity: "peer1.example.com",
		Realm:            "example.com",
		Zone:             "roaming",
		Partner:          "partner-a",
	}, ctx.dict)
	t.Cleanup(p.Stop)
	ctx.peers = []*peer.Peer{p}

	text := metricsText(t, newDiameterCollector(ctx))
	want := `diameter_peer_up{partner="partner-a",peer="",realm="",zone="roaming"} 0`
	if !strings.Contains(text, want) {
		t.Fatalf("missing peer label set %q in:\n%s", want, filterLines(text, "diameter_peer_up"))
	}
}

func TestExtensionMetricsCollectorSortsLabelsForStableExposition(t *testing.T) {
	ctx := newTestContext()
	mgr := ctx.mgr
	ext := &labelledMetricsExt{name: "labelled_ext"}
	mgr.AddExtension(ext)
	if err := mgr.Enable("labelled_ext", nil); err != nil {
		t.Fatalf("enabling extension: %v", err)
	}

	text := metricsText(t, &extensionMetricsCollector{ctx: ctx})
	want := `diameter_ext_labelled_ext_inflight{alpha="a",zeta="z"} 3`
	if !strings.Contains(text, want) {
		t.Fatalf("labels were not exposed in sorted stable order; want %q in:\n%s", want, text)
	}
}

func TestExtensionMetricsCollectorDescribeIsUnchecked(t *testing.T) {
	ch := make(chan *prometheus.Desc, 1)
	(&extensionMetricsCollector{}).Describe(ch)
	close(ch)
	if got := len(ch); got != 0 {
		t.Fatalf("Describe emitted %d descriptors, want unchecked collector", got)
	}
}

func TestStopWithoutServerIsNoop(t *testing.T) {
	if err := (&promMetrics{}).Stop(); err != nil {
		t.Fatalf("Stop() without server failed: %v", err)
	}
}

type labelledMetricsExt struct {
	name string
}

func (e *labelledMetricsExt) Name() string { return e.name }
func (e *labelledMetricsExt) Init(_ extension.InitContext, _ map[string]interface{}) error {
	return nil
}
func (e *labelledMetricsExt) Metrics() []extension.Metric {
	return []extension.Metric{{
		Name:   "inflight",
		Help:   "Requests in flight.",
		Type:   extension.MetricGauge,
		Value:  3,
		Labels: map[string]string{"zeta": "z", "alpha": "a"},
	}}
}

func metricsText(t *testing.T, collectors ...prometheus.Collector) string {
	t.Helper()
	registry := prometheus.NewRegistry()
	for _, collector := range collectors {
		if err := registry.Register(collector); err != nil {
			t.Fatalf("registering collector: %v", err)
		}
	}
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	req := httptestRequest(t, http.MethodGet, "/metrics")
	rec := httptestRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func freeLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("closing listener: %v", err)
	}
	return port
}

func getEventually(t *testing.T, url string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("creating request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				t.Fatalf("reading response: %v", readErr)
			}
			if resp.StatusCode == http.StatusOK {
				return string(body)
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			t.Fatalf("GET %s did not succeed: %v", url, lastErr)
		case <-ticker.C:
		}
	}
}

func httptestRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, path, nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	return req
}

func httptestRecorder() *responseRecorder {
	return &responseRecorder{ResponseRecorder: httptest.NewRecorder()}
}

type responseRecorder struct {
	*httptest.ResponseRecorder
}
