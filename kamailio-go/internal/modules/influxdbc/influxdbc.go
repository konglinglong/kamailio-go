// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * influxdbc module - InfluxDB line-protocol client.
 *
 * A simplified InfluxDB client. Init configures the server address and
 * database; Write buffers points in the line-protocol style; Query
 * returns previously written rows matching a measurement. No network
 * I/O is performed, so the module is functional for testing. It is
 * safe for concurrent use.
 */

package influxdbc

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// influxPoint is a single written data point.
type influxPoint struct {
	measurement string
	tags        map[string]string
	fields      map[string]string
}

// InfluxDBCModule is an in-memory InfluxDB client.
type InfluxDBCModule struct {
	mu        sync.RWMutex
	addr      string
	db        string
	connected bool
	points    []influxPoint
}

// New creates an InfluxDBCModule with no connection.
func New() *InfluxDBCModule {
	return &InfluxDBCModule{}
}

// Init configures the server address and database and marks the module
// connected. An empty addr leaves the module disconnected.
//
//	C: influxdbc_init()
func (m *InfluxDBCModule) Init(addr, db string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addr = addr
	m.db = db
	m.connected = addr != ""
}

// IsConnected reports whether the module has an active connection.
//
//	C: influxdbc_is_connected()
func (m *InfluxDBCModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Write stores a data point. Returns an error when not connected or the
// measurement is empty.
//
//	C: influxdbc_write()
func (m *InfluxDBCModule) Write(measurement string, tags, fields map[string]string) error {
	if measurement == "" {
		return fmt.Errorf("influxdbc: empty measurement")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return fmt.Errorf("influxdbc: not connected")
	}
	pt := influxPoint{measurement: measurement, tags: copyMap(tags), fields: copyMap(fields)}
	m.points = append(m.points, pt)
	return nil
}

// Query returns rows matching q. q is matched as a substring against the
// measurement of each stored point (e.g. "cpu" matches measurement "cpu").
// Each row is [measurement, tagK=tagV..., fieldK=fieldV...].
//
//	C: influxdbc_query()
func (m *InfluxDBCModule) Query(q string) ([][]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.connected {
		return nil, fmt.Errorf("influxdbc: not connected")
	}
	var out [][]string
	for _, pt := range m.points {
		if q != "" && !strings.Contains(pt.measurement, q) {
			continue
		}
		row := []string{pt.measurement}
		// deterministic ordering
		tkeys := sortedKeys(pt.tags)
		for _, k := range tkeys {
			row = append(row, k+"="+pt.tags[k])
		}
		fkeys := sortedKeys(pt.fields)
		for _, k := range fkeys {
			row = append(row, k+"="+pt.fields[k])
		}
		out = append(out, row)
	}
	return out, nil
}

// copyMap returns a shallow copy of in.
func copyMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// sortedKeys returns the keys of m sorted lexicographically.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultInfluxDBC *InfluxDBCModule
)

// DefaultInfluxDBC returns the process-wide InfluxDBCModule, creating it
// on first use.
func DefaultInfluxDBC() *InfluxDBCModule {
	defaultMu.RLock()
	m := defaultInfluxDBC
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultInfluxDBC == nil {
		defaultInfluxDBC = New()
	}
	return defaultInfluxDBC
}

// Init (re)initialises the process-wide InfluxDBCModule to a fresh,
// unconfigured state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultInfluxDBC = New()
}
