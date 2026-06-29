// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * matrix - two-dimensional string lookup table.
 *
 * Stores values keyed by (row, col) string pairs, mirroring the kamailio
 * matrix module used for routing decisions based on source/destination.
 */

package matrix

import (
	"errors"
	"sync"
)

// MatrixModule is a concurrent-safe (row, col) -> value map.
type MatrixModule struct {
	mu   sync.RWMutex
	data map[string]map[string]string
}

// New returns a new MatrixModule.
func New() *MatrixModule {
	return &MatrixModule{data: make(map[string]map[string]string)}
}

// Lookup returns the value at (row, col) or an error if no entry exists.
func (m *MatrixModule) Lookup(row, col string) (string, error) {
	if m == nil {
		return "", errors.New("matrix: nil module")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.data[row]
	if !ok {
		return "", errors.New("matrix: row not found")
	}
	v, ok := r[col]
	if !ok {
		return "", errors.New("matrix: column not found")
	}
	return v, nil
}

// Set stores val at (row, col), creating the row if necessary.
func (m *MatrixModule) Set(row, col, val string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.data[row]
	if !ok {
		r = make(map[string]string)
		m.data[row] = r
	}
	r[col] = val
}

// Remove deletes the entry at (row, col). Returns true if an entry was
// removed. Empty rows are pruned automatically.
func (m *MatrixModule) Remove(row, col string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.data[row]
	if !ok {
		return false
	}
	if _, ok := r[col]; !ok {
		return false
	}
	delete(r, col)
	if len(r) == 0 {
		delete(m.data, row)
	}
	return true
}

// List returns a snapshot copy of the entire matrix.
func (m *MatrixModule) List() map[string]map[string]string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]map[string]string, len(m.data))
	for row, cols := range m.data {
		c := make(map[string]string, len(cols))
		for k, v := range cols {
			c[k] = v
		}
		out[row] = c
	}
	return out
}
