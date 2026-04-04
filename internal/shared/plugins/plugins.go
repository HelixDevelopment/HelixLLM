// Package plugins provides a thin plugin manager for HelixLLM,
// allowing extensible module loading at startup.
package plugins

import (
	"errors"
	"fmt"
	"sync"
)

// Plugin is the interface every HelixLLM plugin must implement.
type Plugin interface {
	// Name returns the unique name of the plugin.
	Name() string
	// Init is called once with the plugin-specific configuration map.
	Init(config map[string]interface{}) error
	// Start launches the plugin's background work, if any.
	Start() error
	// Stop cleanly shuts the plugin down.
	Stop() error
	// HealthCheck returns nil if the plugin is healthy.
	HealthCheck() error
}

// Manager holds and coordinates a set of Plugin instances.
type Manager struct {
	mu      sync.RWMutex
	plugins []Plugin
	index   map[string]int // name → slice index
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{
		index: make(map[string]int),
	}
}

// Register adds p to the manager. Returns an error if a plugin with the
// same name is already registered.
func (m *Manager) Register(p Plugin) error {
	if p == nil {
		return errors.New("plugins: cannot register nil plugin")
	}
	name := p.Name()
	if name == "" {
		return errors.New("plugins: plugin name must not be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.index[name]; exists {
		return fmt.Errorf("plugins: plugin %q is already registered", name)
	}

	m.index[name] = len(m.plugins)
	m.plugins = append(m.plugins, p)
	return nil
}

// StartAll calls Start on every registered plugin in registration order.
// It returns the first error encountered; subsequent plugins are still
// started.
func (m *Manager) StartAll() error {
	m.mu.RLock()
	ps := m.snapshot()
	m.mu.RUnlock()

	var first error
	for _, p := range ps {
		if err := p.Start(); err != nil && first == nil {
			first = fmt.Errorf("plugins: start %q: %w", p.Name(), err)
		}
	}
	return first
}

// StopAll calls Stop on every registered plugin in reverse registration
// order (LIFO). It returns the first error encountered; all plugins are
// still stopped.
func (m *Manager) StopAll() error {
	m.mu.RLock()
	ps := m.snapshot()
	m.mu.RUnlock()

	var first error
	for i := len(ps) - 1; i >= 0; i-- {
		if err := ps[i].Stop(); err != nil && first == nil {
			first = fmt.Errorf("plugins: stop %q: %w", ps[i].Name(), err)
		}
	}
	return first
}

// HealthCheckAll calls HealthCheck on every registered plugin and returns
// a map of plugin name → error (nil means healthy). Plugins that are
// healthy are omitted from the returned map only when the map would be
// empty; callers should treat a missing name as healthy.
func (m *Manager) HealthCheckAll() map[string]error {
	m.mu.RLock()
	ps := m.snapshot()
	m.mu.RUnlock()

	results := make(map[string]error, len(ps))
	for _, p := range ps {
		results[p.Name()] = p.HealthCheck()
	}
	return results
}

// List returns the names of all registered plugins in registration order.
func (m *Manager) List() []string {
	m.mu.RLock()
	ps := m.snapshot()
	m.mu.RUnlock()

	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name()
	}
	return names
}

// snapshot returns a copy of the plugin slice (must be called with at
// least a read lock held).
func (m *Manager) snapshot() []Plugin {
	cp := make([]Plugin, len(m.plugins))
	copy(cp, m.plugins)
	return cp
}
