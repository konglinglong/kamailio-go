// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * NDBRedis module - Redis-style NDB access.
 * Port of the kamailio ndb_redis module (src/modules/ndb_redis).
 *
 * This implementation keeps an in-memory key/value store so tests can
 * inspect what would have been stored. Init records the address and marks
 * the module connected; Set/Get/Del operate on the in-memory map.
 *
 * The module is safe for concurrent use.
 */

package ndb_redis

import (
	"errors"
	"sync"
)

// NDBRedisModule is an in-memory Redis-like store.
type NDBRedisModule struct {
	mu        sync.RWMutex
	addr      string
	store     map[string]string
	connected bool
}

// New creates an NDBRedisModule that is not yet connected.
func New() *NDBRedisModule {
	return &NDBRedisModule{store: make(map[string]string)}
}

// Init configures the address and marks the module connected.
func (m *NDBRedisModule) Init(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addr = addr
	m.connected = true
	m.store = make(map[string]string)
}

// IsConnected reports whether Init has been called.
func (m *NDBRedisModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Set stores value under key. Returns an error when not connected or key
// is empty.
func (m *NDBRedisModule) Set(key, value string) error {
	if key == "" {
		return errors.New("ndb_redis: empty key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("ndb_redis: not connected")
	}
	m.store[key] = value
	return nil
}

// Get returns the value for key. The boolean is false when the key is
// absent or the module is not connected.
func (m *NDBRedisModule) Get(key string) (string, error) {
	if key == "" {
		return "", errors.New("ndb_redis: empty key")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.connected {
		return "", errors.New("ndb_redis: not connected")
	}
	v, ok := m.store[key]
	if !ok {
		return "", nil
	}
	return v, nil
}

// Del removes key. Returns true when a key was removed.
func (m *NDBRedisModule) Del(key string) error {
	if key == "" {
		return errors.New("ndb_redis: empty key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("ndb_redis: not connected")
	}
	delete(m.store, key)
	return nil
}

// Exists reports whether key is present.
func (m *NDBRedisModule) Exists(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.store[key]
	return ok
}

// Close marks the module disconnected.
func (m *NDBRedisModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *NDBRedisModule
)

// DefaultNDBRedis returns the process-wide module, creating it on first use.
func DefaultNDBRedis() *NDBRedisModule {
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

// Init is the package-level (re)initialiser.
func Init(addr string) { DefaultNDBRedis().Init(addr) }

// Set is the package-level wrapper.
func Set(key, value string) error { return DefaultNDBRedis().Set(key, value) }

// Get is the package-level wrapper.
func Get(key string) (string, error) { return DefaultNDBRedis().Get(key) }

// Del is the package-level wrapper.
func Del(key string) error { return DefaultNDBRedis().Del(key) }

// IsConnected is the package-level wrapper.
func IsConnected() bool { return DefaultNDBRedis().IsConnected() }
