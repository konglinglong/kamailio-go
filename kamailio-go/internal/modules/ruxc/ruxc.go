// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * RuxC module - Rust-style extension evaluation.
 * Port of the kamailio ruxc module (src/modules/ruxc).
 *
 * ruxc compiles a pattern (regular expression) once and evaluates
 * expressions against it. Eval returns the first match of the compiled
 * pattern in expr; Compile stores a pattern; IsCompiled reports whether a
 * pattern is active.
 *
 * The module is safe for concurrent use.
 */

package ruxc

import (
	"errors"
	"regexp"
	"sync"
)

// RuxCModule compiles and evaluates regular expressions.
type RuxCModule struct {
	mu      sync.RWMutex
	pattern *regexp.Regexp
	source  string
}

// New creates a RuxCModule with no compiled pattern.
func New() *RuxCModule {
	return &RuxCModule{}
}

// Compile compiles pattern and stores it. Returns an error when pattern
// is empty or invalid.
func (m *RuxCModule) Compile(pattern string) error {
	if pattern == "" {
		return errors.New("ruxc: empty pattern")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pattern = re
	m.source = pattern
	return nil
}

// IsCompiled reports whether a pattern is currently compiled.
func (m *RuxCModule) IsCompiled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pattern != nil
}

// Eval evaluates expr against the compiled pattern and returns the first
// match (or "" when there is none). Returns an error when no pattern is
// compiled or expr is empty.
func (m *RuxCModule) Eval(expr string) (string, error) {
	if expr == "" {
		return "", errors.New("ruxc: empty expression")
	}
	m.mu.RLock()
	re := m.pattern
	m.mu.RUnlock()
	if re == nil {
		return "", errors.New("ruxc: no pattern compiled")
	}
	return re.FindString(expr), nil
}

// Pattern returns the source of the currently compiled pattern.
func (m *RuxCModule) Pattern() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.source
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *RuxCModule
)

// DefaultRuxC returns the process-wide module, creating it on first use.
func DefaultRuxC() *RuxCModule {
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

// Compile is the package-level wrapper.
func Compile(pattern string) error { return DefaultRuxC().Compile(pattern) }

// Eval is the package-level wrapper.
func Eval(expr string) (string, error) { return DefaultRuxC().Eval(expr) }

// IsCompiled is the package-level wrapper.
func IsCompiled() bool { return DefaultRuxC().IsCompiled() }
