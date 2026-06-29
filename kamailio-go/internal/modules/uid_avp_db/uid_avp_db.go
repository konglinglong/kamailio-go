// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * UIDAVPDB module - load/store Attribute-Value Pairs per user.
 * Port of the kamailio uid_avp_db module (src/modules/uid_avp_db).
 *
 * The uid_avp_db module loads and stores SIP AVPs (Attribute-Value Pairs)
 * for a given user from/to a database. This Go counterpart keeps an
 * in-memory store keyed by user so the module is fully testable without
 * a live database.
 *
 * It is safe for concurrent use.
 */

package uid_avp_db

import (
	"sync"
)

// UIDAVPDBModule loads and stores per-user AVPs.
// C: struct module uid_avp_db
type UIDAVPDBModule struct {
	mu   sync.RWMutex
	avps map[string]map[string]string
}

// New creates an empty UIDAVPDBModule.
func New() *UIDAVPDBModule {
	return &UIDAVPDBModule{avps: make(map[string]map[string]string)}
}

// LoadAVPs returns a copy of every AVP stored for user. Returns nil when the
// user has no AVPs.
//
//	C: load_avp()
func (m *UIDAVPDBModule) LoadAVPs(user string) map[string]string {
	if m == nil || user == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	src, ok := m.avps[user]
	if !ok || len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// StoreAVP sets the AVP (name, value) for user, overwriting any existing
// value for that name.
//
//	C: store_avp()
func (m *UIDAVPDBModule) StoreAVP(user, name, value string) {
	if m == nil || user == "" || name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.avps == nil {
		m.avps = make(map[string]map[string]string)
	}
	if m.avps[user] == nil {
		m.avps[user] = make(map[string]string)
	}
	m.avps[user][name] = value
}

// DeleteAVP removes the AVP name for user. Returns true when an AVP was
// removed.
//
//	C: remove_avp()
func (m *UIDAVPDBModule) DeleteAVP(user, name string) bool {
	if m == nil || user == "" || name == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	set, ok := m.avps[user]
	if !ok {
		return false
	}
	if _, ok := set[name]; !ok {
		return false
	}
	delete(set, name)
	if len(set) == 0 {
		delete(m.avps, user)
	}
	return true
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultUIDAVPDB  *UIDAVPDBModule
)

// DefaultUIDAVPDB returns the process-wide UIDAVPDBModule, creating it on
// first use.
func DefaultUIDAVPDB() *UIDAVPDBModule {
	defaultMu.RLock()
	m := defaultUIDAVPDB
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultUIDAVPDB == nil {
		defaultUIDAVPDB = New()
	}
	return defaultUIDAVPDB
}

// Init (re)initialises the process-wide UIDAVPDBModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultUIDAVPDB = New()
}
