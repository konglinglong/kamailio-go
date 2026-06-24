// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * MySQL database backend - matching C db_mysql (db_mysql.c / my_con.c)
 *
 * Provides a MySQL driver that bridges the generic db.DBDriver / db.DBConn
 * interfaces onto database/sql. The actual go-sql-driver/mysql import is
 * performed by the caller (blank import) so this package has no hard
 * dependency on a CGO-free / CGO MySQL client.
 *
 * C equivalent: db_mysql.so - registers itself via db_bind_mod and exposes
 * connection / query primitives through the srdb1 abstraction.
 */

package db_mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// DefaultMySQLPort is the default MySQL server port.
const DefaultMySQLPort = 3306

// MySQLConfig holds MySQL connection parameters.
//
// C equivalent: the my_uri parsed fields (host, port, username, password,
// database) plus the module-level pooling knobs my_connect_to etc.
type MySQLConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DefaultMySQLConfig returns a config with sensible Kamailio-style defaults.
func DefaultMySQLConfig() *MySQLConfig {
	return &MySQLConfig{
		Host:            "localhost",
		Port:            DefaultMySQLPort,
		User:            "root",
		Password:        "",
		Database:        "",
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
	}
}

// Validate checks required config fields and returns an error describing
// the first missing or invalid field.
func (c *MySQLConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("mysql config: nil")
	}
	if c.Host == "" {
		return fmt.Errorf("mysql config: host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("mysql config: invalid port %d", c.Port)
	}
	if c.User == "" {
		return fmt.Errorf("mysql config: user is required")
	}
	if c.Database == "" {
		return fmt.Errorf("mysql config: database is required")
	}
	return nil
}

// DSN returns the go-sql-driver/mysql DSN string:
//
//	user:password@tcp(host:port)/database
func (c *MySQLConfig) DSN() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", c.User, c.Password, c.Host, c.Port, c.Database)
}

// ---------------------------------------------------------------------------
// MySQLConn - implements db.DBConn on top of *sql.DB
// ---------------------------------------------------------------------------

// MySQLConn wraps a *sql.DB and implements the db.DBConn interface.
//
// C equivalent: struct my_con (my_con.h) which embeds the MYSQL handle.
type MySQLConn struct {
	mu  sync.RWMutex
	db  *sql.DB
	cfg *MySQLConfig
}

// Compile-time interface check.
var _ db.DBConn = (*MySQLConn)(nil)

// Query executes a SELECT.
func (c *MySQLConn) Query(table string, keys []db.DBKey, where []db.DBCondition, orderBy string, limit, offset int) (*db.DBResult, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("nil mysql conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	var selCols []string
	var selKeys []db.DBKey
	if len(keys) > 0 {
		for _, k := range keys {
			selCols = append(selCols, quoteIdent(k.Name))
			selKeys = append(selKeys, k)
		}
	} else {
		selCols = []string{"*"}
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
		sb.WriteString(" LIMIT ")
		fmt.Fprintf(&sb, "%d", limit)
	}
	if offset > 0 {
		sb.WriteString(" OFFSET ")
		fmt.Fprintf(&sb, "%d", offset)
	}

	rows, err := c.db.Query(sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	resultKeys := selKeys
	if resultKeys == nil {
		resultKeys = make([]db.DBKey, len(cols))
		for i, col := range cols {
			resultKeys[i] = db.DBKey{Name: col, Type: db.DBValString}
		}
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
			values[i] = toDBValue(resultKeys[i], v)
		}
		outRows = append(outRows, &db.DBRow{Keys: resultKeys, Values: values})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &db.DBResult{Rows: outRows, Keys: resultKeys}, nil
}

// Insert inserts a row.
func (c *MySQLConn) Insert(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("nil mysql conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("insert: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

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

// Update updates rows matching the where clause.
func (c *MySQLConn) Update(table string, keys []db.DBKey, values []db.DBValue, where []db.DBCondition) (int64, error) {
	if c == nil || c.db == nil {
		return 0, fmt.Errorf("nil mysql conn")
	}
	if len(keys) != len(values) {
		return 0, fmt.Errorf("update: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

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

// Delete deletes rows matching the where clause.
func (c *MySQLConn) Delete(table string, where []db.DBCondition) (int64, error) {
	if c == nil || c.db == nil {
		return 0, fmt.Errorf("nil mysql conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

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

// Replace inserts a row, or updates it on duplicate key.
func (c *MySQLConn) Replace(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("nil mysql conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("replace: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	var cols, ph []string
	args := make([]interface{}, 0, len(values))
	for i, k := range keys {
		cols = append(cols, quoteIdent(k.Name))
		ph = append(ph, "?")
		args = append(args, values[i].String())
	}
	q := fmt.Sprintf("REPLACE INTO %s (%s) VALUES (%s)",
		quoteIdent(table), strings.Join(cols, ", "), strings.Join(ph, ", "))
	_, err := c.db.Exec(q, args...)
	return err
}

// Raw executes a raw SQL query and returns rows as DBResult.
func (c *MySQLConn) Raw(query string, args ...interface{}) (*db.DBResult, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("nil mysql conn")
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

// Close closes the underlying connection.
func (c *MySQLConn) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	err := c.db.Close()
	c.db = nil
	return err
}

// Ping verifies the underlying connection is alive.
func (c *MySQLConn) Ping() error {
	if c == nil || c.db == nil {
		return fmt.Errorf("nil mysql conn")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.db.PingContext(ctx)
}

// ---------------------------------------------------------------------------
// MySQLDriver - implements db.DBDriver
// ---------------------------------------------------------------------------

// MySQLDriver implements db.DBDriver for MySQL.
//
// C equivalent: db_mysql_bind_api / the module export table.
type MySQLDriver struct{}

// Compile-time interface check.
var _ db.DBDriver = (*MySQLDriver)(nil)

// NewMySQLDriver creates a new MySQLDriver.
func NewMySQLDriver() *MySQLDriver {
	return &MySQLDriver{}
}

// Name returns the driver name used for registration.
func (d *MySQLDriver) Name() string {
	return "mysql"
}

// Open parses a mysql:// URL (or raw DSN) and returns a db.DBConn.
func (d *MySQLDriver) Open(url string) (db.DBConn, error) {
	cfg, err := parseMySQLURL(url)
	if err != nil {
		return nil, err
	}
	sqlDB, err := Connect(cfg)
	if err != nil {
		return nil, err
	}
	return &MySQLConn{db: sqlDB, cfg: cfg}, nil
}

// ---------------------------------------------------------------------------
// Package-level helpers
// ---------------------------------------------------------------------------

// Connect opens a *sql.DB using the mysql driver. The caller must blank-import
// go-sql-driver/mysql before calling this function.
func Connect(cfg *MySQLConfig) (*sql.DB, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil mysql config")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	return sqlDB, nil
}

// Ping checks if the database connection is alive.
func Ping(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("nil sql.DB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return db.PingContext(ctx)
}

// GetVersion returns the MySQL server version string (e.g. "8.0.31").
func GetVersion(db *sql.DB) (string, error) {
	if db == nil {
		return "", fmt.Errorf("nil sql.DB")
	}
	var version string
	if err := db.QueryRow("SELECT VERSION()").Scan(&version); err != nil {
		return "", fmt.Errorf("get mysql version: %w", err)
	}
	return version, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// quoteIdent wraps an identifier in backticks, escaping embedded backticks.
func quoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// buildWhere renders a slice of DBCondition to a WHERE clause fragment and
// the corresponding positional args. Conditions are joined with AND.
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
			return "", nil, fmt.Errorf("unsupported operator %q", cond.Op)
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
		return db.DBValue{Type: db.DBValString, StrVal: string(raw)}
	case string:
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

// parseMySQLURL parses a mysql:// URL into a MySQLConfig. A raw DSN (no
// scheme) is accepted and wrapped with default pooling settings.
func parseMySQLURL(rawurl string) (*MySQLConfig, error) {
	cfg := DefaultMySQLConfig()
	if rawurl == "" {
		return cfg, nil
	}
	// Raw DSN passthrough: user:pass@tcp(host:port)/db
	if !strings.HasPrefix(rawurl, "mysql://") {
		cfg.User = ""
		cfg.Password = ""
		cfg.Database = ""
		return cfg, nil
	}
	// Strip scheme.
	rest := strings.TrimPrefix(rawurl, "mysql://")
	// Split credentials from host.
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
	// Split database from host.
	var dbPart string
	if idx := strings.Index(hostpart, "/"); idx >= 0 {
		dbPart = hostpart[idx+1:]
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
	if dbPart != "" {
		cfg.Database = dbPart
	}
	return cfg, nil
}

// init registers the MySQL driver with the global db registry.
func init() {
	_ = db.RegisterDriver(NewMySQLDriver())
}
