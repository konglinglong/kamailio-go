// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * MongoDB database backend - matching C db_mongodb (db_mongodb_mod.c)
 *
 * Provides a MongoDB driver that implements the generic db.DBDriver /
 * db.DBConn interfaces. This is a mock implementation: no real MongoDB
 * connection is established. Documents are kept in-memory so the connection
 * can be exercised by tests without a running server.
 *
 * C equivalent: db_mongodb.so - registers itself via db_mongodb_bind_api
 * and exposes connection / query primitives through the srdb1 abstraction.
 */

package db_mongodb

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// DefaultMongoPort is the default MongoDB server port.
const DefaultMongoPort = 27017

// DefaultMongoDatabase is the default database name.
const DefaultMongoDatabase = "kamailio"

// DefaultAuthSource is the default authentication database.
const DefaultAuthSource = "admin"

// DefaultMaxPoolSize is the default connection pool size.
const DefaultMaxPoolSize = 100

// MongoConfig holds MongoDB connection parameters.
//
// C equivalent: the mongodb:// URI parsed fields (host, port, username,
// password, database, authSource) plus the pool size knob.
type MongoConfig struct {
	Host        string
	Port        int
	Database    string
	User        string
	Password    string
	AuthSource  string
	MaxPoolSize int
}

// DefaultMongoConfig returns a config with sensible Kamailio-style defaults.
func DefaultMongoConfig() *MongoConfig {
	return &MongoConfig{
		Host:        "localhost",
		Port:        DefaultMongoPort,
		Database:    DefaultMongoDatabase,
		User:        "",
		Password:    "",
		AuthSource:  DefaultAuthSource,
		MaxPoolSize: DefaultMaxPoolSize,
	}
}

// Validate checks required config fields and returns an error describing
// the first missing or invalid field.
func (c *MongoConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("mongodb config: nil")
	}
	if c.Host == "" {
		return fmt.Errorf("mongodb config: host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("mongodb config: invalid port %d", c.Port)
	}
	if c.Database == "" {
		return fmt.Errorf("mongodb config: database is required")
	}
	return nil
}

// URI returns a mongodb:// connection URI string.
func (c *MongoConfig) URI() string {
	if c == nil {
		return ""
	}
	creds := ""
	if c.User != "" {
		creds = c.User
		if c.Password != "" {
			creds += ":" + c.Password
		}
		creds += "@"
	}
	auth := ""
	if c.AuthSource != "" {
		auth = "?authSource=" + c.AuthSource
	}
	return fmt.Sprintf("mongodb://%s%s:%d/%s%s", creds, c.Host, c.Port, c.Database, auth)
}

// ---------------------------------------------------------------------------
// MongoConn - implements db.DBConn against an in-memory mock store
// ---------------------------------------------------------------------------

// MongoConn implements db.DBConn. Data is stored in-memory as collections of
// db.DBRow so the connection can be exercised without a real MongoDB server.
//
// C equivalent: struct db_mongodb_con which embeds the mongoc client handle.
type MongoConn struct {
	mu     sync.RWMutex
	cfg    *MongoConfig
	closed bool
	data   map[string][]*db.DBRow
}

// Compile-time interface check.
var _ db.DBConn = (*MongoConn)(nil)

// Connect initializes the mock connection. It performs no real network I/O
// and returns nil on a valid config.
func (c *MongoConn) Connect(cfg *MongoConfig) error {
	if c == nil {
		return fmt.Errorf("nil mongodb conn")
	}
	if cfg == nil {
		return fmt.Errorf("nil mongodb config")
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

// GetCollection returns the named collection's documents as a slice of
// maps. Returns nil if the collection does not exist.
func (c *MongoConn) GetCollection(name string) interface{} {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	rows, ok := c.data[name]
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		m := make(map[string]interface{}, len(r.Keys))
		for i, k := range r.Keys {
			if i < len(r.Values) {
				m[k.Name] = r.Values[i].String()
			}
		}
		out = append(out, m)
	}
	return out
}

// Query selects documents from a collection matching the where clause.
func (c *MongoConn) Query(table string, keys []db.DBKey, where []db.DBCondition, orderBy string, limit, offset int) (*db.DBResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil mongodb conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return nil, fmt.Errorf("mongodb conn closed")
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

// Insert inserts a document into a collection.
func (c *MongoConn) Insert(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil {
		return fmt.Errorf("nil mongodb conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("insert: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("mongodb conn closed")
	}
	c.data[table] = append(c.data[table], &db.DBRow{Keys: keys, Values: values})
	return nil
}

// Update updates documents matching the where clause.
func (c *MongoConn) Update(table string, keys []db.DBKey, values []db.DBValue, where []db.DBCondition) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("nil mongodb conn")
	}
	if len(keys) != len(values) {
		return 0, fmt.Errorf("update: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, fmt.Errorf("mongodb conn closed")
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

// Delete removes documents matching the where clause.
func (c *MongoConn) Delete(table string, where []db.DBCondition) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("nil mongodb conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, fmt.Errorf("mongodb conn closed")
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

// Replace inserts or updates a document, matching on the first key.
func (c *MongoConn) Replace(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil {
		return fmt.Errorf("nil mongodb conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("replace: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("mongodb conn closed")
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

// Raw executes a raw query. This mock returns the full collection contents
// when the query string matches a collection name.
func (c *MongoConn) Raw(query string, args ...interface{}) (*db.DBResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil mongodb conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return nil, fmt.Errorf("mongodb conn closed")
	}
	rows := c.data[query]
	return rowsToResult(rows, nil), nil
}

// Close closes the connection.
func (c *MongoConn) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// Ping checks the connection is alive.
func (c *MongoConn) Ping() error {
	if c == nil {
		return fmt.Errorf("nil mongodb conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return fmt.Errorf("mongodb conn closed")
	}
	return nil
}

// ---------------------------------------------------------------------------
// MongoDriver - implements db.DBDriver
// ---------------------------------------------------------------------------

// MongoDriver implements db.DBDriver for MongoDB.
//
// C equivalent: db_mongodb_bind_api / the module export table.
type MongoDriver struct{}

// Compile-time interface check.
var _ db.DBDriver = (*MongoDriver)(nil)

// NewMongoDriver creates a new MongoDriver.
func NewMongoDriver() *MongoDriver {
	return &MongoDriver{}
}

// Name returns the driver name used for registration.
func (d *MongoDriver) Name() string {
	return "mongodb"
}

// Open parses a mongodb:// URL and returns a db.DBConn.
func (d *MongoDriver) Open(url string) (db.DBConn, error) {
	cfg, err := parseMongoURL(url)
	if err != nil {
		return nil, err
	}
	conn := &MongoConn{}
	if err := conn.Connect(cfg); err != nil {
		return nil, err
	}
	return conn, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// parseMongoURL parses a mongodb:// URL into a MongoConfig. A raw string
// (no scheme) is accepted with default settings.
func parseMongoURL(rawurl string) (*MongoConfig, error) {
	cfg := DefaultMongoConfig()
	if rawurl == "" {
		return cfg, nil
	}
	if !strings.HasPrefix(rawurl, "mongodb://") {
		return cfg, nil
	}
	rest := strings.TrimPrefix(rawurl, "mongodb://")

	var queryPart string
	if idx := strings.Index(rest, "?"); idx >= 0 {
		queryPart = rest[idx+1:]
		rest = rest[:idx]
	}

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
	if queryPart != "" {
		for _, kv := range strings.Split(queryPart, "&") {
			if idx := strings.Index(kv, "="); idx >= 0 {
				if kv[:idx] == "authSource" {
					cfg.AuthSource = kv[idx+1:]
				}
			}
		}
	}
	return cfg, nil
}

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

// applyUpdate merges the given key/value pairs into a row, overwriting
// existing columns or appending new ones.
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

// init registers the MongoDB driver with the global db registry.
func init() {
	_ = db.RegisterDriver(NewMongoDriver())
}
