// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package admin_api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
)

func TestAdminAPINameIsStable(t *testing.T) {
	api := &adminAPI{}
	if api.Name() != "admin_api" {
		t.Fatalf("Name() = %q, want admin_api", api.Name())
	}
}

func TestAdminAPIInitInstallsAllHandlersAndStopIsIdempotent(t *testing.T) {
	_, mgr := setupTestAPI()
	ctx := &mockInitContext{mgr: mgr}
	api := &adminAPI{}
	if err := api.Init(ctx, map[string]interface{}{"port": 0, "bind_address": "127.0.0.1"}); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	t.Cleanup(func() {
		if err := api.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	})

	cases := []struct {
		path string
		want int
	}{
		{path: "/api/v1/extensions", want: http.StatusOK},
		{path: "/api/v1/extensions/missing", want: http.StatusNotFound},
		{path: "/api/v1/edge", want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			api.server.Handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	if err := (&adminAPI{}).Stop(); err != nil {
		t.Fatalf("empty Stop() failed: %v", err)
	}
}

func TestExtensionItemRejectsWrongMethodsAndEmptyNames(t *testing.T) {
	handler, _ := setupTestAPI(&mockExt{name: "test_ext"})

	cases := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "get extension post", method: http.MethodPost, path: "/api/v1/extensions/test_ext", want: http.StatusMethodNotAllowed},
		{name: "enable get", method: http.MethodGet, path: "/api/v1/extensions/test_ext/enable", want: http.StatusMethodNotAllowed},
		{name: "disable get", method: http.MethodGet, path: "/api/v1/extensions/test_ext/disable", want: http.StatusMethodNotAllowed},
		{name: "restart put", method: http.MethodPut, path: "/api/v1/extensions/test_ext/restart", want: http.StatusMethodNotAllowed},
		{name: "health post", method: http.MethodPost, path: "/api/v1/extensions/test_ext/health", want: http.StatusMethodNotAllowed},
		{name: "empty extension name", method: http.MethodGet, path: "/api/v1/extensions/", want: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestJSONMutationEndpointsRejectMalformedBodies(t *testing.T) {
	handler, _ := setupTestAPI(&mockExt{name: "test_ext"})

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{name: "enable", method: http.MethodPost, path: "/api/v1/extensions/test_ext/enable"},
		{name: "reconfigure", method: http.MethodPut, path: "/api/v1/extensions/test_ext/config"},
		{name: "restart", method: http.MethodPost, path: "/api/v1/extensions/test_ext/restart"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, strings.NewReader(`{"config":`))
			req.ContentLength = int64(len(`{"config":`))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			assertErrorContains(t, rec, "invalid JSON body")
		})
	}
}

func TestReconfigureReportsExtensionRejectionWithoutClaimingSuccess(t *testing.T) {
	rejected := errors.New("validator rejected threshold")
	ext := &mockExt{name: "test_ext", reconErr: rejected}
	handler, mgr := setupTestAPI(ext)
	if err := mgr.Enable("test_ext", nil); err != nil {
		t.Fatalf("enabling extension: %v", err)
	}

	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPut,
		"/api/v1/extensions/test_ext/config",
		jsonBody(map[string]interface{}{"config": map[string]interface{}{"threshold": -1}}),
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorContains(t, rec, rejected.Error())
	if ext.reconCount != 1 {
		t.Fatalf("Reconfigure called %d times, want 1", ext.reconCount)
	}
}

func TestReconfigureDistinguishesUnknownAndUnsupportedExtensions(t *testing.T) {
	handler, mgr := setupTestAPI(&basicMockExt{name: "basic"})
	if err := mgr.Enable("basic", nil); err != nil {
		t.Fatalf("enabling extension: %v", err)
	}

	cases := []struct {
		name string
		path string
		want int
	}{
		{name: "unknown", path: "/api/v1/extensions/missing/config", want: http.StatusNotFound},
		{name: "unsupported", path: "/api/v1/extensions/basic/config", want: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, tc.path, jsonBody(map[string]interface{}{"config": map[string]interface{}{}}))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestEnableDisableRestartErrorStatusMapping(t *testing.T) {
	stopErr := errors.New("stop boom")
	enableErr := errors.New("enable boom")
	cases := []struct {
		name  string
		run   func(http.Handler, *extensionBundle) *httptest.ResponseRecorder
		want  int
		error string
	}{
		{
			name: "disable unknown",
			run: func(handler http.Handler, _ *extensionBundle) *httptest.ResponseRecorder {
				return serve(handler, http.MethodPost, "/api/v1/extensions/missing/disable")
			},
			want: http.StatusNotFound,
		},
		{
			name: "disable inactive",
			run: func(handler http.Handler, _ *extensionBundle) *httptest.ResponseRecorder {
				return serve(handler, http.MethodPost, "/api/v1/extensions/stoppable/disable")
			},
			want: http.StatusConflict,
		},
		{
			name: "disable stop error",
			run: func(handler http.Handler, b *extensionBundle) *httptest.ResponseRecorder {
				b.stoppable.stopErr = stopErr
				if err := b.manager.Enable("stoppable", nil); err != nil {
					t.Fatalf("enabling extension: %v", err)
				}
				return serve(handler, http.MethodPost, "/api/v1/extensions/stoppable/disable")
			},
			want: http.StatusInternalServerError,
		},
		{
			name: "restart missing extension",
			run: func(handler http.Handler, _ *extensionBundle) *httptest.ResponseRecorder {
				return serve(handler, http.MethodPost, "/api/v1/extensions/missing/restart")
			},
			want: http.StatusNotFound,
		},
		{
			name: "restart enable failure",
			run: func(handler http.Handler, b *extensionBundle) *httptest.ResponseRecorder {
				b.stoppable.initErr = enableErr
				return serve(handler, http.MethodPost, "/api/v1/extensions/stoppable/restart")
			},
			want: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := newExtensionBundle()
			rec := tc.run(bundle.handler, bundle)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestHealthCheckReportsUnknownButAllowsInactiveProviders(t *testing.T) {
	ext := &mockExt{name: "health_ext"}
	handler, mgr := setupTestAPI(ext)
	if err := mgr.Enable("health_ext", nil); err != nil {
		t.Fatalf("enabling extension: %v", err)
	}
	if err := mgr.Disable("health_ext"); err != nil {
		t.Fatalf("disabling extension: %v", err)
	}

	cases := []struct {
		name string
		path string
		want int
	}{
		{name: "unknown", path: "/api/v1/extensions/missing/health", want: http.StatusNotFound},
		{name: "inactive health provider", path: "/api/v1/extensions/health_ext/health", want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(handler, http.MethodGet, tc.path)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestHandleEdgeUnavailableWhenRegistryMissing(t *testing.T) {
	api := &adminAPI{}
	rec := httptest.NewRecorder()
	api.handleEdge(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/edge", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleEdgeResponseIncludesStableShapeForDefaults(t *testing.T) {
	api := &adminAPI{reg: edge.NewRegistry(&config.Config{Identity: "dea.example.com", Realm: "example.com"})}

	rec := httptest.NewRecorder()
	api.handleEdge(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/edge", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	for _, field := range []string{"configured", "identity", "realm", "default_zone", "zones", "partners"} {
		if _, ok := body[field]; !ok {
			t.Fatalf("response missing %q in %#v", field, body)
		}
	}
}

func assertErrorContains(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if !strings.Contains(body["error"], want) {
		t.Fatalf("error = %q, want to contain %q", body["error"], want)
	}
}

func serve(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type extensionBundle struct {
	handler   http.Handler
	manager   *extension.Manager
	stoppable *mockExt
}

func newExtensionBundle() *extensionBundle {
	ext := &mockExt{name: "stoppable"}
	handler, mgr := setupTestAPI(ext)
	return &extensionBundle{handler: handler, manager: mgr, stoppable: ext}
}
