// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Misctest module - registry of self-test functions.
 * Port of the kamailio misctest module (src/modules/misctest).
 *
 * misctest lets the script register named test functions and run them on
 * demand. RunTest returns the boolean result of the named test; an
 * unknown name is an error.
 *
 * The module is safe for concurrent use.
 */

package misctest

import (
	"errors"
	"sort"
	"sync"
)

// TestFunc is a self-test function.
type TestFunc func() bool

// MisctestModule holds a registry of named self-tests.
type MisctestModule struct {
	mu    sync.RWMutex
	tests map[string]TestFunc
}

// New creates an empty MisctestModule.
func New() *MisctestModule {
	return &MisctestModule{tests: make(map[string]TestFunc)}
}

// RegisterTest adds (or replaces) a named test function. A nil function
// is ignored.
func (m *MisctestModule) RegisterTest(name string, fn func() bool) {
	if name == "" || fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tests[name] = fn
}

// RunTest executes the named test and returns its result. An unknown name
// yields an error.
func (m *MisctestModule) RunTest(name string) (bool, error) {
	m.mu.RLock()
	fn, ok := m.tests[name]
	m.mu.RUnlock()
	if !ok {
		return false, errors.New("misctest: unknown test " + name)
	}
	return fn(), nil
}

// ListTests returns the sorted names of all registered tests.
func (m *MisctestModule) ListTests() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.tests))
	for name := range m.tests {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Count returns the number of registered tests.
func (m *MisctestModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tests)
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *MisctestModule
)

// DefaultMisctest returns the process-wide module, creating it on first use.
func DefaultMisctest() *MisctestModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide module to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// RegisterTest is the package-level wrapper.
func RegisterTest(name string, fn func() bool) { DefaultMisctest().RegisterTest(name, fn) }

// RunTest is the package-level wrapper.
func RunTest(name string) (bool, error) { return DefaultMisctest().RunTest(name) }

// ListTests is the package-level wrapper.
func ListTests() []string { return DefaultMisctest().ListTests() }
