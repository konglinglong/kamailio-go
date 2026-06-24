// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Unified timer framework - matching C timer.c/timer_proc.c
 *
 * Provides a unified timer framework that mirrors Kamailio's C timer
 * subsystem (core/timer.c and core/timer_proc.c):
 *   - Periodic timers (matching register_timer / fork_basic_timer)
 *   - One-shot timers (matching timer_add with a single expire)
 *   - Thread-safe timer manager with registration, lookup and lifecycle
 *
 * The C implementation drives tick lists from SIGALRM (timer.c) and runs
 * forked sleep-loop processes (timer_proc.c). This Go port replaces those
 * primitives with goroutines driven by time.Ticker / time.After while
 * preserving the register / start / stop / destroy lifecycle semantics.
 */

package timer

import (
	"sync"
	"sync/atomic"
	"time"
)

// TimerHandler is the callback invoked when a timer fires.
// The param value is the one supplied at registration time, mirroring
// the (void *param) argument of C's timer_function / utimer_function.
type TimerHandler func(param interface{})

// Timer represents a single registered timer instance.
//
// Mirrors struct timer_ln from C timer.h: it holds the handler, its
// private data, the expire interval and runtime flags. The running flag
// plays the role of C's F_TIMER_ACTIVE.
type Timer struct {
	ID       uint64
	Name     string
	Interval time.Duration
	Handler  TimerHandler
	Param    interface{}

	mu      sync.Mutex
	running atomic.Bool
	stopCh  chan struct{}
	done    chan struct{}
	once    bool
}

// IsRunning reports whether the timer goroutine is currently active.
func (t *Timer) IsRunning() bool {
	return t.running.Load()
}

// start launches the timer goroutine. It is a no-op if the timer is
// already running. Safe to call again after stop to restart the timer.
func (t *Timer) start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running.Load() {
		return
	}
	t.running.Store(true)
	t.stopCh = make(chan struct{})
	t.done = make(chan struct{})
	go t.run()
}

// stop signals the timer goroutine to exit and blocks until it has
// terminated, so that on return no handler invocation is in flight.
// It is a no-op (and safe to call repeatedly) if the timer is not running.
func (t *Timer) stop() {
	t.mu.Lock()
	if !t.running.Load() {
		t.mu.Unlock()
		return
	}
	t.running.Store(false)
	close(t.stopCh)
	t.mu.Unlock()
	<-t.done
}

// run is the timer goroutine entry point. stopCh is captured at start
// (set under t.mu in start) so that a later restart does not race the
// previous goroutine's select.
func (t *Timer) run() {
	defer close(t.done)
	stopCh := t.stopCh
	if t.once {
		t.runOnce(stopCh)
	} else {
		t.runPeriodic(stopCh)
	}
}

// runOnce fires the handler a single time after Interval, then exits.
// Mirrors a one-shot timer_add whose handler returns 0.
func (t *Timer) runOnce(stopCh chan struct{}) {
	select {
	case <-time.After(t.Interval):
		t.Handler(t.Param)
		t.running.Store(false)
	case <-stopCh:
	}
}

// runPeriodic fires the handler on every tick until stop is called.
// Mirrors register_timer / fork_basic_timer periodic behaviour.
func (t *Timer) runPeriodic(stopCh chan struct{}) {
	ticker := time.NewTicker(t.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.Handler(t.Param)
		case <-stopCh:
			return
		}
	}
}

// TimerManager owns the set of registered timers and provides the
// register / start / stop / destroy lifecycle from C timer.c
// (init_timer, register_timer, destroy_timer) and timer_proc.c
// (fork_basic_timer).
type TimerManager struct {
	mu     sync.RWMutex
	timers map[string]*Timer
	nextID atomic.Uint64
}

// NewTimerManager creates an empty timer manager.
func NewTimerManager() *TimerManager {
	return &TimerManager{timers: make(map[string]*Timer)}
}

// RegisterTimer registers a periodic timer and starts it immediately,
// mirroring C's register_timer(). If a timer with the same name already
// exists it is stopped and replaced.
func (tm *TimerManager) RegisterTimer(name string, interval time.Duration,
	handler TimerHandler, param interface{}) *Timer {
	return tm.register(name, interval, handler, param, false)
}

// RegisterOnce registers a one-shot timer that fires exactly once after
// delay, mirroring a single-expire timer_add().
func (tm *TimerManager) RegisterOnce(name string, delay time.Duration,
	handler TimerHandler, param interface{}) *Timer {
	return tm.register(name, delay, handler, param, true)
}

func (tm *TimerManager) register(name string, interval time.Duration,
	handler TimerHandler, param interface{}, once bool) *Timer {
	t := &Timer{
		ID:       tm.nextID.Add(1),
		Name:     name,
		Interval: interval,
		Handler:  handler,
		Param:    param,
		once:     once,
	}
	tm.mu.Lock()
	old := tm.timers[name]
	tm.timers[name] = t
	t.start()
	tm.mu.Unlock()
	if old != nil {
		old.stop()
	}
	return t
}

// Start (re)starts a timer's goroutine. It is a no-op if the timer is
// already running.
func (tm *TimerManager) Start(timer *Timer) {
	if timer == nil {
		return
	}
	timer.start()
}

// Stop stops a timer and blocks until its goroutine has exited.
func (tm *TimerManager) Stop(timer *Timer) {
	if timer == nil {
		return
	}
	timer.stop()
}

// StopAll stops every registered timer and clears the registry,
// mirroring C's destroy_timer().
func (tm *TimerManager) StopAll() {
	tm.mu.Lock()
	timers := make([]*Timer, 0, len(tm.timers))
	for _, t := range tm.timers {
		timers = append(timers, t)
	}
	tm.timers = make(map[string]*Timer)
	tm.mu.Unlock()
	for _, t := range timers {
		t.stop()
	}
}

// GetTimer returns the timer registered under name, or nil if none.
func (tm *TimerManager) GetTimer(name string) *Timer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.timers[name]
}

// ListTimers returns a snapshot of all registered timers.
func (tm *TimerManager) ListTimers() []*Timer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	out := make([]*Timer, 0, len(tm.timers))
	for _, t := range tm.timers {
		out = append(out, t)
	}
	return out
}

// Count returns the number of registered timers.
func (tm *TimerManager) Count() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.timers)
}

// DefaultTimerManager is the package-level timer manager used by the
// Init / RegisterTimer / StopAll helpers. It mirrors the implicit global
// timer state initialised by C's init_timer().
var DefaultTimerManager = NewTimerManager()

// Init resets the default timer manager, stopping and clearing any
// previously registered timers. Mirrors C's init_timer().
func Init() {
	DefaultTimerManager.StopAll()
}

// RegisterTimer registers a periodic timer on the default manager.
func RegisterTimer(name string, interval time.Duration,
	handler TimerHandler, param interface{}) *Timer {
	return DefaultTimerManager.RegisterTimer(name, interval, handler, param)
}

// RegisterOnce registers a one-shot timer on the default manager.
func RegisterOnce(name string, delay time.Duration,
	handler TimerHandler, param interface{}) *Timer {
	return DefaultTimerManager.RegisterOnce(name, delay, handler, param)
}

// StopAll stops all timers on the default manager.
func StopAll() {
	DefaultTimerManager.StopAll()
}
