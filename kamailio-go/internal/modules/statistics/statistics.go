// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Statistics module - script interface to the internal statistics manager.
 *
 * Port of the kamailio statistics module (src/modules/statistics). A
 * StatisticsModule holds named counters (Stat) that can be incremented,
 * decremented and reset from the script. Each Stat carries a description
 * and a 64-bit value.
 *
 * The module is the script-facing wrapper around Kamailio's core
 * counters framework: Register mirrors reg_statistic(), Inc/Dec mirror
 * update_stat(), and Reset mirrors reset_stat().
 */
package statistics

import (
	"sync"
	"sync/atomic"
)

// Stat is a single named counter. The Value field is safe for
// concurrent access via atomic operations.
type Stat struct {
	Name        string
	Description string
	value       atomic.Int64
}

// Value returns the current value of the statistic.
func (s *Stat) Value() int64 {
	return s.value.Load()
}

// StatisticsModule implements the statistics module. It is safe for
// concurrent use: the stats map is guarded by mu, and each Stat's value
// is updated atomically.
type StatisticsModule struct {
	mu    sync.RWMutex
	stats map[string]*Stat
}

// NewStatisticsModule creates a new StatisticsModule.
func NewStatisticsModule() *StatisticsModule {
	return &StatisticsModule{stats: make(map[string]*Stat)}
}

// Register creates a new statistic with the given name and description.
// If a statistic with the name already exists it is returned unchanged
// (the description is not overwritten), mirroring Kamailio's
// reg_statistic() which is idempotent.
func (m *StatisticsModule) Register(name string, desc string) *Stat {
	m.mu.RLock()
	if s, ok := m.stats[name]; ok {
		m.mu.RUnlock()
		return s
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.stats[name]; ok {
		return s
	}
	s := &Stat{Name: name, Description: desc}
	m.stats[name] = s
	return s
}

// Get returns the statistic with the given name, or nil if no such
// statistic has been registered.
func (m *StatisticsModule) Get(name string) *Stat {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats[name]
}

// Inc increments the named statistic by 1. If the statistic does not
// exist it is created on the fly with an empty description, mirroring
// the lazy registration behaviour of Kamailio's update_stat().
func (m *StatisticsModule) Inc(name string) {
	m.IncBy(name, 1)
}

// IncBy increments the named statistic by val. A negative val is
// allowed and effectively decrements.
func (m *StatisticsModule) IncBy(name string, val int64) {
	s := m.lookupOrRegister(name)
	s.value.Add(val)
}

// Dec decrements the named statistic by 1.
func (m *StatisticsModule) Dec(name string) {
	m.DecBy(name, 1)
}

// DecBy decrements the named statistic by val.
func (m *StatisticsModule) DecBy(name string, val int64) {
	s := m.lookupOrRegister(name)
	s.value.Add(-val)
}

// Reset sets the named statistic back to 0.
func (m *StatisticsModule) Reset(name string) {
	s := m.lookupOrRegister(name)
	s.value.Store(0)
}

// Value returns the current value of the named statistic, or 0 if it
// does not exist.
func (m *StatisticsModule) Value(name string) int64 {
	m.mu.RLock()
	s, ok := m.stats[name]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return s.value.Load()
}

// List returns all registered statistics. The order is unspecified.
func (m *StatisticsModule) List() []*Stat {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Stat, 0, len(m.stats))
	for _, s := range m.stats {
		out = append(out, s)
	}
	return out
}

// Stats returns a snapshot of all statistic values as a name->value
// map. The map is a copy and may be modified freely.
func (m *StatisticsModule) Stats() map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]int64, len(m.stats))
	for name, s := range m.stats {
		out[name] = s.value.Load()
	}
	return out
}

// Count returns the number of registered statistics.
func (m *StatisticsModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.stats)
}

// lookupOrRegister returns the named statistic, creating it (with an
// empty description) if it does not yet exist.
func (m *StatisticsModule) lookupOrRegister(name string) *Stat {
	m.mu.RLock()
	s, ok := m.stats[name]
	m.mu.RUnlock()
	if ok {
		return s
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.stats[name]; ok {
		return s
	}
	s = &Stat{Name: name}
	m.stats[name] = s
	return s
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu         sync.RWMutex
	defaultStatistics *StatisticsModule
)

// DefaultStatistics returns the process-wide StatisticsModule, creating
// one on first use.
func DefaultStatistics() *StatisticsModule {
	defaultMu.RLock()
	m := defaultStatistics
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultStatistics == nil {
		defaultStatistics = NewStatisticsModule()
	}
	return defaultStatistics
}

// Init (re)initialises the process-wide StatisticsModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultStatistics = NewStatisticsModule()
}
