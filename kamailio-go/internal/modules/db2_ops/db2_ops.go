// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * DB2 operations module - in-memory relational table operations.
 * Port of the kamailio db2_ops module (src/modules/db2_ops).
 *
 * The module stores named tables as slices of row maps and provides
 * query/insert/update/delete operations with simple equality conditions.
 * It is safe for concurrent use.
 */

package db2_ops

import (
	"errors"
	"sync"
)

// DB2OpsModule maintains a set of in-memory tables.
type DB2OpsModule struct {
	mu     sync.RWMutex
	tables map[string][]map[string]string
}

// New creates a DB2OpsModule with empty storage.
func New() *DB2OpsModule {
	return &DB2OpsModule{tables: make(map[string][]map[string]string)}
}

// Query returns the rows of the given table that satisfy all conditions.
// When conditions is empty, all rows are returned. Each returned row is
// a copy so callers cannot mutate the stored data.
//
//	C: db2_query()
func (m *DB2OpsModule) Query(table string, conditions map[string]string) ([]map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rows, ok := m.tables[table]
	if !ok {
		return nil, errors.New("db2_ops: unknown table: " + table)
	}
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		if matches(row, conditions) {
			out = append(out, copyRow(row))
		}
	}
	return out, nil
}

// Insert appends a new row to the given table, creating the table when
// necessary.
//
//	C: db2_insert()
func (m *DB2OpsModule) Insert(table string, data map[string]string) error {
	if table == "" {
		return errors.New("db2_ops: empty table name")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tables == nil {
		m.tables = make(map[string][]map[string]string)
	}
	m.tables[table] = append(m.tables[table], copyRow(data))
	return nil
}

// Update modifies the rows of the given table that satisfy conditions,
// applying the values from data. It returns the number of updated rows.
//
//	C: db2_update()
func (m *DB2OpsModule) Update(table string, conditions, data map[string]string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, ok := m.tables[table]
	if !ok {
		return 0, errors.New("db2_ops: unknown table: " + table)
	}
	count := 0
	for _, row := range rows {
		if matches(row, conditions) {
			for k, v := range data {
				row[k] = v
			}
			count++
		}
	}
	return count, nil
}

// Delete removes the rows of the given table that satisfy conditions and
// returns the number of removed rows.
//
//	C: db2_delete()
func (m *DB2OpsModule) Delete(table string, conditions map[string]string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, ok := m.tables[table]
	if !ok {
		return 0, errors.New("db2_ops: unknown table: " + table)
	}
	kept := rows[:0]
	count := 0
	for _, row := range rows {
		if matches(row, conditions) {
			count++
			continue
		}
		kept = append(kept, row)
	}
	m.tables[table] = kept
	return count, nil
}

// matches returns true when row contains every key/value in conditions.
func matches(row map[string]string, conditions map[string]string) bool {
	for k, v := range conditions {
		if row[k] != v {
			return false
		}
	}
	return true
}

// copyRow returns a shallow copy of the given row.
func copyRow(row map[string]string) map[string]string {
	out := make(map[string]string, len(row))
	for k, v := range row {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *DB2OpsModule
)

// DefaultDB2Ops returns the process-wide DB2OpsModule.
func DefaultDB2Ops() *DB2OpsModule {
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

// Init (re)initialises the process-wide DB2OpsModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Query is the package-level wrapper around DefaultDB2Ops().Query.
func Query(table string, conditions map[string]string) ([]map[string]string, error) {
	return DefaultDB2Ops().Query(table, conditions)
}

// Insert is the package-level wrapper around DefaultDB2Ops().Insert.
func Insert(table string, data map[string]string) error { return DefaultDB2Ops().Insert(table, data) }

// Update is the package-level wrapper around DefaultDB2Ops().Update.
func Update(table string, conditions, data map[string]string) (int, error) {
	return DefaultDB2Ops().Update(table, conditions, data)
}

// Delete is the package-level wrapper around DefaultDB2Ops().Delete.
func Delete(table string, conditions map[string]string) (int, error) {
	return DefaultDB2Ops().Delete(table, conditions)
}
