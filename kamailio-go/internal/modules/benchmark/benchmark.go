// SPDX-License-Identifier-Identifier GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * benchmark - lightweight named timer / statistics collection.
 *
 * Allows scripts to bracket sections of code with Start/Stop calls and
 * later inspect aggregated statistics. Mirrors the kamailio benchmark
 * module.
 */

package benchmark

import (
	"math"
	"sort"
	"sync"
	"time"
)

// BMStats holds aggregated timing statistics for a named benchmark.
type BMStats struct {
	Name      string
	Count     int
	TotalTime time.Duration
	Min       time.Duration
	Max       time.Duration
}

// Avg returns the mean duration across all recorded samples.
func (s *BMStats) Avg() time.Duration {
	if s == nil || s.Count == 0 {
		return 0
	}
	return s.TotalTime / time.Duration(s.Count)
}

// BenchmarkModule collects named timing samples.
type BenchmarkModule struct {
	mu     sync.Mutex
	timers map[string]time.Time
	stats  map[string]*BMStats
}

// New returns a new BenchmarkModule.
func New() *BenchmarkModule {
	return &BenchmarkModule{
		timers: make(map[string]time.Time),
		stats:  make(map[string]*BMStats),
	}
}

// Start begins a timer for name. A previously running timer is overwritten.
func (m *BenchmarkModule) Start(name string) {
	if m == nil || name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.timers[name] = time.Now()
}

// Stop ends the timer for name, records the elapsed duration and returns it.
// Returns 0 if no timer was running for name.
func (m *BenchmarkModule) Stop(name string) time.Duration {
	if m == nil || name == "" {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	start, ok := m.timers[name]
	if !ok {
		return 0
	}
	delete(m.timers, name)
	d := time.Since(start)
	s, exists := m.stats[name]
	if !exists {
		s = &BMStats{Name: name, Min: math.MaxInt64}
		m.stats[name] = s
	}
	s.Count++
	s.TotalTime += d
	if d < s.Min {
		s.Min = d
	}
	if d > s.Max {
		s.Max = d
	}
	return d
}

// GetStats returns a copy of the statistics for name, or nil if unknown.
func (m *BenchmarkModule) GetStats(name string) *BMStats {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.stats[name]
	if !ok {
		return nil
	}
	cp := *s
	return &cp
}

// List returns the names of all benchmarks with recorded statistics, sorted.
func (m *BenchmarkModule) List() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.stats))
	for n := range m.stats {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
