// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package extension

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
)

// Manager orchestrates extension lifecycle. Thread-safe.
type Manager struct {
	mu         sync.RWMutex
	extensions map[string]*ManagedExtension
	initCtx    InitContext
	// initOrder tracks the order extensions were enabled, for orderly shutdown.
	initOrder []string
}

// ManagedExtensionInfo is a snapshot of extension state for external consumption.
type ManagedExtensionInfo struct {
	Name         string                 `json:"name"`
	State        string                 `json:"state"`
	Config       map[string]interface{} `json:"config,omitempty"`
	Error        string                 `json:"error,omitempty"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	StoppedAt    *time.Time             `json:"stopped_at,omitempty"`
	Capabilities []string               `json:"capabilities"`
}

// NewManager creates a new extension manager.
func NewManager(ctx InitContext) *Manager {
	return &Manager{
		extensions: make(map[string]*ManagedExtension),
		initCtx:    ctx,
	}
}

// AddExtension adds an extension to the manager in StateRegistered.
// Useful for testing or programmatic extension injection.
func (m *Manager) AddExtension(ext Extension) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.extensions[ext.Name()] = &ManagedExtension{
		Extension: ext,
		State:     StateRegistered,
	}
}

// LoadFromRegistry populates the manager from the global extension registry.
// All extensions start in StateRegistered.
func (m *Manager) LoadFromRegistry() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, ext := range GetAll() {
		m.extensions[name] = &ManagedExtension{
			Extension: ext,
			State:     StateRegistered,
		}
	}
}

// InitFromConfig enables extensions listed in the config, replacing the
// old startExtensions() approach. This is called during server startup.
func (m *Manager) InitFromConfig(extensions []config.ExtensionConfig) error {
	for _, extCfg := range extensions {
		if err := m.Enable(extCfg.Name, extCfg.Config); err != nil {
			return fmt.Errorf("failed to initialize extension %s: %w", extCfg.Name, err)
		}
	}
	return nil
}

// Enable initializes a registered extension and transitions it to StateActive.
func (m *Manager) Enable(name string, cfg map[string]interface{}) error {
	m.mu.Lock()
	me, ok := m.extensions[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown extension: %s", name)
	}
	if me.State == StateActive {
		m.mu.Unlock()
		return fmt.Errorf("extension already active: %s", name)
	}
	if me.State == StateInitializing || me.State == StateStopping {
		m.mu.Unlock()
		return fmt.Errorf("extension busy (state=%s): %s", me.State, name)
	}
	me.State = StateInitializing
	ext := me.Extension
	m.mu.Unlock()

	// Init may take time; do NOT hold the lock.
	err := ext.Init(m.initCtx, cfg)

	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		me.State = StateError
		me.Error = err.Error()
		return fmt.Errorf("failed to init %s: %w", name, err)
	}
	me.State = StateActive
	me.Config = cfg
	me.Error = ""
	me.StartedAt = time.Now()
	me.StoppedAt = time.Time{}
	m.initOrder = append(m.initOrder, name)
	log.Printf("Extension enabled: %s", name)
	return nil
}

// Disable stops a running extension and deregisters its router handlers.
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	me, ok := m.extensions[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown extension: %s", name)
	}
	if me.State != StateActive {
		m.mu.Unlock()
		return fmt.Errorf("extension not active (state=%s): %s", me.State, name)
	}

	stoppable, isStoppable := me.Extension.(Stoppable)
	if !isStoppable {
		m.mu.Unlock()
		return fmt.Errorf("extension %s does not implement Stoppable interface; cannot disable dynamically", name)
	}

	me.State = StateStopping
	m.mu.Unlock()

	// Deregister router handlers first so no new messages arrive.
	router := m.initCtx.GetRouter()
	router.DeregisterAllByOwner(name)

	// Stop the extension (may take time).
	err := stoppable.Stop()

	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		me.State = StateError
		me.Error = fmt.Sprintf("stop failed: %v", err)
		return fmt.Errorf("failed to stop %s: %w", name, err)
	}
	me.State = StateStopped
	me.Error = ""
	me.StoppedAt = time.Now()

	// Remove from init order
	for i, n := range m.initOrder {
		if n == name {
			m.initOrder = append(m.initOrder[:i], m.initOrder[i+1:]...)
			break
		}
	}

	log.Printf("Extension disabled: %s", name)
	return nil
}

// Reconfigure updates a running extension's configuration.
func (m *Manager) Reconfigure(name string, cfg map[string]interface{}) error {
	m.mu.RLock()
	me, ok := m.extensions[name]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("unknown extension: %s", name)
	}
	if me.State != StateActive {
		m.mu.RUnlock()
		return fmt.Errorf("extension not active (state=%s): %s", me.State, name)
	}

	reconf, isReconf := me.Extension.(Reconfigurable)
	if !isReconf {
		_, isStoppable := me.Extension.(Stoppable)
		m.mu.RUnlock()
		if isStoppable {
			return fmt.Errorf("extension %s does not implement Reconfigurable; use disable+enable to apply new config", name)
		}
		return fmt.Errorf("extension %s does not implement Reconfigurable", name)
	}
	m.mu.RUnlock()

	if err := reconf.Reconfigure(cfg); err != nil {
		return fmt.Errorf("reconfigure %s failed: %w", name, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	me.Config = cfg
	log.Printf("Extension reconfigured: %s", name)
	return nil
}

// List returns a snapshot of all managed extensions.
func (m *Manager) List() []ManagedExtensionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ManagedExtensionInfo, 0, len(m.extensions))
	for name, me := range m.extensions {
		info := ManagedExtensionInfo{
			Name:         name,
			State:        me.State.String(),
			Config:       me.Config,
			Error:        me.Error,
			Capabilities: me.Capabilities(),
		}
		if !me.StartedAt.IsZero() {
			t := me.StartedAt
			info.StartedAt = &t
		}
		if !me.StoppedAt.IsZero() {
			t := me.StoppedAt
			info.StoppedAt = &t
		}
		result = append(result, info)
	}
	return result
}

// Get returns a snapshot of a single managed extension.
func (m *Manager) Get(name string) (*ManagedExtensionInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	me, ok := m.extensions[name]
	if !ok {
		return nil, false
	}

	info := &ManagedExtensionInfo{
		Name:         name,
		State:        me.State.String(),
		Config:       me.Config,
		Error:        me.Error,
		Capabilities: me.Capabilities(),
	}
	if !me.StartedAt.IsZero() {
		t := me.StartedAt
		info.StartedAt = &t
	}
	if !me.StoppedAt.IsZero() {
		t := me.StoppedAt
		info.StoppedAt = &t
	}
	return info, true
}

// StopAll stops all active extensions in reverse initialization order.
// Called during graceful server shutdown.
func (m *Manager) StopAll() error {
	m.mu.RLock()
	// Copy initOrder in reverse
	order := make([]string, len(m.initOrder))
	copy(order, m.initOrder)
	m.mu.RUnlock()

	var lastErr error
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		m.mu.RLock()
		me, ok := m.extensions[name]
		m.mu.RUnlock()
		if !ok || me.State != StateActive {
			continue
		}

		stoppable, isStoppable := me.Extension.(Stoppable)
		if !isStoppable {
			log.Printf("Extension %s does not implement Stoppable, skipping during shutdown", name)
			continue
		}

		log.Printf("Stopping extension: %s", name)
		if err := stoppable.Stop(); err != nil {
			log.Printf("Error stopping extension %s: %v", name, err)
			lastErr = err
		}

		m.mu.Lock()
		me.State = StateStopped
		me.StoppedAt = time.Now()
		m.mu.Unlock()
	}
	return lastErr
}

// ActiveExtensionConfigs returns the current extension configs for all active extensions.
// Useful for persisting the current state to disk.
func (m *Manager) ActiveExtensionConfigs() []config.ExtensionConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []config.ExtensionConfig
	for _, name := range m.initOrder {
		me, ok := m.extensions[name]
		if !ok || me.State != StateActive {
			continue
		}
		result = append(result, config.ExtensionConfig{
			Name:   name,
			Config: me.Config,
		})
	}
	return result
}

// CheckHealth calls HealthCheck on an extension that implements HealthCheckable.
func (m *Manager) CheckHealth(name string) (string, map[string]interface{}, error) {
	m.mu.RLock()
	me, ok := m.extensions[name]
	if !ok {
		m.mu.RUnlock()
		return "", nil, fmt.Errorf("unknown extension: %s", name)
	}
	hc, isHC := me.Extension.(HealthCheckable)
	m.mu.RUnlock()

	if !isHC {
		return "", nil, fmt.Errorf("extension %s does not implement HealthCheckable", name)
	}

	status, details := hc.HealthCheck()
	return status, details, nil
}

// CollectExtensionMetrics calls Metrics() on all active extensions that
// implement MetricsProvider. Returns a map keyed by extension name.
func (m *Manager) CollectExtensionMetrics() map[string][]Metric {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]Metric)
	for name, me := range m.extensions {
		if me.State != StateActive {
			continue
		}
		if mp, ok := me.Extension.(MetricsProvider); ok {
			result[name] = mp.Metrics()
		}
	}
	return result
}
