// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Oracle database backend - matching C db_oracle (db_oracle.c)
 *
 * Provides an Oracle driver that implements the generic db.DBDriver /
 * db.DBConn interfaces. This is a mock implementation: no real Oracle
 * (OCI) connection is established. Rows are kept in-memory so the connection
 * can be exercised by tests without a running database.
 *
 * C equivalent: db_oracle.so - registers itself via db_oracle_bind_api and
 * exposes connection / query primitives through the srdb1 abstraction.
 */

package db_oracle

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// DefaultOraclePort is the default Oracle listener port.
const DefaultOraclePort = 1521

// DefaultMaxOpenConns is the default maximum number of open connections.
const DefaultMaxOpenConns = 10

// DefaultOracleVersion is the mock server version reported by GetVersion.
const DefaultOracleVersion = "Oracle Database 19c (mock)"

// OracleConfig holds Oracle connection parameters.
//
// C equivalent: the Oracle connect string parsed fields (host, port, SID,
// username, password) plus the connection pool size.
type OracleConfig struct {
	Host         string
	Port         int
	SID          string
	User         string
	Password     string
	MaxOpenConns int
}

// DefaultOracleConfig returns a config with sensible defaults.
func DefaultOracleConfig() *OracleConfig {
	return &OracleConfig{
		Host:         "localhost",
		Port:         DefaultOraclePort,
		SID:          "ORCL",
		User:         "kamailio",
		Password:     "",
		MaxOpenConns: DefaultMaxOpenConns,
	}
}

// Validate checks required config fields.
func (c *OracleConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("oracle config: nil")
	}
	if c.Host == "" {
		return fmt.Errorf("oracle config: host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("oracle config: invalid port %d", c.Port)
	}
	if c.SID == "" {
		return fmt.Errorf("oracle config: sid is required")
	}
	if c.User == "" {
		return fmt.Errorf("oracle config: user is required")
	}
	if c.MaxOpenConns < 0 {
		return fmt.Errorf("oracle config: invalid maxOpenConns %d", c.MaxOpenConns)
	}
	return nil
}

// DSN returns the Oracle EZConnect-style connection string:
//
//	user/password@host:port/SID
func (c *OracleConfig) DSN() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s@%s:%d/%s", c.User, c.Password, c.Host, c.Port, c.SID)
}

// ---------------------------------------------------------------------------
// OracleConn - implements db.DBConn against an in-memory mock store
// ---------------------------------------------------------------------------

// OracleConn implements db.DBConn. Data is stored in-memory so the
// connection can be exercised without a real Oracle server.
//
// C equivalent: struct ora_con which embeds the OCI handles.
type OracleConn struct {
	mu      sync.RWMutex
	cfg     *OracleConfig
	closed  bool
	version string
	data    map[string][]*db.DBRow
}

// Compile-time interface check.
var _ db.DBConn = (*OracleConn)(nil)

// Connect initializes the mock connection. It performs no real OCI I/O and
// returns nil on a valid config.
func (c *OracleConn) Connect(cfg *OracleConfig) error {
	if c == nil {
		return fmt.Errorf("nil oracle conn")
	}
	if cfg == nil {
		return fmt.Errorf("nil oracle config")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = cfg
	c.closed = false
	c.version = DefaultOracleVersion
	if c.data == nil {
		c.data = make(map[string][]*db.DBRow)
	}
	return nil
}

// GetDSN returns the Oracle connection string.
func (c *OracleConn) GetDSN() string {
	if c == nil || c.cfg == nil {
		return ""
	}
	return c.cfg.DSN()
}

// GetVersion returns the mock server version string.
func (c *OracleConn) GetVersion() (string, error) {
	if c == nil {
		return "", fmt.Errorf("nil oracle conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return "", fmt.Errorf("oracle conn closed")
	}
	if c.version == "" {
		return DefaultOracleVersion, nil
	}
	return c.version, nil
}

// Query selects rows from a table matching the where clause.
func (c *OracleConn) Query(table string, keys []db.DBKey, where []db.DBCondition, orderBy string, limit, offset int) (*db.DBResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil oracle conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return nil, fmt.Errorf("oracle conn closed")
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
func (c *OracleConn) Insert(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil {
		return fmt.Errorf("nil oracle conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("insert: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("oracle conn closed")
	}
	c.data[table] = append(c.data[table], &db.DBRow{Keys: keys, Values: values})
	return nil
}

// Update updates rows matching the where clause.
func (c *OracleConn) Update(table string, keys []db.DBKey, values []db.DBValue, where []db.DBCondition) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("nil oracle conn")
	}
	if len(keys) != len(values) {
		return 0, fmt.Errorf("update: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, fmt.Errorf("oracle conn closed")
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
func (c *OracleConn) Delete(table string, where []db.DBCondition) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("nil oracle conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, fmt.Errorf("oracle conn closed")
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
func (c *OracleConn) Replace(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil {
		return fmt.Errorf("nil oracle conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("replace: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("oracle conn closed")
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
func (c *OracleConn) Raw(query string, args ...interface{}) (*db.DBResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil oracle conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return nil, fmt.Errorf("oracle conn closed")
	}
	return rowsToResult(c.data[query], nil), nil
}

// Close closes the connection.
func (c *OracleConn) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// Ping checks the connection is alive.
func (c *OracleConn) Ping() error {
	if c == nil {
		return fmt.Errorf("nil oracle conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return fmt.Errorf("oracle conn closed")
	}
	return nil
}

// ---------------------------------------------------------------------------
// OracleDriver - implements db.DBDriver
// ---------------------------------------------------------------------------

// OracleDriver implements db.DBDriver for Oracle.
//
// C equivalent: db_oracle_bind_api / the module export table.
type OracleDriver struct{}

// Compile-time interface check.
var _ db.DBDriver = (*OracleDriver)(nil)

// NewOracleDriver creates a new OracleDriver.
func NewOracleDriver() *OracleDriver {
	return &OracleDriver{}
}

// Name returns the driver name used for registration.
func (d *OracleDriver) Name() string {
	return "oracle"
}

// Open parses an oracle:// URL and returns a db.DBConn.
func (d *OracleDriver) Open(url string) (db.DBConn, error) {
	cfg, err := parseOracleURL(url)
	if err != nil {
		return nil, err
	}
	conn := &OracleConn{}
	if err := conn.Connect(cfg); err != nil {
		return nil, err
	}
	return conn, nil
}

// parseOracleURL parses an oracle:// URL into an OracleConfig.
func parseOracleURL(rawurl string) (*OracleConfig, error) {
	cfg := DefaultOracleConfig()
	if rawurl == "" {
		return cfg, nil
	}
	if !strings.HasPrefix(rawurl, "oracle://") {
		return cfg, nil
	}
	rest := strings.TrimPrefix(rawurl, "oracle://")
	var creds, hostpart string
	if idx := strings.Index(rest, "@"); idx >= 0 {
		creds = rest[:idx]
		hostpart = rest[idx+1:]
	} else {
		hostpart = rest
	}
	if creds != "" {
		if idx := strings.Index(creds, ":"); idx >= 0 {
			cfg.User = creds[:idx]
			cfg.Password = creds[idx+1:]
		} else {
			cfg.User = creds
		}
	}
	var sidPart string
	if idx := strings.Index(hostpart, "/"); idx >= 0 {
		sidPart = hostpart[idx+1:]
		hostpart = hostpart[:idx]
	}
	if hostpart != "" {
		if idx := strings.Index(hostpart, ":"); idx >= 0 {
			cfg.Host = hostpart[:idx]
			fmt.Sscanf(hostpart[idx+1:], "%d", &cfg.Port)
		} else {
			cfg.Host = hostpart
		}
	}
	if sidPart != "" {
		cfg.SID = sidPart
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

// init registers the Oracle driver with the global db registry.
func init() {
	_ = db.RegisterDriver(NewOracleDriver())
}
