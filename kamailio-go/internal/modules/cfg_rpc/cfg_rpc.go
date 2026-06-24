// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Config RPC module - runtime configuration via RPC.
 * Port of the kamailio cfg_rpc module (src/modules/cfg_rpc).
 *
 * The module exposes get/set/list/reset operations over an in-memory
 * key/value store. Each key may have a registered default value that
 * Reset restores. It is safe for concurrent use.
 */

package cfg_rpc

import (
	"errors"
	"sync"
)

// CfgRPCModule maintains runtime configuration values with optional
// defaults.
type CfgRPCModule struct {
	mu       sync.RWMutex
	values   map[string]string
	defaults map[string]string
}

// New creates a CfgRPCModule with empty storage.
func New() *CfgRPCModule {
	return &CfgRPCModule{
		values:   make(map[string]string),
		defaults: make(map[string]string),
	}
}

// SetDefault registers a default value for a key.
func (m *CfgRPCModule) SetDefault(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaults[key] = value
}

// Get returns the current value for the given key. When the key has no
// current value but has a registered default, the default is returned.
// It returns an error when the key is unknown.
//
//	C: cfg_rpc.get()
func (m *CfgRPCModule) Get(key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	if d, ok := m.defaults[key]; ok {
		return d, nil
	}
	return "", errors.New("cfg_rpc: unknown key: " + key)
}

// Set assigns a new value to the given key.
//
//	C: cfg_rpc.set()
func (m *CfgRPCModule) Set(key, value string) error {
	if key == "" {
		return errors.New("cfg_rpc: empty key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = make(map[string]string)
	}
	m.values[key] = value
	return nil
}

// List returns a copy of all current values merged with any defaults
// that have no overriding current value.
func (m *CfgRPCModule) List() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.defaults)+len(m.values))
	for k, v := range m.defaults {
		out[k] = v
	}
	for k, v := range m.values {
		out[k] = v
	}
	return out
}

// Reset restores the default value for the given key, removing any
// overriding current value. It returns an error when the key has no
// registered default.
//
//	C: cfg_rpc.reset()
func (m *CfgRPCModule) Reset(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.defaults[key]; !ok {
		return errors.New("cfg_rpc: no default for key: " + key)
	}
	delete(m.values, key)
	return nil
}

// Count returns the number of keys with a current (non-default) value.
func (m *CfgRPCModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.values)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *CfgRPCModule
)

// DefaultCfgRPC returns the process-wide CfgRPCModule.
func DefaultCfgRPC() *CfgRPCModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide CfgRPCModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Get is the package-level wrapper around DefaultCfgRPC().Get.
func Get(key string) (string, error) { return DefaultCfgRPC().Get(key) }

// Set is the package-level wrapper around DefaultCfgRPC().Set.
func Set(key, value string) error { return DefaultCfgRPC().Set(key, value) }

// List is the package-level wrapper around DefaultCfgRPC().List.
func List() map[string]string { return DefaultCfgRPC().List() }

// Reset is the package-level wrapper around DefaultCfgRPC().Reset.
func Reset(key string) error { return DefaultCfgRPC().Reset(key) }
