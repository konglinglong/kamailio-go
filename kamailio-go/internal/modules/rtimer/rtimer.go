// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * RTimer module - named recurring timers.
 * Port of the kamailio rtimer module (src/modules/rtimer).
 *
 * rtimer lets scripts register named recurring timers that invoke a
 * handler at a fixed interval. Start begins (or replaces) a timer; Stop
 * cancels it; IsRunning reports liveness; List enumerates running timers.
 *
 * The module is safe for concurrent use.
 */

package rtimer

import (
	"sort"
	"sync"
	"time"
)

// timer is one running recurring timer.
type timer struct {
	stop chan struct{}
	done chan struct{}
}

// RTimerModule manages named recurring timers.
type RTimerModule struct {
	mu     sync.Mutex
	timers map[string]*timer
}

// New creates an empty RTimerModule.
func New() *RTimerModule {
	return &RTimerModule{timers: make(map[string]*timer)}
}

// Start begins (or replaces) a named timer that invokes handler every
// interval. A nil handler or non-positive interval is ignored. A running
// timer with the same name is stopped first.
func (m *RTimerModule) Start(name string, interval time.Duration, handler func()) {
	if name == "" || handler == nil || interval <= 0 {
		return
	}
	m.mu.Lock()
	if existing, ok := m.timers[name]; ok {
		close(existing.stop)
		<-existing.done
	}
	t := &timer{stop: make(chan struct{}), done: make(chan struct{})}
	m.timers[name] = t
	m.mu.Unlock()
	go func() {
		defer close(t.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-t.stop:
				return
			case <-ticker.C:
				handler()
			}
		}
	}()
}

// Stop cancels the named timer. Returns true when a timer was stopped.
func (m *RTimerModule) Stop(name string) {
	m.mu.Lock()
	t, ok := m.timers[name]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.timers, name)
	m.mu.Unlock()
	close(t.stop)
	<-t.done
}

// IsRunning reports whether the named timer is currently running.
func (m *RTimerModule) IsRunning(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.timers[name]
	return ok
}

// List returns the sorted names of all running timers.
func (m *RTimerModule) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.timers))
	for name := range m.timers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// StopAll cancels every running timer.
func (m *RTimerModule) StopAll() {
	m.mu.Lock()
	names := make([]string, 0, len(m.timers))
	for name := range m.timers {
		names = append(names, name)
	}
	m.mu.Unlock()
	for _, n := range names {
		m.Stop(n)
	}
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *RTimerModule
)

// DefaultRTimer returns the process-wide module, creating it on first use.
func DefaultRTimer() *RTimerModule {
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
	if defaultM != nil {
		defaultM.StopAll()
	}
	defaultM = New()
}

// Start is the package-level wrapper.
func Start(name string, interval time.Duration, handler func()) {
	DefaultRTimer().Start(name, interval, handler)
}

// Stop is the package-level wrapper.
func Stop(name string) { DefaultRTimer().Stop(name) }

// IsRunning is the package-level wrapper.
func IsRunning(name string) bool { return DefaultRTimer().IsRunning(name) }

// List is the package-level wrapper.
func List() []string { return DefaultRTimer().List() }
