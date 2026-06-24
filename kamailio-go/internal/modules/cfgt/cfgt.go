// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Config tracker module - tracks configuration value changes.
 * Port of the kamailio cfgt module (src/modules/cfgt).
 *
 * The module records baseline values for configuration keys and reports
 * which keys have since changed. It is safe for concurrent use.
 */

package cfgt

import "sync"

// CfgTModule tracks baseline and current values for configuration keys.
type CfgTModule struct {
	mu        sync.RWMutex
	baseline  map[string]string
	current   map[string]string
}

// New creates a CfgTModule with empty storage.
func New() *CfgTModule {
	return &CfgTModule{
		baseline: make(map[string]string),
		current:  make(map[string]string),
	}
}

// Track records the baseline value for the given key and sets the
// current value to the same. Re-tracking a key resets its baseline.
//
//	C: cfgt_track()
func (m *CfgTModule) Track(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.baseline == nil {
		m.baseline = make(map[string]string)
	}
	if m.current == nil {
		m.current = make(map[string]string)
	}
	m.baseline[key] = value
	m.current[key] = value
}

// Get returns the current value for the given key.
func (m *CfgTModule) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.current[key]
	return v, ok
}

// Update sets a new current value for a tracked key.
func (m *CfgTModule) Update(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		m.current = make(map[string]string)
	}
	m.current[key] = value
}

// Diff returns the keys whose current value differs from their baseline.
//
//	C: cfgt_diff()
func (m *CfgTModule) Diff() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string)
	for k, base := range m.baseline {
		if cur, ok := m.current[k]; ok && cur != base {
			out[k] = cur
		}
	}
	return out
}

// Clear removes all tracked keys.
func (m *CfgTModule) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baseline = make(map[string]string)
	m.current = make(map[string]string)
}

// Count returns the number of tracked keys.
func (m *CfgTModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.baseline)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *CfgTModule
)

// DefaultCfgT returns the process-wide CfgTModule.
func DefaultCfgT() *CfgTModule {
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

// Init (re)initialises the process-wide CfgTModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Track is the package-level wrapper around DefaultCfgT().Track.
func Track(key, value string) { DefaultCfgT().Track(key, value) }

// Get is the package-level wrapper around DefaultCfgT().Get.
func Get(key string) (string, bool) { return DefaultCfgT().Get(key) }

// Diff is the package-level wrapper around DefaultCfgT().Diff.
func Diff() map[string]string { return DefaultCfgT().Diff() }

// Update is the package-level wrapper around DefaultCfgT().Update.
func Update(key, value string) { DefaultCfgT().Update(key, value) }

// Clear is the package-level wrapper around DefaultCfgT().Clear.
func Clear() { DefaultCfgT().Clear() }
