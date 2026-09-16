// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package admin_api provides a REST API for dynamic extension management.
package admin_api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
)

type adminAPI struct {
	server *http.Server
	mgr    *extension.Manager
	cfg    *config.Config
	reg    *edge.Registry
}

// Name returns the extension name.
func (a *adminAPI) Name() string { return "admin_api" }

// Init initializes the extension.
func (a *adminAPI) Init(ctx extension.InitContext, cfg map[string]interface{}) error {
	log.Printf("Initializing extension: admin_api")

	a.mgr = ctx.GetExtensionManager()
	a.cfg = ctx.GetConfig()
	a.reg = ctx.GetEdgeRegistry()

	port := 9001
	if p, ok := cfg["port"].(int); ok {
		port = p
	} else if p, ok := cfg["port"].(float64); ok {
		port = int(p)
	}

	bindAddr := "127.0.0.1"
	if addr, ok := cfg["bind_address"].(string); ok {
		bindAddr = addr
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/extensions", a.handleExtensions)
	mux.HandleFunc("/api/v1/extensions/", a.handleExtensionByName)
	mux.HandleFunc("/api/v1/edge", a.handleEdge)

	listenAddr := net.JoinHostPort(bindAddr, fmt.Sprintf("%d", port))
	a.server = &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("admin_api listening on http://%s", listenAddr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("admin_api HTTP server error: %v", err)
		}
	}()

	return nil
}

// Stop shuts down the extension.
func (a *adminAPI) Stop() error {
	if a.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return a.server.Shutdown(ctx)
	}
	return nil
}

// handleExtensions handles GET /api/v1/extensions
func (a *adminAPI) handleExtensions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	list := a.mgr.List()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"extensions": list,
	})
}

// handleExtensionByName routes /api/v1/extensions/{name}[/action]
func (a *adminAPI) handleExtensionByName(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/v1/extensions/{name}[/{action}]
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/extensions/")
	parts := strings.SplitN(path, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if name == "" {
		writeError(w, http.StatusBadRequest, "extension name required")
		return
	}

	switch action {
	case "":
		a.handleGetExtension(w, r, name)
	case "enable":
		a.handleEnable(w, r, name)
	case "disable":
		a.handleDisable(w, r, name)
	case "config":
		a.handleReconfigure(w, r, name)
	case "restart":
		a.handleRestart(w, r, name)
	case "health":
		a.handleHealth(w, r, name)
	default:
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown action: %s", action))
	}
}

// GET /api/v1/extensions/{name}
func (a *adminAPI) handleGetExtension(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	info, ok := a.mgr.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("extension not found: %s", name))
		return
	}

	writeJSON(w, http.StatusOK, info)
}

// POST /api/v1/extensions/{name}/enable
func (a *adminAPI) handleEnable(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		Config map[string]interface{} `json:"config"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
			return
		}
	}

	if err := a.mgr.Enable(name, body.Config); err != nil {
		code := http.StatusInternalServerError
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "already active"):
			code = http.StatusConflict
		case strings.Contains(errMsg, "unknown extension"):
			code = http.StatusNotFound
		case strings.Contains(errMsg, "busy"):
			code = http.StatusConflict
		}
		writeError(w, code, errMsg)
		return
	}

	a.maybePersist(r)

	info, _ := a.mgr.Get(name)
	writeJSON(w, http.StatusOK, info)
}

// POST /api/v1/extensions/{name}/disable
func (a *adminAPI) handleDisable(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if name == "admin_api" {
		writeError(w, http.StatusForbidden, "cannot disable admin_api through its own endpoint")
		return
	}

	if err := a.mgr.Disable(name); err != nil {
		code := http.StatusInternalServerError
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not active"):
			code = http.StatusConflict
		case strings.Contains(errMsg, "unknown extension"):
			code = http.StatusNotFound
		case strings.Contains(errMsg, "does not implement Stoppable"):
			code = http.StatusBadRequest
		}
		writeError(w, code, errMsg)
		return
	}

	a.maybePersist(r)

	info, _ := a.mgr.Get(name)
	writeJSON(w, http.StatusOK, info)
}

// PUT /api/v1/extensions/{name}/config
func (a *adminAPI) handleReconfigure(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		Config map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
		return
	}

	if err := a.mgr.Reconfigure(name, body.Config); err != nil {
		code := http.StatusInternalServerError
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not active"):
			code = http.StatusConflict
		case strings.Contains(errMsg, "unknown extension"):
			code = http.StatusNotFound
		case strings.Contains(errMsg, "does not implement Reconfigurable"):
			code = http.StatusBadRequest
		}
		writeError(w, code, errMsg)
		return
	}

	a.maybePersist(r)

	info, _ := a.mgr.Get(name)
	writeJSON(w, http.StatusOK, info)
}

// POST /api/v1/extensions/{name}/restart
func (a *adminAPI) handleRestart(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if name == "admin_api" {
		writeError(w, http.StatusForbidden, "cannot restart admin_api through its own endpoint")
		return
	}

	// Get current config as fallback
	currentInfo, ok := a.mgr.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("extension not found: %s", name))
		return
	}

	// Parse optional new config from body
	var body struct {
		Config map[string]interface{} `json:"config"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
			return
		}
	}

	cfg := body.Config
	if cfg == nil {
		cfg = currentInfo.Config
	}

	// Stop if active
	if currentInfo.State == "active" {
		if err := a.mgr.Disable(name); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("disable failed: %v", err))
			return
		}
	}

	// Re-enable
	if err := a.mgr.Enable(name, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("enable after restart failed: %v", err))
		return
	}

	a.maybePersist(r)

	info, _ := a.mgr.Get(name)
	writeJSON(w, http.StatusOK, info)
}

// GET /api/v1/extensions/{name}/health
func (a *adminAPI) handleHealth(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	info, ok := a.mgr.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("extension not found: %s", name))
		return
	}
	_ = info

	// We need the actual extension object to type-assert HealthCheckable.
	// Use the manager's list which includes capabilities.
	list := a.mgr.List()
	var found *extension.ManagedExtensionInfo
	for i := range list {
		if list[i].Name == name {
			found = &list[i]
			break
		}
	}

	if found == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("extension not found: %s", name))
		return
	}

	hasHealth := false
	for _, cap := range found.Capabilities {
		if cap == "health_checkable" {
			hasHealth = true
			break
		}
	}
	if !hasHealth {
		writeError(w, http.StatusNotImplemented, fmt.Sprintf("extension %s does not implement HealthCheckable", name))
		return
	}

	// Delegate to CheckHealth on the manager
	status, details, err := a.mgr.CheckHealth(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":    name,
		"status":  status,
		"details": details,
	})
}

// maybePersist writes config to disk if ?persist=true
func (a *adminAPI) maybePersist(r *http.Request) {
	if r.URL.Query().Get("persist") == "true" {
		exts := a.mgr.ActiveExtensionConfigs()
		if err := a.cfg.SaveExtensions(exts); err != nil {
			log.Printf("admin_api: failed to persist config: %v", err)
		} else {
			log.Printf("admin_api: config persisted to %s", a.cfg.ConfigPath)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func init() {
	extension.Register(&adminAPI{})
}
