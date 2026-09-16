// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package admin_api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// --- Mock types ---

type mockInitContext struct {
	router *routing.Router
	mgr    *extension.Manager
}

func (m *mockInitContext) GetDictionary() *dictionary.Dictionary { return nil }
func (m *mockInitContext) GetRouter() *routing.Router            { return m.router }
func (m *mockInitContext) GetConfig() *config.Config             { return &config.Config{} }
func (m *mockInitContext) GetPeers() []*peer.Peer                { return nil }
func (m *mockInitContext) GetStartTime() time.Time               { return time.Now() }
func (m *mockInitContext) GetExtensionManager() *extension.Manager {
	return m.mgr
}

type mockExt struct {
	name       string
	initCount  int
	stopCount  int
	reconCount int
	lastConfig map[string]interface{}
	initErr    error
	stopErr    error
	reconErr   error
}

func (e *mockExt) Name() string { return e.name }
func (e *mockExt) Init(_ extension.InitContext, cfg map[string]interface{}) error {
	e.initCount++
	e.lastConfig = cfg
	return e.initErr
}
func (e *mockExt) Stop() error {
	e.stopCount++
	return e.stopErr
}
func (e *mockExt) Reconfigure(cfg map[string]interface{}) error {
	e.reconCount++
	e.lastConfig = cfg
	return e.reconErr
}
func (e *mockExt) HealthCheck() (string, map[string]interface{}) {
	return "healthy", map[string]interface{}{"sessions": 10}
}

// basicMockExt has no Stop/Reconfigure/HealthCheck.
type basicMockExt struct {
	name string
}

func (e *basicMockExt) Name() string { return e.name }
func (e *basicMockExt) Init(_ extension.InitContext, _ map[string]interface{}) error {
	return nil
}

// --- Test setup ---

// setupTestAPI creates an adminAPI wired to a Manager with test extensions.
// Returns the HTTP handler (mux) and the manager for verification.
func setupTestAPI(exts ...extension.Extension) (http.Handler, *extension.Manager) {
	router := routing.NewRouter()
	ctx := &mockInitContext{router: router}
	mgr := extension.NewManager(ctx)
	ctx.mgr = mgr

	// Bypass LoadFromRegistry; inject extensions directly.
	for _, ext := range exts {
		mgr.AddExtension(ext)
	}

	api := &adminAPI{
		mgr: mgr,
		cfg: &config.Config{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/extensions", api.handleExtensions)
	mux.HandleFunc("/api/v1/extensions/", api.handleExtensionByName)
	return mux, mgr
}

func decodeJSON(t *testing.T, resp *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
}

func jsonBody(v interface{}) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

// --- Tests ---

func TestListExtensions(t *testing.T) {
	ext1 := &mockExt{name: "ext_a"}
	ext2 := &mockExt{name: "ext_b"}
	handler, _ := setupTestAPI(ext1, ext2)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/extensions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		Extensions []extension.ManagedExtensionInfo `json:"extensions"`
	}
	decodeJSON(t, w, &body)
	if len(body.Extensions) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(body.Extensions))
	}
}

func TestListExtensionsMethodNotAllowed(t *testing.T) {
	handler, _ := setupTestAPI()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestGetExtension(t *testing.T) {
	ext := &mockExt{name: "test_ext"}
	handler, _ := setupTestAPI(ext)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/extensions/test_ext", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var info extension.ManagedExtensionInfo
	decodeJSON(t, w, &info)
	if info.Name != "test_ext" {
		t.Fatalf("expected name test_ext, got %s", info.Name)
	}
	if info.State != "registered" {
		t.Fatalf("expected state registered, got %s", info.State)
	}
}

func TestGetExtensionNotFound(t *testing.T) {
	handler, _ := setupTestAPI()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/extensions/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestEnableExtension(t *testing.T) {
	ext := &mockExt{name: "test_ext"}
	handler, _ := setupTestAPI(ext)

	body := jsonBody(map[string]interface{}{
		"config": map[string]interface{}{"port": 9000},
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions/test_ext/enable", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ext.initCount != 1 {
		t.Fatalf("expected Init called once, got %d", ext.initCount)
	}

	var info extension.ManagedExtensionInfo
	decodeJSON(t, w, &info)
	if info.State != "active" {
		t.Fatalf("expected state active, got %s", info.State)
	}
}

func TestEnableExtensionAlreadyActive(t *testing.T) {
	ext := &mockExt{name: "test_ext"}
	handler, mgr := setupTestAPI(ext)
	_ = mgr.Enable("test_ext", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions/test_ext/enable", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestEnableExtensionNotFound(t *testing.T) {
	handler, _ := setupTestAPI()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions/nonexistent/enable", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDisableExtension(t *testing.T) {
	ext := &mockExt{name: "test_ext"}
	handler, mgr := setupTestAPI(ext)
	_ = mgr.Enable("test_ext", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions/test_ext/disable", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ext.stopCount != 1 {
		t.Fatalf("expected Stop called once, got %d", ext.stopCount)
	}

	var info extension.ManagedExtensionInfo
	decodeJSON(t, w, &info)
	if info.State != "stopped" {
		t.Fatalf("expected state stopped, got %s", info.State)
	}
}

func TestDisableExtensionSelfProtection(t *testing.T) {
	handler, _ := setupTestAPI()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions/admin_api/disable", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDisableNotStoppable(t *testing.T) {
	ext := &basicMockExt{name: "basic"}
	handler, mgr := setupTestAPI(ext)
	_ = mgr.Enable("basic", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions/basic/disable", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReconfigureExtension(t *testing.T) {
	ext := &mockExt{name: "test_ext"}
	handler, mgr := setupTestAPI(ext)
	_ = mgr.Enable("test_ext", map[string]interface{}{"port": 9000})

	body := jsonBody(map[string]interface{}{
		"config": map[string]interface{}{"port": 9001},
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/extensions/test_ext/config", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ext.reconCount != 1 {
		t.Fatalf("expected Reconfigure called once, got %d", ext.reconCount)
	}
}

func TestReconfigureNotActive(t *testing.T) {
	ext := &mockExt{name: "test_ext"}
	handler, _ := setupTestAPI(ext)

	body := jsonBody(map[string]interface{}{"config": map[string]interface{}{}})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/extensions/test_ext/config", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRestartExtension(t *testing.T) {
	ext := &mockExt{name: "test_ext"}
	handler, mgr := setupTestAPI(ext)
	_ = mgr.Enable("test_ext", map[string]interface{}{"port": 9000})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions/test_ext/restart", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ext.stopCount != 1 {
		t.Fatalf("expected Stop called once, got %d", ext.stopCount)
	}
	if ext.initCount != 2 { // once for initial Enable, once for restart
		t.Fatalf("expected Init called twice, got %d", ext.initCount)
	}
}

func TestRestartSelfProtection(t *testing.T) {
	handler, _ := setupTestAPI()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions/admin_api/restart", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestHealthCheck(t *testing.T) {
	ext := &mockExt{name: "health_ext"}
	handler, mgr := setupTestAPI(ext)
	_ = mgr.Enable("health_ext", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/extensions/health_ext/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	decodeJSON(t, w, &body)
	if body["status"] != "healthy" {
		t.Fatalf("expected status healthy, got %v", body["status"])
	}
}

func TestHealthCheckNotImplemented(t *testing.T) {
	ext := &basicMockExt{name: "no_health"}
	handler, mgr := setupTestAPI(ext)
	_ = mgr.Enable("no_health", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/extensions/no_health/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUnknownAction(t *testing.T) {
	handler, _ := setupTestAPI()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/extensions/test/bogus", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	var body map[string]string
	decodeJSON(t, w, &body)
	if body["error"] == "" {
		t.Fatal("expected error message")
	}
}

func TestEnableWithInitFailure(t *testing.T) {
	ext := &mockExt{name: "fail_ext", initErr: fmt.Errorf("init boom")}
	handler, _ := setupTestAPI(ext)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions/fail_ext/enable", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestRestartWithNewConfig(t *testing.T) {
	ext := &mockExt{name: "test_ext"}
	handler, mgr := setupTestAPI(ext)
	_ = mgr.Enable("test_ext", map[string]interface{}{"port": 9000})

	body := jsonBody(map[string]interface{}{
		"config": map[string]interface{}{"port": 9999},
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/extensions/test_ext/restart", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify new config was used
	if ext.lastConfig["port"] != float64(9999) {
		t.Fatalf("expected port 9999, got %v", ext.lastConfig["port"])
	}
}

func (m *mockInitContext) GetEdgeRegistry() *edge.Registry {
	return edge.NewRegistry(m.GetConfig())
}
