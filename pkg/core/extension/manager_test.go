// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package extension

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
)

// --- Mock types ---

// mockInitContext satisfies InitContext for testing.
type mockInitContext struct {
	router *routing.Router
}

func (m *mockInitContext) GetDictionary() *dictionary.Dictionary { return nil }
func (m *mockInitContext) GetRouter() *routing.Router            { return m.router }
func (m *mockInitContext) GetConfig() *config.Config             { return &config.Config{} }
func (m *mockInitContext) GetPeers() []*peer.Peer                { return nil }
func (m *mockInitContext) GetStartTime() time.Time               { return time.Now() }
func (m *mockInitContext) GetExtensionManager() *Manager         { return nil }

// basicExt implements only Extension (not Stoppable).
type basicExt struct {
	name      string
	initCount int
	initErr   error
}

func (e *basicExt) Name() string { return e.name }
func (e *basicExt) Init(_ InitContext, _ map[string]interface{}) error {
	e.initCount++
	return e.initErr
}

// stoppableExt implements Extension + Stoppable.
type stoppableExt struct {
	name      string
	initCount int
	stopCount int
	initErr   error
	stopErr   error
}

func (e *stoppableExt) Name() string { return e.name }
func (e *stoppableExt) Init(_ InitContext, _ map[string]interface{}) error {
	e.initCount++
	return e.initErr
}
func (e *stoppableExt) Stop() error {
	e.stopCount++
	return e.stopErr
}

// fullExt implements Extension + Stoppable + Reconfigurable + HealthCheckable.
type fullExt struct {
	name       string
	initCount  int
	stopCount  int
	reconCount int
	lastConfig map[string]interface{}
	initErr    error
	stopErr    error
	reconErr   error
}

func (e *fullExt) Name() string { return e.name }
func (e *fullExt) Init(_ InitContext, cfg map[string]interface{}) error {
	e.initCount++
	e.lastConfig = cfg
	return e.initErr
}
func (e *fullExt) Stop() error {
	e.stopCount++
	return e.stopErr
}
func (e *fullExt) Reconfigure(cfg map[string]interface{}) error {
	e.reconCount++
	e.lastConfig = cfg
	return e.reconErr
}
func (e *fullExt) HealthCheck() (string, map[string]interface{}) {
	return "healthy", map[string]interface{}{"sessions": 42}
}

// newTestManager creates a Manager pre-loaded with the given extensions
// (bypasses the global registry).
func newTestManager(exts ...Extension) *Manager {
	ctx := &mockInitContext{router: routing.NewRouter()}
	m := NewManager(ctx)
	for _, ext := range exts {
		m.extensions[ext.Name()] = &ManagedExtension{
			Extension: ext,
			State:     StateRegistered,
		}
	}
	return m
}

// --- Tests ---

func TestManagerEnableBasic(t *testing.T) {
	ext := &stoppableExt{name: "test_ext"}
	m := newTestManager(ext)

	if err := m.Enable("test_ext", nil); err != nil {
		t.Fatalf("Enable failed: %v", err)
	}
	if ext.initCount != 1 {
		t.Fatalf("expected Init called once, got %d", ext.initCount)
	}

	info, ok := m.Get("test_ext")
	if !ok {
		t.Fatal("Get returned not found")
	}
	if info.State != "active" {
		t.Fatalf("expected state active, got %s", info.State)
	}
	if info.StartedAt == nil {
		t.Fatal("expected StartedAt to be set")
	}
}

func TestManagerEnableUnknown(t *testing.T) {
	m := newTestManager()
	err := m.Enable("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown extension")
	}
	if !strings.Contains(err.Error(), "unknown extension") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerEnableAlreadyActive(t *testing.T) {
	ext := &stoppableExt{name: "test_ext"}
	m := newTestManager(ext)

	if err := m.Enable("test_ext", nil); err != nil {
		t.Fatalf("first Enable failed: %v", err)
	}

	err := m.Enable("test_ext", nil)
	if err == nil {
		t.Fatal("expected error for already active extension")
	}
	if !strings.Contains(err.Error(), "already active") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerEnableInitFailure(t *testing.T) {
	ext := &stoppableExt{name: "fail_ext", initErr: fmt.Errorf("init boom")}
	m := newTestManager(ext)

	err := m.Enable("fail_ext", nil)
	if err == nil {
		t.Fatal("expected error from failing Init")
	}
	if !strings.Contains(err.Error(), "init boom") {
		t.Fatalf("unexpected error: %v", err)
	}

	info, ok := m.Get("fail_ext")
	if !ok {
		t.Fatal("Get returned not found")
	}
	if info.State != "error" {
		t.Fatalf("expected state error, got %s", info.State)
	}
	if info.Error == "" {
		t.Fatal("expected Error field to be set")
	}
}

func TestManagerDisable(t *testing.T) {
	ext := &stoppableExt{name: "test_ext"}
	m := newTestManager(ext)

	_ = m.Enable("test_ext", nil)

	if err := m.Disable("test_ext"); err != nil {
		t.Fatalf("Disable failed: %v", err)
	}
	if ext.stopCount != 1 {
		t.Fatalf("expected Stop called once, got %d", ext.stopCount)
	}

	info, _ := m.Get("test_ext")
	if info.State != "stopped" {
		t.Fatalf("expected state stopped, got %s", info.State)
	}
	if info.StoppedAt == nil {
		t.Fatal("expected StoppedAt to be set")
	}
}

func TestManagerDisableNotActive(t *testing.T) {
	ext := &stoppableExt{name: "test_ext"}
	m := newTestManager(ext)

	err := m.Disable("test_ext")
	if err == nil {
		t.Fatal("expected error for disabling non-active extension")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerDisableNotStoppable(t *testing.T) {
	ext := &basicExt{name: "basic"}
	m := newTestManager(ext)
	_ = m.Enable("basic", nil)

	err := m.Disable("basic")
	if err == nil {
		t.Fatal("expected error for non-stoppable extension")
	}
	if !strings.Contains(err.Error(), "does not implement Stoppable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerDisableStopFailure(t *testing.T) {
	ext := &stoppableExt{name: "test_ext", stopErr: fmt.Errorf("stop boom")}
	m := newTestManager(ext)
	_ = m.Enable("test_ext", nil)

	err := m.Disable("test_ext")
	if err == nil {
		t.Fatal("expected error from failing Stop")
	}
	if !strings.Contains(err.Error(), "stop boom") {
		t.Fatalf("unexpected error: %v", err)
	}

	info, _ := m.Get("test_ext")
	if info.State != "error" {
		t.Fatalf("expected state error, got %s", info.State)
	}
}

func TestManagerEnableAfterDisable(t *testing.T) {
	ext := &stoppableExt{name: "test_ext"}
	m := newTestManager(ext)

	_ = m.Enable("test_ext", map[string]interface{}{"v": 1})
	_ = m.Disable("test_ext")

	// Re-enable with new config
	if err := m.Enable("test_ext", map[string]interface{}{"v": 2}); err != nil {
		t.Fatalf("re-Enable failed: %v", err)
	}
	if ext.initCount != 2 {
		t.Fatalf("expected Init called twice, got %d", ext.initCount)
	}

	info, _ := m.Get("test_ext")
	if info.State != "active" {
		t.Fatalf("expected state active, got %s", info.State)
	}
}

func TestManagerReconfigure(t *testing.T) {
	ext := &fullExt{name: "full_ext"}
	m := newTestManager(ext)
	_ = m.Enable("full_ext", map[string]interface{}{"port": 9000})

	newCfg := map[string]interface{}{"port": 9001}
	if err := m.Reconfigure("full_ext", newCfg); err != nil {
		t.Fatalf("Reconfigure failed: %v", err)
	}
	if ext.reconCount != 1 {
		t.Fatalf("expected Reconfigure called once, got %d", ext.reconCount)
	}
	if ext.lastConfig["port"] != 9001 {
		t.Fatalf("expected config updated, got %v", ext.lastConfig)
	}

	info, _ := m.Get("full_ext")
	if info.Config["port"] != 9001 {
		t.Fatalf("expected config persisted in manager, got %v", info.Config)
	}
}

func TestManagerReconfigureNotReconfigurable(t *testing.T) {
	ext := &stoppableExt{name: "stop_only"}
	m := newTestManager(ext)
	_ = m.Enable("stop_only", nil)

	err := m.Reconfigure("stop_only", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for non-reconfigurable extension")
	}
	if !strings.Contains(err.Error(), "does not implement Reconfigurable") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should suggest disable+enable since it's stoppable
	if !strings.Contains(err.Error(), "disable+enable") {
		t.Fatalf("expected disable+enable hint, got: %v", err)
	}
}

func TestManagerReconfigureNotActive(t *testing.T) {
	ext := &fullExt{name: "full_ext"}
	m := newTestManager(ext)

	err := m.Reconfigure("full_ext", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for non-active extension")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerList(t *testing.T) {
	ext1 := &stoppableExt{name: "ext_a"}
	ext2 := &stoppableExt{name: "ext_b"}
	m := newTestManager(ext1, ext2)
	_ = m.Enable("ext_a", nil)

	list := m.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(list))
	}

	found := map[string]string{}
	for _, info := range list {
		found[info.Name] = info.State
	}
	if found["ext_a"] != "active" {
		t.Fatalf("expected ext_a active, got %s", found["ext_a"])
	}
	if found["ext_b"] != "registered" {
		t.Fatalf("expected ext_b registered, got %s", found["ext_b"])
	}
}

func TestManagerGetNotFound(t *testing.T) {
	m := newTestManager()
	_, ok := m.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestManagerStopAll(t *testing.T) {
	ext1 := &stoppableExt{name: "ext_a"}
	ext2 := &stoppableExt{name: "ext_b"}
	ext3 := &basicExt{name: "ext_c"} // not stoppable
	m := newTestManager(ext1, ext2, ext3)
	_ = m.Enable("ext_a", nil)
	_ = m.Enable("ext_b", nil)
	_ = m.Enable("ext_c", nil)

	err := m.StopAll()
	if err != nil {
		t.Fatalf("StopAll returned error: %v", err)
	}

	// ext_a and ext_b should have been stopped; ext_c skipped
	if ext1.stopCount != 1 {
		t.Fatalf("expected ext_a stopped once, got %d", ext1.stopCount)
	}
	if ext2.stopCount != 1 {
		t.Fatalf("expected ext_b stopped once, got %d", ext2.stopCount)
	}

	info1, _ := m.Get("ext_a")
	info2, _ := m.Get("ext_b")
	if info1.State != "stopped" {
		t.Fatalf("expected ext_a stopped, got %s", info1.State)
	}
	if info2.State != "stopped" {
		t.Fatalf("expected ext_b stopped, got %s", info2.State)
	}
}

func TestManagerStopAllReverseOrder(t *testing.T) {
	var stopOrder []string
	ext1 := &orderTrackingExt{name: "first", order: &stopOrder}
	ext2 := &orderTrackingExt{name: "second", order: &stopOrder}
	ext3 := &orderTrackingExt{name: "third", order: &stopOrder}
	m := newTestManager(ext1, ext2, ext3)
	_ = m.Enable("first", nil)
	_ = m.Enable("second", nil)
	_ = m.Enable("third", nil)

	_ = m.StopAll()

	if len(stopOrder) != 3 {
		t.Fatalf("expected 3 stops, got %d", len(stopOrder))
	}
	if stopOrder[0] != "third" || stopOrder[1] != "second" || stopOrder[2] != "first" {
		t.Fatalf("expected reverse order [third, second, first], got %v", stopOrder)
	}
}

func TestManagerInitFromConfig(t *testing.T) {
	ext1 := &stoppableExt{name: "ext_a"}
	ext2 := &stoppableExt{name: "ext_b"}
	m := newTestManager(ext1, ext2)

	cfgs := []config.ExtensionConfig{
		{Name: "ext_a", Config: map[string]interface{}{"a": 1}},
		{Name: "ext_b", Config: map[string]interface{}{"b": 2}},
	}

	if err := m.InitFromConfig(cfgs); err != nil {
		t.Fatalf("InitFromConfig failed: %v", err)
	}
	if ext1.initCount != 1 || ext2.initCount != 1 {
		t.Fatalf("expected each extension initialized once")
	}
}

func TestManagerInitFromConfigFailure(t *testing.T) {
	ext1 := &stoppableExt{name: "ext_ok"}
	ext2 := &stoppableExt{name: "ext_fail", initErr: fmt.Errorf("boom")}
	m := newTestManager(ext1, ext2)

	cfgs := []config.ExtensionConfig{
		{Name: "ext_ok"},
		{Name: "ext_fail"},
	}

	err := m.InitFromConfig(cfgs)
	if err == nil {
		t.Fatal("expected error from InitFromConfig")
	}
	if !strings.Contains(err.Error(), "ext_fail") {
		t.Fatalf("expected error to mention ext_fail, got: %v", err)
	}
	// First extension should have been initialized
	if ext1.initCount != 1 {
		t.Fatalf("expected ext_ok initialized, got count %d", ext1.initCount)
	}
}

func TestManagerActiveExtensionConfigs(t *testing.T) {
	ext1 := &stoppableExt{name: "ext_a"}
	ext2 := &stoppableExt{name: "ext_b"}
	m := newTestManager(ext1, ext2)
	_ = m.Enable("ext_a", map[string]interface{}{"port": 1000})
	_ = m.Enable("ext_b", map[string]interface{}{"port": 2000})

	cfgs := m.ActiveExtensionConfigs()
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(cfgs))
	}
	if cfgs[0].Name != "ext_a" || cfgs[1].Name != "ext_b" {
		t.Fatalf("unexpected config names: %v, %v", cfgs[0].Name, cfgs[1].Name)
	}
}

func TestManagerActiveExtensionConfigsAfterDisable(t *testing.T) {
	ext1 := &stoppableExt{name: "ext_a"}
	ext2 := &stoppableExt{name: "ext_b"}
	m := newTestManager(ext1, ext2)
	_ = m.Enable("ext_a", nil)
	_ = m.Enable("ext_b", nil)
	_ = m.Disable("ext_a")

	cfgs := m.ActiveExtensionConfigs()
	if len(cfgs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(cfgs))
	}
	if cfgs[0].Name != "ext_b" {
		t.Fatalf("expected ext_b, got %s", cfgs[0].Name)
	}
}

func TestManagerCheckHealth(t *testing.T) {
	ext := &fullExt{name: "health_ext"}
	m := newTestManager(ext)
	_ = m.Enable("health_ext", nil)

	status, details, err := m.CheckHealth("health_ext")
	if err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
	if status != "healthy" {
		t.Fatalf("expected healthy, got %s", status)
	}
	if details["sessions"] != 42 {
		t.Fatalf("unexpected details: %v", details)
	}
}

func TestManagerCheckHealthNotImplemented(t *testing.T) {
	ext := &stoppableExt{name: "no_health"}
	m := newTestManager(ext)
	_ = m.Enable("no_health", nil)

	_, _, err := m.CheckHealth("no_health")
	if err == nil {
		t.Fatal("expected error for non-HealthCheckable extension")
	}
	if !strings.Contains(err.Error(), "does not implement HealthCheckable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerCapabilities(t *testing.T) {
	basic := &basicExt{name: "basic"}
	stop := &stoppableExt{name: "stop"}
	full := &fullExt{name: "full"}
	m := newTestManager(basic, stop, full)

	info, _ := m.Get("basic")
	if len(info.Capabilities) != 0 {
		t.Fatalf("expected no capabilities for basic, got %v", info.Capabilities)
	}

	info, _ = m.Get("stop")
	if len(info.Capabilities) != 1 || info.Capabilities[0] != "stoppable" {
		t.Fatalf("expected [stoppable] for stop, got %v", info.Capabilities)
	}

	info, _ = m.Get("full")
	if len(info.Capabilities) != 3 {
		t.Fatalf("expected 3 capabilities for full, got %v", info.Capabilities)
	}
}

func TestExtensionStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateRegistered, "registered"},
		{StateInitializing, "initializing"},
		{StateActive, "active"},
		{StateStopping, "stopping"},
		{StateStopped, "stopped"},
		{StateError, "error"},
		{State(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %s, want %s", tt.state, got, tt.want)
		}
	}
}

// --- MetricsProvider tests ---

// metricsExt implements Extension + Stoppable + MetricsProvider.
type metricsExt struct {
	name    string
	metrics []Metric
}

func (e *metricsExt) Name() string                                       { return e.name }
func (e *metricsExt) Init(_ InitContext, _ map[string]interface{}) error { return nil }
func (e *metricsExt) Stop() error                                        { return nil }
func (e *metricsExt) Metrics() []Metric                                  { return e.metrics }

func TestCollectExtensionMetrics(t *testing.T) {
	ext := &metricsExt{
		name: "test_metrics",
		metrics: []Metric{
			{Name: "requests_total", Help: "Total requests.", Type: MetricCounter, Value: 42},
			{Name: "active_sessions", Help: "Active sessions.", Type: MetricGauge, Value: 7},
		},
	}
	m := newTestManager(ext)
	_ = m.Enable("test_metrics", nil)

	result := m.CollectExtensionMetrics()
	if len(result) != 1 {
		t.Fatalf("expected 1 extension with metrics, got %d", len(result))
	}
	metrics, ok := result["test_metrics"]
	if !ok {
		t.Fatal("expected test_metrics in result")
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	if metrics[0].Name != "requests_total" || metrics[0].Value != 42 {
		t.Errorf("unexpected first metric: %+v", metrics[0])
	}
	if metrics[1].Name != "active_sessions" || metrics[1].Value != 7 {
		t.Errorf("unexpected second metric: %+v", metrics[1])
	}
}

func TestCollectExtensionMetricsSkipsNonProvider(t *testing.T) {
	ext1 := &metricsExt{
		name:    "with_metrics",
		metrics: []Metric{{Name: "counter", Help: "A counter.", Type: MetricCounter, Value: 1}},
	}
	ext2 := &stoppableExt{name: "no_metrics"}
	m := newTestManager(ext1, ext2)
	_ = m.Enable("with_metrics", nil)
	_ = m.Enable("no_metrics", nil)

	result := m.CollectExtensionMetrics()
	if len(result) != 1 {
		t.Fatalf("expected 1 extension with metrics, got %d", len(result))
	}
	if _, ok := result["no_metrics"]; ok {
		t.Fatal("no_metrics should not appear in result")
	}
}

func TestCollectExtensionMetricsSkipsInactive(t *testing.T) {
	ext := &metricsExt{
		name:    "inactive_metrics",
		metrics: []Metric{{Name: "counter", Help: "A counter.", Type: MetricCounter, Value: 1}},
	}
	m := newTestManager(ext)
	// Extension is registered but not enabled

	result := m.CollectExtensionMetrics()
	if len(result) != 0 {
		t.Fatalf("expected 0 extensions with metrics, got %d", len(result))
	}
}

func TestCollectExtensionMetricsEmpty(t *testing.T) {
	m := newTestManager()
	result := m.CollectExtensionMetrics()
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(result))
	}
}

func TestManagerCapabilitiesWithMetricsProvider(t *testing.T) {
	ext := &metricsExt{name: "mp_ext", metrics: nil}
	m := newTestManager(ext)

	info, _ := m.Get("mp_ext")
	found := false
	for _, cap := range info.Capabilities {
		if cap == "metrics_provider" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected metrics_provider capability, got %v", info.Capabilities)
	}
}

// --- Helpers ---

type orderTrackingExt struct {
	name  string
	order *[]string
}

func (e *orderTrackingExt) Name() string { return e.name }
func (e *orderTrackingExt) Init(_ InitContext, _ map[string]interface{}) error {
	return nil
}
func (e *orderTrackingExt) Stop() error {
	*e.order = append(*e.order, e.name)
	return nil
}

func (m *mockInitContext) GetEdgeRegistry() *edge.Registry {
	return edge.NewRegistry(m.GetConfig())
}
