// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Module parameter types and fixup mechanism - matching Kamailio core
 * modparam.c.
 *
 * Defines parameter types (int, str, bool, func) and a fixup registry
 * that allows parameters to be transformed from their raw configuration
 * representation to their runtime form at load time. The fixup mechanism
 * is the Go counterpart of C's fixup_function / fix_param() system.
 */

package srmodule

import (
	"fmt"
	"sync"
)

// ParamType identifies the type of a module parameter. It mirrors the
// C PARAM_INT / PARAM_STR / PARAM_STRING / PARAM_FUNC constants.
type ParamType int

const (
	// ParamInt indicates an integer parameter.
	ParamInt ParamType = 1
	// ParamStr indicates a string parameter (counted-length, like C's str).
	ParamStr ParamType = 2
	// ParamBool indicates a boolean parameter.
	ParamBool ParamType = 3
	// ParamFunc indicates a function-type parameter (the value is
	// processed by a registered fixup at load time).
	ParamFunc ParamType = 4
)

// String returns a human-readable name for the parameter type.
func (t ParamType) String() string {
	switch t {
	case ParamInt:
		return "int"
	case ParamStr:
		return "str"
	case ParamBool:
		return "bool"
	case ParamFunc:
		return "func"
	default:
		return fmt.Sprintf("unknown(%d)", int(t))
	}
}

// FixupFunc transforms a raw parameter value into its runtime form.
// It is the Go counterpart of C's fixup_function. The input is typically
// a string read from the configuration file; the output is the compiled
// or parsed representation (e.g. *regexp.Regexp, int, bool).
type FixupFunc func(param interface{}) (interface{}, error)

// FixupRegistry is a thread-safe registry of named fixup functions.
// It is the Go counterpart of the fixup lookup table maintained
// implicitly by C's get_fixup_free() / mod_fix_get_fixup_free().
type FixupRegistry struct {
	mu     sync.RWMutex
	fixups map[string]FixupFunc
}

// NewFixupRegistry creates an empty FixupRegistry.
func NewFixupRegistry() *FixupRegistry {
	return &FixupRegistry{
		fixups: make(map[string]FixupFunc),
	}
}

// RegisterFixup registers a fixup function under the given name. If a
// fixup with the same name already exists it is replaced.
func (fr *FixupRegistry) RegisterFixup(name string, fn FixupFunc) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.fixups[name] = fn
}

// FindFixup returns the fixup function registered under name, or nil
// if none exists.
func (fr *FixupRegistry) FindFixup(name string) FixupFunc {
	fr.mu.RLock()
	defer fr.mu.RUnlock()
	return fr.fixups[name]
}

// ApplyFixup applies the fixup referenced by param.FixupName to
// param.Value and stores the result in param.FixedValue. If the
// parameter has no FixupName this is a no-op. It mirrors the fixup
// application that happens during C's fixup_*() calls at config parse
// time.
func (fr *FixupRegistry) ApplyFixup(param *ParamExport) error {
	if param.FixupName == "" {
		return nil
	}
	fn := fr.FindFixup(param.FixupName)
	if fn == nil {
		return fmt.Errorf("fixup %q not found", param.FixupName)
	}
	fixed, err := fn(param.Value)
	if err != nil {
		return fmt.Errorf("fixup %q on parameter %q: %w",
			param.FixupName, param.Name, err)
	}
	param.FixedValue = fixed
	return nil
}

// ApplyAllFixups applies fixups to all parameters in the given exports.
// It returns the first error encountered.
func (fr *FixupRegistry) ApplyAllFixups(exports *ModuleExports) error {
	if exports == nil {
		return nil
	}
	for i := range exports.Params {
		if err := fr.ApplyFixup(&exports.Params[i]); err != nil {
			return err
		}
	}
	return nil
}

// Count returns the number of registered fixup functions.
func (fr *FixupRegistry) Count() int {
	fr.mu.RLock()
	defer fr.mu.RUnlock()
	return len(fr.fixups)
}

// ---------------------------------------------------------------------------
// Default fixup registry singleton and package-level helpers
// ---------------------------------------------------------------------------

var (
	defaultFixupRegistry *FixupRegistry
	defaultFixupMu       sync.RWMutex
)

// DefaultFixupRegistry returns the process-wide FixupRegistry, creating
// it and registering the built-in fixups on first use.
func DefaultFixupRegistry() *FixupRegistry {
	defaultFixupMu.RLock()
	fr := defaultFixupRegistry
	defaultFixupMu.RUnlock()
	if fr != nil {
		return fr
	}
	defaultFixupMu.Lock()
	defer defaultFixupMu.Unlock()
	if defaultFixupRegistry == nil {
		defaultFixupRegistry = NewFixupRegistry()
		registerDefaults(defaultFixupRegistry)
	}
	return defaultFixupRegistry
}

// resetFixupRegistry replaces the default fixup registry with a fresh
// one and re-registers the built-in fixups. Called by Init().
func resetFixupRegistry() {
	defaultFixupMu.Lock()
	defaultFixupRegistry = NewFixupRegistry()
	registerDefaults(defaultFixupRegistry)
	defaultFixupMu.Unlock()
}

// Package-level helper functions that delegate to DefaultFixupRegistry().

// RegisterFixup registers a fixup function on the default registry.
func RegisterFixup(name string, fn FixupFunc) {
	DefaultFixupRegistry().RegisterFixup(name, fn)
}

// FindFixup looks up a fixup function by name on the default registry.
func FindFixup(name string) FixupFunc {
	return DefaultFixupRegistry().FindFixup(name)
}

// ApplyFixup applies a fixup to a parameter on the default registry.
func ApplyFixup(param *ParamExport) error {
	return DefaultFixupRegistry().ApplyFixup(param)
}
