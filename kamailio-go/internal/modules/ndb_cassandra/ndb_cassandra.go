// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * NDBCassandra module - Cassandra-style NDB (NoSQL database) access.
 * Port of the kamailio ndb_cassandra module (src/modules/ndb_cassandra).
 *
 * This implementation buffers CQL statements in memory so tests can
 * inspect what would have been executed. Init records the cluster hosts
 * and marks the module connected; Query returns a buffered result row
 * set; Execute records a statement without returning rows.
 *
 * The module is safe for concurrent use.
 */

package ndb_cassandra

import (
	"errors"
	"strings"
	"sync"
)

// Result is a buffered query result.
type Result struct {
	CQL   string
	Rows  []map[string]interface{}
}

// NDBCassandraModule buffers CQL statements.
type NDBCassandraModule struct {
	mu       sync.RWMutex
	hosts    string
	queries  []Result
	executed []string
	connected bool
}

// New creates an NDBCassandraModule that is not yet connected.
func New() *NDBCassandraModule {
	return &NDBCassandraModule{}
}

// Init configures the cluster hosts and marks the module connected.
func (m *NDBCassandraModule) Init(hosts string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hosts = hosts
	m.connected = true
	m.queries = nil
	m.executed = nil
}

// IsConnected reports whether Init has been called.
func (m *NDBCassandraModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Query records a SELECT-style CQL statement and returns a synthetic
// result set. The result is a single row echoing the statement under the
// key "cql" so callers can verify round-tripping. Returns an error when
// not connected or cql is empty.
func (m *NDBCassandraModule) Query(cql string) (interface{}, error) {
	cql = strings.TrimSpace(cql)
	if cql == "" {
		return nil, errors.New("ndb_cassandra: empty cql")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return nil, errors.New("ndb_cassandra: not connected")
	}
	res := Result{CQL: cql, Rows: []map[string]interface{}{{"cql": cql}}}
	m.queries = append(m.queries, res)
	return res, nil
}

// Execute records a DDL/DML-style CQL statement. Returns an error when
// not connected or cql is empty.
func (m *NDBCassandraModule) Execute(cql string) error {
	cql = strings.TrimSpace(cql)
	if cql == "" {
		return errors.New("ndb_cassandra: empty cql")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("ndb_cassandra: not connected")
	}
	m.executed = append(m.executed, cql)
	return nil
}

// Queries returns a copy of buffered query results.
func (m *NDBCassandraModule) Queries() []Result {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Result, len(m.queries))
	copy(out, m.queries)
	return out
}

// Executed returns a copy of buffered executed statements.
func (m *NDBCassandraModule) Executed() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, len(m.executed))
	copy(out, m.executed)
	return out
}

// Close marks the module disconnected.
func (m *NDBCassandraModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *NDBCassandraModule
)

// DefaultNDBCassandra returns the process-wide module, creating it on first use.
func DefaultNDBCassandra() *NDBCassandraModule {
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
func Init(hosts string) { DefaultNDBCassandra().Init(hosts) }

// Query is the package-level wrapper.
func Query(cql string) (interface{}, error) { return DefaultNDBCassandra().Query(cql) }

// Execute is the package-level wrapper.
func Execute(cql string) error { return DefaultNDBCassandra().Execute(cql) }

// IsConnected is the package-level wrapper.
func IsConnected() bool { return DefaultNDBCassandra().IsConnected() }
