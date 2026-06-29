// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * app_jsdt module - JavaScript (QuickJS) script bindings (mock).
 * Port of the kamailio app_jsdt module (src/modules/app_jsdt).
 *
 * The original C module embeds the QuickJS interpreter to run Kamailio
 * routing logic written in JavaScript. This Go counterpart provides the
 * same API surface backed by an in-memory function registry so scripts
 * can be "loaded" and registered Go functions invoked without a real
 * QuickJS interpreter.
 *
 * It is safe for concurrent use.
 */

package app_jsdt

import (
	"fmt"
	"sync"
)

// JSDTConfig configures a JSDTModule.
type JSDTConfig struct {
	ScriptPath string
	Functions  []string
}

// jsHandler is a registered Go function callable from "JavaScript".
type jsHandler func(args ...interface{}) (interface{}, error)

// JSDTModule loads and runs JavaScript scripts.
// It is the Go counterpart of the kamailio app_jsdt module.
type JSDTModule struct {
	mu         sync.RWMutex
	scriptPath string
	loaded     bool
	functions  map[string]jsHandler
}

// New creates a JSDTModule.
func New() *JSDTModule {
	return &JSDTModule{functions: make(map[string]jsHandler)}
}

// Init (re)configures the module from cfg and loads the configured
// script when present. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *JSDTModule) Init(cfg *JSDTConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &JSDTConfig{}
	}
	m.scriptPath = cfg.ScriptPath
	if m.functions == nil {
		m.functions = make(map[string]jsHandler)
	}
	m.loaded = false
	if m.scriptPath != "" {
		m.loaded = true
	}
	return nil
}

// LoadScript loads a JavaScript script from path.
//
//	C: jsdt_load_script()
func (m *JSDTModule) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if path == "" {
		return fmt.Errorf("app_jsdt: empty script path")
	}
	m.scriptPath = path
	m.loaded = true
	return nil
}

// CallFunction invokes a previously registered function by name.
//
//	C: jsdt_call_function()
func (m *JSDTModule) CallFunction(name string, args ...interface{}) (interface{}, error) {
	m.mu.RLock()
	handler, ok := m.functions[name]
	loaded := m.loaded
	m.mu.RUnlock()
	if !loaded {
		return nil, fmt.Errorf("app_jsdt: no script loaded")
	}
	if !ok {
		return nil, fmt.Errorf("app_jsdt: function %q not registered", name)
	}
	return handler(args...)
}

// RegisterFunction registers a Go function callable from "JavaScript" scripts.
//
//	C: jsdt_register_function()
func (m *JSDTModule) RegisterFunction(name string, fn func(...interface{}) (interface{}, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.functions[name] = fn
}

// IsLoaded reports whether a script has been loaded.
//
//	C: jsdt_is_loaded()
func (m *JSDTModule) IsLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loaded
}

// Reload reloads the currently configured script.
//
//	C: jsdt_reload()
func (m *JSDTModule) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.scriptPath == "" {
		return fmt.Errorf("app_jsdt: no script to reload")
	}
	m.loaded = true
	return nil
}

// --- package-level API ---

var defaultModule = New()

// DefaultJSDT returns the package-level default JSDTModule.
func DefaultJSDT() *JSDTModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	_ = defaultModule.Init(&JSDTConfig{})
}
