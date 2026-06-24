// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * PostgreSQL database backend - matching C db_postgres (pg_con.c)
 *
 * Provides a PostgreSQL driver that bridges the generic db.DBDriver /
 * db.DBConn interfaces onto database/sql. The actual github.com/lib/pq (or
 * pgx) import is performed by the caller (blank import) so this package has
 * no hard dependency on a specific Postgres client.
 *
 * C equivalent: db_postgres.so - registers itself via db_bind_mod and
 * exposes connection / query primitives through the srdb1 abstraction.
 */

package db_postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// DefaultPostgresPort is the default PostgreSQL server port.
const DefaultPostgresPort = 5432

// DefaultSSLMode is the default SSL mode.
const DefaultSSLMode = "disable"

// PostgresConfig holds PostgreSQL connection parameters.
//
// C equivalent: the pg_uri parsed fields (host, port, username, password,
// database) plus SSL mode.
type PostgresConfig struct {
	Host         string
	Port         int
	User         string
	Password     string
	Database     string
	SSLMode      string
	MaxOpenConns int
	MaxIdleConns int
}

// DefaultPostgresConfig returns a config with sensible defaults.
func DefaultPostgresConfig() *PostgresConfig {
	return &PostgresConfig{
		Host:         "localhost",
		Port:         DefaultPostgresPort,
		User:         "postgres",
		Password:     "",
		Database:     "",
		SSLMode:      DefaultSSLMode,
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	}
}

// Validate checks required config fields.
func (c *PostgresConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("postgres config: nil")
	}
	if c.Host == "" {
		return fmt.Errorf("postgres config: host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("postgres config: invalid port %d", c.Port)
	}
	if c.User == "" {
		return fmt.Errorf("postgres config: user is required")
	}
	if c.Database == "" {
		return fmt.Errorf("postgres config: database is required")
	}
	return nil
}

// DSN returns the lib/pq keyword/value connection string:
//
//	host=H port=P user=U password=PW dbname=DB sslmode=S
func (c *PostgresConfig) DSN() string {
	if c == nil {
		return ""
	}
	sslmode := c.SSLMode
	if sslmode == "" {
		sslmode = DefaultSSLMode
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Database, sslmode)
}

// ---------------------------------------------------------------------------
// PostgresConn - implements db.DBConn on top of *sql.DB
// ---------------------------------------------------------------------------

// PostgresConn wraps a *sql.DB and implements the db.DBConn interface.
//
// C equivalent: struct pg_con (pg_con.h) which embeds the PGconn handle.
type PostgresConn struct {
	mu  sync.RWMutex
	db  *sql.DB
	cfg *PostgresConfig
}

// Compile-time interface check.
var _ db.DBConn = (*PostgresConn)(nil)

// Query executes a SELECT.
func (c *PostgresConn) Query(table string, keys []db.DBKey, where []db.DBCondition, orderBy string, limit, offset int) (*db.DBResult, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("nil postgres conn")
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
		clause, wargs, err := buildWhere(where, &argCounter{})
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
func (c *PostgresConn) Insert(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("nil postgres conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("insert: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	var cols []string
	args := make([]interface{}, 0, len(values))
	ac := &argCounter{}
	var ph []string
	for i, k := range keys {
		cols = append(cols, quoteIdent(k.Name))
		ac.n++
		ph = append(ph, fmt.Sprintf("$%d", ac.n))
		args = append(args, values[i].String())
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(table), strings.Join(cols, ", "), strings.Join(ph, ", "))
	_, err := c.db.Exec(q, args...)
	return err
}

// Update updates rows matching the where clause.
func (c *PostgresConn) Update(table string, keys []db.DBKey, values []db.DBValue, where []db.DBCondition) (int64, error) {
	if c == nil || c.db == nil {
		return 0, fmt.Errorf("nil postgres conn")
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
	ac := &argCounter{}
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		ac.n++
		sb.WriteString(quoteIdent(k.Name))
		sb.WriteString(fmt.Sprintf(" = $%d", ac.n))
		args = append(args, values[i].String())
	}
	if len(where) > 0 {
		clause, wargs, err := buildWhere(where, ac)
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
func (c *PostgresConn) Delete(table string, where []db.DBCondition) (int64, error) {
	if c == nil || c.db == nil {
		return 0, fmt.Errorf("nil postgres conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("DELETE FROM ")
	sb.WriteString(quoteIdent(table))
	var args []interface{}
	if len(where) > 0 {
		clause, wargs, err := buildWhere(where, &argCounter{})
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

// Replace inserts a row, or updates it on conflict (upsert).
func (c *PostgresConn) Replace(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("nil postgres conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("replace: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	var cols []string
	args := make([]interface{}, 0, len(values))
	ac := &argCounter{}
	var ph []string
	for i, k := range keys {
		cols = append(cols, quoteIdent(k.Name))
		ac.n++
		ph = append(ph, fmt.Sprintf("$%d", ac.n))
		args = append(args, values[i].String())
	}
	// ON CONFLICT DO UPDATE - uses first column as conflict target.
	conflictCol := quoteIdent(keys[0].Name)
	var updates []string
	for i, k := range keys {
		if i == 0 {
			continue
		}
		updates = append(updates, fmt.Sprintf("%s = EXCLUDED.%s", quoteIdent(k.Name), quoteIdent(k.Name)))
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		quoteIdent(table), strings.Join(cols, ", "), strings.Join(ph, ", "),
		conflictCol, strings.Join(updates, ", "))
	_, err := c.db.Exec(q, args...)
	return err
}

// Raw executes a raw SQL query and returns rows as DBResult.
func (c *PostgresConn) Raw(query string, args ...interface{}) (*db.DBResult, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("nil postgres conn")
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
func (c *PostgresConn) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	err := c.db.Close()
	c.db = nil
	return err
}

// Ping verifies the underlying connection is alive.
func (c *PostgresConn) Ping() error {
	if c == nil || c.db == nil {
		return fmt.Errorf("nil postgres conn")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.db.PingContext(ctx)
}

// ---------------------------------------------------------------------------
// PostgresDriver - implements db.DBDriver
// ---------------------------------------------------------------------------

// PostgresDriver implements db.DBDriver for PostgreSQL.
//
// C equivalent: db_postgres_bind_api / the module export table.
type PostgresDriver struct{}

// Compile-time interface check.
var _ db.DBDriver = (*PostgresDriver)(nil)

// NewPostgresDriver creates a new PostgresDriver.
func NewPostgresDriver() *PostgresDriver {
	return &PostgresDriver{}
}

// Name returns the driver name used for registration.
func (d *PostgresDriver) Name() string {
	return "postgres"
}

// Open parses a postgres:// URL (or raw DSN) and returns a db.DBConn.
func (d *PostgresDriver) Open(url string) (db.DBConn, error) {
	cfg, err := parsePostgresURL(url)
	if err != nil {
		return nil, err
	}
	sqlDB, err := Connect(cfg)
	if err != nil {
		return nil, err
	}
	return &PostgresConn{db: sqlDB, cfg: cfg}, nil
}

// ---------------------------------------------------------------------------
// Package-level helpers
// ---------------------------------------------------------------------------

// Connect opens a *sql.DB using the postgres driver. The caller must
// blank-import github.com/lib/pq (or pgx) before calling this function.
func Connect(cfg *PostgresConfig) (*sql.DB, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil postgres config")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
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

// GetVersion returns the PostgreSQL server version string.
func GetVersion(db *sql.DB) (string, error) {
	if db == nil {
		return "", fmt.Errorf("nil sql.DB")
	}
	var version string
	if err := db.QueryRow("SELECT version()").Scan(&version); err != nil {
		return "", fmt.Errorf("get postgres version: %w", err)
	}
	return version, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// argCounter tracks the current positional parameter index ($1, $2, ...).
type argCounter struct{ n int }

// quoteIdent double-quotes an identifier, escaping embedded quotes.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// buildWhere renders a slice of DBCondition to a WHERE clause fragment and
// the corresponding positional args. Conditions are joined with AND.
func buildWhere(where []db.DBCondition, ac *argCounter) (string, []interface{}, error) {
	var sb strings.Builder
	args := make([]interface{}, 0, len(where))
	for i, cond := range where {
		if i > 0 {
			sb.WriteString(" AND ")
		}
		sb.WriteString(quoteIdent(cond.Key))
		op := strings.ToUpper(strings.TrimSpace(cond.Op))
		ac.n++
		switch op {
		case "", "=", "==":
			sb.WriteString(fmt.Sprintf(" = $%d", ac.n))
		case "!=", "<>":
			sb.WriteString(fmt.Sprintf(" <> $%d", ac.n))
		case "<", ">", "<=", ">=":
			sb.WriteString(fmt.Sprintf(" %s $%d", op, ac.n))
		case "LIKE":
			sb.WriteString(fmt.Sprintf(" LIKE $%d", ac.n))
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

// parsePostgresURL parses a postgres:// URL into a PostgresConfig. A raw
// keyword/value DSN (no scheme) is accepted with default pooling settings.
func parsePostgresURL(rawurl string) (*PostgresConfig, error) {
	cfg := DefaultPostgresConfig()
	if rawurl == "" {
		return cfg, nil
	}
	// Raw keyword/value DSN passthrough.
	if !strings.HasPrefix(rawurl, "postgres://") && !strings.HasPrefix(rawurl, "postgresql://") {
		return cfg, nil
	}
	// Strip scheme.
	rest := rawurl
	rest = strings.TrimPrefix(rest, "postgresql://")
	rest = strings.TrimPrefix(rest, "postgres://")

	// Split query string (sslmode etc.).
	var queryPart string
	if idx := strings.Index(rest, "?"); idx >= 0 {
		queryPart = rest[idx+1:]
		rest = rest[:idx]
	}

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
	// Parse query params for sslmode.
	if queryPart != "" {
		for _, kv := range strings.Split(queryPart, "&") {
			if idx := strings.Index(kv, "="); idx >= 0 {
				if kv[:idx] == "sslmode" {
					cfg.SSLMode = kv[idx+1:]
				}
			}
		}
	}
	return cfg, nil
}

// init registers the PostgreSQL driver with the global db registry.
func init() {
	_ = db.RegisterDriver(NewPostgresDriver())
}
