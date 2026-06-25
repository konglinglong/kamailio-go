// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * KEMI (Kamailio Embedded Interface) framework - matching Kamailio core
 * kemi.c.
 *
 * Provides a thread-safe registry of exported functions that can be
 * invoked by the script engine and by embedded scripting languages
 * (Lua, Python, Ruby, JavaScript). Each function is described by a
 * KemiExport (name, parameter types, return type) and grouped into
 * KemiModule entries. The Engine indexes functions for fast lookup
 * by both bare name ("sl_send_reply") and qualified name
 * ("sl.sl_send_reply").
 *
 * Go counterpart of C's sr_kemi_t, sr_kemi_module_t, sr_kemi_lookup()
 * and sr_kemi_modules_add().
 */

package kemi

import (
	"fmt"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Return codes matching Kamailio's SR_KEMI_TRUE / SR_KEMI_FALSE and the
// srmodule.RetError / RetOK conventions.
const (
	// KemiTrue is the KEMI boolean true / success return value.
	KemiTrue = 1
	// KemiFalse is the KEMI boolean false return value.
	KemiFalse = -1
	// KemiDrop signals that the message should be dropped.
	KemiDrop = 0
)

// VarParams indicates that a function accepts a variable number of
// parameters. It mirrors C's VAR_PARAM_NO and srmodule.VarParams.
const VarParams = -1

// ParamType identifies the type of a KEMI function parameter. It mirrors
// C's SR_KEMIP_* constants.
type ParamType int

const (
	// ParamString expects a string parameter.
	ParamString ParamType = iota
	// ParamInt expects an integer parameter.
	ParamInt
	// ParamBool expects a boolean parameter.
	ParamBool
	// ParamPVar expects a pseudo-variable name parameter.
	ParamPVar
	// ParamAny accepts any parameter type.
	ParamAny
)

// String returns a human-readable name for the parameter type.
func (t ParamType) String() string {
	switch t {
	case ParamString:
		return "string"
	case ParamInt:
		return "int"
	case ParamBool:
		return "bool"
	case ParamPVar:
		return "pvar"
	case ParamAny:
		return "any"
	default:
		return fmt.Sprintf("unknown(%d)", int(t))
	}
}

// ReturnType identifies the return type of a KEMI function. It mirrors
// C's rtype field of sr_kemi_t.
type ReturnType int

const (
	// RetInt indicates the function returns an integer.
	RetInt ReturnType = iota
	// RetStr indicates the function returns a string.
	RetStr
	// RetBool indicates the function returns a boolean.
	RetBool
	// RetNone indicates the function has no meaningful return value.
	RetNone
)

// String returns a human-readable name for the return type.
func (r ReturnType) String() string {
	switch r {
	case RetInt:
		return "int"
	case RetStr:
		return "str"
	case RetBool:
		return "bool"
	case RetNone:
		return "none"
	default:
		return fmt.Sprintf("unknown(%d)", int(r))
	}
}

// KemiFunc is the signature of a KEMI function. It mirrors C's
// sr_kemi_f: the message being processed plus a variable number of
// parameters. The return value follows Kamailio conventions:
// KemiTrue (1) on success, KemiFalse (-1) on failure, KemiDrop (0)
// to drop the message.
type KemiFunc func(msg *parser.SIPMsg, params ...interface{}) int

// KemiExport describes a single exported KEMI function. It is the Go
// counterpart of C's sr_kemi_t.
type KemiExport struct {
	// Name is the function name (e.g. "sl_send_reply").
	Name string
	// Func is the function implementation.
	Func KemiFunc
	// MinParams is the minimum number of parameters.
	MinParams int
	// MaxParams is the maximum number of parameters (VarParams = any).
	MaxParams int
	// ParamTypes describes the expected type of each parameter.
	ParamTypes []ParamType
	// ReturnType describes what the function returns.
	ReturnType ReturnType
	// Flags is a bitfield of route-type flags (see srmodule.CmdFlag*).
	Flags uint32
	// Doc is a short documentation string.
	Doc string
}

// KemiModule groups a set of exported KEMI functions under a module
// name. It is the Go counterpart of C's sr_kemi_module_t.
type KemiModule struct {
	Name  string
	Funcs []KemiExport
}

// Engine is a thread-safe registry of KEMI modules and functions. It
// is the Go counterpart of C's _sr_kemi_modules[] array plus the
// sr_kemi_lookup() function.
type Engine struct {
	mu      sync.RWMutex
	modules map[string]*KemiModule // module name -> module
	funcs   map[string]*KemiExport // lookup key -> export (both "mod.func" and "func")
	order   []string               // module registration order
}

// New creates an empty KEMI Engine.
func New() *Engine {
	return &Engine{
		modules: make(map[string]*KemiModule),
		funcs:   make(map[string]*KemiExport),
	}
}

// RegisterModule registers a module and all its functions. It mirrors
// C's sr_kemi_modules_add(). Registering a module with a duplicate or
// empty name returns an error. Functions are indexed under both their
// qualified name ("module.func") and bare name ("func"); the first
// registration of a bare name wins.
func (e *Engine) RegisterModule(mod *KemiModule) error {
	if mod == nil {
		return fmt.Errorf("nil module")
	}
	if mod.Name == "" {
		return fmt.Errorf("module has empty name")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.modules[mod.Name]; exists {
		return fmt.Errorf("module %q already registered", mod.Name)
	}

	// Clone the module so external mutation does not affect the
	// engine's internal state after registration.
	clone := &KemiModule{
		Name:  mod.Name,
		Funcs: make([]KemiExport, len(mod.Funcs)),
	}
	copy(clone.Funcs, mod.Funcs)

	e.modules[mod.Name] = clone
	e.order = append(e.order, mod.Name)

	for i := range clone.Funcs {
		fn := &clone.Funcs[i]
		if fn.Name == "" {
			return fmt.Errorf("module %q has a function with empty name", mod.Name)
		}
		// Qualified name always wins (module-scoped lookup).
		qualified := mod.Name + "." + fn.Name
		e.funcs[qualified] = fn
		// Bare name: first registered wins (matches C find_export).
		if _, exists := e.funcs[fn.Name]; !exists {
			e.funcs[fn.Name] = fn
		}
	}
	return nil
}

// RegisterFunc registers a single function under the given module
// name. If the module does not exist it is created. This is useful for
// incremental registration without building a full KemiModule.
func (e *Engine) RegisterFunc(modName string, exp *KemiExport) error {
	if modName == "" {
		return fmt.Errorf("empty module name")
	}
	if exp == nil || exp.Name == "" {
		return fmt.Errorf("invalid function export")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	mod, exists := e.modules[modName]
	if !exists {
		mod = &KemiModule{Name: modName}
		e.modules[modName] = mod
		e.order = append(e.order, modName)
	}
	mod.Funcs = append(mod.Funcs, *exp)

	fn := &mod.Funcs[len(mod.Funcs)-1]
	qualified := modName + "." + exp.Name
	e.funcs[qualified] = fn
	if _, exists := e.funcs[exp.Name]; !exists {
		e.funcs[exp.Name] = fn
	}
	return nil
}

// FindFunc looks up a function by name. The name may be a bare
// function name ("sl_send_reply") or a qualified name
// ("sl.sl_send_reply"). Returns nil if not found.
func (e *Engine) FindFunc(name string) *KemiExport {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.funcs[name]
}

// FindFuncByModule looks up a function within a specific module. It
// mirrors C's sr_kemi_lookup() with a non-empty mname. Returns nil if
// the module or function is not found.
func (e *Engine) FindFuncByModule(modName, funcName string) *KemiExport {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.funcs[modName+"."+funcName]
}

// Call finds and invokes the function named name. The parameter count
// is validated against the export's MinParams/MaxParams before
// invocation. Returns the function's return code and nil error on
// success, or KemiFalse and an error if the function is not found or
// the parameters are invalid.
func (e *Engine) Call(name string, msg *parser.SIPMsg, params ...interface{}) (int, error) {
	e.mu.RLock()
	exp, ok := e.funcs[name]
	e.mu.RUnlock()
	if !ok || exp == nil {
		return KemiFalse, fmt.Errorf("kemi function %q not found", name)
	}
	if err := e.ValidateParams(exp, params); err != nil {
		return KemiFalse, err
	}
	return exp.Func(msg, params...), nil
}

// CallByModule finds and invokes a function within a specific module.
func (e *Engine) CallByModule(modName, funcName string, msg *parser.SIPMsg, params ...interface{}) (int, error) {
	e.mu.RLock()
	exp, ok := e.funcs[modName+"."+funcName]
	e.mu.RUnlock()
	if !ok || exp == nil {
		return KemiFalse, fmt.Errorf("kemi function %q.%q not found", modName, funcName)
	}
	if err := e.ValidateParams(exp, params); err != nil {
		return KemiFalse, err
	}
	return exp.Func(msg, params...), nil
}

// ListFunctions returns a snapshot of all registered functions across
// all modules, in registration order.
func (e *Engine) ListFunctions() []KemiExport {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]KemiExport, 0)
	for _, name := range e.order {
		mod := e.modules[name]
		if mod == nil {
			continue
		}
		out = append(out, mod.Funcs...)
	}
	return out
}

// ListModuleFunctions returns a snapshot of the functions registered
// under the named module. Returns nil if the module is not found.
func (e *Engine) ListModuleFunctions(modName string) []KemiExport {
	e.mu.RLock()
	defer e.mu.RUnlock()
	mod, ok := e.modules[modName]
	if !ok {
		return nil
	}
	out := make([]KemiExport, len(mod.Funcs))
	copy(out, mod.Funcs)
	return out
}

// HasModule reports whether a module with the given name is registered.
func (e *Engine) HasModule(modName string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.modules[modName]
	return ok
}

// ModuleCount returns the number of registered modules.
func (e *Engine) ModuleCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.modules)
}

// FuncCount returns the total number of registered functions (counting
// each function once, not double-counting the bare/qualified keys).
func (e *Engine) FuncCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	total := 0
	for _, name := range e.order {
		total += len(e.modules[name].Funcs)
	}
	return total
}

// qualifiedKey builds the canonical "module.function" lookup key.
func qualifiedKey(modName, funcName string) string {
	return modName + "." + funcName
}

// isQualified reports whether name contains a module separator.
func isQualified(name string) bool {
	return strings.Contains(name, ".")
}

// ---------------------------------------------------------------------------
// Default engine singleton and package-level helpers
// ---------------------------------------------------------------------------

var (
	defaultEngine *Engine
	defaultMu     sync.RWMutex
)

// DefaultEngine returns the process-wide Engine, creating it on first
// use.
func DefaultEngine() *Engine {
	defaultMu.RLock()
	e := defaultEngine
	defaultMu.RUnlock()
	if e != nil {
		return e
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultEngine == nil {
		defaultEngine = New()
	}
	return defaultEngine
}

// Init (re)initialises the process-wide Engine to an empty state,
// mirroring Kamailio's reset semantics. It is safe to call multiple
// times.
func Init() {
	defaultMu.Lock()
	defaultEngine = New()
	defaultMu.Unlock()
}

// Package-level helper functions that delegate to DefaultEngine().

// RegisterModule registers a module on the default engine.
func RegisterModule(mod *KemiModule) error { return DefaultEngine().RegisterModule(mod) }

// RegisterFunc registers a single function on the default engine.
func RegisterFunc(modName string, exp *KemiExport) error {
	return DefaultEngine().RegisterFunc(modName, exp)
}

// FindFunc looks up a function by name on the default engine.
func FindFunc(name string) *KemiExport { return DefaultEngine().FindFunc(name) }

// FindFuncByModule looks up a function by module on the default engine.
func FindFuncByModule(modName, funcName string) *KemiExport {
	return DefaultEngine().FindFuncByModule(modName, funcName)
}

// Call finds and invokes a function on the default engine.
func Call(name string, msg *parser.SIPMsg, params ...interface{}) (int, error) {
	return DefaultEngine().Call(name, msg, params...)
}

// CallByModule finds and invokes a function by module on the default engine.
func CallByModule(modName, funcName string, msg *parser.SIPMsg, params ...interface{}) (int, error) {
	return DefaultEngine().CallByModule(modName, funcName, msg, params...)
}

// ListFunctions lists all functions on the default engine.
func ListFunctions() []KemiExport { return DefaultEngine().ListFunctions() }

// ListModuleFunctions lists a module's functions on the default engine.
func ListModuleFunctions(modName string) []KemiExport {
	return DefaultEngine().ListModuleFunctions(modName)
}
