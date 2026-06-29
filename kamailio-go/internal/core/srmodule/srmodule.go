// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Module registration framework - matching Kamailio core sr_module.c.
 *
 * Provides a thread-safe registry of modules and their exports (commands,
 * parameters, pseudo-variable items, route callbacks and process callbacks).
 * Modules implement the ModuleInterface and are registered via Register().
 * The registry indexes exports for fast lookup at runtime.
 *
 * Go counterpart of C's sr_module.c: register_module(), ksr_load_module(),
 * find_export_record(), find_param_export(), init_modules(), destroy_modules().
 */

package srmodule

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Command return codes matching Kamailio C conventions.
const (
	RetError = -1 // command failed
	RetDrop  = 0  // drop the message / stop processing
	RetOK    = 1  // command succeeded
)

// VarParams indicates a command accepts a variable number of parameters.
// It is the Go counterpart of C's VAR_PARAM_NO.
const VarParams = -1

// Command route-type flags matching Kamailio's *_ROUTE defines. A command
// declares which route types it is allowed to run in via these flags.
const (
	CmdFlagRequestRoute uint32 = 1 << 0
	CmdFlagReplyRoute   uint32 = 1 << 1
	CmdFlagFailureRoute uint32 = 1 << 2
	CmdFlagBranchRoute  uint32 = 1 << 3
	CmdFlagOnsendRoute  uint32 = 1 << 4
	CmdFlagEventRoute   uint32 = 1 << 5
)

// ModuleInterface defines the interface every Kamailio-Go module must
// implement. It is the Go counterpart of the C module_exports_t structure
// plus the init_mod_f / destroy_mod_f function pointers.
type ModuleInterface interface {
	// Name returns the unique module name (e.g. "tm", "sl").
	Name() string
	// Version returns the module version string.
	Version() string
	// Init performs module initialisation. Called by InitAll or LoadModule.
	Init() error
	// Destroy releases module resources. Called by DestroyAll.
	Destroy() error
	// Exports returns the module's exported commands, parameters and items.
	Exports() *ModuleExports
}

// ModuleExports holds everything a module exports: commands, parameters,
// pseudo-variable items, route callbacks and process callbacks. It is the
// Go counterpart of C's module_exports_t.
type ModuleExports struct {
	Cmds   []CmdExport     // exported command functions
	Params []ParamExport   // exported parameters
	Items  []ItemExport    // exported pseudo-variable items
	Routes []RouteExport   // exported route callbacks
	Procs  []ProcExport    // exported processes
	OnLoad func() error    // post-registration, pre-init callback
}

// CmdExport describes a single exported command function. It is the Go
// counterpart of C's ksr_cmd_export_t.
type CmdExport struct {
	Name      string   // command name
	Function  CmdFunc  // command implementation
	MinParams int      // minimum number of parameters
	MaxParams int      // maximum number of parameters (VarParams = any)
	Flags     uint32   // route-type flags
}

// CmdFunc is the signature of an exported command. It mirrors C's
// cmd_function: int (*cmd_function)(sip_msg_t* msg, ...).
// The return value follows Kamailio conventions: RetOK (1) on success,
// RetError (-1) on failure, RetDrop (0) to drop the message.
type CmdFunc func(msg *parser.SIPMsg, params []interface{}) int

// ParamExport describes a single module parameter. It is the Go
// counterpart of C's param_export_t. The Value field should hold a
// pointer to the module's configuration variable so that SetParam can
// update it in place via reflection.
type ParamExport struct {
	Name       string      // parameter name
	Type       ParamType   // parameter type
	Value      interface{} // pointer to the config variable (e.g. *int)
	FixupName  string      // optional name of a registered fixup
	FixedValue interface{} // populated by ApplyFixup
}

// ItemExport describes a pseudo-variable exported by a module.
type ItemExport struct {
	Name    string       // pseudo-variable name (e.g. "$my_var")
	GetFunc ItemGetFunc  // value getter
	SetFunc ItemSetFunc  // value setter (may be nil for read-only)
}

// ItemGetFunc retrieves a pseudo-variable value for the given message.
type ItemGetFunc func(msg *parser.SIPMsg) (interface{}, error)

// ItemSetFunc sets a pseudo-variable value for the given message.
type ItemSetFunc func(msg *parser.SIPMsg, value interface{}) error

// RouteExport describes a route callback exported by a module.
type RouteExport struct {
	Name     string         // route name
	Priority int            // higher priority executes first
	Callback RouteCallback  // callback function
}

// RouteCallback is the signature of a route callback. It mirrors C's
// route_function: int (*route_function)(sip_msg_t* msg).
type RouteCallback func(msg *parser.SIPMsg) int

// ProcExport describes a process exported by a module.
type ProcExport struct {
	Name     string        // process name
	Callback ProcCallback  // process entry function
	No       int           // number of process instances
}

// ProcCallback is the signature of a process callback. It mirrors C's
// proc_function: int (*proc_function)(int rank).
type ProcCallback func(rank int) int

// ModuleInfo is a read-only snapshot of a registered module, returned
// by ListModules().
type ModuleInfo struct {
	Name       string
	Version    string
	CmdCount   int
	ParamCount int
	ItemCount  int
}

// Registry is a thread-safe registry of modules and their exports. It
// is the Go counterpart of C's _ksr_modules_list plus the lookup tables
// built by register_module().
type Registry struct {
	mu          sync.RWMutex
	modules     map[string]ModuleInterface          // name -> module
	exports     map[string]*ModuleExports           // name -> cached exports
	order       []string                            // registration order
	cmds        map[string]*CmdExport              // cmd name -> export (first wins)
	params      map[string]*ParamExport             // param name -> export (first wins)
	items       map[string]*ItemExport             // item name -> export (first wins)
	initialized map[string]bool                     // name -> initialised?
}

// NewRegistry creates an empty module Registry.
func NewRegistry() *Registry {
	return &Registry{
		modules:     make(map[string]ModuleInterface),
		exports:     make(map[string]*ModuleExports),
		cmds:        make(map[string]*CmdExport),
		params:      make(map[string]*ParamExport),
		items:       make(map[string]*ItemExport),
		initialized: make(map[string]bool),
	}
}

// Register adds mod to the registry and indexes its exported commands,
// parameters and pseudo-variable items. It mirrors C's register_module().
// Registering a module with a duplicate or empty name returns an error.
// If the module has an OnLoad callback it is invoked after successful
// indexing; failure causes the module to be removed from the registry.
func (r *Registry) Register(mod ModuleInterface) error {
	name := mod.Name()
	if name == "" {
		return fmt.Errorf("module has empty name")
	}

	r.mu.Lock()
	if _, exists := r.modules[name]; exists {
		r.mu.Unlock()
		return fmt.Errorf("module %q already registered", name)
	}

	exports := mod.Exports()
	if exports == nil {
		exports = &ModuleExports{}
	}

	r.modules[name] = mod
	r.exports[name] = exports
	r.order = append(r.order, name)

	// Index commands (first registered wins, matching C find_export)
	for i := range exports.Cmds {
		cmd := &exports.Cmds[i]
		if _, exists := r.cmds[cmd.Name]; !exists {
			r.cmds[cmd.Name] = cmd
		}
	}

	// Index parameters (first registered wins for global lookup)
	for i := range exports.Params {
		param := &exports.Params[i]
		if _, exists := r.params[param.Name]; !exists {
			r.params[param.Name] = param
		}
	}

	// Index pseudo-variable items (first registered wins)
	for i := range exports.Items {
		item := &exports.Items[i]
		if _, exists := r.items[item.Name]; !exists {
			r.items[item.Name] = item
		}
	}
	r.mu.Unlock()

	// Call OnLoad outside the lock to avoid potential deadlocks if
	// the callback tries to interact with the registry.
	if exports.OnLoad != nil {
		if err := exports.OnLoad(); err != nil {
			// Roll back registration on OnLoad failure.
			r.mu.Lock()
			r.removeModuleLocked(name)
			r.mu.Unlock()
			return fmt.Errorf("module %q OnLoad: %w", name, err)
		}
	}

	return nil
}

// removeModuleLocked removes a module and rebuilds the export indexes.
// The caller must hold r.mu.
func (r *Registry) removeModuleLocked(name string) {
	delete(r.modules, name)
	delete(r.exports, name)
	delete(r.initialized, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	// Rebuild flat lookup maps from remaining modules.
	r.cmds = make(map[string]*CmdExport)
	r.params = make(map[string]*ParamExport)
	r.items = make(map[string]*ItemExport)
	for _, n := range r.order {
		exports := r.exports[n]
		if exports == nil {
			continue
		}
		for i := range exports.Cmds {
			cmd := &exports.Cmds[i]
			if _, ok := r.cmds[cmd.Name]; !ok {
				r.cmds[cmd.Name] = cmd
			}
		}
		for i := range exports.Params {
			param := &exports.Params[i]
			if _, ok := r.params[param.Name]; !ok {
				r.params[param.Name] = param
			}
		}
		for i := range exports.Items {
			item := &exports.Items[i]
			if _, ok := r.items[item.Name]; !ok {
				r.items[item.Name] = item
			}
		}
	}
}

// LoadModule finds the module named name and initialises it. It is the
// Go counterpart of C's ksr_load_module (without the dlopen part, since
// Go modules are compiled in). If the module is already initialised this
// is a no-op. Returns an error if the module is not registered or if
// initialisation fails.
func (r *Registry) LoadModule(name string) error {
	r.mu.RLock()
	_, ok := r.modules[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("module %q not found", name)
	}
	return r.initModule(name)
}

// initModule initialises a single module if it has not been initialised
// yet. The lock is released before calling OnLoad/Init to avoid deadlocks.
func (r *Registry) initModule(name string) (err error) {
	r.mu.Lock()
	if r.initialized[name] {
		r.mu.Unlock()
		return nil
	}
	r.initialized[name] = true
	mod := r.modules[name]
	exports := r.exports[name]
	r.mu.Unlock()

	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("module %q init panic: %v", name, rec)
		}
	}()

	if exports != nil && exports.OnLoad != nil {
		if e := exports.OnLoad(); e != nil {
			return fmt.Errorf("module %q OnLoad: %w", name, e)
		}
	}
	if mod == nil {
		return fmt.Errorf("module %q has nil interface", name)
	}
	if e := mod.Init(); e != nil {
		return fmt.Errorf("module %q init: %w", name, e)
	}
	return nil
}

// FindModule returns the module registered under name, or nil.
func (r *Registry) FindModule(name string) ModuleInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modules[name]
}

// HasModule reports whether a module with the given name is registered.
func (r *Registry) HasModule(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.modules[name]
	return ok
}

// FindCmd looks up a command by name across all registered modules.
// It mirrors C's find_export_record(). Returns nil if not found.
func (r *Registry) FindCmd(name string) *CmdExport {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cmds[name]
}

// FindModuleCmd looks up a command by name within a specific module.
// It mirrors C's find_mod_export_record(). Returns nil if not found.
func (r *Registry) FindModuleCmd(moduleName, cmdName string) *CmdExport {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exports, ok := r.exports[moduleName]
	if !ok {
		return nil
	}
	for i := range exports.Cmds {
		if exports.Cmds[i].Name == cmdName {
			return &exports.Cmds[i]
		}
	}
	return nil
}

// FindParam looks up a parameter by name across all registered modules.
// It mirrors C's find_param_export() with a wildcard module. Returns nil
// if not found.
func (r *Registry) FindParam(name string) *ParamExport {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.params[name]
}

// FindModuleParam looks up a parameter by name within a specific module.
// Returns nil if not found.
func (r *Registry) FindModuleParam(moduleName, paramName string) *ParamExport {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exports, ok := r.exports[moduleName]
	if !ok {
		return nil
	}
	for i := range exports.Params {
		if exports.Params[i].Name == paramName {
			return &exports.Params[i]
		}
	}
	return nil
}

// FindItem looks up a pseudo-variable item by name. Returns nil if not
// found.
func (r *Registry) FindItem(name string) *ItemExport {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.items[name]
}

// CallCmd finds and invokes the command named name. It mirrors the
// Kamailio action executor calling a module function. The parameter
// count is validated against the command's MinParams/MaxParams before
// invocation. Returns the command's return code and nil error on
// success, or RetError and an error if the command is not found or
// the parameter count is invalid.
func (r *Registry) CallCmd(name string, msg *parser.SIPMsg, params []interface{}) (int, error) {
	r.mu.RLock()
	cmd, ok := r.cmds[name]
	r.mu.RUnlock()
	if !ok {
		return RetError, fmt.Errorf("command %q not found", name)
	}
	n := len(params)
	if n < cmd.MinParams {
		return RetError, fmt.Errorf("command %q requires at least %d params, got %d",
			name, cmd.MinParams, n)
	}
	if cmd.MaxParams != VarParams && n > cmd.MaxParams {
		return RetError, fmt.Errorf("command %q accepts at most %d params, got %d",
			name, cmd.MaxParams, n)
	}
	return cmd.Function(msg, params), nil
}

// SetParam sets the value of the parameter named name. It mirrors C's
// set_mod_param_regex(). The value is written through the pointer stored
// in the ParamExport.Value field using reflection, so the module's
// configuration variable is updated in place. String values are
// automatically converted to the target type (int, bool, etc.).
func (r *Registry) SetParam(name string, value interface{}) error {
	r.mu.RLock()
	param, ok := r.params[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("parameter %q not found", name)
	}
	return setParamValue(param, value)
}

// SetModuleParam sets the value of a parameter within a specific module.
// This is useful when multiple modules export parameters with the same
// name.
func (r *Registry) SetModuleParam(moduleName, paramName string, value interface{}) error {
	r.mu.RLock()
	param := r.FindModuleParam(moduleName, paramName)
	r.mu.RUnlock()
	if param == nil {
		return fmt.Errorf("parameter %q not found in module %q", paramName, moduleName)
	}
	return setParamValue(param, value)
}

// ListModules returns a snapshot of all registered modules in
// registration order.
func (r *Registry) ListModules() []ModuleInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ModuleInfo, 0, len(r.order))
	for _, name := range r.order {
		mod := r.modules[name]
		exports := r.exports[name]
		info := ModuleInfo{
			Name:    mod.Name(),
			Version: mod.Version(),
		}
		if exports != nil {
			info.CmdCount = len(exports.Cmds)
			info.ParamCount = len(exports.Params)
			info.ItemCount = len(exports.Items)
		}
		out = append(out, info)
	}
	return out
}

// Count returns the number of registered modules.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.modules)
}

// InitAll initialises all registered modules in registration order. It
// mirrors C's init_modules(). Modules that are already initialised are
// skipped. Initialisation stops on the first error.
func (r *Registry) InitAll() error {
	r.mu.RLock()
	order := make([]string, len(r.order))
	copy(order, r.order)
	r.mu.RUnlock()

	for _, name := range order {
		if err := r.initModule(name); err != nil {
			return err
		}
	}
	return nil
}

// DestroyAll destroys all registered modules in reverse registration
// order. It mirrors C's destroy_modules(). All modules are destroyed
// even if one fails; the first error is returned.
func (r *Registry) DestroyAll() error {
	r.mu.RLock()
	order := make([]string, len(r.order))
	copy(order, r.order)
	r.mu.RUnlock()

	var firstErr error
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		r.mu.Lock()
		mod := r.modules[name]
		initialized := r.initialized[name]
		r.initialized[name] = false
		r.mu.Unlock()

		if mod == nil || !initialized {
			continue
		}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("module %q destroy panic: %v",
							name, rec)
					}
				}
			}()
			if err := mod.Destroy(); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("module %q destroy: %w", name, err)
			}
		}()
	}
	return firstErr
}

// setParamValue writes value through the pointer stored in param.Value
// using reflection. If Value is not a pointer the value is replaced
// directly. String values are converted to the target type.
func setParamValue(param *ParamExport, value interface{}) error {
	if param.Value == nil {
		param.Value = value
		return nil
	}

	rv := reflect.ValueOf(param.Value)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		// Not a pointer - just replace the value.
		param.Value = value
		return nil
	}

	elem := rv.Elem()
	return setReflectValue(elem, value)
}

// setReflectValue sets elem (a reflect.Value pointing to a settable
// variable) to the given value, performing type conversions as needed.
func setReflectValue(elem reflect.Value, value interface{}) error {
	val := reflect.ValueOf(value)

	// Direct assignment if types are assignable.
	if val.Type().AssignableTo(elem.Type()) {
		elem.Set(val)
		return nil
	}

	// Convert string to int/uint/bool/string.
	if s, ok := value.(string); ok {
		switch elem.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16,
			reflect.Int32, reflect.Int64:
			n, err := strconv.ParseInt(s, 0, 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as int: %w", s, err)
			}
			elem.SetInt(n)
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16,
			reflect.Uint32, reflect.Uint64:
			n, err := strconv.ParseUint(s, 0, 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as uint: %w", s, err)
			}
			elem.SetUint(n)
			return nil
		case reflect.Bool:
			b, err := parseBool(s)
			if err != nil {
				return fmt.Errorf("cannot parse %q as bool: %w", s, err)
			}
			elem.SetBool(b)
			return nil
		case reflect.String:
			elem.SetString(s)
			return nil
		}
	}

	// Convert integer to bool (non-zero = true, matching Kamailio semantics).
	if elem.Kind() == reflect.Bool {
		iv := reflect.ValueOf(value)
		switch iv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16,
			reflect.Int32, reflect.Int64:
			elem.SetBool(iv.Int() != 0)
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16,
			reflect.Uint32, reflect.Uint64:
			elem.SetBool(iv.Uint() != 0)
			return nil
		}
	}

	// Try a convertible assignment.
	if val.Type().ConvertibleTo(elem.Type()) {
		elem.Set(val.Convert(elem.Type()))
		return nil
	}

	return fmt.Errorf("cannot assign %T to %s", value, elem.Type())
}

// parseBool accepts the same string values as Kamailio's boolean
// parameters: true/false, yes/no, on/off, 1/0 (case-insensitive).
func parseBool(s string) (bool, error) {
	switch s {
	case "1", "true", "True", "TRUE", "yes", "Yes", "YES",
		"on", "On", "ON":
		return true, nil
	case "0", "false", "False", "FALSE", "no", "No", "NO",
		"off", "Off", "OFF":
		return false, nil
	}
	return strconv.ParseBool(s)
}

// ---------------------------------------------------------------------------
// Default registry singleton and package-level helpers
// ---------------------------------------------------------------------------

var (
	defaultRegistry *Registry
	defaultMu       sync.RWMutex
)

// DefaultRegistry returns the process-wide Registry, creating it on
// first use.
func DefaultRegistry() *Registry {
	defaultMu.RLock()
	r := defaultRegistry
	defaultMu.RUnlock()
	if r != nil {
		return r
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRegistry == nil {
		defaultRegistry = NewRegistry()
	}
	return defaultRegistry
}

// Init (re)initialises the process-wide Registry and fixup registry to
// an empty state, mirroring Kamailio's init_modules() reset semantics.
// It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defaultRegistry = NewRegistry()
	defaultMu.Unlock()
	resetFixupRegistry()
}

// Package-level helper functions that delegate to DefaultRegistry().

// Register registers a module on the default registry.
func Register(mod ModuleInterface) error { return DefaultRegistry().Register(mod) }

// LoadModule loads (initialises) a named module on the default registry.
func LoadModule(name string) error { return DefaultRegistry().LoadModule(name) }

// FindModule looks up a module by name on the default registry.
func FindModule(name string) ModuleInterface { return DefaultRegistry().FindModule(name) }

// HasModule reports whether a module with the given name is registered
// on the default registry.
func HasModule(name string) bool { return DefaultRegistry().HasModule(name) }

// FindCmd looks up a command by name on the default registry.
func FindCmd(name string) *CmdExport { return DefaultRegistry().FindCmd(name) }

// FindParam looks up a parameter by name on the default registry.
func FindParam(name string) *ParamExport { return DefaultRegistry().FindParam(name) }

// FindItem looks up a pseudo-variable item by name on the default registry.
func FindItem(name string) *ItemExport { return DefaultRegistry().FindItem(name) }

// CallCmd finds and invokes a command on the default registry.
func CallCmd(name string, msg *parser.SIPMsg, params []interface{}) (int, error) {
	return DefaultRegistry().CallCmd(name, msg, params)
}

// SetParam sets a parameter value on the default registry.
func SetParam(name string, value interface{}) error {
	return DefaultRegistry().SetParam(name, value)
}

// ListModules returns all registered modules on the default registry.
func ListModules() []ModuleInfo { return DefaultRegistry().ListModules() }

// InitAll initialises all modules on the default registry.
func InitAll() error { return DefaultRegistry().InitAll() }

// DestroyAll destroys all modules on the default registry.
func DestroyAll() error { return DefaultRegistry().DestroyAll() }
