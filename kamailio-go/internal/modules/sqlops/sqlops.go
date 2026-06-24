// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SQL operations module - registered SQL query execution.
 * Port of the kamailio sqlops module (src/modules/sqlops).
 *
 * The sqlops module lets the routing script run arbitrary SQL queries
 * against a configured database and inspect the result set by row/column.
 * Queries are registered by name at init time and executed at runtime,
 * optionally with parameters.
 *
 * This Go counterpart does not bind to a real database driver: execution
 * is delegated to a pluggable QueryExecutor function. The default executor
 * returns an empty result, which makes the module usable out of the box
 * and trivially mockable from tests.
 *
 * It is safe for concurrent use: the query and result maps are guarded by
 * a read/write lock and the process-wide singleton is guarded by a mutex.
 */

package sqlops

import (
	"errors"
	"fmt"
	"sync"
)

// SQLQuery describes a named, registered SQL query.
type SQLQuery struct {
	Name     string
	Query    string
	DBDriver string
}

// SQLResult holds the outcome of a query execution.
type SQLResult struct {
	Columns  []string
	Rows     [][]string
	Affected int64
}

// QueryExecutor executes a query with the supplied parameters and returns
// the result. Implementations may return a canned/mock result.
type QueryExecutor func(query string, params []interface{}) (*SQLResult, error)

// SQLOpsModule implements the sqlops module functionality.
// C: struct module sqlops
type SQLOpsModule struct {
	mu       sync.RWMutex
	queries  map[string]*SQLQuery
	results  map[string]*SQLResult
	executor QueryExecutor
}

// New creates a SQLOpsModule with an empty query set and a default
// (no-op) executor.
func New() *SQLOpsModule {
	return &SQLOpsModule{
		queries:  make(map[string]*SQLQuery),
		results:  make(map[string]*SQLResult),
		executor: defaultExecutor,
	}
}

// defaultExecutor returns an empty result. It is used when no real database
// is configured, keeping the module usable without a driver.
func defaultExecutor(query string, params []interface{}) (*SQLResult, error) {
	return &SQLResult{}, nil
}

// SetExecutor replaces the query executor. Passing nil restores the
// default (no-op) executor. This is the primary hook used by tests to
// inject a mock backend.
func (m *SQLOpsModule) SetExecutor(ex QueryExecutor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ex == nil {
		m.executor = defaultExecutor
		return
	}
	m.executor = ex
}

// RegisterQuery registers a named SQL query. Returns 0 on success or -1
// when name or query is empty, or when name is already registered.
//
//	C: sql_query_register() analogue
func (m *SQLOpsModule) RegisterQuery(name string, query string) int {
	if name == "" || query == "" {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.queries == nil {
		m.queries = make(map[string]*SQLQuery)
	}
	if _, exists := m.queries[name]; exists {
		return -1
	}
	m.queries[name] = &SQLQuery{Name: name, Query: query}
	return 0
}

// Execute runs the query registered under name with the supplied
// parameters and caches the result for later inspection. Returns an error
// when name is not registered or the executor fails.
//
//	C: sql_query_exec() analogue
func (m *SQLOpsModule) Execute(name string, params ...interface{}) (*SQLResult, error) {
	m.mu.Lock()
	q, ok := m.queries[name]
	exec := m.executor
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("sqlops: unknown query %q", name)
	}
	res, err := exec(q.Query, params)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.results == nil {
		m.results = make(map[string]*SQLResult)
	}
	m.results[name] = res
	m.mu.Unlock()
	return res, nil
}

// ExecuteRaw runs an ad-hoc query (not registered) with the supplied
// parameters and caches the result under the query string itself.
//
//	C: sql_query_exec_raw() analogue
func (m *SQLOpsModule) ExecuteRaw(query string, params ...interface{}) (*SQLResult, error) {
	if query == "" {
		return nil, errors.New("sqlops: empty query")
	}
	m.mu.RLock()
	exec := m.executor
	m.mu.RUnlock()
	res, err := exec(query, params)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.results == nil {
		m.results = make(map[string]*SQLResult)
	}
	m.results[query] = res
	m.mu.Unlock()
	return res, nil
}

// GetResult returns the cached result for the named query, or nil.
//
//	C: sql_result_get() analogue
func (m *SQLOpsModule) GetResult(name string) *SQLResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.results[name]
}

// RowCount returns the number of rows in the cached result for name, or 0
// when there is no cached result.
//
//	C: sql_result_row_count() analogue
func (m *SQLOpsModule) RowCount(name string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.results[name]
	if !ok || r == nil {
		return 0
	}
	return len(r.Rows)
}

// FieldValue returns the value at (row, col) of the cached result for
// name. The bool result is false when the coordinates are out of range or
// there is no cached result.
//
//	C: sql_result_field_value() analogue
func (m *SQLOpsModule) FieldValue(name string, row int, col int) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.results[name]
	if !ok || r == nil {
		return "", false
	}
	if row < 0 || row >= len(r.Rows) {
		return "", false
	}
	if col < 0 || col >= len(r.Rows[row]) {
		return "", false
	}
	return r.Rows[row][col], true
}

// ListQueries returns the names of every registered query.
func (m *SQLOpsModule) ListQueries() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.queries))
	for name := range m.queries {
		out = append(out, name)
	}
	return out
}

// Count returns the number of registered queries.
func (m *SQLOpsModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.queries)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu     sync.RWMutex
	defaultSQLOps *SQLOpsModule
)

// DefaultSQLOps returns the process-wide SQLOpsModule, creating it on first
// use.
func DefaultSQLOps() *SQLOpsModule {
	defaultMu.RLock()
	m := defaultSQLOps
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSQLOps == nil {
		defaultSQLOps = New()
	}
	return defaultSQLOps
}

// Init (re)initialises the process-wide SQLOpsModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSQLOps = New()
}
