// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Timer module - named periodic timers.
 * Port of the kamailio timer module (src/modules/timer).
 *
 * The timer module lets the config script register named periodic
 * timers that invoke a callback at a fixed interval. Each timer runs
 * on its own goroutine backed by a time.Ticker. Timers are identified
 * by name and can be stopped individually or all at once.
 *
 * It is safe for concurrent use.
 */

package timer

import (
	"sort"
	"sync"
	"time"
)

// timerEntry is one running named timer.
type timerEntry struct {
	name    string
	ticker  *time.Ticker
	stop    chan struct{}
	done    chan struct{}
	handler func()
}

// TimerModule manages named periodic timers.
// C: struct module timer
type TimerModule struct {
	mu     sync.Mutex
	timers map[string]*timerEntry
}

// New creates a TimerModule with no running timers.
func New() *TimerModule {
	return &TimerModule{timers: make(map[string]*timerEntry)}
}

// Start registers a named timer that invokes handler every interval.
// If a timer with the same name is already running it is restarted.
// A non-positive interval, empty name or nil handler is ignored.
//
//	C: timer_register() / timer_add()
func (m *TimerModule) Start(name string, interval time.Duration, handler func()) {
	if m == nil || name == "" || handler == nil || interval <= 0 {
		return
	}
	m.Stop(name)
	entry := &timerEntry{
		name:    name,
		ticker:  time.NewTicker(interval),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		handler: handler,
	}
	m.mu.Lock()
	if m.timers == nil {
		m.timers = make(map[string]*timerEntry)
	}
	m.timers[name] = entry
	m.mu.Unlock()
	go m.run(entry)
}

// run is the per-timer loop.
func (m *TimerModule) run(e *timerEntry) {
	defer close(e.done)
	for {
		select {
		case <-e.ticker.C:
			e.handler()
		case <-e.stop:
			e.ticker.Stop()
			return
		}
	}
}

// Stop stops the named timer and waits for its loop to exit. Returns true
// when a timer was stopped.
//
//	C: timer_stop() / timer_del()
func (m *TimerModule) Stop(name string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	entry, ok := m.timers[name]
	if ok {
		delete(m.timers, name)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	close(entry.stop)
	<-entry.done
	return true
}

// IsRunning reports whether a timer with the given name is running.
func (m *TimerModule) IsRunning(name string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.timers[name]
	return ok
}

// List returns the names of all running timers in sorted order.
func (m *TimerModule) List() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.timers))
	for name := range m.timers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// StopAll stops every running timer and waits for each loop to exit.
func (m *TimerModule) StopAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	entries := m.timers
	m.timers = make(map[string]*timerEntry)
	m.mu.Unlock()
	for _, e := range entries {
		close(e.stop)
		<-e.done
	}
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu    sync.RWMutex
	defaultTimer *TimerModule
)

// DefaultTimer returns the process-wide TimerModule, creating it on first use.
func DefaultTimer() *TimerModule {
	defaultMu.RLock()
	m := defaultTimer
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTimer == nil {
		defaultTimer = New()
	}
	return defaultTimer
}

// Init (re)initialises the process-wide TimerModule to a fresh state,
// stopping every previously running timer. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTimer != nil {
		defaultTimer.StopAll()
	}
	defaultTimer = New()
}
