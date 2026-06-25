// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * ptimer module - script-programmable timers.
 *
 * Port of the kamailio ptimer module (src/modules/ptimer). A
 * PTimerModule maintains a registry of named timers that invoke a
 * caller-supplied callback at a fixed interval. Timers are identified
 * by an integer ID and can be started/stopped individually or all at
 * once. One-shot timers fire exactly once and then deactivate
 * themselves.
 *
 * Each running timer is backed by its own goroutine driving a
 * time.Ticker; the registry can be mutated dynamically while timers
 * are running. The module is safe for concurrent use.
 */
package ptimer

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// TimerCallback is the signature of a timer callback. It mirrors the
// Kamailio route return convention: a non-negative value is treated as
// success, a negative value aborts the timer.
type TimerCallback func() int

// TimerEntry describes one registered timer.
type TimerEntry struct {
	ID       int
	Name     string
	Interval time.Duration
	Callback TimerCallback
	LastRun  time.Time
	NextRun  time.Time
	Active   bool
	OneShot  bool

	// internal runtime state (not part of the public contract).
	stop chan struct{}
	done chan struct{}
}

// PTimerModule manages a registry of programmable timers.
// C: struct module ptimer
type PTimerModule struct {
	mu     sync.RWMutex
	timers map[int]*TimerEntry
	nextID int32
}

// New creates a PTimerModule with an empty timer registry.
func New() *PTimerModule {
	return &PTimerModule{timers: make(map[int]*TimerEntry)}
}

// RegisterTimer adds a new timer to the registry without starting it.
// Returns the assigned timer ID. A non-positive interval, empty name
// or nil callback is rejected with an error.
//
//	C: param_parse_timer()
func (m *PTimerModule) RegisterTimer(name string, interval time.Duration, callback TimerCallback, oneshot bool) (int, error) {
	if m == nil {
		return -1, fmt.Errorf("ptimer: nil module")
	}
	if name == "" {
		return -1, fmt.Errorf("ptimer: empty timer name")
	}
	if callback == nil {
		return -1, fmt.Errorf("ptimer: nil callback")
	}
	if interval <= 0 {
		return -1, fmt.Errorf("ptimer: non-positive interval")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timers == nil {
		m.timers = make(map[int]*TimerEntry)
	}
	// Reject duplicate names so lookups by name stay unambiguous.
	for _, t := range m.timers {
		if t.Name == name {
			return -1, fmt.Errorf("ptimer: duplicate timer name %q", name)
		}
	}
	id := int(atomic.AddInt32(&m.nextID, 1))
	entry := &TimerEntry{
		ID:       id,
		Name:     name,
		Interval: interval,
		Callback: callback,
		OneShot:  oneshot,
	}
	m.timers[id] = entry
	return id, nil
}

// UnregisterTimer removes the timer with the given ID. A running timer
// is stopped before being removed. Returns an error when no such timer
// exists.
//
//	C: rpc ptimer.end
func (m *PTimerModule) UnregisterTimer(id int) error {
	if m == nil {
		return fmt.Errorf("ptimer: nil module")
	}
	m.mu.Lock()
	entry, ok := m.timers[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("ptimer: no timer with id %d", id)
	}
	delete(m.timers, id)
	m.mu.Unlock()
	m.stopEntry(entry)
	return nil
}

// UnregisterTimerByName removes the timer with the given name. A
// running timer is stopped before being removed. Returns an error when
// no such timer exists.
func (m *PTimerModule) UnregisterTimerByName(name string) error {
	if m == nil {
		return fmt.Errorf("ptimer: nil module")
	}
	m.mu.Lock()
	var found *TimerEntry
	for id, t := range m.timers {
		if t.Name == name {
			found = t
			delete(m.timers, id)
			break
		}
	}
	m.mu.Unlock()
	if found == nil {
		return fmt.Errorf("ptimer: no timer named %q", name)
	}
	m.stopEntry(found)
	return nil
}

// StartTimer activates the timer with the given ID, launching its
// background goroutine. Returns an error when the timer is missing or
// already active.
//
//	C: rpc ptimer.start
func (m *PTimerModule) StartTimer(id int) error {
	if m == nil {
		return fmt.Errorf("ptimer: nil module")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.timers[id]
	if !ok {
		return fmt.Errorf("ptimer: no timer with id %d", id)
	}
	if entry.Active {
		return fmt.Errorf("ptimer: timer %d already active", id)
	}
	entry.Active = true
	now := time.Now()
	entry.LastRun = time.Time{}
	entry.NextRun = now.Add(entry.Interval)
	stop := make(chan struct{})
	done := make(chan struct{})
	entry.stop = stop
	entry.done = done
	go m.run(entry, stop, done)
	return nil
}

// StopTimer deactivates the timer with the given ID and waits for its
// goroutine to exit. Returns an error when the timer is missing; a
// timer that was not running is a no-op.
//
//	C: rpc ptimer.pause
func (m *PTimerModule) StopTimer(id int) error {
	if m == nil {
		return fmt.Errorf("ptimer: nil module")
	}
	m.mu.Lock()
	entry, ok := m.timers[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("ptimer: no timer with id %d", id)
	}
	m.stopEntry(entry)
	return nil
}

// StartAll activates every registered timer that is not yet running.
func (m *PTimerModule) StartAll() error {
	if m == nil {
		return fmt.Errorf("ptimer: nil module")
	}
	m.mu.RLock()
	ids := make([]int, 0, len(m.timers))
	for id, t := range m.timers {
		if !t.Active {
			ids = append(ids, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range ids {
		_ = m.StartTimer(id)
	}
	return nil
}

// StopAll deactivates every running timer and waits for each goroutine
// to exit.
func (m *PTimerModule) StopAll() error {
	if m == nil {
		return fmt.Errorf("ptimer: nil module")
	}
	m.mu.RLock()
	entries := make([]*TimerEntry, 0, len(m.timers))
	for _, t := range m.timers {
		if t.Active {
			entries = append(entries, t)
		}
	}
	m.mu.RUnlock()
	for _, e := range entries {
		m.stopEntry(e)
	}
	return nil
}

// ListTimers returns a snapshot of every registered timer sorted by
// ID. The returned entries are copies; mutating them does not affect
// the registry.
func (m *PTimerModule) ListTimers() []TimerEntry {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TimerEntry, 0, len(m.timers))
	for _, t := range m.timers {
		cp := *t
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetTimer returns a snapshot of the timer with the given ID, or nil
// when no such timer exists.
func (m *PTimerModule) GetTimer(id int) *TimerEntry {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.timers[id]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// GetTimerByName returns a snapshot of the timer with the given name,
// or nil when no such timer exists.
func (m *PTimerModule) GetTimerByName(name string) *TimerEntry {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.timers {
		if t.Name == name {
			cp := *t
			return &cp
		}
	}
	return nil
}

// Count returns the number of registered timers.
func (m *PTimerModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.timers)
}

// run is the per-timer loop. It ticks at the timer's interval and
// invokes the callback, updating LastRun/NextRun. A negative callback
// return value or a one-shot timer terminates the loop. The stop and
// done channels are passed in (rather than read from the entry) so
// that stopEntry can swap them under lock without racing this loop.
func (m *PTimerModule) run(e *TimerEntry, stop, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(e.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			ret := e.Callback()
			m.mu.Lock()
			e.LastRun = now
			e.NextRun = now.Add(e.Interval)
			abort := e.OneShot || ret < 0
			if abort {
				e.Active = false
			}
			m.mu.Unlock()
			if abort {
				return
			}
		}
	}
}

// stopEntry stops a running timer and waits for its goroutine to exit.
// It is a no-op for timers that are not active.
func (m *PTimerModule) stopEntry(e *TimerEntry) {
	m.mu.Lock()
	if !e.Active {
		m.mu.Unlock()
		return
	}
	e.Active = false
	stop := e.stop
	done := e.done
	m.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu    sync.RWMutex
	defaultPTimer *PTimerModule
)

// DefaultPTimer returns the process-wide PTimerModule, creating it on
// first use.
func DefaultPTimer() *PTimerModule {
	defaultMu.RLock()
	p := defaultPTimer
	defaultMu.RUnlock()
	if p != nil {
		return p
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPTimer == nil {
		defaultPTimer = New()
	}
	return defaultPTimer
}

// Init (re)initialises the process-wide PTimerModule to a fresh state,
// stopping every previously running timer. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPTimer != nil {
		_ = defaultPTimer.StopAll()
	}
	defaultPTimer = New()
}
