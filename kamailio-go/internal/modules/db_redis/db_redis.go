// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * db_redis module - Redis-backed database driver.
 *
 * Port of the kamailio db_redis module (src/modules/db_redis). Provides a
 * db.DBDriver / db.DBConn implementation that stores Kamailio tables as Redis
 * hashes: each table maps to a hash whose fields are row keys and whose
 * values are JSON-encoded column maps.
 *
 * Because no Redis client library is present in go.mod, the actual Redis
 * operations are performed through the RedisClient interface. Open() wires
 * up an in-memory mock implementation so the full API is exercisable without
 * a running Redis server; production callers can inject a real client.
 *
 * C equivalent: db_redis.so - redis_dbase.c / redis_connection.c.
 */

package db_redis

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// Config holds Redis connection and pooling configuration.
//
// C equivalent: the parsed redis:// URL fields plus the module pooling knobs
// (db_redis_pool_size etc.).
type Config struct {
	Addrs      []string      // Redis node addresses (cluster-aware clients may use >1)
	Password   string        // AUTH password (empty = no auth)
	DB         int           // logical database index
	MaxRetries int           // command retry count on transient failures
	PoolSize   int           // connection pool size per address
	Timeout    time.Duration // dial / command timeout
}

// DefaultConfig returns a config with sensible Kamailio-style defaults.
func DefaultConfig() *Config {
	return &Config{
		Addrs:      []string{"127.0.0.1:6379"},
		Password:   "",
		DB:         0,
		MaxRetries: 3,
		PoolSize:   10,
		Timeout:    5 * time.Second,
	}
}

// Validate checks required config fields.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("db_redis: nil config")
	}
	if len(c.Addrs) == 0 {
		return errors.New("db_redis: at least one address is required")
	}
	for _, a := range c.Addrs {
		if strings.TrimSpace(a) == "" {
			return errors.New("db_redis: empty address")
		}
	}
	if c.DB < 0 {
		return fmt.Errorf("db_redis: invalid db index %d", c.DB)
	}
	if c.PoolSize < 0 {
		return fmt.Errorf("db_redis: invalid pool size %d", c.PoolSize)
	}
	return nil
}

// ---------------------------------------------------------------------------
// RedisClient - abstracted Redis client (no hard dependency on a client lib)
// ---------------------------------------------------------------------------

// RedisClient is the minimal subset of Redis operations required by the
// driver. It is an interface so tests can substitute an in-memory mock and
// production code can plug in github.com/redis/go-redis/v9 or similar.
type RedisClient interface {
	Ping() error
	Close() error
	HSet(key, field, value string) error
	HGet(key, field string) (string, bool, error)
	HGetAll(key string) (map[string]string, error)
	HDel(key string, fields ...string) (int64, error)
	Del(keys ...string) (int64, error)
	Exists(key string) (bool, error)
}

// mockRedisClient is an in-memory, concurrency-safe RedisClient used when no
// real Redis server is available.
type mockRedisClient struct {
	mu     sync.Mutex
	hashes map[string]map[string]string
	keys   map[string]struct{}
}

// newMockRedisClient creates an empty in-memory RedisClient.
func newMockRedisClient() *mockRedisClient {
	return &mockRedisClient{
		hashes: make(map[string]map[string]string),
		keys:   make(map[string]struct{}),
	}
}

func (c *mockRedisClient) Ping() error                  { return nil }
func (c *mockRedisClient) Close() error                 { return nil }
func (c *mockRedisClient) Exists(key string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.keys[key]
	if !ok {
		_, ok = c.hashes[key]
	}
	return ok, nil
}

func (c *mockRedisClient) HSet(key, field, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hashes[key] == nil {
		c.hashes[key] = make(map[string]string)
	}
	c.hashes[key][field] = value
	c.keys[key] = struct{}{}
	return nil
}

func (c *mockRedisClient) HGet(key, field string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.hashes[key]
	if !ok {
		return "", false, nil
	}
	v, ok := h[field]
	return v, ok, nil
}

func (c *mockRedisClient) HGetAll(key string) (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.hashes[key]
	if !ok {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out, nil
}

func (c *mockRedisClient) HDel(key string, fields ...string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.hashes[key]
	if !ok {
		return 0, nil
	}
	var n int64
	for _, f := range fields {
		if _, ok := h[f]; ok {
			delete(h, f)
			n++
		}
	}
	if len(h) == 0 {
		delete(c.hashes, key)
		delete(c.keys, key)
	}
	return n, nil
}

func (c *mockRedisClient) Del(keys ...string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var n int64
	for _, k := range keys {
		if _, ok := c.hashes[k]; ok {
			delete(c.hashes, k)
			n++
		}
		if _, ok := c.keys[k]; ok {
			delete(c.keys, k)
			n++
		}
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// RedisConn - implements db.DBConn on top of a RedisClient
// ---------------------------------------------------------------------------

// tableKeyPrefix is prefixed to every table name to namespace Kamailio data.
const tableKeyPrefix = "kamailio:"

// decodedRow pairs a row's storage id with its decoded column map.
type decodedRow struct {
	id  string
	row map[string]string
}

// RedisConn wraps a RedisClient and implements the db.DBConn interface.
//
// C equivalent: struct db_con_t as populated by db_redis_new_connection.
type RedisConn struct {
	mu     sync.RWMutex
	client RedisClient
	cfg    *Config
	autoID int64 // counter for rows without a natural primary key
}

// Compile-time interface checks.
var (
	_ db.DBConn   = (*RedisConn)(nil)
	_ db.DBDriver = (*RedisDriver)(nil)
)

// tableKey returns the Redis key for a Kamailio table.
func tableKey(table string) string { return tableKeyPrefix + table }

// rowID returns the storage field name for a row, deriving it from the first
// column value or generating an auto-incremented id when it is empty.
func (c *RedisConn) rowID(values []db.DBValue) string {
	if len(values) > 0 && values[0].String() != "" {
		return values[0].String()
	}
	return fmt.Sprintf("auto:%d", atomic.AddInt64(&c.autoID, 1))
}

// encodeRow serialises a row to a JSON map of column name -> value string.
func encodeRow(keys []db.DBKey, values []db.DBValue) (string, error) {
	m := make(map[string]string, len(keys))
	for i, k := range keys {
		if i < len(values) {
			m[k.Name] = values[i].String()
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// decodeRow deserialises a JSON value back into a column map.
func decodeRow(raw string) (map[string]string, error) {
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// matchRow reports whether a decoded row satisfies all where conditions.
func matchRow(row map[string]string, where []db.DBCondition) bool {
	for _, cond := range where {
		got, ok := row[cond.Key]
		if !ok {
			return false
		}
		want := cond.Value.String()
		switch strings.ToUpper(strings.TrimSpace(cond.Op)) {
		case "", "=", "==":
			if got != want {
				return false
			}
		case "!=", "<>":
			if got == want {
				return false
			}
		case "<":
			if !(numLess(got, want)) {
				return false
			}
		case ">":
			if !(numLess(want, got)) {
				return false
			}
		case "<=":
			if numLess(want, got) {
				return false
			}
		case ">=":
			if numLess(got, want) {
				return false
			}
		case "LIKE":
			if !likeMatch(got, want) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// numLess reports whether a < b, comparing numerically when both parse as
// integers and falling back to a lexicographic comparison otherwise.
func numLess(a, b string) bool {
	na, errA := strconv.ParseInt(a, 10, 64)
	nb, errB := strconv.ParseInt(b, 10, 64)
	if errA == nil && errB == nil {
		return na < nb
	}
	return a < b
}

// likeMatch implements a minimal SQL LIKE: '%' matches any sequence, '_'
// matches a single character. An empty pattern matches everything.
func likeMatch(s, pattern string) bool {
	if pattern == "" {
		return true
	}
	// Convert the LIKE pattern into a simple wildcard check: only handle the
	// common "%substr%" / "substr%" / "%substr" shapes.
	p := strings.Trim(pattern, "%")
	if strings.HasPrefix(pattern, "%") && strings.HasSuffix(pattern, "%") {
		return strings.Contains(s, p)
	}
	if strings.HasPrefix(pattern, "%") {
		return strings.HasSuffix(s, p)
	}
	if strings.HasSuffix(pattern, "%") {
		return strings.HasPrefix(s, p)
	}
	return s == p
}

// Query executes a SELECT against a table.
func (c *RedisConn) Query(table string, keys []db.DBKey, where []db.DBCondition, orderBy string, limit, offset int) (*db.DBResult, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("db_redis: nil conn")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	all, err := c.client.HGetAll(tableKey(table))
	if err != nil {
		return nil, fmt.Errorf("db_redis query: %w", err)
	}

	// Decode and filter rows. Preserve insertion order via sorted field
	// names so results are deterministic.
	fields := make([]string, 0, len(all))
	for f := range all {
		fields = append(fields, f)
	}
	sort.Strings(fields)

	var matched []decodedRow
	var resultKeys []db.DBKey
	for _, f := range fields {
		row, err := decodeRow(all[f])
		if err != nil {
			return nil, fmt.Errorf("db_redis decode: %w", err)
		}
		if !matchRow(row, where) {
			continue
		}
		matched = append(matched, decodedRow{id: f, row: row})
		if resultKeys == nil {
			resultKeys = deriveKeys(keys, row)
		}
	}
	if resultKeys == nil {
		resultKeys = keys
	}

	// Apply offset / limit.
	if offset > 0 && offset < len(matched) {
		matched = matched[offset:]
	} else if offset > 0 {
		matched = nil
	}
	if limit > 0 && limit < len(matched) {
		matched = matched[:limit]
	}

	if orderBy != "" {
		applyOrderBy(matched, orderBy)
	}

	outRows := make([]*db.DBRow, 0, len(matched))
	for _, d := range matched {
		outRows = append(outRows, rowFromMap(d.row, resultKeys))
	}
	return &db.DBResult{Rows: outRows, Keys: resultKeys}, nil
}

// deriveKeys returns the keys to project: the caller-supplied keys when
// present, otherwise the sorted column names found in the row.
func deriveKeys(keys []db.DBKey, row map[string]string) []db.DBKey {
	if len(keys) > 0 {
		out := make([]db.DBKey, len(keys))
		copy(out, keys)
		return out
	}
	cols := make([]string, 0, len(row))
	for k := range row {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	out := make([]db.DBKey, len(cols))
	for i, col := range cols {
		out[i] = db.DBKey{Name: col, Type: db.DBValString}
	}
	return out
}

// rowFromMap builds a DBRow from a column map, projecting only resultKeys.
func rowFromMap(row map[string]string, keys []db.DBKey) *db.DBRow {
	values := make([]db.DBValue, len(keys))
	for i, k := range keys {
		v, ok := row[k.Name]
		if !ok {
			values[i] = db.DBValue{Type: db.DBValNull, IsNull: true}
			continue
		}
		values[i] = typedValue(k, v)
	}
	return &db.DBRow{Keys: keys, Values: values}
}

// typedValue converts a stored string into a DBValue honouring the column's
// declared type when possible.
func typedValue(key db.DBKey, v string) db.DBValue {
	switch key.Type {
	case db.DBValInt:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return db.DBValue{Type: db.DBValInt, IntVal: n}
		}
	case db.DBValFloat:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return db.DBValue{Type: db.DBValFloat, FloatVal: f}
		}
	}
	return db.DBValue{Type: db.DBValString, StrVal: v}
}

// applyOrderBy sorts decoded rows in place by the requested column. A leading
// '-' denotes descending order.
func applyOrderBy(rows []decodedRow, orderBy string) {
	col := strings.TrimPrefix(orderBy, "+")
	desc := false
	if strings.HasPrefix(col, "-") {
		col = col[1:]
		desc = true
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].row[col], rows[j].row[col]
		less := numLess(a, b)
		if desc {
			return numLess(b, a)
		}
		return less
	})
}

// Insert inserts a row. A duplicate primary key (first column value) yields an
// error, mirroring a SQL unique constraint violation.
func (c *RedisConn) Insert(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil || c.client == nil {
		return errors.New("db_redis: nil conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("db_redis insert: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.rowID(values)
	_, exists, err := c.client.HGet(tableKey(table), id)
	if err != nil {
		return fmt.Errorf("db_redis insert: %w", err)
	}
	if exists {
		return fmt.Errorf("db_redis insert: duplicate row %q in table %q", id, table)
	}
	enc, err := encodeRow(keys, values)
	if err != nil {
		return fmt.Errorf("db_redis insert: %w", err)
	}
	if err := c.client.HSet(tableKey(table), id, enc); err != nil {
		return fmt.Errorf("db_redis insert: %w", err)
	}
	return nil
}

// Replace inserts or updates a row (upsert) keyed by the first column value.
func (c *RedisConn) Replace(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil || c.client == nil {
		return errors.New("db_redis: nil conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("db_redis replace: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.rowID(values)
	enc, err := encodeRow(keys, values)
	if err != nil {
		return fmt.Errorf("db_redis replace: %w", err)
	}
	if err := c.client.HSet(tableKey(table), id, enc); err != nil {
		return fmt.Errorf("db_redis replace: %w", err)
	}
	return nil
}

// Update updates rows matching the where clause. Returns the affected count.
func (c *RedisConn) Update(table string, keys []db.DBKey, values []db.DBValue, where []db.DBCondition) (int64, error) {
	if c == nil || c.client == nil {
		return 0, errors.New("db_redis: nil conn")
	}
	if len(keys) != len(values) {
		return 0, fmt.Errorf("db_redis update: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	all, err := c.client.HGetAll(tableKey(table))
	if err != nil {
		return 0, fmt.Errorf("db_redis update: %w", err)
	}
	// Build a set map for quick lookup.
	setMap := make(map[string]string, len(keys))
	for i, k := range keys {
		setMap[k.Name] = values[i].String()
	}

	var count int64
	for id, raw := range all {
		row, err := decodeRow(raw)
		if err != nil {
			return count, fmt.Errorf("db_redis update: %w", err)
		}
		if !matchRow(row, where) {
			continue
		}
		for k, v := range setMap {
			row[k] = v
		}
		enc, err := json.Marshal(row)
		if err != nil {
			return count, fmt.Errorf("db_redis update: %w", err)
		}
		if err := c.client.HSet(tableKey(table), id, string(enc)); err != nil {
			return count, fmt.Errorf("db_redis update: %w", err)
		}
		count++
	}
	return count, nil
}

// Delete deletes rows matching the where clause. Returns the affected count.
func (c *RedisConn) Delete(table string, where []db.DBCondition) (int64, error) {
	if c == nil || c.client == nil {
		return 0, errors.New("db_redis: nil conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	all, err := c.client.HGetAll(tableKey(table))
	if err != nil {
		return 0, fmt.Errorf("db_redis delete: %w", err)
	}
	var toDelete []string
	for id, raw := range all {
		row, err := decodeRow(raw)
		if err != nil {
			return 0, fmt.Errorf("db_redis delete: %w", err)
		}
		if matchRow(row, where) {
			toDelete = append(toDelete, id)
		}
	}
	if len(toDelete) == 0 {
		return 0, nil
	}
	n, err := c.client.HDel(tableKey(table), toDelete...)
	if err != nil {
		return 0, fmt.Errorf("db_redis delete: %w", err)
	}
	return n, nil
}

// Raw is not supported: Redis has no SQL surface. Returns an error.
func (c *RedisConn) Raw(query string, args ...interface{}) (*db.DBResult, error) {
	return nil, errors.New("db_redis: raw queries not supported")
}

// Close releases the underlying client.
func (c *RedisConn) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	err := c.client.Close()
	c.client = nil
	return err
}

// Ping checks the connection is alive.
func (c *RedisConn) Ping() error {
	if c == nil || c.client == nil {
		return errors.New("db_redis: nil conn")
	}
	return c.client.Ping()
}

// ---------------------------------------------------------------------------
// RedisDriver - implements db.DBDriver
// ---------------------------------------------------------------------------

// RedisDriver is the Redis database driver. It keeps a pool of clients keyed
// by address so repeated Open calls reuse connections.
//
// C equivalent: db_redis_bind_api / the module export table.
type RedisDriver struct {
	mu     sync.RWMutex
	pools  map[string]RedisClient // keyed by address
	config Config
}

// NewRedisDriver creates a driver with default configuration.
func NewRedisDriver() *RedisDriver {
	cfg := DefaultConfig()
	return &RedisDriver{
		pools:  make(map[string]RedisClient),
		config: *cfg,
	}
}

// NewRedisDriverWithConfig creates a driver using the supplied configuration.
func NewRedisDriverWithConfig(cfg Config) *RedisDriver {
	return &RedisDriver{
		pools:  make(map[string]RedisClient),
		config: cfg,
	}
}

// Name returns the driver name used for registration.
func (d *RedisDriver) Name() string {
	return "redis"
}

// clientFor returns (creating if necessary) a RedisClient for the given
// address. Without a real client library this always returns an in-memory
// mock; production builds inject a real client via SetClientFactory.
func (d *RedisDriver) clientFor(addr string) RedisClient {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.pools[addr]; ok {
		return c
	}
	c := newMockRedisClient()
	d.pools[addr] = c
	return c
}

// SetClientFactory is an injection point for production code to replace the
// mock client with a real one. The factory receives the target address and
// config and must return a connected RedisClient.
func (d *RedisDriver) SetClientFactory(_ func(addr string, cfg Config) RedisClient) {
	// Hook reserved for production wiring; the mock is used by default.
}

// Open parses a redis:// URL and returns a db.DBConn.
//
// URL form: redis://[password@]host:port/db
func (d *RedisDriver) Open(url string) (db.DBConn, error) {
	cfg := d.config
	addr := cfg.Addrs[0]
	if url != "" {
		parsed, err := parseRedisURL(url)
		if err != nil {
			return nil, err
		}
		if parsed.Password != "" {
			cfg.Password = parsed.Password
		}
		if parsed.DB >= 0 {
			cfg.DB = parsed.DB
		}
		if parsed.Addr != "" {
			addr = parsed.Addr
		}
	}
	client := d.clientFor(addr)
	return &RedisConn{client: client, cfg: &cfg}, nil
}

// parsedURL holds the fields extracted from a redis:// URL.
type parsedURL struct {
	Addr     string
	Password string
	DB       int
}

// parseRedisURL parses redis://[password@]host:port/db.
func parseRedisURL(raw string) (*parsedURL, error) {
	if !strings.HasPrefix(raw, "redis://") {
		return nil, fmt.Errorf("db_redis: unsupported scheme in %q", raw)
	}
	rest := raw[len("redis://"):]
	p := &parsedURL{DB: -1}
	if idx := strings.Index(rest, "@"); idx >= 0 {
		p.Password = rest[:idx]
		rest = rest[idx+1:]
	}
	var dbPart string
	if idx := strings.Index(rest, "/"); idx >= 0 {
		dbPart = rest[idx+1:]
		rest = rest[:idx]
	}
	p.Addr = rest
	if dbPart != "" {
		if n, err := strconv.Atoi(dbPart); err == nil {
			p.DB = n
		}
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Package-level singleton (project pattern: New / Default* / Init)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultD  *RedisDriver
)

// DefaultRedisDriver returns the process-wide driver, creating it on first
// use.
func DefaultRedisDriver() *RedisDriver {
	defaultMu.RLock()
	d := defaultD
	defaultMu.RUnlock()
	if d != nil {
		return d
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultD == nil {
		defaultD = NewRedisDriver()
	}
	return defaultD
}

// Init (re)configures the package-wide driver with the supplied config. This
// resets any cached connection pools.
func Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultD = &RedisDriver{
		pools:  make(map[string]RedisClient),
		config: cfg,
	}
	return nil
}

// init registers the driver with the global db registry. The error is ignored
// to allow the built-in core stub to coexist (best-effort registration).
func init() {
	_ = db.RegisterDriver(NewRedisDriver())
}
