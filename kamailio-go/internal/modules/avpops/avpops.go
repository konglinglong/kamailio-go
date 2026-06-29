// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * AVPOps module - AVP operations, matching Kamailio modules/avpops
 * (avpops.c / avpops_impl.c).
 *
 * The C avpops module exposes script functions to manipulate AVPs
 * (Attribute-Value Pairs): set/get/delete/exists/count, printf-style
 * formatting, copy between names, and append (multi-valued AVPs).
 *
 * Here we keep an in-memory, name-keyed store of string values. A name can
 * hold multiple values (mirroring C's multi-valued AVPs); AVPSet replaces
 * the value list while AVPAppend extends it.
 */

package avpops

import (
	"fmt"
	"strings"
	"sync"
)

// AVPOpsModule implements the avpops module functionality.
// C: struct module avpops (avpops.c) + the global AVP list.
type AVPOpsModule struct {
	mu   sync.RWMutex
	avps map[string][]string // name -> ordered list of values
}

// NewAVPOpsModule creates a new AVPOpsModule with an empty AVP store.
func NewAVPOpsModule() *AVPOpsModule {
	return &AVPOpsModule{avps: make(map[string][]string)}
}

// normaliseName returns the canonical key for an AVP name. The C module
// matches AVPs by name case-sensitively but trims surrounding whitespace;
// we do the same here.
func normaliseName(name string) string {
	return strings.TrimSpace(name)
}

// ---------------------------------------------------------------------------
// Set / Get / Delete / Exists / Count (C: avp_set / avp_get / delete_avps)
// ---------------------------------------------------------------------------

// AVPSet sets the AVP named name to val, replacing any existing values.
// Returns 1 on success or -1 on error (empty name).
// C: avp_set() / ops_set_avp().
func (m *AVPOpsModule) AVPSet(name string, val string) int {
	if m == nil {
		return -1
	}
	name = normaliseName(name)
	if name == "" {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.avps[name] = []string{val}
	return 1
}

// AVPGet returns the first value of the AVP named name and true if it
// exists, or "" and false otherwise.
// C: avp_get() / is_avp_set() value retrieval.
func (m *AVPOpsModule) AVPGet(name string) (string, bool) {
	if m == nil {
		return "", false
	}
	name = normaliseName(name)
	if name == "" {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	vals := m.avps[name]
	if len(vals) == 0 {
		return "", false
	}
	return vals[0], true
}

// AVPDelete removes every value stored under name and returns the number of
// values removed. Returns -1 on error (empty name).
// C: delete_avps() / w_delete_avps().
func (m *AVPOpsModule) AVPDelete(name string) int {
	if m == nil {
		return -1
	}
	name = normaliseName(name)
	if name == "" {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.avps[name])
	delete(m.avps, name)
	return n
}

// AVPExists returns true if at least one value is stored under name.
// C: is_avp_set() presence check.
func (m *AVPOpsModule) AVPExists(name string) bool {
	if m == nil {
		return false
	}
	name = normaliseName(name)
	if name == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.avps[name]) > 0
}

// AVPCount returns the number of values stored under name (0 if none).
// C: avp_count() (via is_avp_set count).
func (m *AVPOpsModule) AVPCount(name string) int {
	if m == nil {
		return 0
	}
	name = normaliseName(name)
	if name == "" {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.avps[name])
}

// ---------------------------------------------------------------------------
// Printf / Copy / Append (C: avp_printf / copy_avps / append_avp)
// ---------------------------------------------------------------------------

// AVPPrintf sets the AVP named name to the string produced by formatting
// format with args (Go fmt.Sprintf semantics). Returns 1 on success or -1
// on error (empty name).
// C: avp_printf().
func (m *AVPOpsModule) AVPPrintf(name string, format string, args ...interface{}) int {
	if m == nil {
		return -1
	}
	name = normaliseName(name)
	if name == "" {
		return -1
	}
	val := fmt.Sprintf(format, args...)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.avps[name] = []string{val}
	return 1
}

// AVPCopy copies every value stored under srcName to dstName, appending to
// any values already present at dstName. Returns the number of values copied,
// or -1 on error (empty name).
// C: copy_avps() / w_copy_avps().
func (m *AVPOpsModule) AVPCopy(srcName, dstName string) int {
	if m == nil {
		return -1
	}
	srcName = normaliseName(srcName)
	dstName = normaliseName(dstName)
	if srcName == "" || dstName == "" {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.avps[srcName]
	if len(src) == 0 {
		return 0
	}
	// Copy by value to avoid aliasing the source slice.
	cp := make([]string, len(src))
	copy(cp, src)
	m.avps[dstName] = append(m.avps[dstName], cp...)
	return len(cp)
}

// AVPAppend appends val to the multi-valued AVP named name, preserving any
// existing values. Returns 1 on success or -1 on error (empty name).
// C: append_avp() / avp_append (multi-valued add).
func (m *AVPOpsModule) AVPAppend(name string, val string) int {
	if m == nil {
		return -1
	}
	name = normaliseName(name)
	if name == "" {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.avps[name] = append(m.avps[name], val)
	return 1
}

// ---------------------------------------------------------------------------
// Inspection helpers (not in the C script API but useful for tests/diagnostics)
// ---------------------------------------------------------------------------

// AVPGetAll returns every value stored under name (in insertion order).
func (m *AVPOpsModule) AVPGetAll(name string) []string {
	if m == nil {
		return nil
	}
	name = normaliseName(name)
	if name == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	vals := m.avps[name]
	out := make([]string, len(vals))
	copy(out, vals)
	return out
}

// AVPClear removes every AVP from the store.
func (m *AVPOpsModule) AVPClear() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.avps = make(map[string][]string)
}

// AVPNames returns a snapshot of all AVP names currently in the store.
func (m *AVPOpsModule) AVPNames() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.avps))
	for name := range m.avps {
		out = append(out, name)
	}
	return out
}

// ---------------------------------------------------------------------------
// Package-level default instance and global functions
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.Mutex
	defaultAVPOps  *AVPOpsModule
	defaultOnce    sync.Once
)

// DefaultAVPOps returns the package-level default AVPOpsModule instance.
func DefaultAVPOps() *AVPOpsModule {
	defaultOnce.Do(func() {
		defaultAVPOps = NewAVPOpsModule()
	})
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultAVPOps == nil {
		defaultAVPOps = NewAVPOpsModule()
	}
	return defaultAVPOps
}

// Init initialises the avpops module (resets the default instance).
// Mirrors C avpops_init().
func Init() error {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultAVPOps = NewAVPOpsModule()
	return nil
}

// AVPSet is the package-level wrapper around the default instance.
func AVPSet(name string, val string) int {
	return DefaultAVPOps().AVPSet(name, val)
}

// AVPGet is the package-level wrapper around the default instance.
func AVPGet(name string) (string, bool) {
	return DefaultAVPOps().AVPGet(name)
}

// AVPExists is the package-level wrapper around the default instance.
func AVPExists(name string) bool {
	return DefaultAVPOps().AVPExists(name)
}
