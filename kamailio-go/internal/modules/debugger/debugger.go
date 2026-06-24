// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Debugger module - script execution tracing and breakpoints.
 *
 * Port of the kamailio debugger module (src/modules/debugger). A
 * DebuggerModule records each executed script action as a DebugEntry
 * and maintains a set of breakpoints keyed by (route, line). When
 * enabled, LogAction appends to the in-memory log; when disabled it is
 * a no-op, mirroring the C module's _dbg_cfgtrace flag.
 *
 * Breakpoints mirror the C module's dbg_breakpoint() command: setting a
 * breakpoint on a (route, line) pair marks it so a future CheckBreakpoint
 * can report a hit.
 */
package debugger

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// DebugConfig holds the debugger configuration, mirroring the C
// module's modparam state (_dbg_cfgtrace, _dbg_cfgtrace_level, ...).
type DebugConfig struct {
	Enabled  bool
	LogLevel int
	LogMask  int
	StepMode bool
}

// DebugEntry is a single recorded script action.
type DebugEntry struct {
	Time   time.Time
	Action string
	Route  string
	Line   int
	Msg    string
}

// breakpointKey is the composite key for a breakpoint.
type breakpointKey struct {
	route string
	line  int
}

// DebuggerModule implements the debugger module. It is safe for
// concurrent use: the log slice and the breakpoint set are guarded by
// mu, and the enabled flag is atomic.
type DebuggerModule struct {
	mu          sync.RWMutex
	config      DebugConfig
	entries     []*DebugEntry
	breakpoints map[breakpointKey]bool
	enabled     atomic.Bool
}

// NewDebuggerModule creates a new DebuggerModule with tracing disabled
// by default (matching Kamailio, where cfgtrace is off until enabled).
func NewDebuggerModule() *DebuggerModule {
	m := &DebuggerModule{
		breakpoints: make(map[breakpointKey]bool),
	}
	return m
}

// Enable turns tracing on.
func (m *DebuggerModule) Enable() {
	m.enabled.Store(true)
}

// Disable turns tracing off.
func (m *DebuggerModule) Disable() {
	m.enabled.Store(false)
}

// IsEnabled reports whether tracing is currently enabled.
func (m *DebuggerModule) IsEnabled() bool {
	return m.enabled.Load()
}

// SetConfig updates the debugger configuration.
func (m *DebuggerModule) SetConfig(cfg DebugConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
	m.enabled.Store(cfg.Enabled)
}

// GetConfig returns a copy of the current configuration.
func (m *DebuggerModule) GetConfig() DebugConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// LogAction records a script action. When tracing is disabled the call
// is a no-op, mirroring the C module's trace gating.
func (m *DebuggerModule) LogAction(action string, route string, line int, msg string) {
	if !m.enabled.Load() {
		return
	}
	entry := &DebugEntry{
		Time:   time.Now(),
		Action: action,
		Route:  route,
		Line:   line,
		Msg:    msg,
	}
	m.mu.Lock()
	m.entries = append(m.entries, entry)
	m.mu.Unlock()
}

// GetEntries returns a copy of all log entries in the order they were
// recorded.
func (m *DebuggerModule) GetEntries() []*DebugEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*DebugEntry, len(m.entries))
	copy(out, m.entries)
	return out
}

// ClearLog removes all log entries.
func (m *DebuggerModule) ClearLog() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = nil
}

// SetBreakpoint adds a breakpoint at the given (route, line) pair.
// Adding the same breakpoint twice is idempotent.
func (m *DebuggerModule) SetBreakpoint(route string, line int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.breakpoints[breakpointKey{route: route, line: line}] = true
}

// RemoveBreakpoint removes the breakpoint at the given (route, line)
// pair. Returns true if a breakpoint was removed.
func (m *DebuggerModule) RemoveBreakpoint(route string, line int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := breakpointKey{route: route, line: line}
	if _, ok := m.breakpoints[key]; !ok {
		return false
	}
	delete(m.breakpoints, key)
	return true
}

// HasBreakpoint reports whether a breakpoint is set at the given
// (route, line) pair.
func (m *DebuggerModule) HasBreakpoint(route string, line int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.breakpoints[breakpointKey{route: route, line: line}]
}

// ListBreakpoints returns all breakpoints as "route:line" strings. The
// order is unspecified.
func (m *DebuggerModule) ListBreakpoints() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.breakpoints))
	for k := range m.breakpoints {
		out = append(out, fmt.Sprintf("%s:%d", k.route, k.line))
	}
	return out
}

// Count returns the number of log entries.
func (m *DebuggerModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// BreakpointCount returns the number of breakpoints currently set.
func (m *DebuggerModule) BreakpointCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.breakpoints)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultDebugger  *DebuggerModule
)

// DefaultDebugger returns the process-wide DebuggerModule, creating
// one on first use.
func DefaultDebugger() *DebuggerModule {
	defaultMu.RLock()
	m := defaultDebugger
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDebugger == nil {
		defaultDebugger = NewDebuggerModule()
	}
	return defaultDebugger
}

// Init (re)initialises the process-wide DebuggerModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDebugger = NewDebuggerModule()
}
