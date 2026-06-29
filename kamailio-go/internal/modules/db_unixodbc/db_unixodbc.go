// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * unixODBC database backend - matching C db_unixodbc (db_unixodbc.c)
 *
 * Provides an ODBC driver that implements the generic db.DBDriver /
 * db.DBConn interfaces. This is a mock implementation: no real ODBC
 * connection is established. Rows are kept in-memory so the connection can
 * be exercised by tests without a running database.
 *
 * C equivalent: db_unixodbc.so - registers itself via db_unixodbc_bind_api
 * and exposes connection / query primitives through the srdb1 abstraction.
 */

package db_unixodbc

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// DefaultMaxOpenConns is the default maximum number of open connections.
const DefaultMaxOpenConns = 10

// ODBCConfig holds unixODBC connection parameters.
//
// C equivalent: the ODBC DSN plus credentials parsed from the connection
// URI, plus the ping_interval / auto_reconnect module knobs.
type ODBCConfig struct {
	DSN         string
	User        string
	Password    string
	MaxOpenConns int
}

// DefaultODBCConfig returns a config with sensible defaults.
func DefaultODBCConfig() *ODBCConfig {
	return &ODBCConfig{
		DSN:          "kamailio",
		User:         "kamailio",
		Password:     "",
		MaxOpenConns: DefaultMaxOpenConns,
	}
}

// Validate checks required config fields.
func (c *ODBCConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("unixodbc config: nil")
	}
	if c.DSN == "" {
		return fmt.Errorf("unixodbc config: dsn is required")
	}
	if c.User == "" {
		return fmt.Errorf("unixodbc config: user is required")
	}
	if c.MaxOpenConns < 0 {
		return fmt.Errorf("unixodbc config: invalid maxOpenConns %d", c.MaxOpenConns)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ODBCConn - implements db.DBConn against an in-memory mock store
// ---------------------------------------------------------------------------

// ODBCConn implements db.DBConn. Data is stored in-memory so the connection
// can be exercised without a real ODBC data source.
//
// C equivalent: struct db_unixodbc_con which embeds the SQLHDBC handle.
type ODBCConn struct {
	mu     sync.RWMutex
	cfg    *ODBCConfig
	closed bool
	data   map[string][]*db.DBRow
}

// Compile-time interface check.
var _ db.DBConn = (*ODBCConn)(nil)

// Connect initializes the mock connection. It performs no real ODBC I/O and
// returns nil on a valid config.
func (c *ODBCConn) Connect(cfg *ODBCConfig) error {
	if c == nil {
		return fmt.Errorf("nil unixodbc conn")
	}
	if cfg == nil {
		return fmt.Errorf("nil unixodbc config")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = cfg
	c.closed = false
	if c.data == nil {
		c.data = make(map[string][]*db.DBRow)
	}
	return nil
}

// GetDSN returns the configured DSN.
func (c *ODBCConn) GetDSN() string {
	if c == nil || c.cfg == nil {
		return ""
	}
	return c.cfg.DSN
}

// Query selects rows from a table matching the where clause.
func (c *ODBCConn) Query(table string, keys []db.DBKey, where []db.DBCondition, orderBy string, limit, offset int) (*db.DBResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil unixodbc conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return nil, fmt.Errorf("unixodbc conn closed")
	}
	var matched []*db.DBRow
	for _, r := range c.data[table] {
		if rowMatches(r, where) {
			matched = append(matched, r)
		}
	}
	if offset > 0 {
		if offset >= len(matched) {
			matched = nil
		} else {
			matched = matched[offset:]
		}
	}
	if limit > 0 && limit < len(matched) {
		matched = matched[:limit]
	}
	return rowsToResult(matched, keys), nil
}

// Insert inserts a row.
func (c *ODBCConn) Insert(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil {
		return fmt.Errorf("nil unixodbc conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("insert: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("unixodbc conn closed")
	}
	c.data[table] = append(c.data[table], &db.DBRow{Keys: keys, Values: values})
	return nil
}

// Update updates rows matching the where clause.
func (c *ODBCConn) Update(table string, keys []db.DBKey, values []db.DBValue, where []db.DBCondition) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("nil unixodbc conn")
	}
	if len(keys) != len(values) {
		return 0, fmt.Errorf("update: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, fmt.Errorf("unixodbc conn closed")
	}
	var n int64
	for _, r := range c.data[table] {
		if rowMatches(r, where) {
			applyUpdate(r, keys, values)
			n++
		}
	}
	return n, nil
}

// Delete removes rows matching the where clause.
func (c *ODBCConn) Delete(table string, where []db.DBCondition) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("nil unixodbc conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, fmt.Errorf("unixodbc conn closed")
	}
	rows := c.data[table]
	kept := make([]*db.DBRow, 0, len(rows))
	var n int64
	for _, r := range rows {
		if rowMatches(r, where) {
			n++
			continue
		}
		kept = append(kept, r)
	}
	c.data[table] = kept
	return n, nil
}

// Replace inserts or updates a row, matching on the first key.
func (c *ODBCConn) Replace(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil {
		return fmt.Errorf("nil unixodbc conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("replace: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("unixodbc conn closed")
	}
	if len(keys) > 0 {
		pk := keys[0]
		pv := values[0]
		for _, r := range c.data[table] {
			if r.Get(pk.Name).String() == pv.String() {
				applyUpdate(r, keys, values)
				return nil
			}
		}
	}
	c.data[table] = append(c.data[table], &db.DBRow{Keys: keys, Values: values})
	return nil
}

// Raw executes a raw query. This mock returns the full table contents when
// the query string matches a table name.
func (c *ODBCConn) Raw(query string, args ...interface{}) (*db.DBResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil unixodbc conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return nil, fmt.Errorf("unixodbc conn closed")
	}
	return rowsToResult(c.data[query], nil), nil
}

// Close closes the connection.
func (c *ODBCConn) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// Ping checks the connection is alive.
func (c *ODBCConn) Ping() error {
	if c == nil {
		return fmt.Errorf("nil unixodbc conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return fmt.Errorf("unixodbc conn closed")
	}
	return nil
}

// ---------------------------------------------------------------------------
// ODBCDriver - implements db.DBDriver
// ---------------------------------------------------------------------------

// ODBCDriver implements db.DBDriver for unixODBC.
//
// C equivalent: db_unixodbc_bind_api / the module export table.
type ODBCDriver struct{}

// Compile-time interface check.
var _ db.DBDriver = (*ODBCDriver)(nil)

// NewODBCDriver creates a new ODBCDriver.
func NewODBCDriver() *ODBCDriver {
	return &ODBCDriver{}
}

// Name returns the driver name used for registration.
func (d *ODBCDriver) Name() string {
	return "unixodbc"
}

// Open parses a unixodbc:// URL (or treats the URL as a DSN) and returns a
// db.DBConn.
func (d *ODBCDriver) Open(url string) (db.DBConn, error) {
	cfg, err := parseODBCURL(url)
	if err != nil {
		return nil, err
	}
	conn := &ODBCConn{}
	if err := conn.Connect(cfg); err != nil {
		return nil, err
	}
	return conn, nil
}

// parseODBCURL parses a unixodbc:// URL into an ODBCConfig. A raw string
// (no scheme) is treated as a DSN name with default credentials.
func parseODBCURL(rawurl string) (*ODBCConfig, error) {
	cfg := DefaultODBCConfig()
	if rawurl == "" {
		return cfg, nil
	}
	if !strings.HasPrefix(rawurl, "unixodbc://") {
		cfg.DSN = rawurl
		return cfg, nil
	}
	rest := strings.TrimPrefix(rawurl, "unixodbc://")
	var creds, dsnpart string
	if idx := strings.Index(rest, "@"); idx >= 0 {
		creds = rest[:idx]
		dsnpart = rest[idx+1:]
	} else {
		dsnpart = rest
	}
	if creds != "" {
		if idx := strings.Index(creds, ":"); idx >= 0 {
			cfg.User = creds[:idx]
			cfg.Password = creds[idx+1:]
		} else {
			cfg.User = creds
		}
	}
	if dsnpart != "" {
		cfg.DSN = dsnpart
	}
	return cfg, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// rowMatches reports whether a row satisfies all where conditions.
func rowMatches(row *db.DBRow, where []db.DBCondition) bool {
	for _, cond := range where {
		if !compareValues(row.Get(cond.Key).String(), cond.Op, cond.Value.String()) {
			return false
		}
	}
	return true
}

// compareValues compares two strings using the given operator.
func compareValues(a, op, b string) bool {
	switch strings.ToUpper(strings.TrimSpace(op)) {
	case "", "=", "==":
		return a == b
	case "!=", "<>":
		return a != b
	case "<":
		return a < b
	case ">":
		return a > b
	case "<=":
		return a <= b
	case ">=":
		return a >= b
	case "LIKE":
		return likeMatch(a, b)
	default:
		return a == b
	}
}

// likeMatch implements a simple SQL LIKE with % and _ wildcards.
func likeMatch(s, pattern string) bool {
	expr := "^"
	for _, r := range pattern {
		switch r {
		case '%':
			expr += ".*"
		case '_':
			expr += "."
		default:
			expr += regexp.QuoteMeta(string(r))
		}
	}
	expr += "$"
	re, err := regexp.Compile(expr)
	if err != nil {
		return s == pattern
	}
	return re.MatchString(s)
}

// rowsToResult builds a DBResult from a slice of rows, projecting the given
// keys (or the row's own keys when none are requested).
func rowsToResult(rows []*db.DBRow, keys []db.DBKey) *db.DBResult {
	res := &db.DBResult{}
	cols := keys
	if len(cols) == 0 && len(rows) > 0 {
		cols = rows[0].Keys
	}
	res.Keys = cols
	for _, r := range rows {
		vals := make([]db.DBValue, len(cols))
		for i, k := range cols {
			vals[i] = r.Get(k.Name)
		}
		res.Rows = append(res.Rows, &db.DBRow{Keys: cols, Values: vals})
	}
	return res
}

// applyUpdate merges the given key/value pairs into a row.
func applyUpdate(row *db.DBRow, keys []db.DBKey, values []db.DBValue) {
	for i, k := range keys {
		idx := -1
		for j, rk := range row.Keys {
			if rk.Name == k.Name {
				idx = j
				break
			}
		}
		if idx >= 0 {
			row.Values[idx] = values[i]
		} else {
			row.Keys = append(row.Keys, k)
			row.Values = append(row.Values, values[i])
		}
	}
}

// init registers the unixODBC driver with the global db registry.
func init() {
	_ = db.RegisterDriver(NewODBCDriver())
}
