// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Counters module - grouped, named counters with handle-based access.
 * Port of the kamailio counters module (src/modules/counters) and the
 * core counters framework (src/core/counters.{h,c}).
 *
 * The C counters framework uses a per-process value array indexed by
 * counter ID, where each process writes only to its own row (lock-free).
 * In Go we replace this with a single atomic.Int64 per counter — there
 * is no multi-process fork model, and atomic operations provide the
 * same lock-free write semantics for concurrent goroutines.
 *
 * Counters are identified by the pair (group, name). The script syntax
 * for referencing a counter is "group.name" (or just "name" when a
 * default script group is configured). A counter may optionally carry
 * a read callback; when present, GetVal invokes the callback instead
 * of returning the accumulated value.
 *
 * It is safe for concurrent use.
 */

package counters

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// CounterVal is the counter value type (mirrors C counter_val_t = long).
type CounterVal = int64

// CounterHandle is an opaque handle for fast counter access.
// Mirrors C counter_handle_t.
type CounterHandle struct {
	ID int
}

// InvalidHandle is the zero handle, returned on failure.
var InvalidHandle = CounterHandle{ID: 0}

// IsValid reports whether the handle refers to a registered counter.
func (h CounterHandle) IsValid() bool { return h.ID > 0 }

// CounterFlags values.
const (
	// FlagNoReset prevents Reset from zeroing the counter.
	// Mirrors CNT_F_NO_RESET.
	FlagNoReset = 1
)

// CounterCallback computes the counter value on demand. When set on a
// counter, GetVal invokes the callback instead of returning the
// accumulated value. Mirrors C counter_cbk_f.
type CounterCallback func(handle CounterHandle) CounterVal

// CounterDef defines a counter for batch registration.
// Mirrors C counter_def_t.
type CounterDef struct {
	Name     string
	Flags    int
	Callback CounterCallback
	Doc      string
}

// counter is the internal record for one registered counter.
// Mirrors C struct counter_record.
type counter struct {
	id       int
	group    string
	name     string
	doc      string
	flags    int
	callback CounterCallback
	value    atomic.Int64
}

// CountersModule provides the grouped counter registry. It is the Go
// equivalent of the C counters hash table + grp_hash_table + grp_sorted.
type CountersModule struct {
	mu          sync.RWMutex
	counters    map[int]*counter          // id -> record
	byName      map[string]*counter       // "group.name" -> record
	groups      map[string]map[int]bool   // group -> set of counter ids
	scriptGroup string
	nextID      int
}

// NewCountersModule creates a module with the default script group ("script").
func NewCountersModule() *CountersModule {
	return &CountersModule{
		counters:    make(map[int]*counter),
		byName:      make(map[string]*counter),
		groups:      make(map[string]map[int]bool),
		scriptGroup: "script",
		nextID:      1, // 0 is reserved for InvalidHandle
	}
}

// ---------------------------------------------------------------------------
// Registration / lookup
// ---------------------------------------------------------------------------

// Register creates a new counter in the given group. If the counter
// already exists and regFlags&1 != 0, the existing handle is returned
// (idempotent). Otherwise a duplicate registration returns
// ErrAlreadyRegistered.
//
//	C: counter_register()
func (m *CountersModule) Register(group, name string, flags int, callback CounterCallback, doc string, regFlags int) (CounterHandle, error) {
	if name == "" {
		return InvalidHandle, errors.New("counters: empty counter name")
	}
	if group == "" {
		group = m.ScriptGroup()
	}
	fullName := group + "." + name

	// Check for existing.
	m.mu.RLock()
	if c, ok := m.byName[fullName]; ok {
		m.mu.RUnlock()
		if regFlags&1 != 0 {
			return CounterHandle{ID: c.id}, nil
		}
		return InvalidHandle, fmt.Errorf("counters: %s already registered", fullName)
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	// Double-check after acquiring write lock.
	if c, ok := m.byName[fullName]; ok {
		if regFlags&1 != 0 {
			return CounterHandle{ID: c.id}, nil
		}
		return InvalidHandle, fmt.Errorf("counters: %s already registered", fullName)
	}
	id := m.nextID
	m.nextID++
	c := &counter{
		id:       id,
		group:    group,
		name:     name,
		doc:      doc,
		flags:    flags,
		callback: callback,
	}
	m.counters[id] = c
	m.byName[fullName] = c
	if m.groups[group] == nil {
		m.groups[group] = make(map[int]bool)
	}
	m.groups[group][id] = true
	return CounterHandle{ID: id}, nil
}

// RegisterArray registers multiple counters in one group.
//
//	C: counter_register_array()
func (m *CountersModule) RegisterArray(group string, defs []CounterDef) error {
	for _, d := range defs {
		if _, err := m.Register(group, d.Name, d.Flags, d.Callback, d.Doc, 1); err != nil {
			return err
		}
	}
	return nil
}

// Lookup finds an already-registered counter. With an empty group, the
// first counter with a matching name is returned (Kamailio backward-compat).
//
//	C: counter_lookup()
func (m *CountersModule) Lookup(group, name string) (CounterHandle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if group == "" {
		for _, c := range m.counters {
			if c.name == name {
				return CounterHandle{ID: c.id}, nil
			}
		}
		return InvalidHandle, fmt.Errorf("counters: %q not found", name)
	}
	c, ok := m.byName[group+"."+name]
	if !ok {
		return InvalidHandle, fmt.Errorf("counters: %s.%s not found", group, name)
	}
	return CounterHandle{ID: c.id}, nil
}

// ---------------------------------------------------------------------------
// Handle-based operations (hot path)
// ---------------------------------------------------------------------------

func (m *CountersModule) getCounter(h CounterHandle) *counter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.counters[h.ID]
}

// Inc increments the counter by 1.
//
//	C: counter_inc()
func (m *CountersModule) Inc(h CounterHandle) {
	if c := m.getCounter(h); c != nil {
		c.value.Add(1)
	}
}

// Add adds val to the counter.
//
//	C: counter_add()
func (m *CountersModule) Add(h CounterHandle, val int) {
	if c := m.getCounter(h); c != nil {
		c.value.Add(int64(val))
	}
}

// Reset zeroes the counter, unless FlagNoReset is set.
//
//	C: counter_reset()
func (m *CountersModule) Reset(h CounterHandle) {
	c := m.getCounter(h)
	if c == nil {
		return
	}
	if c.flags&FlagNoReset != 0 {
		return
	}
	c.value.Store(0)
}

// GetVal returns the counter value. If a callback is registered, it is
// invoked instead of returning the accumulated value.
//
//	C: counter_get_val()
func (m *CountersModule) GetVal(h CounterHandle) CounterVal {
	c := m.getCounter(h)
	if c == nil {
		return 0
	}
	if c.callback != nil {
		return c.callback(h)
	}
	return c.value.Load()
}

// GetRawVal returns the accumulated value, bypassing any callback.
//
//	C: counter_get_raw_val()
func (m *CountersModule) GetRawVal(h CounterHandle) CounterVal {
	if c := m.getCounter(h); c != nil {
		return c.value.Load()
	}
	return 0
}

// GetName returns the counter name.
//
//	C: counter_get_name()
func (m *CountersModule) GetName(h CounterHandle) string {
	if c := m.getCounter(h); c != nil {
		return c.name
	}
	return ""
}

// GetGroup returns the counter's group.
//
//	C: counter_get_group()
func (m *CountersModule) GetGroup(h CounterHandle) string {
	if c := m.getCounter(h); c != nil {
		return c.group
	}
	return ""
}

// GetDoc returns the counter's description.
//
//	C: counter_get_doc()
func (m *CountersModule) GetDoc(h CounterHandle) string {
	if c := m.getCounter(h); c != nil {
		return c.doc
	}
	return ""
}

// ---------------------------------------------------------------------------
// Name-based operations (script functions)
// ---------------------------------------------------------------------------

// parseName parses "group.name" or just "name" (using the default
// script group). Returns (group, name).
func (m *CountersModule) parseName(spec string) (string, string) {
	if idx := strings.IndexByte(spec, '.'); idx >= 0 {
		return spec[:idx], spec[idx+1:]
	}
	return m.ScriptGroup(), spec
}

// IncByName increments the counter identified by "group.name" (or just
// "name" in the default group).
//
//	C: cnt_inc / ki_cnt_inc
func (m *CountersModule) IncByName(spec string) error {
	h, err := m.Lookup(m.parseName(spec))
	if err != nil {
		return err
	}
	m.Inc(h)
	return nil
}

// AddByName adds val to the counter identified by "group.name".
//
//	C: cnt_add / ki_cnt_add
func (m *CountersModule) AddByName(spec string, val int) error {
	h, err := m.Lookup(m.parseName(spec))
	if err != nil {
		return err
	}
	m.Add(h, val)
	return nil
}

// ResetByName resets the counter identified by "group.name".
//
//	C: cnt_reset / ki_cnt_reset
func (m *CountersModule) ResetByName(spec string) error {
	h, err := m.Lookup(m.parseName(spec))
	if err != nil {
		return err
	}
	m.Reset(h)
	return nil
}

// GetValByName returns the value of the counter identified by "group.name".
func (m *CountersModule) GetValByName(spec string) (CounterVal, error) {
	h, err := m.Lookup(m.parseName(spec))
	if err != nil {
		return 0, err
	}
	return m.GetVal(h), nil
}

// ---------------------------------------------------------------------------
// Iteration
// ---------------------------------------------------------------------------

// IterateGroupNames calls cb for each group name in sorted order.
//
//	C: counter_iterate_grp_names()
func (m *CountersModule) IterateGroupNames(cb func(group string)) {
	m.mu.RLock()
	groups := make([]string, 0, len(m.groups))
	for g := range m.groups {
		groups = append(groups, g)
	}
	m.mu.RUnlock()
	sort.Strings(groups)
	for _, g := range groups {
		cb(g)
	}
}

// IterateGroupVarNames calls cb for each counter name in the group.
//
//	C: counter_iterate_grp_var_names()
func (m *CountersModule) IterateGroupVarNames(group string, cb func(name string)) {
	m.mu.RLock()
	ids := m.groups[group]
	names := make([]string, 0, len(ids))
	for id := range ids {
		if c := m.counters[id]; c != nil {
			names = append(names, c.name)
		}
	}
	m.mu.RUnlock()
	sort.Strings(names)
	for _, n := range names {
		cb(n)
	}
}

// IterateGroupVars calls cb for each counter in the group, passing the
// group name, counter name, and handle.
//
//	C: counter_iterate_grp_vars()
func (m *CountersModule) IterateGroupVars(group string, cb func(group, name string, handle CounterHandle)) {
	m.mu.RLock()
	ids := m.groups[group]
	type entry struct {
		name string
		id   int
	}
	entries := make([]entry, 0, len(ids))
	for id := range ids {
		if c := m.counters[id]; c != nil {
			entries = append(entries, entry{name: c.name, id: id})
		}
	}
	m.mu.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	for _, e := range entries {
		cb(group, e.name, CounterHandle{ID: e.id})
	}
}

// GroupCount returns the number of registered groups.
func (m *CountersModule) GroupCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.groups)
}

// CounterCount returns the total number of registered counters.
func (m *CountersModule) CounterCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.counters)
}

// ---------------------------------------------------------------------------
// Configuration (modparams)
// ---------------------------------------------------------------------------

// SetScriptGroup sets the default group for name-based script operations.
//
//	C: modparam("counters", "script_cnt_grp_name", ...)
func (m *CountersModule) SetScriptGroup(group string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if group == "" {
		group = "script"
	}
	m.scriptGroup = group
}

// ScriptGroup returns the configured default script group.
func (m *CountersModule) ScriptGroup() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.scriptGroup
}

// AddScriptCounter registers a counter from a config string. The format
// is "[grp.]name[( |:)desc]". When no group is given, the default script
// group is used.
//
//	C: modparam("counters", "script_counter", ...) / add_script_counter()
func (m *CountersModule) AddScriptCounter(spec string) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return errors.New("counters: empty script_counter spec")
	}
	name := spec
	doc := "custom script counter."
	// Split description: first ' ' or ':'.
	if idx := strings.IndexAny(name, " :"); idx >= 0 {
		doc = strings.TrimSpace(name[idx+1:])
		name = strings.TrimSpace(name[:idx])
	}
	// Split group.name.
	group := ""
	if idx := strings.IndexByte(name, '.'); idx >= 0 {
		group = name[:idx]
		name = name[idx+1:]
	}
	_, err := m.Register(group, name, 0, nil, doc, 1)
	return err
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultCM *CountersModule
)

// DefaultCounters returns the process-wide CountersModule, creating one
// on first use.
func DefaultCounters() *CountersModule {
	defaultMu.RLock()
	cm := defaultCM
	defaultMu.RUnlock()
	if cm != nil {
		return cm
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultCM == nil {
		defaultCM = NewCountersModule()
	}
	return defaultCM
}

// Init (re)initialises the process-wide CountersModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultCM = NewCountersModule()
}
