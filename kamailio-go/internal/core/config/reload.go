// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Runtime configuration manager - hot-reload support.
 *
 * Mirrors the C Kamailio behaviour where SIGHUP (or the cfg_rpc
 * "cfg.reload" RPC) re-reads the configuration file and propagates the
 * new values to every subsystem that opted in.
 *
 * The Manager holds the live *Config behind an atomic pointer so
 * concurrent readers never block. Reloads are serialised by an internal
 * mutex; subscribers are invoked sequentially after the swap so they
 * observe a consistent snapshot.
 */

package config

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// ReloadCallback is invoked after a successful reload with the previous
// and the newly-installed configuration. Callbacks may read either
// pointer freely; they must not mutate them. Errors returned by a
// callback are logged by the Manager but do not abort the reload.
type ReloadCallback func(old, new *Config) error

// Manager owns the live *Config and coordinates hot-reloads. The zero
// value is not usable — construct one with NewManager.
type Manager struct {
	cfg    atomic.Pointer[Config]
	path   string
	mu     sync.Mutex
	subs   []ReloadCallback
}

// NewManager installs cfg as the live configuration and records path as
// the source file for subsequent Reload() calls. A non-empty path is
// required for Reload() to work; SetConfig() can be used regardless.
func NewManager(cfg *Config, path string) *Manager {
	m := &Manager{path: path}
	if cfg == nil {
		cfg = DefaultConfig()
	}
	m.cfg.Store(cfg)
	return m
}

// Get returns the current configuration. The returned pointer is safe
// to read concurrently and is never mutated in place by the Manager.
func (m *Manager) Get() *Config {
	if m == nil {
		return nil
	}
	return m.cfg.Load()
}

// Path returns the source file path used by Reload(), or empty when
// the Manager was constructed without one.
func (m *Manager) Path() string {
	if m == nil {
		return ""
	}
	return m.path
}

// SetPath updates the source file path used by Reload(). This is useful
// when the initial config came from bytes (or defaults) and an operator
// later points the process at a file to enable SIGHUP-based reloads.
func (m *Manager) SetPath(path string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.path = path
	m.mu.Unlock()
}

// Subscribe registers fn to be invoked after every successful reload.
// The returned function deregisters fn when called. Subscribers are
// notified in registration order.
func (m *Manager) Subscribe(fn ReloadCallback) (unsubscribe func()) {
	if m == nil || fn == nil {
		return func() {}
	}
	m.mu.Lock()
	m.subs = append(m.subs, fn)
	idx := len(m.subs) - 1
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if idx < len(m.subs) {
			m.subs[idx] = nil
		}
	}
}

// Reload re-reads the source file, validates it, swaps the live config
// and notifies subscribers. It returns an error when no path is set, the
// file cannot be read/parsed, or validation fails. On error the live
// config is left untouched.
func (m *Manager) Reload() (*Config, error) {
	if m == nil {
		return nil, fmt.Errorf("nil config manager")
	}
	m.mu.Lock()
	path := m.path
	subs := append([]ReloadCallback(nil), m.subs...)
	m.mu.Unlock()
	if path == "" {
		return nil, fmt.Errorf("no config path set — cannot reload")
	}
	newCfg, err := Load(path)
	if err != nil {
		return nil, fmt.Errorf("reload %s: %w", path, err)
	}
	report := newCfg.ValidateStrict()
	if report.HasErrors() {
		return nil, fmt.Errorf("reload %s: invalid configuration: %s", path, report.Error())
	}
	old := m.swap(newCfg)
	for _, fn := range subs {
		if fn == nil {
			continue
		}
		// Subscriber errors are surfaced but do not roll back the swap.
		_ = fn(old, newCfg)
	}
	return newCfg, nil
}

// SetConfig validates and installs cfg as the new live configuration,
// invoking subscribers as if a reload had occurred. It is the
// programmatic counterpart to Reload() and is primarily intended for
// tests and RPC-driven overrides that bypass the file.
func (m *Manager) SetConfig(cfg *Config) error {
	if m == nil {
		return fmt.Errorf("nil config manager")
	}
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	report := cfg.ValidateStrict()
	if report.HasErrors() {
		return fmt.Errorf("invalid configuration: %s", report.Error())
	}
	m.mu.Lock()
	subs := append([]ReloadCallback(nil), m.subs...)
	m.mu.Unlock()
	old := m.swap(cfg)
	for _, fn := range subs {
		if fn == nil {
			continue
		}
		_ = fn(old, cfg)
	}
	return nil
}

// swap atomically replaces the live config and returns the previous one.
func (m *Manager) swap(cfg *Config) *Config {
	return m.cfg.Swap(cfg)
}
