// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * NDBMongo module - MongoDB-style NDB access.
 * Port of the kamailio ndb_mongodb module (src/modules/ndb_mongodb).
 *
 * This implementation buffers documents in memory keyed by
 * db.collection so tests can inspect what would have been stored.
 * Init records the connection URI and marks the module connected; Insert
 * appends a document; Find returns documents matching a simple filter.
 *
 * The module is safe for concurrent use.
 */

package ndb_mongodb

import (
	"errors"
	"fmt"
	"sync"
)

// NDBMongoModule buffers documents in memory.
type NDBMongoModule struct {
	mu        sync.RWMutex
	uri       string
	docs      map[string][]map[string]interface{}
	connected bool
}

// New creates an NDBMongoModule that is not yet connected.
func New() *NDBMongoModule {
	return &NDBMongoModule{docs: make(map[string][]map[string]interface{})}
}

// Init configures the connection URI and marks the module connected.
func (m *NDBMongoModule) Init(uri string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uri = uri
	m.connected = true
	m.docs = make(map[string][]map[string]interface{})
}

// IsConnected reports whether Init has been called.
func (m *NDBMongoModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// key returns the internal storage key for db.collection.
func key(db, collection string) string {
	return fmt.Sprintf("%s.%s", db, collection)
}

// Insert appends doc to the db.collection store. doc is deep-copied.
func (m *NDBMongoModule) Insert(db, collection string, doc interface{}) error {
	if db == "" || collection == "" {
		return errors.New("ndb_mongodb: empty db or collection")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("ndb_mongodb: not connected")
	}
	m.docs[key(db, collection)] = append(m.docs[key(db, collection)], toMap(doc))
	return nil
}

// Find returns documents in db.collection whose key/value pairs all match
// the supplied filter. A nil/empty filter matches everything.
func (m *NDBMongoModule) Find(db, collection string, filter map[string]interface{}) ([]interface{}, error) {
	if db == "" || collection == "" {
		return nil, errors.New("ndb_mongodb: empty db or collection")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.connected {
		return nil, errors.New("ndb_mongodb: not connected")
	}
	var out []interface{}
	for _, d := range m.docs[key(db, collection)] {
		if matchesFilter(d, filter) {
			out = append(out, copyMap(d))
		}
	}
	return out, nil
}

// Count returns the number of documents in db.collection.
func (m *NDBMongoModule) Count(db, collection string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.docs[key(db, collection)])
}

// Close marks the module disconnected.
func (m *NDBMongoModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
}

// toMap converts a document into a map[string]interface{}. Maps are
// copied; other values are wrapped under the key "value".
func toMap(doc interface{}) map[string]interface{} {
	if d, ok := doc.(map[string]interface{}); ok {
		return copyMap(d)
	}
	return map[string]interface{}{"value": doc}
}

// copyMap returns a shallow copy of m.
func copyMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// matchesFilter reports whether doc contains every key/value in filter.
func matchesFilter(doc map[string]interface{}, filter map[string]interface{}) bool {
	for k, v := range filter {
		got, ok := doc[k]
		if !ok || got != v {
			return false
		}
	}
	return true
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *NDBMongoModule
)

// DefaultNDBMongo returns the process-wide module, creating it on first use.
func DefaultNDBMongo() *NDBMongoModule {
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
func Init(uri string) { DefaultNDBMongo().Init(uri) }

// Insert is the package-level wrapper.
func Insert(db, collection string, doc interface{}) error {
	return DefaultNDBMongo().Insert(db, collection, doc)
}

// Find is the package-level wrapper.
func Find(db, collection string, filter map[string]interface{}) ([]interface{}, error) {
	return DefaultNDBMongo().Find(db, collection, filter)
}

// IsConnected is the package-level wrapper.
func IsConnected() bool { return DefaultNDBMongo().IsConnected() }
