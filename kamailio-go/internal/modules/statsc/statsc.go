// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * StatsC module - statistics collector.
 * Port of the kamailio statsc module (src/modules/statsc).
 *
 * Collects named integer statistics (accumulating values across Collect
 * calls) and exposes them for inspection, clearing and text export. The
 * values are held in memory.
 *
 * It is safe for concurrent use.
 */
package statsc

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// statEntry is a single collected statistic.
type statEntry struct {
	value int64
}

// StatsCModule implements the statsc module functionality.
// C: struct module statsc
type StatsCModule struct {
	mu    sync.RWMutex
	stats map[string]*statEntry
}

// NewStatsCModule creates a StatsCModule.
func NewStatsCModule() *StatsCModule {
	return &StatsCModule{stats: make(map[string]*statEntry)}
}

// Collect adds value to the named statistic, creating it on first use.
// C: statsc_collect()
func (m *StatsCModule) Collect(name string, value int64) {
	if name == "" {
		return
	}
	m.mu.RLock()
	e, ok := m.stats[name]
	m.mu.RUnlock()
	if !ok {
		m.mu.Lock()
		if m.stats == nil {
			m.stats = make(map[string]*statEntry)
		}
		if e, ok = m.stats[name]; !ok {
			e = &statEntry{}
			m.stats[name] = e
		}
		m.mu.Unlock()
	}
	atomic.AddInt64(&e.value, value)
}

// Get returns the current value of the named statistic, or 0 when it has
// not been collected.
// C: statsc_get()
func (m *StatsCModule) Get(name string) int64 {
	m.mu.RLock()
	e, ok := m.stats[name]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return atomic.LoadInt64(&e.value)
}

// List returns a snapshot of every statistic name -> value.
func (m *StatsCModule) List() map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]int64, len(m.stats))
	for k, e := range m.stats {
		out[k] = atomic.LoadInt64(&e.value)
	}
	return out
}

// Clear removes every collected statistic.
// C: statsc_clear()
func (m *StatsCModule) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats = make(map[string]*statEntry)
}

// Export returns the collected statistics as a stable, newline-delimited
// "name=value" text document, sorted by name.
// C: statsc_export()
func (m *StatsCModule) Export() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.stats))
	for k := range m.stats {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "%s=%d\n", n, atomic.LoadInt64(&m.stats[n].value))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu     sync.RWMutex
	defaultStatsC *StatsCModule
)

// DefaultStatsC returns the process-wide StatsCModule, creating one on first
// use.
func DefaultStatsC() *StatsCModule {
	defaultMu.RLock()
	m := defaultStatsC
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultStatsC == nil {
		defaultStatsC = NewStatsCModule()
	}
	return defaultStatsC
}

// Init (re)initialises the process-wide StatsCModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultStatsC = NewStatsCModule()
}
