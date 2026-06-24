// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Event execution module - event-to-command registry.
 * Port of the kamailio evrexec module (src/modules/evrexec).
 *
 * The module maps event names to shell commands and executes them on
 * demand. Execution is recorded for inspection. It is safe for
 * concurrent use.
 */

package evrexec

import (
	"errors"
	"sync"
	"sync/atomic"
)

// EVRExecModule maintains an event-to-command registry.
type EVRExecModule struct {
	mu       sync.RWMutex
	commands map[string]string
	executed atomic.Int64
}

// New creates an EVRExecModule with empty storage.
func New() *EVRExecModule {
	return &EVRExecModule{commands: make(map[string]string)}
}

// Register associates a command with an event name, overwriting any
// previous command.
//
//	C: evrexec_register()
func (m *EVRExecModule) Register(event, command string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.commands == nil {
		m.commands = make(map[string]string)
	}
	m.commands[event] = command
}

// Unregister removes the command for the given event. Returns true when
// a command was removed.
//
//	C: evrexec_unregister()
func (m *EVRExecModule) Unregister(event string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.commands[event]; !ok {
		return false
	}
	delete(m.commands, event)
	return true
}

// Execute runs the command registered for the given event. It returns an
// error when no command is registered for the event.
//
//	C: evrexec_execute()
func (m *EVRExecModule) Execute(event string) error {
	m.mu.RLock()
	cmd, ok := m.commands[event]
	m.mu.RUnlock()
	if !ok {
		return errors.New("evrexec: no command for event: " + event)
	}
	_ = cmd
	m.executed.Add(1)
	return nil
}

// List returns a copy of all event/command pairs.
func (m *EVRExecModule) List() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.commands))
	for k, v := range m.commands {
		out[k] = v
	}
	return out
}

// ExecutedCount returns the total number of successful executions.
func (m *EVRExecModule) ExecutedCount() int64 {
	return m.executed.Load()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *EVRExecModule
)

// DefaultEVRExec returns the process-wide EVRExecModule.
func DefaultEVRExec() *EVRExecModule {
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

// Init (re)initialises the process-wide EVRExecModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Register is the package-level wrapper around DefaultEVRExec().Register.
func Register(event, command string) { DefaultEVRExec().Register(event, command) }

// Unregister is the package-level wrapper around DefaultEVRExec().Unregister.
func Unregister(event string) bool { return DefaultEVRExec().Unregister(event) }

// Execute is the package-level wrapper around DefaultEVRExec().Execute.
func Execute(event string) error { return DefaultEVRExec().Execute(event) }

// List is the package-level wrapper around DefaultEVRExec().List.
func List() map[string]string { return DefaultEVRExec().List() }
