// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * app_ruby module - Ruby script bindings (mock).
 * Port of the kamailio app_ruby module (src/modules/app_ruby).
 *
 * The original C module embeds the mruby interpreter to run Kamailio
 * routing logic written in Ruby. This Go counterpart provides the same
 * API surface backed by an in-memory function registry so scripts can
 * be "loaded" and registered Go functions invoked without a real Ruby
 * interpreter.
 *
 * It is safe for concurrent use.
 */

package app_ruby

import (
	"fmt"
	"sync"
)

// RubyConfig configures a RubyModule.
type RubyConfig struct {
	ScriptPath string
	Functions  []string
}

// rbHandler is a registered Go function callable from "Ruby".
type rbHandler func(args ...interface{}) (interface{}, error)

// RubyModule loads and runs Ruby scripts.
// It is the Go counterpart of the kamailio app_ruby module.
type RubyModule struct {
	mu         sync.RWMutex
	scriptPath string
	loaded     bool
	functions  map[string]rbHandler
}

// New creates a RubyModule.
func New() *RubyModule {
	return &RubyModule{functions: make(map[string]rbHandler)}
}

// Init (re)configures the module from cfg and loads the configured
// script when present. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *RubyModule) Init(cfg *RubyConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &RubyConfig{}
	}
	m.scriptPath = cfg.ScriptPath
	if m.functions == nil {
		m.functions = make(map[string]rbHandler)
	}
	m.loaded = false
	if m.scriptPath != "" {
		m.loaded = true
	}
	return nil
}

// LoadScript loads a Ruby script from path.
//
//	C: ruby_load_script()
func (m *RubyModule) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if path == "" {
		return fmt.Errorf("app_ruby: empty script path")
	}
	m.scriptPath = path
	m.loaded = true
	return nil
}

// CallFunction invokes a previously registered function by name.
//
//	C: ruby_call_function()
func (m *RubyModule) CallFunction(name string, args ...interface{}) (interface{}, error) {
	m.mu.RLock()
	handler, ok := m.functions[name]
	loaded := m.loaded
	m.mu.RUnlock()
	if !loaded {
		return nil, fmt.Errorf("app_ruby: no script loaded")
	}
	if !ok {
		return nil, fmt.Errorf("app_ruby: function %q not registered", name)
	}
	return handler(args...)
}

// RegisterFunction registers a Go function callable from "Ruby" scripts.
//
//	C: ruby_register_function()
func (m *RubyModule) RegisterFunction(name string, fn func(...interface{}) (interface{}, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.functions[name] = fn
}

// IsLoaded reports whether a script has been loaded.
//
//	C: ruby_is_loaded()
func (m *RubyModule) IsLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loaded
}

// Reload reloads the currently configured script.
//
//	C: ruby_reload()
func (m *RubyModule) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.scriptPath == "" {
		return fmt.Errorf("app_ruby: no script to reload")
	}
	m.loaded = true
	return nil
}

// --- package-level API ---

var defaultModule = New()

// DefaultRuby returns the package-level default RubyModule.
func DefaultRuby() *RubyModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	_ = defaultModule.Init(&RubyConfig{})
}
