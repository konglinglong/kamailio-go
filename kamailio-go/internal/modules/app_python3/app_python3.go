// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * app_python3 module - Python3 script bindings (mock).
 * Port of the kamailio app_python3 module (src/modules/app_python3).
 *
 * The original C module embeds the CPython interpreter to run Kamailio
 * routing logic written in Python. This Go counterpart provides the same
 * API surface backed by an in-memory function registry so scripts can
 * be "loaded" and registered Go functions invoked without a real Python
 * interpreter.
 *
 * It is safe for concurrent use.
 */

package app_python3

import (
	"fmt"
	"sync"
)

// PythonConfig configures a PythonModule.
type PythonConfig struct {
	ScriptPath string
	Functions  []string
}

// pyHandler is a registered Go function callable from "Python".
type pyHandler func(args ...interface{}) (interface{}, error)

// PythonModule loads and runs Python scripts.
// It is the Go counterpart of the kamailio app_python3 module.
type PythonModule struct {
	mu         sync.RWMutex
	scriptPath string
	loaded     bool
	functions  map[string]pyHandler
}

// New creates a PythonModule.
func New() *PythonModule {
	return &PythonModule{functions: make(map[string]pyHandler)}
}

// Init (re)configures the module from cfg and loads the configured
// script when present. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *PythonModule) Init(cfg *PythonConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &PythonConfig{}
	}
	m.scriptPath = cfg.ScriptPath
	if m.functions == nil {
		m.functions = make(map[string]pyHandler)
	}
	m.loaded = false
	if m.scriptPath != "" {
		m.loaded = true
	}
	return nil
}

// LoadScript loads a Python script from path.
//
//	C: python_load_script()
func (m *PythonModule) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if path == "" {
		return fmt.Errorf("app_python3: empty script path")
	}
	m.scriptPath = path
	m.loaded = true
	return nil
}

// CallFunction invokes a previously registered function by name.
//
//	C: python_call_function()
func (m *PythonModule) CallFunction(name string, args ...interface{}) (interface{}, error) {
	m.mu.RLock()
	handler, ok := m.functions[name]
	loaded := m.loaded
	m.mu.RUnlock()
	if !loaded {
		return nil, fmt.Errorf("app_python3: no script loaded")
	}
	if !ok {
		return nil, fmt.Errorf("app_python3: function %q not registered", name)
	}
	return handler(args...)
}

// RegisterFunction registers a Go function callable from "Python" scripts.
//
//	C: python_register_function()
func (m *PythonModule) RegisterFunction(name string, fn func(...interface{}) (interface{}, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.functions[name] = fn
}

// IsLoaded reports whether a script has been loaded.
//
//	C: python_is_loaded()
func (m *PythonModule) IsLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loaded
}

// Reload reloads the currently configured script.
//
//	C: python_reload()
func (m *PythonModule) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.scriptPath == "" {
		return fmt.Errorf("app_python3: no script to reload")
	}
	m.loaded = true
	return nil
}

// --- package-level API ---

var defaultModule = New()

// DefaultPython3 returns the package-level default PythonModule.
func DefaultPython3() *PythonModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	_ = defaultModule.Init(&PythonConfig{})
}
