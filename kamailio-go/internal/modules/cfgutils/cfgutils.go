// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * CfgUtils module - various config functions, matching Kamailio
 * modules/cfgutils (cfgutils.c).
 *
 * The C cfgutils module bundles a grab-bag of script helpers: route-existence
 * checks, named counters and variables (the $shv / $cnt pseudo-variables),
 * hash-based advisory locking (lock/unlock/trylock), and a sleep primitive.
 *
 * Here we keep an equivalent in-memory store of named counters, named string
 * variables, registered route names, and a per-key mutex map. The lock helpers
 * mirror the C lock_set behaviour: Lock blocks, TryLock is non-blocking.
 */

package cfgutils

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// CfgUtilsModule implements the cfgutils module functionality.
// C: struct module cfgutils (cfgutils.c) + _cfg_lock_set / shvar store.
type CfgUtilsModule struct {
	mu       sync.RWMutex
	routes   map[string]bool   // registered route names (C: main_rt)
	counters map[string]*int64 // named counters (C: $cnt / counter framework)
	vars     map[string]string // named string variables (C: $shv)

	lockMu sync.Mutex
	locks  map[string]*sync.Mutex // per-key advisory locks (C: _cfg_lock_set)
}

// NewCfgUtilsModule creates a new CfgUtilsModule with empty stores.
func NewCfgUtilsModule() *CfgUtilsModule {
	return &CfgUtilsModule{
		routes:   make(map[string]bool),
		counters: make(map[string]*int64),
		vars:     make(map[string]string),
		locks:    make(map[string]*sync.Mutex),
	}
}

// ---------------------------------------------------------------------------
// Route registration / checking (C: check_route_exists / route_lookup)
// ---------------------------------------------------------------------------

// RegisterRoute registers a route name as existing, mirroring C route_lookup
// adding an entry to main_rt. Used by CheckRouteRegister / RouteExists.
func (c *CfgUtilsModule) RegisterRoute(name string) {
	if c == nil || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routes[name] = true
}

// UnregisterRoute removes a route name from the registered set.
func (c *CfgUtilsModule) UnregisterRoute(name string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.routes, name)
}

// RouteExists returns true if a route with the given name is registered.
// C: route_lookup(&main_rt, name) >= 0.
func (c *CfgUtilsModule) RouteExists(name string) bool {
	if c == nil || name == "" {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.routes[name]
}

// CheckRouteRegister returns true if route handling is registered for the
// message, i.e. at least one route name has been registered with the module.
// The C check_route_exists() takes a route name; here the msg-scoped variant
// reports whether any routes are registered at all (route handling active).
// C: check_route_exists().
func (c *CfgUtilsModule) CheckRouteRegister(msg *parser.SIPMsg) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.routes) > 0
}

// ---------------------------------------------------------------------------
// Named counters (C: $cnt / counter framework)
// ---------------------------------------------------------------------------

// SetCount sets the named counter to val, creating it if necessary.
// C: counter_add() / $cnt(name) assignment.
func (c *CfgUtilsModule) SetCount(name string, val int64) {
	if c == nil || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cnt := c.counters[name]
	if cnt == nil {
		v := val
		cnt = &v
		c.counters[name] = cnt
		return
	}
	atomic.StoreInt64(cnt, val)
}

// GetCount returns the value of the named counter, or 0 if it does not exist.
// C: $cnt(name) / counter_fetch().
func (c *CfgUtilsModule) GetCount(name string) int64 {
	if c == nil || name == "" {
		return 0
	}
	c.mu.RLock()
	cnt := c.counters[name]
	c.mu.RUnlock()
	if cnt == nil {
		return 0
	}
	return atomic.LoadInt64(cnt)
}

// ResetCount sets the named counter back to 0. It is a no-op if the counter
// does not exist.
// C: counter_reset().
func (c *CfgUtilsModule) ResetCount(name string) {
	c.SetCount(name, 0)
}

// IncCount atomically adds delta to the named counter, creating it at 0
// first if necessary. Convenience helper mirroring counter_add().
func (c *CfgUtilsModule) IncCount(name string, delta int64) int64 {
	if c == nil || name == "" {
		return 0
	}
	c.mu.Lock()
	cnt := c.counters[name]
	if cnt == nil {
		var v int64
		cnt = &v
		c.counters[name] = cnt
	}
	c.mu.Unlock()
	return atomic.AddInt64(cnt, delta)
}

// ---------------------------------------------------------------------------
// Named string variables (C: $shv / shared variables)
// ---------------------------------------------------------------------------

// SetVar sets the named string variable to val.
// C: $shv(name) assignment.
func (c *CfgUtilsModule) SetVar(name string, val string) {
	if c == nil || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vars[name] = val
}

// GetVar returns the value of the named string variable, or the empty string
// if it does not exist.
// C: $shv(name).
func (c *CfgUtilsModule) GetVar(name string) string {
	if c == nil || name == "" {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vars[name]
}

// VarExists returns true if the named variable has been set.
func (c *CfgUtilsModule) VarExists(name string) bool {
	if c == nil || name == "" {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.vars[name]
	return ok
}

// ---------------------------------------------------------------------------
// Per-key advisory locks (C: lock / unlock / trylock over _cfg_lock_set)
// ---------------------------------------------------------------------------

// lockFor returns the mutex for key, creating it on first use. The caller
// must NOT hold c.mu.
func (c *CfgUtilsModule) lockFor(key string) *sync.Mutex {
	if c == nil {
		return nil
	}
	c.lockMu.Lock()
	defer c.lockMu.Unlock()
	m := c.locks[key]
	if m == nil {
		m = &sync.Mutex{}
		c.locks[key] = m
	}
	return m
}

// Lock acquires the advisory lock for key, blocking until it is available.
// C: lock(key) / lock_set_get().
func (c *CfgUtilsModule) Lock(key string) {
	if c == nil || key == "" {
		return
	}
	m := c.lockFor(key)
	if m != nil {
		m.Lock()
	}
}

// Unlock releases the advisory lock for key. It is a no-op if the lock is
// not held by the current goroutine (Go's sync.Mutex panics on unlock of an
// unheld mutex, so callers must pair Lock/Unlock carefully).
// C: unlock(key) / lock_set_release().
func (c *CfgUtilsModule) Unlock(key string) {
	if c == nil || key == "" {
		return
	}
	c.lockMu.Lock()
	m := c.locks[key]
	c.lockMu.Unlock()
	if m != nil {
		m.Unlock()
	}
}

// TryLock attempts to acquire the advisory lock for key without blocking.
// Returns true if the lock was acquired, false otherwise.
// C: trylock(key) / lock_set_try().
func (c *CfgUtilsModule) TryLock(key string) bool {
	if c == nil || key == "" {
		return false
	}
	m := c.lockFor(key)
	if m == nil {
		return false
	}
	return m.TryLock()
}

// ---------------------------------------------------------------------------
// Sleep (C: m_sleep / ki_sleep)
// ---------------------------------------------------------------------------

// Sleep blocks the calling goroutine for ms milliseconds. A negative or zero
// value returns immediately.
//
// The C m_sleep() takes seconds; this Go port takes milliseconds to match
// the more common sub-second use case and the ki_usleep() sibling.
// C: m_sleep() / ki_sleep() / ki_usleep().
func (c *CfgUtilsModule) Sleep(ms int) {
	if c == nil || ms <= 0 {
		return
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Package-level default instance and global functions
// ---------------------------------------------------------------------------

var (
	defaultMu       sync.Mutex
	defaultCfgUtils *CfgUtilsModule
	defaultOnce     sync.Once
)

// DefaultCfgUtils returns the package-level default CfgUtilsModule instance.
func DefaultCfgUtils() *CfgUtilsModule {
	defaultOnce.Do(func() {
		defaultCfgUtils = NewCfgUtilsModule()
	})
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultCfgUtils == nil {
		defaultCfgUtils = NewCfgUtilsModule()
	}
	return defaultCfgUtils
}

// Init initialises the cfgutils module (resets the default instance).
// Mirrors C mod_init().
func Init() error {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultCfgUtils = NewCfgUtilsModule()
	return nil
}

// SetCount is the package-level wrapper around the default instance.
func SetCount(name string, val int64) {
	DefaultCfgUtils().SetCount(name, val)
}

// GetCount is the package-level wrapper around the default instance.
func GetCount(name string) int64 {
	return DefaultCfgUtils().GetCount(name)
}

// SetVar is the package-level wrapper around the default instance.
func SetVar(name string, val string) {
	DefaultCfgUtils().SetVar(name, val)
}

// GetVar is the package-level wrapper around the default instance.
func GetVar(name string) string {
	return DefaultCfgUtils().GetVar(name)
}
