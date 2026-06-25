// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * app_python3s module - Python3 SIP-specific script bindings (mock).
 * Port of the kamailio app_python3s module (src/modules/app_python3s).
 *
 * The original C module embeds the CPython3 interpreter to run Kamailio
 * routing logic written in Python. Unlike app_python3, app_python3s is
 * SIP-message oriented: it supports passing the active sip_msg_t to
 * Python functions and exposes a message registry so scripts can look
 * up messages by handle. This Go counterpart reuses the
 * PythonInterpreter interface from the app_python package and adds the
 * SIP message registry. A mock implementation is provided for testing.
 *
 * It is safe for concurrent use.
 */

package app_python3s

import (
	"fmt"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/modules/app_python"
)

// Config configures an AppPython3SModule.
//
//	C: mod_init() params: load, script_init, script_child_init, threads_mode
type Config struct {
	ScriptPath  string
	ThreadsMode bool
}

// AppPython3SModule loads and runs Python3 scripts with SIP message
// passing support. It is the Go counterpart of the kamailio app_python3s
// module.
type AppPython3SModule struct {
	mu            sync.RWMutex
	interp        app_python.PythonInterpreter // reused from app_python
	scriptPath    string
	loadedScripts map[string]bool
	msgRegistry   map[uint64]*parser.SIPMsg // msg handle -> SIPMsg
	nextHandle    uint64
}

// New creates an AppPython3SModule backed by a mock Python interpreter.
//
//	C: mod_init()
func New() *AppPython3SModule {
	return &AppPython3SModule{
		interp:        newMockPython3SInterp(),
		loadedScripts: make(map[string]bool),
		msgRegistry:   make(map[uint64]*parser.SIPMsg),
	}
}

// NewWithInterpreter creates an AppPython3SModule with a custom interpreter.
// If interp is nil a mock interpreter is used.
func NewWithInterpreter(interp app_python.PythonInterpreter) *AppPython3SModule {
	if interp == nil {
		interp = newMockPython3SInterp()
	}
	return &AppPython3SModule{
		interp:        interp,
		loadedScripts: make(map[string]bool),
		msgRegistry:   make(map[uint64]*parser.SIPMsg),
	}
}

// Init (re)configures the module from cfg and loads the configured
// script when present. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *AppPython3SModule) Init(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &Config{}
	}
	m.scriptPath = cfg.ScriptPath
	m.loadedScripts = make(map[string]bool)
	m.msgRegistry = make(map[uint64]*parser.SIPMsg)
	m.nextHandle = 0
	if m.scriptPath != "" {
		if err := m.interp.LoadScript(m.scriptPath); err != nil {
			return err
		}
		m.loadedScripts[m.scriptPath] = true
	}
	return nil
}

// LoadScript loads a Python3 script from path.
//
//	C: apy_load_script()
func (m *AppPython3SModule) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if path == "" {
		return fmt.Errorf("app_python3s: empty script path")
	}
	if err := m.interp.LoadScript(path); err != nil {
		return err
	}
	m.scriptPath = path
	m.loadedScripts[path] = true
	return nil
}

// CallFunction invokes a Python function by name, passing the SIP
// message handle and string args. The message is registered in the
// registry for the duration of the call so the script can retrieve it
// via GetMsg. Returns the integer return code from the Python function.
//
//	C: apy3s_exec_func()
func (m *AppPython3SModule) CallFunction(name string, msg *parser.SIPMsg, args ...string) (int, error) {
	m.mu.RLock()
	loaded := len(m.loadedScripts) > 0
	interp := m.interp
	m.mu.RUnlock()
	if !loaded {
		return -1, fmt.Errorf("app_python3s: no script loaded")
	}
	// Register the message so the script can look it up by handle.
	handle := m.RegisterMsg(msg)
	defer m.ReleaseMsg(handle)

	callArgs := make([]interface{}, 0, len(args)+1)
	callArgs = append(callArgs, handle)
	for _, a := range args {
		callArgs = append(callArgs, a)
	}
	res, err := interp.CallFunction(name, callArgs...)
	if err != nil {
		return -1, err
	}
	return toInt(res)
}

// RegisterMsg registers a SIP message and returns a handle that can be
// used to retrieve it via GetMsg. A nil message is still registered and
// returns a valid handle.
//
//	C: sip_msg_t handle exposed to Python
func (m *AppPython3SModule) RegisterMsg(msg *parser.SIPMsg) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextHandle++
	handle := m.nextHandle
	m.msgRegistry[handle] = msg
	return handle
}

// GetMsg returns the SIP message registered under handle, or nil if the
// handle is unknown or has been released.
//
//	C: Python KSR.msg() lookup
func (m *AppPython3SModule) GetMsg(handle uint64) *parser.SIPMsg {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.msgRegistry[handle]
}

// ReleaseMsg releases the SIP message registered under handle. It is
// safe to call with an unknown handle (no-op).
//
//	C: Python message cleanup
func (m *AppPython3SModule) ReleaseMsg(handle uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.msgRegistry, handle)
}

// EvalExpr evaluates a Python expression in the context of the given SIP
// message. The message is registered for the duration of the evaluation.
//
//	C: PyRun_SimpleString() with msg context
func (m *AppPython3SModule) EvalExpr(expr string, msg *parser.SIPMsg) (interface{}, error) {
	m.mu.RLock()
	loaded := len(m.loadedScripts) > 0
	interp := m.interp
	m.mu.RUnlock()
	if !loaded {
		return nil, fmt.Errorf("app_python3s: no script loaded")
	}
	handle := m.RegisterMsg(msg)
	defer m.ReleaseMsg(handle)
	return interp.Eval(expr)
}

// IsLoaded reports whether at least one script has been loaded.
func (m *AppPython3SModule) IsLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.loadedScripts) > 0
}

// ScriptCount returns the number of loaded scripts.
func (m *AppPython3SModule) ScriptCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.loadedScripts)
}

// MsgCount returns the number of currently registered messages.
func (m *AppPython3SModule) MsgCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.msgRegistry)
}

// Close releases the interpreter resources and clears the registry.
//
//	C: mod_destroy()
func (m *AppPython3SModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.interp != nil {
		m.interp.Close()
	}
	m.loadedScripts = make(map[string]bool)
	m.msgRegistry = make(map[uint64]*parser.SIPMsg)
	m.nextHandle = 0
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
		return -1, fmt.Errorf("app_python3s: unexpected return type %T", v)
	}
}

// ---------------------------------------------------------------------------
// Mock Python3s interpreter (implements app_python.PythonInterpreter)
// ---------------------------------------------------------------------------

// mockPython3SInterp is an in-memory PythonInterpreter used for testing
// and as the default when no real CPython binding is available.
type mockPython3SInterp struct {
	mu      sync.Mutex
	scripts map[string]bool
	funcs   map[string]func(args ...interface{}) interface{}
	closed  bool
}

// newMockPython3SInterp creates an empty mock interpreter.
func newMockPython3SInterp() *mockPython3SInterp {
	return &mockPython3SInterp{
		scripts: make(map[string]bool),
		funcs:   make(map[string]func(args ...interface{}) interface{}),
	}
}

// LoadScript records the script path in the mock.
func (m *mockPython3SInterp) LoadScript(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("app_python3s: interpreter closed")
	}
	if path == "" {
		return fmt.Errorf("app_python3s: empty script path")
	}
	m.scripts[path] = true
	return nil
}

// CallFunction looks up and invokes a registered function.
func (m *mockPython3SInterp) CallFunction(name string, args ...interface{}) (interface{}, error) {
	m.mu.Lock()
	fn, ok := m.funcs[name]
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("app_python3s: interpreter closed")
	}
	if !ok {
		return nil, fmt.Errorf("app_python3s: unknown function %q", name)
	}
	return fn(args...), nil
}

// Eval returns the evaluated expression string (mock semantics).
func (m *mockPython3SInterp) Eval(expr string) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fmt.Errorf("app_python3s: interpreter closed")
	}
	return expr, nil
}

// Close clears the mock state.
func (m *mockPython3SInterp) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scripts = make(map[string]bool)
	m.funcs = make(map[string]func(args ...interface{}) interface{})
	m.closed = true
}

// RegisterFunc registers a Go function implementation for a named Python
// function. This is a test helper used to program the mock's behaviour.
func (m *mockPython3SInterp) RegisterFunc(name string, fn func(args ...interface{}) interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = false
	m.funcs[name] = fn
}

// HasScript reports whether a script path was loaded.
func (m *mockPython3SInterp) HasScript(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.scripts[path]
}

// Compile-time check that mockPython3SInterp implements PythonInterpreter.
var _ app_python.PythonInterpreter = (*mockPython3SInterp)(nil)

// ---------------------------------------------------------------------------
// Package-level API (default singleton with double-checked locking)
// ---------------------------------------------------------------------------

var (
	defaultModule *AppPython3SModule
	defaultMu     sync.RWMutex
)

// DefaultAppPython3S returns the package-level default AppPython3SModule,
// creating it on first use.
func DefaultAppPython3S() *AppPython3SModule {
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
func LoadScript(path string) error { return DefaultAppPython3S().LoadScript(path) }

// CallFunction invokes a function on the default module.
func CallFunction(name string, msg *parser.SIPMsg, args ...string) (int, error) {
	return DefaultAppPython3S().CallFunction(name, msg, args...)
}

// RegisterMsg registers a message on the default module.
func RegisterMsg(msg *parser.SIPMsg) uint64 { return DefaultAppPython3S().RegisterMsg(msg) }

// GetMsg retrieves a message by handle on the default module.
func GetMsg(handle uint64) *parser.SIPMsg { return DefaultAppPython3S().GetMsg(handle) }

// ReleaseMsg releases a message handle on the default module.
func ReleaseMsg(handle uint64) { DefaultAppPython3S().ReleaseMsg(handle) }

// EvalExpr evaluates an expression on the default module.
func EvalExpr(expr string, msg *parser.SIPMsg) (interface{}, error) {
	return DefaultAppPython3S().EvalExpr(expr, msg)
}
