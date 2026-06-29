// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * app_ruby_proc module - Ruby procedural script bindings (mock).
 * Port of the kamailio app_ruby_proc module (src/modules/app_ruby_proc).
 *
 * The original C module is a procedural variant of app_ruby: instead of
 * dispatching to Ruby class methods it calls top-level Ruby functions
 * (app_ruby_proc_run_ex). This Go counterpart provides the same API
 * surface backed by a RubyInterpreter interface so a real mruby binding
 * can be plugged in. A mock implementation is provided for testing.
 *
 * It is safe for concurrent use.
 */

package app_ruby_proc

import (
	"fmt"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// RubyInterpreter abstracts a Ruby interpreter. A real deployment would
// bind to mruby; the mockRubyInterp below is used by default.
type RubyInterpreter interface {
	LoadScript(path string) error
	CallFunction(name string, args ...interface{}) (interface{}, error)
	Eval(expr string) (interface{}, error)
	Close()
}

// Config configures an AppRubyProcModule.
//
//	C: mod_init() params
type Config struct {
	ScriptPath string
	AutoReload bool
}

// AppRubyProcModule loads and runs Ruby procedural scripts.
// It is the Go counterpart of the kamailio app_ruby_proc module.
type AppRubyProcModule struct {
	mu      sync.RWMutex
	interp  RubyInterpreter
	scripts map[string]bool   // loaded script paths
	funcs   map[string]string // function name -> script path
	config  Config
}

// New creates an AppRubyProcModule backed by a mock Ruby interpreter.
//
//	C: app_ruby_proc_init_child()
func New() *AppRubyProcModule {
	return &AppRubyProcModule{
		interp:  newMockRubyInterp(),
		scripts: make(map[string]bool),
		funcs:   make(map[string]string),
	}
}

// NewWithInterpreter creates an AppRubyProcModule with a custom interpreter.
// If interp is nil a mock interpreter is used.
func NewWithInterpreter(interp RubyInterpreter) *AppRubyProcModule {
	if interp == nil {
		interp = newMockRubyInterp()
	}
	return &AppRubyProcModule{
		interp:  interp,
		scripts: make(map[string]bool),
		funcs:   make(map[string]string),
	}
}

// Init (re)configures the module from cfg and loads the configured
// script when present. A nil cfg applies defaults.
//
//	C: app_ruby_proc_init_child()
func (m *AppRubyProcModule) Init(cfg *Config) error {
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

// LoadScript loads a Ruby script from path.
//
//	C: app_ruby_proc_load_script()
func (m *AppRubyProcModule) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if path == "" {
		return fmt.Errorf("app_ruby_proc: empty script path")
	}
	if err := m.interp.LoadScript(path); err != nil {
		return err
	}
	m.scripts[path] = true
	return nil
}

// CallFunction invokes a previously registered Ruby function by name.
// The SIP message and string args are forwarded to the interpreter.
// Returns the integer return code from the Ruby function.
//
//	C: app_ruby_proc_run_ex()
func (m *AppRubyProcModule) CallFunction(name string, msg *parser.SIPMsg, args ...string) (int, error) {
	m.mu.RLock()
	_, registered := m.funcs[name]
	loaded := len(m.scripts) > 0
	interp := m.interp
	m.mu.RUnlock()
	if !loaded {
		return -1, fmt.Errorf("app_ruby_proc: no script loaded")
	}
	if !registered {
		return -1, fmt.Errorf("app_ruby_proc: function %q not registered", name)
	}
	callArgs := make([]interface{}, 0, len(args)+1)
	callArgs = append(callArgs, msg)
	for _, a := range args {
		callArgs = append(callArgs, a)
	}
	res, err := interp.CallFunction(name, callArgs...)
	if err != nil {
		return -1, err
	}
	return toInt(res)
}

// RegisterFunction registers a Ruby function name as living in scriptPath.
//
//	C: app_ruby_proc_get_export() lookup table
func (m *AppRubyProcModule) RegisterFunction(name, scriptPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == "" {
		return fmt.Errorf("app_ruby_proc: empty function name")
	}
	if scriptPath == "" {
		return fmt.Errorf("app_ruby_proc: empty script path")
	}
	m.funcs[name] = scriptPath
	return nil
}

// Eval evaluates a Ruby expression and returns the result.
//
//	C: mruby_eval() equivalent
func (m *AppRubyProcModule) Eval(script string) (interface{}, error) {
	m.mu.RLock()
	loaded := len(m.scripts) > 0
	interp := m.interp
	m.mu.RUnlock()
	if !loaded {
		return nil, fmt.Errorf("app_ruby_proc: no script loaded")
	}
	return interp.Eval(script)
}

// Reload reloads the currently configured script.
//
//	C: app_ruby_proc reload
func (m *AppRubyProcModule) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.ScriptPath == "" && len(m.scripts) == 0 {
		return fmt.Errorf("app_ruby_proc: no script to reload")
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
func (m *AppRubyProcModule) IsLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.scripts) > 0
}

// ScriptCount returns the number of loaded scripts.
func (m *AppRubyProcModule) ScriptCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.scripts)
}

// Close releases the interpreter resources.
//
//	C: app_ruby_proc_mod_destroy()
func (m *AppRubyProcModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.interp != nil {
		m.interp.Close()
	}
	m.scripts = make(map[string]bool)
	m.funcs = make(map[string]string)
}

// toInt converts an interpreter return value to an int.
func toInt(v interface{}) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	case bool:
		if n {
			return 1, nil
		}
		return -1, nil
	case nil:
		return -1, nil
	default:
		return -1, fmt.Errorf("app_ruby_proc: unexpected return type %T", v)
	}
}

// ---------------------------------------------------------------------------
// Mock Ruby interpreter
// ---------------------------------------------------------------------------

// mockRubyInterp is an in-memory RubyInterpreter used for testing and as
// the default when no real mruby binding is available.
type mockRubyInterp struct {
	mu      sync.Mutex
	scripts map[string]string                              // path -> content
	funcs   map[string]func(args ...interface{}) interface{} // name -> impl
	closed  bool
}

// newMockRubyInterp creates an empty mock interpreter.
func newMockRubyInterp() *mockRubyInterp {
	return &mockRubyInterp{
		scripts: make(map[string]string),
		funcs:   make(map[string]func(args ...interface{}) interface{}),
	}
}

// LoadScript records the script path in the mock.
func (m *mockRubyInterp) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("app_ruby_proc: interpreter closed")
	}
	if path == "" {
		return fmt.Errorf("app_ruby_proc: empty script path")
	}
	m.scripts[path] = ""
	return nil
}

// CallFunction looks up and invokes a registered function.
func (m *mockRubyInterp) CallFunction(name string, args ...interface{}) (interface{}, error) {
	m.mu.Lock()
	fn, ok := m.funcs[name]
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("app_ruby_proc: interpreter closed")
	}
	if !ok {
		return nil, fmt.Errorf("app_ruby_proc: unknown function %q", name)
	}
	return fn(args...), nil
}

// Eval returns the evaluated expression string (mock semantics).
func (m *mockRubyInterp) Eval(expr string) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fmt.Errorf("app_ruby_proc: interpreter closed")
	}
	return expr, nil
}

// Close clears the mock state.
func (m *mockRubyInterp) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scripts = make(map[string]string)
	m.funcs = make(map[string]func(args ...interface{}) interface{})
	m.closed = true
}

// RegisterFunc registers a Go function implementation for a named Ruby
// function. This is a test helper used to program the mock's behaviour.
func (m *mockRubyInterp) RegisterFunc(name string, fn func(args ...interface{}) interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = false
	m.funcs[name] = fn
}

// HasScript reports whether a script path was loaded.
func (m *mockRubyInterp) HasScript(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.scripts[path]
	return ok
}

// ---------------------------------------------------------------------------
// Package-level API (default singleton with double-checked locking)
// ---------------------------------------------------------------------------

var (
	defaultModule *AppRubyProcModule
	defaultMu     sync.RWMutex
)

// DefaultAppRubyProc returns the package-level default AppRubyProcModule,
// creating it on first use.
func DefaultAppRubyProc() *AppRubyProcModule {
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
func LoadScript(path string) error { return DefaultAppRubyProc().LoadScript(path) }

// CallFunction invokes a function on the default module.
func CallFunction(name string, msg *parser.SIPMsg, args ...string) (int, error) {
	return DefaultAppRubyProc().CallFunction(name, msg, args...)
}

// RegisterFunction registers a function on the default module.
func RegisterFunction(name, scriptPath string) error {
	return DefaultAppRubyProc().RegisterFunction(name, scriptPath)
}

// Eval evaluates an expression on the default module.
func Eval(script string) (interface{}, error) { return DefaultAppRubyProc().Eval(script) }

// Reload reloads the default module.
func Reload() error { return DefaultAppRubyProc().Reload() }
