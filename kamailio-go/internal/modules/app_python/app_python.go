// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * app_python module - Python2 script bindings (mock).
 * Port of the kamailio app_python module (src/modules/app_python).
 *
 * The original C module embeds the CPython2 interpreter to run Kamailio
 * routing logic written in Python (python_exec). This Go counterpart
 * provides the same API surface backed by a PythonInterpreter interface
 * so a real Python binding can be plugged in. A mock implementation is
 * provided for testing and for environments without CPython.
 *
 * It is safe for concurrent use.
 */

package app_python

import (
	"fmt"
	"sync"
)

// PythonInterpreter abstracts a Python interpreter. A real deployment
// would bind to CPython; the mockPythonInterp below is used by default.
// This interface is also reused by the app_python3s module.
type PythonInterpreter interface {
	LoadScript(path string) error
	CallFunction(name string, args ...interface{}) (interface{}, error)
	Eval(expr string) (interface{}, error)
	Close()
}

// Config configures an AppPythonModule.
//
//	C: mod_init() params: script_name/load, mod_init_function
type Config struct {
	ScriptPath      string
	ModInitFunction string
	AutoReload      bool
}

// AppPythonModule loads and runs Python2 scripts.
// It is the Go counterpart of the kamailio app_python module.
type AppPythonModule struct {
	mu      sync.RWMutex
	interp  PythonInterpreter
	scripts map[string]bool   // loaded script paths
	funcs   map[string]string // function name -> script path
	config  Config
}

// New creates an AppPythonModule backed by a mock Python interpreter.
//
//	C: mod_init()
func New() *AppPythonModule {
	return &AppPythonModule{
		interp:  newMockPythonInterp(),
		scripts: make(map[string]bool),
		funcs:   make(map[string]string),
	}
}

// NewWithInterpreter creates an AppPythonModule with a custom interpreter.
// If interp is nil a mock interpreter is used.
func NewWithInterpreter(interp PythonInterpreter) *AppPythonModule {
	if interp == nil {
		interp = newMockPythonInterp()
	}
	return &AppPythonModule{
		interp:  interp,
		scripts: make(map[string]bool),
		funcs:   make(map[string]string),
	}
}

// Init (re)configures the module from cfg and loads the configured
// script when present. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *AppPythonModule) Init(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &Config{}
	}
	m.config = *cfg
	m.scripts = make(map[string]bool)
	m.funcs = make(map[string]string)
	if m.config.ScriptPath != "" {
		if err := m.interp.LoadScript(m.config.ScriptPath); err != nil {
			return err
		}
		m.scripts[m.config.ScriptPath] = true
	}
	return nil
}

// LoadScript loads a Python script from path.
//
//	C: apy_load_script()
func (m *AppPythonModule) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if path == "" {
		return fmt.Errorf("app_python: empty script path")
	}
	if err := m.interp.LoadScript(path); err != nil {
		return err
	}
	m.scripts[path] = true
	return nil
}

// CallFunction invokes a previously registered Python function by name.
//
//	C: python_exec() / apy_exec()
func (m *AppPythonModule) CallFunction(name string, args ...interface{}) (interface{}, error) {
	m.mu.RLock()
	_, registered := m.funcs[name]
	loaded := len(m.scripts) > 0
	interp := m.interp
	m.mu.RUnlock()
	if !loaded {
		return nil, fmt.Errorf("app_python: no script loaded")
	}
	if !registered {
		return nil, fmt.Errorf("app_python: function %q not registered", name)
	}
	return interp.CallFunction(name, args...)
}

// RegisterFunction registers a Python function name as living in scriptPath.
//
//	C: python function lookup table
func (m *AppPythonModule) RegisterFunction(name, scriptPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == "" {
		return fmt.Errorf("app_python: empty function name")
	}
	if scriptPath == "" {
		return fmt.Errorf("app_python: empty script path")
	}
	m.funcs[name] = scriptPath
	return nil
}

// Eval evaluates a Python expression and returns the result.
//
//	C: PyRun_SimpleString() equivalent
func (m *AppPythonModule) Eval(expr string) (interface{}, error) {
	m.mu.RLock()
	loaded := len(m.scripts) > 0
	interp := m.interp
	m.mu.RUnlock()
	if !loaded {
		return nil, fmt.Errorf("app_python: no script loaded")
	}
	return interp.Eval(expr)
}

// Reload reloads the currently configured script.
//
//	C: apy_reload_script()
func (m *AppPythonModule) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.ScriptPath == "" && len(m.scripts) == 0 {
		return fmt.Errorf("app_python: no script to reload")
	}
	path := m.config.ScriptPath
	if path == "" {
		for p := range m.scripts {
			path = p
			break
		}
	}
	if err := m.interp.LoadScript(path); err != nil {
		return err
	}
	m.scripts[path] = true
	return nil
}

// IsLoaded reports whether at least one script has been loaded.
func (m *AppPythonModule) IsLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.scripts) > 0
}

// ScriptCount returns the number of loaded scripts.
func (m *AppPythonModule) ScriptCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.scripts)
}

// Close releases the interpreter resources.
//
//	C: mod_destroy()
func (m *AppPythonModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.interp != nil {
		m.interp.Close()
	}
	m.scripts = make(map[string]bool)
	m.funcs = make(map[string]string)
}

// ---------------------------------------------------------------------------
// Mock Python interpreter
// ---------------------------------------------------------------------------

// mockPythonInterp is an in-memory PythonInterpreter used for testing and as
// the default when no real CPython binding is available.
type mockPythonInterp struct {
	mu      sync.Mutex
	scripts map[string]bool                                // loaded paths
	funcs   map[string]func(args ...interface{}) interface{} // name -> impl
	closed  bool
}

// newMockPythonInterp creates an empty mock interpreter.
func newMockPythonInterp() *mockPythonInterp {
	return &mockPythonInterp{
		scripts: make(map[string]bool),
		funcs:   make(map[string]func(args ...interface{}) interface{}),
	}
}

// LoadScript records the script path in the mock.
func (m *mockPythonInterp) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("app_python: interpreter closed")
	}
	if path == "" {
		return fmt.Errorf("app_python: empty script path")
	}
	m.scripts[path] = true
	return nil
}

// CallFunction looks up and invokes a registered function.
func (m *mockPythonInterp) CallFunction(name string, args ...interface{}) (interface{}, error) {
	m.mu.Lock()
	fn, ok := m.funcs[name]
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("app_python: interpreter closed")
	}
	if !ok {
		return nil, fmt.Errorf("app_python: unknown function %q", name)
	}
	return fn(args...), nil
}

// Eval returns the evaluated expression string (mock semantics).
func (m *mockPythonInterp) Eval(expr string) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fmt.Errorf("app_python: interpreter closed")
	}
	return expr, nil
}

// Close clears the mock state.
func (m *mockPythonInterp) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scripts = make(map[string]bool)
	m.funcs = make(map[string]func(args ...interface{}) interface{})
	m.closed = true
}

// RegisterFunc registers a Go function implementation for a named Python
// function. This is a test helper used to program the mock's behaviour.
func (m *mockPythonInterp) RegisterFunc(name string, fn func(args ...interface{}) interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = false
	m.funcs[name] = fn
}

// HasScript reports whether a script path was loaded.
func (m *mockPythonInterp) HasScript(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.scripts[path]
}

// ---------------------------------------------------------------------------
// Package-level API (default singleton with double-checked locking)
// ---------------------------------------------------------------------------

var (
	defaultModule *AppPythonModule
	defaultMu     sync.RWMutex
)

// DefaultAppPython returns the package-level default AppPythonModule,
// creating it on first use.
func DefaultAppPython() *AppPythonModule {
	defaultMu.RLock()
	m := defaultModule
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultModule == nil {
		defaultModule = New()
	}
	return defaultModule
}

// Init (re)initialises the package-level default module to a fresh state.
func Init() {
	defaultMu.Lock()
	defaultModule = New()
	defaultMu.Unlock()
}

// LoadScript loads a script on the default module.
func LoadScript(path string) error { return DefaultAppPython().LoadScript(path) }

// CallFunction invokes a function on the default module.
func CallFunction(name string, args ...interface{}) (interface{}, error) {
	return DefaultAppPython().CallFunction(name, args...)
}

// RegisterFunction registers a function on the default module.
func RegisterFunction(name, scriptPath string) error {
	return DefaultAppPython().RegisterFunction(name, scriptPath)
}

// Eval evaluates an expression on the default module.
func Eval(expr string) (interface{}, error) { return DefaultAppPython().Eval(expr) }

// Reload reloads the default module.
func Reload() error { return DefaultAppPython().Reload() }
