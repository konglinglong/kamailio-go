// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SNMPStats module - SNMP statistics counters.
 * Port of the kamailio snmpstats module (src/modules/snmpstats).
 *
 * Exposes named integer counters that the routing script can register and
 * increment; Start/Stop model the SNMP agent lifecycle. The actual SNMP
 * agent is not started here - counters are tracked in memory - which keeps
 * the module usable and testable out of the box.
 *
 * It is safe for concurrent use.
 */
package snmpstats

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// SNMPStatsModule implements the snmpstats module functionality.
// C: struct module snmpstats
type SNMPStatsModule struct {
	mu       sync.RWMutex
	addr     string
	counters map[string]*int64
	running  atomic.Bool
}

// NewSNMPStatsModule creates a SNMPStatsModule.
func NewSNMPStatsModule() *SNMPStatsModule {
	return &SNMPStatsModule{
		counters: make(map[string]*int64),
	}
}

// Init configures the SNMP agent listen address.
// C: mod_init()
func (m *SNMPStatsModule) Init(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addr = addr
}

// Addr returns the configured listen address.
func (m *SNMPStatsModule) Addr() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.addr
}

// RegisterCounter registers a named counter initialised to zero. Re-registering
// an existing name is a no-op.
// C: snmp_register_counter()
func (m *SNMPStatsModule) RegisterCounter(name string) {
	if name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counters == nil {
		m.counters = make(map[string]*int64)
	}
	if _, ok := m.counters[name]; ok {
		return
	}
	var v int64
	m.counters[name] = &v
}

// IncCounter atomically increments the named counter by one. Unknown
// counters are auto-registered.
// C: snmp_inc_counter()
func (m *SNMPStatsModule) IncCounter(name string) {
	m.mu.RLock()
	p, ok := m.counters[name]
	m.mu.RUnlock()
	if !ok {
		m.RegisterCounter(name)
		m.mu.RLock()
		p = m.counters[name]
		m.mu.RUnlock()
	}
	atomic.AddInt64(p, 1)
}

// GetCounter returns the current value of the named counter, or 0 when the
// counter is not registered.
// C: snmp_get_counter()
func (m *SNMPStatsModule) GetCounter(name string) int64 {
	m.mu.RLock()
	p, ok := m.counters[name]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return atomic.LoadInt64(p)
}

// Start marks the SNMP agent as running. Returns an error when no address
// has been configured or it is already running.
// C: snmp_start()
func (m *SNMPStatsModule) Start() error {
	if m.running.Load() {
		return fmt.Errorf("snmpstats: already running")
	}
	m.mu.RLock()
	addr := m.addr
	m.mu.RUnlock()
	if addr == "" {
		return fmt.Errorf("snmpstats: no address configured")
	}
	m.running.Store(true)
	return nil
}

// Stop marks the SNMP agent as stopped.
// C: snmp_stop()
func (m *SNMPStatsModule) Stop() {
	m.running.Store(false)
}

// IsRunning reports whether the SNMP agent is currently running.
func (m *SNMPStatsModule) IsRunning() bool {
	return m.running.Load()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu         sync.RWMutex
	defaultSNMPStats *SNMPStatsModule
)

// DefaultSNMPStats returns the process-wide SNMPStatsModule, creating one
// on first use.
func DefaultSNMPStats() *SNMPStatsModule {
	defaultMu.RLock()
	m := defaultSNMPStats
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSNMPStats == nil {
		defaultSNMPStats = NewSNMPStatsModule()
	}
	return defaultSNMPStats
}

// Init (re)initialises the process-wide SNMPStatsModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSNMPStats = NewSNMPStatsModule()
}
