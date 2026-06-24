// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Config DB module - persistent configuration key/value store.
 * Port of the kamailio cfg_db module (src/modules/cfg_db).
 *
 * The module stores configuration values in an in-memory key/value map
 * with load/store/delete/list operations. It is safe for concurrent use.
 */

package cfg_db

import (
	"errors"
	"sync"
)

// CfgDBModule maintains a key/value configuration store.
type CfgDBModule struct {
	mu   sync.RWMutex
	data map[string]string
}

// New creates a CfgDBModule with empty storage.
func New() *CfgDBModule {
	return &CfgDBModule{data: make(map[string]string)}
}

// Load returns the value for the given key. It returns an error when
// the key is not present.
//
//	C: cfg_db_load()
func (m *CfgDBModule) Load(key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return "", errors.New("cfg_db: key not found: " + key)
	}
	return v, nil
}

// Store sets the value for the given key, creating or overwriting it.
//
//	C: cfg_db_store()
func (m *CfgDBModule) Store(key, value string) error {
	if key == "" {
		return errors.New("cfg_db: empty key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = make(map[string]string)
	}
	m.data[key] = value
	return nil
}

// Delete removes the given key. It returns an error when the key is
// not present.
//
//	C: cfg_db_delete()
func (m *CfgDBModule) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[key]; !ok {
		return errors.New("cfg_db: key not found: " + key)
	}
	delete(m.data, key)
	return nil
}

// List returns a copy of all stored key/value pairs.
func (m *CfgDBModule) List() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out
}

// Count returns the number of stored keys.
func (m *CfgDBModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *CfgDBModule
)

// DefaultCfgDB returns the process-wide CfgDBModule.
func DefaultCfgDB() *CfgDBModule {
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

// Init (re)initialises the process-wide CfgDBModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Load is the package-level wrapper around DefaultCfgDB().Load.
func Load(key string) (string, error) { return DefaultCfgDB().Load(key) }

// Store is the package-level wrapper around DefaultCfgDB().Store.
func Store(key, value string) error { return DefaultCfgDB().Store(key, value) }

// Delete is the package-level wrapper around DefaultCfgDB().Delete.
func Delete(key string) error { return DefaultCfgDB().Delete(key) }

// List is the package-level wrapper around DefaultCfgDB().List.
func List() map[string]string { return DefaultCfgDB().List() }
