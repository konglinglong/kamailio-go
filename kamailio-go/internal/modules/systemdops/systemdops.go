// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SystemdOps module - systemd notification operations.
 * Port of the kamailio systemdops module (src/modules/systemdops).
 *
 * Wraps the sd_notify protocol: Notify sends READY=1 (and the current
 * status), Watchdog sends WATCHDOG=1, and SetStatus updates the service
 * status string. The actual notification socket is not written here; each
 * call is recorded so the routing script and tests can inspect what would
 * have been sent.
 *
 * It is safe for concurrent use.
 */
package systemdops

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// SystemdOpsModule implements the systemdops module functionality.
// C: struct module systemdops
type SystemdOpsModule struct {
	mu            sync.Mutex
	status        string
	notifyCount   atomic.Int64
	watchdogCount atomic.Int64
	ready         atomic.Bool
}

// NewSystemdOpsModule creates a SystemdOpsModule.
func NewSystemdOpsModule() *SystemdOpsModule {
	return &SystemdOpsModule{}
}

// Notify sends a READY=1 notification together with the current status.
// C: sd_notify()
func (m *SystemdOpsModule) Notify(status string) {
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
	m.ready.Store(true)
	m.notifyCount.Add(1)
}

// Watchdog sends a WATCHDOG=1 keepalive ping.
// C: sd_notify(WATCHDOG=1)
func (m *SystemdOpsModule) Watchdog() {
	m.watchdogCount.Add(1)
}

// SetStatus updates the service status string without sending READY.
// C: sd_notify(STATUS=...)
func (m *SystemdOpsModule) SetStatus(status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = status
}

// GetStatus returns the current service status string.
func (m *SystemdOpsModule) GetStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// NotifyCount returns the number of Notify calls made.
func (m *SystemdOpsModule) NotifyCount() int64 {
	return m.notifyCount.Load()
}

// WatchdogCount returns the number of Watchdog pings sent.
func (m *SystemdOpsModule) WatchdogCount() int64 {
	return m.watchdogCount.Load()
}

// IsReady reports whether READY=1 has been sent at least once.
func (m *SystemdOpsModule) IsReady() bool {
	return m.ready.Load()
}

// NotifyMessage returns the sd_notify-style message that Notify would have
// sent for the given status, e.g. "READY=1\nSTATUS=foo".
func NotifyMessage(status string) string {
	return fmt.Sprintf("READY=1\nSTATUS=%s", status)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu          sync.RWMutex
	defaultSystemdOps *SystemdOpsModule
)

// DefaultSystemdOps returns the process-wide SystemdOpsModule, creating one
// on first use.
func DefaultSystemdOps() *SystemdOpsModule {
	defaultMu.RLock()
	m := defaultSystemdOps
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSystemdOps == nil {
		defaultSystemdOps = NewSystemdOpsModule()
	}
	return defaultSystemdOps
}

// Init (re)initialises the process-wide SystemdOpsModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSystemdOps = NewSystemdOpsModule()
}
