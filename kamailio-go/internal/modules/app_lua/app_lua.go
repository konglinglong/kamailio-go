// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * app_lua module - Lua script bindings (mock).
 * Port of the kamailio app_lua module (src/modules/app_lua).
 *
 * The original C module embeds the Lua interpreter to run Kamailio
 * routing logic written in Lua. This Go counterpart provides the same
 * API surface backed by an in-memory function registry so scripts can
 * be "loaded" and registered Go functions invoked without a real Lua
 * interpreter. A real deployment would replace the dispatch with a
 * gopher-lua call.
 *
 * It is safe for concurrent use.
 */

package app_lua

import (
	"fmt"
	"sync"
)

// LuaConfig configures a LuaModule.
type LuaConfig struct {
	ScriptPath string
	Functions  []string
}

// luaHandler is a registered Go function callable from "Lua".
type luaHandler func(args ...interface{}) (interface{}, error)

// LuaModule loads and runs Lua scripts.
// It is the Go counterpart of the kamailio app_lua module.
type LuaModule struct {
	mu        sync.RWMutex
	scriptPath string
	loaded     bool
	functions  map[string]luaHandler
}

// New creates a LuaModule.
func New() *LuaModule {
	return &LuaModule{functions: make(map[string]luaHandler)}
}

// Init (re)configures the module from cfg and loads the configured
// script when present. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *LuaModule) Init(cfg *LuaConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &LuaConfig{}
	}
	m.scriptPath = cfg.ScriptPath
	if m.functions == nil {
		m.functions = make(map[string]luaHandler)
	}
	m.loaded = false
	if m.scriptPath != "" {
		// In the mock, "loading" simply marks the script as loaded.
		m.loaded = true
	}
	return nil
}

// LoadScript loads a Lua script from path. In the mock implementation
// this records the path and marks the module as loaded.
//
//	C: lua_load_script()
func (m *LuaModule) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if path == "" {
		return fmt.Errorf("app_lua: empty script path")
	}
	m.scriptPath = path
	m.loaded = true
	return nil
}

// CallFunction invokes a previously registered function by name.
//
//	C: lua_call_function()
func (m *LuaModule) CallFunction(name string, args ...interface{}) (interface{}, error) {
	m.mu.RLock()
	handler, ok := m.functions[name]
	loaded := m.loaded
	m.mu.RUnlock()
	if !loaded {
		return nil, fmt.Errorf("app_lua: no script loaded")
	}
	if !ok {
		return nil, fmt.Errorf("app_lua: function %q not registered", name)
	}
	return handler(args...)
}

// RegisterFunction registers a Go function callable from "Lua" scripts.
//
//	C: lua_register_function()
func (m *LuaModule) RegisterFunction(name string, fn func(...interface{}) (interface{}, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.functions[name] = fn
}

// IsLoaded reports whether a script has been loaded.
//
//	C: lua_is_loaded()
func (m *LuaModule) IsLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loaded
}

// Reload reloads the currently configured script.
//
//	C: lua_reload()
func (m *LuaModule) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.scriptPath == "" {
		return fmt.Errorf("app_lua: no script to reload")
	}
	m.loaded = true
	return nil
}

// --- package-level API ---

var defaultModule = New()

// DefaultLua returns the package-level default LuaModule.
func DefaultLua() *LuaModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	_ = defaultModule.Init(&LuaConfig{})
}
