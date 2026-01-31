// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package extension provides the Diameter extension system.
package extension

import (
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
)

// InitContext provides extensions with access to core server components.
type InitContext interface {
	GetDictionary() *dictionary.Dictionary
	GetRouter() *routing.Router
	GetConfig() *config.Config
	GetPeers() []*peer.Peer
	GetStartTime() time.Time
	GetExtensionManager() *Manager
}

// Extension is the interface implemented by Diameter extensions.
type Extension interface {
	// Name returns the unique name of the extension.
	Name() string
	// Init initializes the extension.
	Init(ctx InitContext, config map[string]interface{}) error
}

// Stoppable is optionally implemented by extensions that support graceful shutdown.
// Extensions that start goroutines, open HTTP servers, or hold resources should
// implement this to allow dynamic disable via the admin API.
type Stoppable interface {
	Stop() error
}

// Reconfigurable is optionally implemented by extensions that support live
// reconfiguration without a full stop/start cycle.
type Reconfigurable interface {
	Reconfigure(config map[string]interface{}) error
}

// HealthCheckable is optionally implemented by extensions for status reporting.
type HealthCheckable interface {
	HealthCheck() (status string, details map[string]interface{})
}

// MetricType represents the type of a performance metric.
type MetricType int

const (
	// MetricGauge is a point-in-time value (e.g., current binding count).
	MetricGauge MetricType = iota
	// MetricCounter is a monotonically increasing value (e.g., total requests).
	MetricCounter
)

// Metric represents a single metric value exposed by an extension.
// Extensions return these from Metrics(); the prom_metrics extension
// converts them to prometheus format. This keeps per-extension code
// free of prometheus dependencies.
type Metric struct {
	Name   string            // Short metric name, e.g. "bindings_total". Prefixed with "diameter_ext_{extension_name}_".
	Help   string            // Human-readable description.
	Type   MetricType        // Gauge or Counter.
	Value  float64           // Current value.
	Labels map[string]string // Optional labels.
}

// MetricsProvider is optionally implemented by extensions that expose
// their own performance management (PM) metrics. The prom_metrics extension
// discovers these automatically and includes them in the /metrics scrape.
//
// Implementations MUST be safe for concurrent calls.
type MetricsProvider interface {
	Metrics() []Metric
}

// State represents the runtime lifecycle state of an extension.
type State int

const (
	// StateRegistered indicates the extension is known but never initialized.
	StateRegistered State = iota
	// StateInitializing indicates initialization is in progress.
	StateInitializing
	// StateActive indicates the extension is running.
	StateActive
	// StateStopping indicates the extension is stopping.
	StateStopping
	// StateStopped indicates the extension was active but is now stopped.
	StateStopped
	// StateError indicates initialization or stopping failed.
	StateError
)

func (s State) String() string {
	switch s {
	case StateRegistered:
		return "registered"
	case StateInitializing:
		return "initializing"
	case StateActive:
		return "active"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// ManagedExtension wraps an Extension with runtime metadata.
type ManagedExtension struct {
	Extension Extension
	State     State
	Config    map[string]interface{}
	Error     string
	StartedAt time.Time
	StoppedAt time.Time
}

// Capabilities returns the optional interfaces implemented by the extension.
func (me *ManagedExtension) Capabilities() []string {
	var caps []string
	if _, ok := me.Extension.(Stoppable); ok {
		caps = append(caps, "stoppable")
	}
	if _, ok := me.Extension.(Reconfigurable); ok {
		caps = append(caps, "reconfigurable")
	}
	if _, ok := me.Extension.(HealthCheckable); ok {
		caps = append(caps, "health_checkable")
	}
	if _, ok := me.Extension.(MetricsProvider); ok {
		caps = append(caps, "metrics_provider")
	}
	return caps
}

var (
	registry = make(map[string]Extension)
)

// Register registers an extension with the global registry.
func Register(ext Extension) {
	registry[ext.Name()] = ext
}

// Unregister removes an extension from the global registry.
func Unregister(name string) {
	delete(registry, name)
}

// Get returns the extension with the given name.
func Get(name string) (Extension, bool) {
	ext, ok := registry[name]
	return ext, ok
}

// GetAll returns a copy of the full extension registry.
func GetAll() map[string]Extension {
	result := make(map[string]Extension, len(registry))
	for name, ext := range registry {
		result[name] = ext
	}
	return result
}
