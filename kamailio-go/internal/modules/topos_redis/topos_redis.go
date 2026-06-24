// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * ToposRedis module - Redis-backed storage for topos.
 * Port of the kamailio topos_redis module (src/modules/topos_redis).
 *
 * The topos_redis module stores the topology-hiding state of SIP
 * dialogs (identified by Call-ID and From-tag) in a Redis instance.
 * This Go counterpart keeps an in-memory simulation of the Redis store
 * so that the module is fully testable without a live Redis server.
 * Init establishes the (simulated) connection; Store/Retrieve/Delete
 * only succeed while connected.
 *
 * It is safe for concurrent use.
 */

package topos_redis

import (
	"errors"
	"fmt"
	"sync"
)

// ToposRedisModule is the Redis-backed topos storage backend.
// C: struct module topos_redis
type ToposRedisModule struct {
	mu        sync.RWMutex
	connected bool
	addr     string
	data     map[string][]byte
}

// New creates a disconnected ToposRedisModule.
func New() *ToposRedisModule {
	return &ToposRedisModule{}
}

// Init establishes the (simulated) connection to addr. After a successful
// Init the module reports as connected and Store/Retrieve/Delete operate
// against the in-memory store.
//
//	C: redis_init() / topos_redis_connect()
func (m *ToposRedisModule) Init(addr string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addr = addr
	m.data = make(map[string][]byte)
	m.connected = true
}

// IsConnected reports whether Init has established a connection.
func (m *ToposRedisModule) IsConnected() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Store saves data for the dialog identified by (callID, fromTag). Returns
// an error when not connected or when callID is empty.
//
//	C: redis_store()
func (m *ToposRedisModule) Store(callID, fromTag string, data []byte) error {
	if m == nil {
		return errors.New("topos_redis: nil module")
	}
	if !m.IsConnected() {
		return errors.New("topos_redis: not connected")
	}
	if callID == "" {
		return errors.New("topos_redis: empty call-id")
	}
	key := recordKey(callID, fromTag)
	cp := make([]byte, len(data))
	copy(cp, data)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = cp
	return nil
}

// Retrieve returns the data stored for (callID, fromTag). Returns an error
// when not connected or no record exists.
//
//	C: redis_load()
func (m *ToposRedisModule) Retrieve(callID, fromTag string) ([]byte, error) {
	if m == nil {
		return nil, errors.New("topos_redis: nil module")
	}
	if !m.IsConnected() {
		return nil, errors.New("topos_redis: not connected")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := recordKey(callID, fromTag)
	data, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("topos_redis: no record for call-id %q tag %q", callID, fromTag)
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

// Delete removes every record whose Call-ID matches callID. Returns true
// when at least one record was removed. Returns false when not connected.
//
//	C: redis_clean()
func (m *ToposRedisModule) Delete(callID string) bool {
	if m == nil || !m.IsConnected() || callID == "" {
		return false
	}
	prefix := callID + "|"
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := false
	for key := range m.data {
		if key == callID || (len(key) > len(prefix) && key[:len(prefix)] == prefix) {
			delete(m.data, key)
			removed = true
		}
	}
	return removed
}

// recordKey produces a stable key from a Call-ID and From-tag.
func recordKey(callID, fromTag string) string {
	return callID + "|" + fromTag
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu          sync.RWMutex
	defaultToposRedis  *ToposRedisModule
)

// DefaultToposRedis returns the process-wide ToposRedisModule, creating it on
// first use.
func DefaultToposRedis() *ToposRedisModule {
	defaultMu.RLock()
	m := defaultToposRedis
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultToposRedis == nil {
		defaultToposRedis = New()
	}
	return defaultToposRedis
}

// Init (re)initialises the process-wide ToposRedisModule to a fresh,
// disconnected state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultToposRedis = New()
}
