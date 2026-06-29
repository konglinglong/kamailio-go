// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * db_sqlite module - SQLite-backed database driver.
 *
 * Port of the kamailio db_sqlite module (src/modules/db_sqlite). Provides a
 * db.DBDriver / db.DBConn implementation backed by an SQLite3 file (or
 * ":memory:") via database/sql and the pure-Go modernc.org/sqlite driver.
 *
 * C equivalent: db_sqlite.so - dbase.c / db_sqlite.c.
 */

package db_sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/db"

	_ "modernc.org/sqlite" // registers the "sqlite" driver with database/sql
)

// Config holds SQLite connection configuration.
//
// C equivalent: the db_filename / busy_timeout / journal_mode parameters.
type Config struct {
	Path            string        // database file path (":memory:" for RAM)
	MaxOpenConn     int           // max open connections
	MaxIdleConn     int           // max idle connections
	ConnMaxLifetime time.Duration // connection max lifetime
	BusyTimeout     time.Duration // SQLite busy timeout (pragma busy_timeout)
}

// DefaultConfig returns a config with sensible Kamailio-style defaults.
func DefaultConfig() *Config {
	return &Config{
		Path:            ":memory:",
		MaxOpenConn:     4,
		MaxIdleConn:     2,
		ConnMaxLifetime: 5 * time.Minute,
		BusyTimeout:     5 * time.Second,
	}
}

// Validate checks required config fields.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("db_sqlite: nil config")
	}
	if strings.TrimSpace(c.Path) == "" {
		return errors.New("db_sqlite: path is required")
	}
	if c.MaxOpenConn < 0 {
		return fmt.Errorf("db_sqlite: invalid MaxOpenConn %d", c.MaxOpenConn)
	}
	if c.MaxIdleConn < 0 {
		return fmt.Errorf("db_sqlite: invalid MaxIdleConn %d", c.MaxIdleConn)
	}
	return nil
}

// ---------------------------------------------------------------------------
// SQLiteConn - implements db.DBConn on top of *sql.DB
// ---------------------------------------------------------------------------

// SQLiteConn wraps a *sql.DB and implements the db.DBConn interface.
//
// C equivalent: struct db_con_t as opened by db_sqlite_new_connection.
type SQLiteConn struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
	cfg  *Config
}

// Compile-time interface checks.
var (
	_ db.DBConn   = (*SQLiteConn)(nil)
	_ db.DBDriver = (*SQLiteDriver)(nil)
)

// ensureTable lazily creates a simple columnar schema if it does not yet
// exist, using the first column as PRIMARY KEY for idempotent replace
// semantics.
func (c *SQLiteConn) ensureTable(table string, keys []db.DBKey) error {
	if c == nil || c.db == nil {
		return errors.New("db_sqlite: nil conn")
	}
	if len(keys) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("CREATE TABLE IF NOT EXISTS ")
	sb.WriteString(quoteIdent(table))
	sb.WriteString(" (")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(quoteIdent(k.Name))
		sb.WriteString(" TEXT")
		if i == 0 {
			sb.WriteString(" PRIMARY KEY")
		}
	}
	sb.WriteString(")")
	_, err := c.db.Exec(sb.String())
	return err
}

// tableExists reports whether a table has been created yet.
func (c *SQLiteConn) tableExists(table string) (bool, error) {
	row := c.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table)
	var name string
	if err := row.Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// colsFor returns the column names of a table in declaration order.
func (c *SQLiteConn) colsFor(table string) ([]string, error) {
	rows, err := c.db.Query("SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}

// Query executes a SELECT. keys may be nil to select all columns.
func (c *SQLiteConn) Query(table string, keys []db.DBKey, where []db.DBCondition, orderBy string, limit, offset int) (*db.DBResult, error) {
	if c == nil || c.db == nil {
		return nil, errors.New("db_sqlite: nil conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	exists, err := c.tableExists(table)
	if err != nil {
		return nil, err
	}
	if !exists {
		return &db.DBResult{Rows: []*db.DBRow{}, Keys: keys}, nil
	}

	var selCols []string
	var selKeys []db.DBKey
	if len(keys) > 0 {
		for _, k := range keys {
			selCols = append(selCols, quoteIdent(k.Name))
			selKeys = append(selKeys, k)
		}
	} else {
		cols, err := c.colsFor(table)
		if err != nil {
			return nil, err
		}
		for _, col := range cols {
			selCols = append(selCols, quoteIdent(col))
			selKeys = append(selKeys, db.DBKey{Name: col, Type: db.DBValString})
		}
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(selCols, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(quoteIdent(table))

	var args []interface{}
	if len(where) > 0 {
		clause, wargs, err := buildWhere(where)
		if err != nil {
			return nil, err
		}
		sb.WriteString(" WHERE ")
		sb.WriteString(clause)
		args = append(args, wargs...)
	}
	if orderBy != "" {
		col := strings.TrimPrefix(orderBy, "+")
		desc := false
		if strings.HasPrefix(col, "-") {
			col = col[1:]
			desc = true
		}
		sb.WriteString(" ORDER BY ")
		sb.WriteString(quoteIdent(col))
		if desc {
			sb.WriteString(" DESC")
		} else {
			sb.WriteString(" ASC")
		}
	}
	if limit > 0 {
		fmt.Fprintf(&sb, " LIMIT %d", limit)
	}
	if offset > 0 {
		if limit <= 0 {
			sb.WriteString(" LIMIT -1")
		}
		fmt.Fprintf(&sb, " OFFSET %d", offset)
	}

	rows, err := c.db.Query(sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	outRows := make([]*db.DBRow, 0)
	for rows.Next() {
		raw := make([]interface{}, len(selKeys))
		ptrs := make([]interface{}, len(selKeys))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		values := make([]db.DBValue, len(selKeys))
		for i, v := range raw {
			values[i] = toDBValue(selKeys[i], v)
		}
		outRows = append(outRows, &db.DBRow{Keys: selKeys, Values: values})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &db.DBResult{Rows: outRows, Keys: selKeys}, nil
}

// Insert inserts a row, auto-creating the table from keys if needed.
func (c *SQLiteConn) Insert(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil || c.db == nil {
		return errors.New("db_sqlite: nil conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("db_sqlite insert: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureTable(table, keys); err != nil {
		return err
	}
	var cols, ph []string
	args := make([]interface{}, 0, len(values))
	for i, k := range keys {
		cols = append(cols, quoteIdent(k.Name))
		ph = append(ph, "?")
		args = append(args, values[i].String())
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(table), strings.Join(cols, ", "), strings.Join(ph, ", "))
	_, err := c.db.Exec(q, args...)
	return err
}

// Replace inserts a row, or replaces it if the primary key already exists.
func (c *SQLiteConn) Replace(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil || c.db == nil {
		return errors.New("db_sqlite: nil conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("db_sqlite replace: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureTable(table, keys); err != nil {
		return err
	}
	var cols, ph []string
	args := make([]interface{}, 0, len(values))
	for i, k := range keys {
		cols = append(cols, quoteIdent(k.Name))
		ph = append(ph, "?")
		args = append(args, values[i].String())
	}
	q := fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)",
		quoteIdent(table), strings.Join(cols, ", "), strings.Join(ph, ", "))
	_, err := c.db.Exec(q, args...)
	return err
}

// Update updates rows matching the where clause. Returns the affected count.
func (c *SQLiteConn) Update(table string, keys []db.DBKey, values []db.DBValue, where []db.DBCondition) (int64, error) {
	if c == nil || c.db == nil {
		return 0, errors.New("db_sqlite: nil conn")
	}
	if len(keys) != len(values) {
		return 0, fmt.Errorf("db_sqlite update: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	exists, err := c.tableExists(table)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	var sb strings.Builder
	sb.WriteString("UPDATE ")
	sb.WriteString(quoteIdent(table))
	sb.WriteString(" SET ")
	args := make([]interface{}, 0, len(values)+len(where))
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(quoteIdent(k.Name))
		sb.WriteString(" = ?")
		args = append(args, values[i].String())
	}
	if len(where) > 0 {
		clause, wargs, err := buildWhere(where)
		if err != nil {
			return 0, err
		}
		sb.WriteString(" WHERE ")
		sb.WriteString(clause)
		args = append(args, wargs...)
	}
	res, err := c.db.Exec(sb.String(), args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Delete deletes rows matching the where clause. Returns the affected count.
func (c *SQLiteConn) Delete(table string, where []db.DBCondition) (int64, error) {
	if c == nil || c.db == nil {
		return 0, errors.New("db_sqlite: nil conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	exists, err := c.tableExists(table)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	var sb strings.Builder
	sb.WriteString("DELETE FROM ")
	sb.WriteString(quoteIdent(table))
	var args []interface{}
	if len(where) > 0 {
		clause, wargs, err := buildWhere(where)
		if err != nil {
			return 0, err
		}
		sb.WriteString(" WHERE ")
		sb.WriteString(clause)
		args = wargs
	}
	res, err := c.db.Exec(sb.String(), args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Raw executes a raw SQL query and returns rows as DBResult.
func (c *SQLiteConn) Raw(query string, args ...interface{}) (*db.DBResult, error) {
	if c == nil || c.db == nil {
		return nil, errors.New("db_sqlite: nil conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	keys := make([]db.DBKey, len(cols))
	for i, col := range cols {
		keys[i] = db.DBKey{Name: col, Type: db.DBValString}
	}
	outRows := make([]*db.DBRow, 0)
	for rows.Next() {
		raw := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		values := make([]db.DBValue, len(cols))
		for i, v := range raw {
			values[i] = toDBValue(keys[i], v)
		}
		outRows = append(outRows, &db.DBRow{Keys: keys, Values: values})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &db.DBResult{Rows: outRows, Keys: keys}, nil
}

// Close closes the underlying connection. Safe on nil / closed conns.
func (c *SQLiteConn) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	err := c.db.Close()
	c.db = nil
	return err
}

// Ping verifies the underlying connection is alive.
func (c *SQLiteConn) Ping() error {
	if c == nil || c.db == nil {
		return errors.New("db_sqlite: nil conn")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return c.db.PingContext(ctx)
}

// ---------------------------------------------------------------------------
// SQLiteDriver - implements db.DBDriver
// ---------------------------------------------------------------------------

// SQLiteDriver implements db.DBDriver for SQLite. It caches *sql.DB handles
// by path so repeated Open calls reuse the same connection pool.
//
// C equivalent: db_sqlite_bind_api / the module export table.
type SQLiteDriver struct {
	mu     sync.RWMutex
	dbs    map[string]*sql.DB // cached pools keyed by path
	config Config
}

// NewSQLiteDriver creates a driver with default configuration.
func NewSQLiteDriver() *SQLiteDriver {
	cfg := DefaultConfig()
	return &SQLiteDriver{
		dbs:    make(map[string]*sql.DB),
		config: *cfg,
	}
}

// NewSQLiteDriverWithConfig creates a driver using the supplied configuration.
func NewSQLiteDriverWithConfig(cfg Config) *SQLiteDriver {
	return &SQLiteDriver{
		dbs:    make(map[string]*sql.DB),
		config: cfg,
	}
}

// Name returns the driver name used for registration.
func (d *SQLiteDriver) Name() string {
	return "sqlite"
}

// dbFor returns (creating if necessary) a *sql.DB for the given path.
func (d *SQLiteDriver) dbFor(path string) (*sql.DB, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if sqlDB, ok := d.dbs[path]; ok {
		return sqlDB, nil
	}
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("db_sqlite: open %q: %w", path, err)
	}
	if d.config.MaxOpenConn > 0 {
		sqlDB.SetMaxOpenConns(d.config.MaxOpenConn)
	}
	if d.config.MaxIdleConn > 0 {
		sqlDB.SetMaxIdleConns(d.config.MaxIdleConn)
	}
	if d.config.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(d.config.ConnMaxLifetime)
	}
	if d.config.BusyTimeout > 0 {
		// Best-effort: set the busy timeout pragma on the pool.
		ms := int(d.config.BusyTimeout / time.Millisecond)
		if _, err := sqlDB.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d", ms)); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("db_sqlite: set busy_timeout: %w", err)
		}
	}
	d.dbs[path] = sqlDB
	return sqlDB, nil
}

// Open opens (or creates) an SQLite database at path. ":memory:" yields an
// in-memory database.
func (d *SQLiteDriver) Open(path string) (db.DBConn, error) {
	if path == "" {
		path = d.config.Path
	}
	if path == "" {
		path = ":memory:"
	}
	sqlDB, err := d.dbFor(path)
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("db_sqlite: ping %q: %w", path, err)
	}
	return &SQLiteConn{db: sqlDB, path: path, cfg: &d.config}, nil
}

// CloseAll closes every cached *sql.DB. Useful for orderly shutdown.
func (d *SQLiteDriver) CloseAll() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var firstErr error
	for path, sqlDB := range d.dbs {
		if err := sqlDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(d.dbs, path)
	}
	return firstErr
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// quoteIdent double-quotes an identifier and escapes embedded quotes.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// buildWhere renders a slice of DBCondition to a WHERE clause fragment and the
// corresponding positional args. Conditions are joined with AND.
func buildWhere(where []db.DBCondition) (string, []interface{}, error) {
	var sb strings.Builder
	args := make([]interface{}, 0, len(where))
	for i, cond := range where {
		if i > 0 {
			sb.WriteString(" AND ")
		}
		sb.WriteString(quoteIdent(cond.Key))
		op := strings.ToUpper(strings.TrimSpace(cond.Op))
		switch op {
		case "", "=", "==":
			sb.WriteString(" = ?")
		case "!=", "<>":
			sb.WriteString(" <> ?")
		case "<", ">", "<=", ">=":
			sb.WriteString(" " + op + " ?")
		case "LIKE":
			sb.WriteString(" LIKE ?")
		default:
			return "", nil, fmt.Errorf("db_sqlite: unsupported operator %q", cond.Op)
		}
		args = append(args, cond.Value.String())
	}
	return sb.String(), args, nil
}

// toDBValue converts a value scanned from database/sql into a DBValue.
func toDBValue(key db.DBKey, v interface{}) db.DBValue {
	if v == nil {
		return db.DBValue{Type: db.DBValNull, IsNull: true}
	}
	switch raw := v.(type) {
	case []byte:
		switch key.Type {
		case db.DBValInt:
			if n, ok := parseIntBytes(raw); ok {
				return db.DBValue{Type: db.DBValInt, IntVal: n}
			}
		case db.DBValFloat:
			if f, ok := parseFloatBytes(raw); ok {
				return db.DBValue{Type: db.DBValFloat, FloatVal: f}
			}
		}
		return db.DBValue{Type: db.DBValString, StrVal: string(raw)}
	case string:
		switch key.Type {
		case db.DBValInt:
			if n, ok := parseIntBytes([]byte(raw)); ok {
				return db.DBValue{Type: db.DBValInt, IntVal: n}
			}
		case db.DBValFloat:
			if f, ok := parseFloatBytes([]byte(raw)); ok {
				return db.DBValue{Type: db.DBValFloat, FloatVal: f}
			}
		}
		return db.DBValue{Type: db.DBValString, StrVal: raw}
	case int64:
		if key.Type == db.DBValFloat {
			return db.DBValue{Type: db.DBValFloat, FloatVal: float64(raw)}
		}
		return db.DBValue{Type: db.DBValInt, IntVal: raw}
	case float64:
		return db.DBValue{Type: db.DBValFloat, FloatVal: raw}
	case bool:
		s := "0"
		if raw {
			s = "1"
		}
		return db.DBValue{Type: db.DBValString, StrVal: s}
	default:
		return db.DBValue{Type: db.DBValString, StrVal: fmt.Sprintf("%v", raw)}
	}
}

func parseIntBytes(b []byte) (int64, bool) {
	var n int64
	if _, err := fmt.Sscanf(string(b), "%d", &n); err == nil {
		return n, true
	}
	return 0, false
}

func parseFloatBytes(b []byte) (float64, bool) {
	var f float64
	if _, err := fmt.Sscanf(string(b), "%f", &f); err == nil {
		return f, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Package-level singleton (project pattern: New / Default* / Init)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultD  *SQLiteDriver
)

// DefaultSQLiteDriver returns the process-wide driver, creating it on first
// use.
func DefaultSQLiteDriver() *SQLiteDriver {
	defaultMu.RLock()
	d := defaultD
	defaultMu.RUnlock()
	if d != nil {
		return d
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultD == nil {
		defaultD = NewSQLiteDriver()
	}
	return defaultD
}

// Init (re)configures the package-wide driver with the supplied config. This
// closes any previously cached connection pools.
func Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultD != nil {
		_ = defaultD.CloseAll()
	}
	defaultD = &SQLiteDriver{
		dbs:    make(map[string]*sql.DB),
		config: cfg,
	}
	return nil
}

// init registers the driver with the global db registry. The error is ignored
// to allow the built-in core driver to coexist (best-effort registration).
func init() {
	_ = db.RegisterDriver(NewSQLiteDriver())
}
