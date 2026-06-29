// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Counter framework - matching Kamailio core counters.c.
 * Provides named counters grouped by category, with atomic
 * increment/decrement/reset operations and thread-safe iteration.
 */

package counter

import (
	"sync"
	"sync/atomic"
)

// Counter represents a single named counter belonging to a group.
// The value field is updated atomically; Name, Group and Desc are
// immutable after registration and therefore safe to read concurrently.
// A Counter must not be copied because it contains an atomic.Int64.
type Counter struct {
	Name  string
	Group string
	Desc  string
	value atomic.Int64
}

// CounterGroup holds all counters that share a group name. The mu
// mutex protects the Counters map; the map is mutated only while the
// owning registry's write lock is held, but readers may iterate it
// under mu alone.
type CounterGroup struct {
	Name     string
	Counters map[string]*Counter
	mu       sync.RWMutex
}

// CounterRegistry is the registry of counters keyed by name. It is
// safe for concurrent use.
type CounterRegistry struct {
	mu       sync.RWMutex
	counters map[string]*Counter
	groups   map[string]*CounterGroup
}

// NewCounterRegistry creates an empty CounterRegistry.
func NewCounterRegistry() *CounterRegistry {
	return &CounterRegistry{
		counters: make(map[string]*Counter),
		groups:   make(map[string]*CounterGroup),
	}
}

// Register creates a new counter with the given name, group and
// description. If a counter with the same name already exists it is
// returned unchanged (idempotent registration), mirroring the lookup
// behaviour of Kamailio's counter_register/counter_lookup pair.
func (r *CounterRegistry) Register(name, group, desc string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &Counter{Name: name, Group: group, Desc: desc}
	r.counters[name] = c
	g := r.groups[group]
	if g == nil {
		g = &CounterGroup{Name: group, Counters: make(map[string]*Counter)}
		r.groups[group] = g
	}
	g.mu.Lock()
	g.Counters[name] = c
	g.mu.Unlock()
	return c
}

// Get returns the counter registered under name, or nil if none exists.
func (r *CounterRegistry) Get(name string) *Counter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.counters[name]
}

// GetGroup returns the CounterGroup for the given group name, or nil if
// the group does not exist.
func (r *CounterRegistry) GetGroup(group string) *CounterGroup {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.groups[group]
}

// Inc increments the counter named name by 1. It is a no-op if the
// counter does not exist.
func (r *CounterRegistry) Inc(name string) {
	r.IncBy(name, 1)
}

// IncBy adds val to the counter named name. It is a no-op if the
// counter does not exist.
func (r *CounterRegistry) IncBy(name string, val int64) {
	r.mu.RLock()
	c, ok := r.counters[name]
	r.mu.RUnlock()
	if !ok {
		return
	}
	c.value.Add(val)
}

// Dec decrements the counter named name by 1.
func (r *CounterRegistry) Dec(name string) {
	r.DecBy(name, 1)
}

// DecBy subtracts val from the counter named name.
func (r *CounterRegistry) DecBy(name string, val int64) {
	r.IncBy(name, -val)
}

// Reset sets the counter named name back to 0.
func (r *CounterRegistry) Reset(name string) {
	r.mu.RLock()
	c, ok := r.counters[name]
	r.mu.RUnlock()
	if !ok {
		return
	}
	c.value.Store(0)
}

// Value returns the current value of the counter named name, or 0 if
// the counter does not exist.
func (r *CounterRegistry) Value(name string) int64 {
	r.mu.RLock()
	c, ok := r.counters[name]
	r.mu.RUnlock()
	if !ok {
		return 0
	}
	return c.value.Load()
}

// List returns a snapshot slice of all registered counters.
func (r *CounterRegistry) List() []*Counter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Counter, 0, len(r.counters))
	for _, c := range r.counters {
		out = append(out, c)
	}
	return out
}

// ListGroup returns a snapshot slice of all counters in the given group.
func (r *CounterRegistry) ListGroup(group string) []*Counter {
	r.mu.RLock()
	g, ok := r.groups[group]
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*Counter, 0, len(g.Counters))
	for _, c := range g.Counters {
		out = append(out, c)
	}
	return out
}

// Stats returns a map of counter name to current value for all
// registered counters.
func (r *CounterRegistry) Stats() map[string]int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]int64, len(r.counters))
	for name, c := range r.counters {
		out[name] = c.value.Load()
	}
	return out
}

// defaultRegistry is the process-wide registry used by the package-level
// helper functions.
var (
	defaultRegistry *CounterRegistry
	registryOnce    sync.Once
)

// DefaultRegistry returns the process-wide CounterRegistry, creating it
// on first use.
func DefaultRegistry() *CounterRegistry {
	registryOnce.Do(func() {
		defaultRegistry = NewCounterRegistry()
	})
	return defaultRegistry
}

// Init eagerly initialises the default registry. It mirrors Kamailio's
// init_counters() and is safe to call multiple times.
func Init() {
	DefaultRegistry()
}

// Register registers a counter on the default registry.
func Register(name, group, desc string) *Counter {
	return DefaultRegistry().Register(name, group, desc)
}

// Inc increments the named counter on the default registry by 1.
func Inc(name string) {
	DefaultRegistry().Inc(name)
}

// IncBy adds val to the named counter on the default registry.
func IncBy(name string, val int64) {
	DefaultRegistry().IncBy(name, val)
}

// Dec decrements the named counter on the default registry by 1.
func Dec(name string) {
	DefaultRegistry().Dec(name)
}

// Reset resets the named counter on the default registry to 0.
func Reset(name string) {
	DefaultRegistry().Reset(name)
}

// Value returns the current value of the named counter on the default
// registry.
func Value(name string) int64 {
	return DefaultRegistry().Value(name)
}

// Stats returns a map of all counter values on the default registry.
func Stats() map[string]int64 {
	return DefaultRegistry().Stats()
}
