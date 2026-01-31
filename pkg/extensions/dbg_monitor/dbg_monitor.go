// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package dbg_monitor provides a debug monitoring extension for Diameter peers and messages.
package dbg_monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
)

type dbgMonitor struct {
	ctx    extension.InitContext
	server *http.Server
}

// Name returns the extension name.
func (e *dbgMonitor) Name() string { return "dbg_monitor" }

// Init initializes the extension.
func (e *dbgMonitor) Init(ctx extension.InitContext, config map[string]interface{}) error {
	e.ctx = ctx
	log.Printf("Initializing extension: dbg_monitor")

	port := 9000
	if p, ok := config["port"].(int); ok {
		port = p
	} else if p, ok := config["port"].(float64); ok { // map[string]interface{} often has float64 for numbers
		port = int(p)
	}

	addr := fmt.Sprintf(":%d", port)
	mux := http.NewServeMux()
	mux.HandleFunc("/status", e.handleStatus)
	mux.HandleFunc("/peers", e.handlePeers)
	mux.HandleFunc("/dict", e.handleDict)

	e.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("dbg_monitor listening on http://localhost%s", addr)
		if err := e.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("dbg_monitor HTTP server error: %v", err)
		}
	}()

	return nil
}

// Stop implements the Stoppable interface. It gracefully shuts down the HTTP server.
func (e *dbgMonitor) Stop() error {
	if e.server != nil {
		log.Printf("dbg_monitor: shutting down HTTP server")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return e.server.Shutdown(ctx)
	}
	return nil
}

// StatusResponse contains the server status information returned by the admin API.
type StatusResponse struct {
	Identity  string    `json:"identity"`
	Realm     string    `json:"realm"`
	Uptime    string    `json:"uptime"`
	StartTime time.Time `json:"start_time"`
	Peers     int       `json:"peers_count"`
}

func (e *dbgMonitor) handleStatus(w http.ResponseWriter, _ *http.Request) {
	cfg := e.ctx.GetConfig()
	startTime := e.ctx.GetStartTime()
	peers := e.ctx.GetPeers()

	resp := StatusResponse{
		Identity:  cfg.Identity,
		Realm:     cfg.Realm,
		Uptime:    time.Since(startTime).String(),
		StartTime: startTime,
		Peers:     len(peers),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// PeerInfo contains information about a connected Diameter peer.
type PeerInfo struct {
	Identity   string     `json:"identity"`
	Realm      string     `json:"realm"`
	State      string     `json:"state"`
	Statistics peer.Stats `json:"stats"`
}

func (e *dbgMonitor) handlePeers(w http.ResponseWriter, _ *http.Request) {
	peers := e.ctx.GetPeers()
	info := make([]PeerInfo, 0, len(peers))

	for _, p := range peers {
		info = append(info, PeerInfo{
			Identity:   string(p.DiameterID()),
			Realm:      string(p.Realm()),
			State:      p.State().String(),
			Statistics: p.Stats(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

func (e *dbgMonitor) handleDict(w http.ResponseWriter, _ *http.Request) {
	dict := e.ctx.GetDictionary()
	stats := dict.Stats()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func init() {
	extension.Register(&dbgMonitor{})
}
