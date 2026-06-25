// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * app_perl module - Perl script bindings (mock).
 * Port of the kamailio app_perl module (src/modules/app_perl).
 *
 * The original C module embeds the Perl interpreter to run Kamailio
 * routing logic written in Perl (perl_exec/perl_exec_simple). This Go
 * counterpart provides the same API surface backed by a PerlInterpreter
 * interface so a real Perl binding can be plugged in. A mock
 * implementation is provided for testing and for environments without
 * libperl.
 *
 * It is safe for concurrent use.
 */

package app_perl

import (
	"fmt"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// PerlInterpreter abstracts a Perl interpreter. A real deployment would
// bind to libperl; the mockPerlInterp below is used by default.
type PerlInterpreter interface {
	LoadScript(path string) error
	CallFunction(name string, args ...interface{}) (interface{}, error)
	Eval(script string) (interface{}, error)
	Close()
}

// Config configures an AppPerlModule.
//
//	C: mod_init() params: filename, modpath
type Config struct {
	ScriptPath string
	ModPath    string
	AutoReload bool
}

// AppPerlModule loads and runs Perl scripts.
// It is the Go counterpart of the kamailio app_perl module.
type AppPerlModule struct {
	mu      sync.RWMutex
	interp  PerlInterpreter
	scripts map[string]bool   // loaded script paths
	funcs   map[string]string // function name -> script path
	config  Config
}

// New creates an AppPerlModule backed by a mock Perl interpreter.
//
//	C: mod_init()
func New() *AppPerlModule {
	return &AppPerlModule{
		interp:  newMockPerlInterp(),
		scripts: make(map[string]bool),
		funcs:   make(map[string]string),
	}
}

// NewWithInterpreter creates an AppPerlModule with a custom interpreter.
// If interp is nil a mock interpreter is used.
func NewWithInterpreter(interp PerlInterpreter) *AppPerlModule {
	if interp == nil {
		interp = newMockPerlInterp()
	}
	return &AppPerlModule{
		interp:  interp,
		scripts: make(map[string]bool),
		funcs:   make(map[string]string),
	}
}

// Init (re)configures the module from cfg and loads the configured
// script when present. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *AppPerlModule) Init(cfg *Config) error {
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

// LoadScript loads a Perl script from path.
//
//	C: parser_init() / perl_parse()
func (m *AppPerlModule) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if path == "" {
		return fmt.Errorf("app_perl: empty script path")
	}
	if err := m.interp.LoadScript(path); err != nil {
		return err
	}
	m.scripts[path] = true
	return nil
}

// CallFunction invokes a previously registered Perl function by name.
// The SIP message and string args are forwarded to the interpreter.
// Returns the integer return code from the Perl function.
//
//	C: perl_exec() / perl_exec2()
func (m *AppPerlModule) CallFunction(name string, msg *parser.SIPMsg, args ...string) (int, error) {
	m.mu.RLock()
	_, registered := m.funcs[name]
	loaded := len(m.scripts) > 0
	interp := m.interp
	m.mu.RUnlock()
	if !loaded {
		return -1, fmt.Errorf("app_perl: no script loaded")
	}
	if !registered {
		return -1, fmt.Errorf("app_perl: function %q not registered", name)
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

// RegisterFunction registers a Perl function name as living in scriptPath.
//
//	C: perl_checkfnc() lookup table
func (m *AppPerlModule) RegisterFunction(name, scriptPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == "" {
		return fmt.Errorf("app_perl: empty function name")
	}
	if scriptPath == "" {
		return fmt.Errorf("app_perl: empty script path")
	}
	m.funcs[name] = scriptPath
	return nil
}

// Eval evaluates a Perl expression and returns the result.
//
//	C: Perl_eval_pv() equivalent
func (m *AppPerlModule) Eval(script string) (interface{}, error) {
	m.mu.RLock()
	loaded := len(m.scripts) > 0
	interp := m.interp
	m.mu.RUnlock()
	if !loaded {
		return nil, fmt.Errorf("app_perl: no script loaded")
	}
	return interp.Eval(script)
}

// Reload reloads the currently configured script.
//
//	C: perl_reload()
func (m *AppPerlModule) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.ScriptPath == "" && len(m.scripts) == 0 {
		return fmt.Errorf("app_perl: no script to reload")
	}
	path := m.config.ScriptPath
	if path == "" {
		// reload the first loaded script
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
func (m *AppPerlModule) IsLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.scripts) > 0
}

// ScriptCount returns the number of loaded scripts.
func (m *AppPerlModule) ScriptCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.scripts)
}

// Close releases the interpreter resources.
//
//	C: destroy()
func (m *AppPerlModule) Close() {
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
		return -1, fmt.Errorf("app_perl: unexpected return type %T", v)
	}
}

// ---------------------------------------------------------------------------
// Mock Perl interpreter
// ---------------------------------------------------------------------------

// mockPerlInterp is an in-memory PerlInterpreter used for testing and as
// the default when no real Perl binding is available.
type mockPerlInterp struct {
	mu      sync.Mutex
	scripts map[string]string                              // path -> content
	funcs   map[string]func(args ...interface{}) interface{} // name -> impl
	closed  bool
}

// newMockPerlInterp creates an empty mock interpreter.
func newMockPerlInterp() *mockPerlInterp {
	return &mockPerlInterp{
		scripts: make(map[string]string),
		funcs:   make(map[string]func(args ...interface{}) interface{}),
	}
}

// LoadScript records the script path in the mock.
func (m *mockPerlInterp) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("app_perl: interpreter closed")
	}
	if path == "" {
		return fmt.Errorf("app_perl: empty script path")
	}
	m.scripts[path] = ""
	return nil
}

// CallFunction looks up and invokes a registered function.
func (m *mockPerlInterp) CallFunction(name string, args ...interface{}) (interface{}, error) {
	m.mu.Lock()
	fn, ok := m.funcs[name]
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("app_perl: interpreter closed")
	}
	if !ok {
		return nil, fmt.Errorf("app_perl: unknown function %q", name)
	}
	return fn(args...), nil
}

// Eval returns the evaluated script string (mock semantics).
func (m *mockPerlInterp) Eval(script string) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fmt.Errorf("app_perl: interpreter closed")
	}
	return script, nil
}

// Close clears the mock state.
func (m *mockPerlInterp) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scripts = make(map[string]string)
	m.funcs = make(map[string]func(args ...interface{}) interface{})
	m.closed = true
}

// RegisterFunc registers a Go function implementation for a named Perl
// function. This is a test helper used to program the mock's behaviour.
func (m *mockPerlInterp) RegisterFunc(name string, fn func(args ...interface{}) interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = false
	m.funcs[name] = fn
}

// HasScript reports whether a script path was loaded.
func (m *mockPerlInterp) HasScript(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.scripts[path]
	return ok
}

// ---------------------------------------------------------------------------
// Package-level API (default singleton with double-checked locking)
// ---------------------------------------------------------------------------

var (
	defaultModule *AppPerlModule
	defaultMu     sync.RWMutex
)

// DefaultAppPerl returns the package-level default AppPerlModule, creating
// it on first use.
func DefaultAppPerl() *AppPerlModule {
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
func LoadScript(path string) error { return DefaultAppPerl().LoadScript(path) }

// CallFunction invokes a function on the default module.
func CallFunction(name string, msg *parser.SIPMsg, args ...string) (int, error) {
	return DefaultAppPerl().CallFunction(name, msg, args...)
}

// RegisterFunction registers a function on the default module.
func RegisterFunction(name, scriptPath string) error {
	return DefaultAppPerl().RegisterFunction(name, scriptPath)
}

// Eval evaluates an expression on the default module.
func Eval(script string) (interface{}, error) { return DefaultAppPerl().Eval(script) }

// Reload reloads the default module.
func Reload() error { return DefaultAppPerl().Reload() }
