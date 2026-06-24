// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipP module - SIPp scenario integration.
 * Port of the kamailio sipp module (src/modules/sipp).
 *
 * Drives SIPp test scenarios by name: a scenario is started, runs until it
 * is explicitly stopped, and exposes per-scenario statistics. The actual
 * SIPp process is not spawned here; instead each running scenario is
 * tracked in memory with counters that the routing script can bump, which
 * keeps the module usable and deterministic in tests.
 *
 * It is safe for concurrent use.
 */
package sipp

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// SippStats holds the statistics for a single running scenario.
type SippStats struct {
	ScenarioID      string
	Calls           int64
	SuccessfulCalls int64
	FailedCalls     int64
	Running         bool
}

// scenarioState is the internal bookkeeping for a started scenario.
type scenarioState struct {
	stats *SippStats
}

// SippModule implements the sipp module functionality.
// C: struct module sipp
type SippModule struct {
	mu        sync.RWMutex
	scenarios map[string]*scenarioState
	running   atomic.Int64
}

// NewSippModule creates a SippModule.
func NewSippModule() *SippModule {
	return &SippModule{
		scenarios: make(map[string]*scenarioState),
	}
}

// StartScenario starts the named scenario. Starting an already-running
// scenario returns an error.
// C: sipp_start_scenario()
func (m *SippModule) StartScenario(scenario string) error {
	if scenario == "" {
		return fmt.Errorf("sipp: empty scenario name")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scenarios[scenario]; ok {
		return fmt.Errorf("sipp: scenario %q already running", scenario)
	}
	m.scenarios[scenario] = &scenarioState{
		stats: &SippStats{ScenarioID: scenario, Running: true},
	}
	m.running.Add(1)
	return nil
}

// StopScenario stops the named scenario. Stopping an unknown scenario is a
// no-op.
// C: sipp_stop_scenario()
func (m *SippModule) StopScenario(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sc, ok := m.scenarios[id]
	if !ok {
		return
	}
	sc.stats.Running = false
	delete(m.scenarios, id)
	m.running.Add(-1)
}

// GetStats returns a snapshot of the statistics for scenarioID, or nil when
// the scenario is not running.
// C: sipp_get_stats()
func (m *SippModule) GetStats(scenarioID string) *SippStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sc, ok := m.scenarios[scenarioID]
	if !ok {
		return nil
	}
	return &SippStats{
		ScenarioID:      sc.stats.ScenarioID,
		Calls:           sc.stats.Calls,
		SuccessfulCalls: sc.stats.SuccessfulCalls,
		FailedCalls:     sc.stats.FailedCalls,
		Running:         sc.stats.Running,
	}
}

// RecordCall bumps the call counters for scenarioID. Successful indicates
// whether the call succeeded. It is a no-op for an unknown scenario.
func (m *SippModule) RecordCall(scenarioID string, successful bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sc, ok := m.scenarios[scenarioID]
	if !ok {
		return
	}
	sc.stats.Calls++
	if successful {
		sc.stats.SuccessfulCalls++
	} else {
		sc.stats.FailedCalls++
	}
}

// IsRunning reports whether at least one scenario is currently running.
func (m *SippModule) IsRunning() bool {
	return m.running.Load() > 0
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu   sync.RWMutex
	defaultSipp *SippModule
)

// DefaultSipp returns the process-wide SippModule, creating one on first use.
func DefaultSipp() *SippModule {
	defaultMu.RLock()
	m := defaultSipp
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSipp == nil {
		defaultSipp = NewSippModule()
	}
	return defaultSipp
}

// Init (re)initialises the process-wide SippModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSipp = NewSippModule()
}
