// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - db_redis module tests.
 *
 * These tests do NOT require a running Redis server. They exercise the
 * driver against an in-memory mock RedisClient so the full db.DBConn /
 * db.DBDriver contract is verified, including concurrent access.
 */

package db_redis

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// TestConfigDefaults verifies the default config values.
func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.Addrs) == 0 || cfg.Addrs[0] != "127.0.0.1:6379" {
		t.Errorf("default addrs = %v, want [127.0.0.1:6379]", cfg.Addrs)
	}
	if cfg.DB != 0 {
		t.Errorf("default db = %d, want 0", cfg.DB)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("default max retries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.PoolSize != 10 {
		t.Errorf("default pool size = %d, want 10", cfg.PoolSize)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("default timeout = %v, want 5s", cfg.Timeout)
	}
}

// TestConfigValidate exercises Validate for valid and invalid configs.
func TestConfigValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Errorf("default config: unexpected error: %v", err)
	}
	if err := (&Config{}).Validate(); err == nil {
		t.Error("empty config: expected error, got nil")
	}
}

// TestNewRedisDriver verifies driver construction and name.
func TestNewRedisDriver(t *testing.T) {
	d := NewRedisDriver()
	if d == nil {
		t.Fatal("NewRedisDriver returned nil")
	}
	if d.Name() != "redis" {
		t.Errorf("Name() = %q, want redis", d.Name())
	}
}

// TestOpenReturnsConn verifies Open returns a usable connection.
func TestOpenReturnsConn(t *testing.T) {
	d := NewRedisDriver()
	conn, err := d.Open("redis://127.0.0.1:6379/0")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if conn == nil {
		t.Fatal("Open returned nil conn")
	}
	var _ db.DBConn = conn
	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestPing verifies Ping succeeds on a fresh connection.
func TestPing(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

// TestInsertAndQuery verifies a row can be inserted and queried back.
func TestInsertAndQuery(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()

	keys := []db.DBKey{
		{Name: "id", Type: db.DBValString},
		{Name: "user", Type: db.DBValString},
		{Name: "domain", Type: db.DBValString},
	}
	values := []db.DBValue{
		db.NewStringValue("alice@example.com"),
		db.NewStringValue("alice"),
		db.NewStringValue("example.com"),
	}
	if err := conn.Insert("subscriber", keys, values); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	res, err := conn.Query("subscriber", keys, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 1 {
		t.Fatalf("RowCount = %d, want 1", res.RowCount())
	}
	row := res.Row(0)
	if row.GetString("user") != "alice" {
		t.Errorf("user = %q, want alice", row.GetString("user"))
	}
	if row.GetString("domain") != "example.com" {
		t.Errorf("domain = %q, want example.com", row.GetString("domain"))
	}
}

// TestQueryWithWhere verifies filtering by a where clause.
func TestQueryWithWhere(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	seedSubscribers(t, conn)

	where := []db.DBCondition{
		{Key: "domain", Op: "=", Value: db.NewStringValue("example.com")},
	}
	res, err := conn.Query("subscriber", nil, where, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 2 {
		t.Fatalf("RowCount = %d, want 2", res.RowCount())
	}
}

// TestQueryLimit verifies the limit clause.
func TestQueryLimit(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	seedSubscribers(t, conn)

	res, err := conn.Query("subscriber", nil, nil, "", 1, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 1 {
		t.Fatalf("RowCount = %d, want 1 (limit)", res.RowCount())
	}
}

// TestUpdate verifies rows matching a where clause are updated.
func TestUpdate(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	seedSubscribers(t, conn)

	where := []db.DBCondition{
		{Key: "id", Op: "=", Value: db.NewStringValue("alice@example.com")},
	}
	n, err := conn.Update("subscriber",
		[]db.DBKey{{Name: "domain", Type: db.DBValString}},
		[]db.DBValue{db.NewStringValue("newdomain.com")},
		where)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if n != 1 {
		t.Errorf("rows affected = %d, want 1", n)
	}

	res, err := conn.Query("subscriber", nil, where, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Row(0).GetString("domain") != "newdomain.com" {
		t.Errorf("domain = %q, want newdomain.com", res.Row(0).GetString("domain"))
	}
}

// TestDelete verifies rows matching a where clause are deleted.
func TestDelete(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	seedSubscribers(t, conn)

	where := []db.DBCondition{
		{Key: "id", Op: "=", Value: db.NewStringValue("bob@example.com")},
	}
	n, err := conn.Delete("subscriber", where)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n != 1 {
		t.Errorf("rows affected = %d, want 1", n)
	}
	res, err := conn.Query("subscriber", nil, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 2 {
		t.Errorf("RowCount after delete = %d, want 2", res.RowCount())
	}
}

// TestReplace verifies Replace upserts a row.
func TestReplace(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()

	keys := []db.DBKey{
		{Name: "id", Type: db.DBValString},
		{Name: "v", Type: db.DBValString},
	}
	if err := conn.Replace("t", keys, []db.DBValue{
		db.NewStringValue("k1"), db.NewStringValue("a"),
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	// Replace again with same id -> upsert, no error.
	if err := conn.Replace("t", keys, []db.DBValue{
		db.NewStringValue("k1"), db.NewStringValue("b"),
	}); err != nil {
		t.Fatalf("Replace upsert: %v", err)
	}
	res, err := conn.Query("t", nil, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 1 {
		t.Fatalf("RowCount = %d, want 1 (upserted)", res.RowCount())
	}
	if res.Row(0).GetString("v") != "b" {
		t.Errorf("v = %q, want b", res.Row(0).GetString("v"))
	}
}

// TestInsertDuplicate verifies a second Insert with the same primary key
// fails (mimicking a unique constraint violation).
func TestInsertDuplicate(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	keys := []db.DBKey{
		{Name: "id", Type: db.DBValString},
		{Name: "v", Type: db.DBValString},
	}
	vals := []db.DBValue{db.NewStringValue("k1"), db.NewStringValue("a")}
	if err := conn.Insert("t", keys, vals); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := conn.Insert("t", keys, vals); err == nil {
		t.Error("duplicate Insert: expected error, got nil")
	}
}

// TestRawNotSupported verifies Raw returns an error (Redis has no SQL).
func TestRawNotSupported(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	if _, err := conn.Raw("GET foo"); err == nil {
		t.Error("Raw: expected error, got nil")
	}
}

// TestQueryEmptyTable verifies querying a non-existent table returns empty.
func TestQueryEmptyTable(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	res, err := conn.Query("nope", nil, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 0 {
		t.Errorf("RowCount = %d, want 0", res.RowCount())
	}
}

// TestConcurrentAccess exercises the driver under concurrent load to
// surface data races when run with -race.
func TestConcurrentAccess(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("user%d@test", i)
			if err := conn.Insert("c",
				[]db.DBKey{{Name: "id", Type: db.DBValString}, {Name: "n", Type: db.DBValInt}},
				[]db.DBValue{db.NewStringValue(id), {Type: db.DBValInt, IntVal: int64(i)}},
			); err != nil {
				t.Errorf("Insert %d: %v", i, err)
				return
			}
			res, err := conn.Query("c", nil,
				[]db.DBCondition{{Key: "id", Op: "=", Value: db.NewStringValue(id)}},
				"", 0, 0)
			if err != nil {
				t.Errorf("Query %d: %v", i, err)
				return
			}
			if res.RowCount() != 1 {
				t.Errorf("row %d: count=%d, want 1", i, res.RowCount())
			}
		}()
	}
	wg.Wait()

	res, err := conn.Query("c", nil, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("final Query: %v", err)
	}
	if res.RowCount() != 50 {
		t.Errorf("final RowCount = %d, want 50", res.RowCount())
	}
}

// TestMockRedisClient exercises the in-memory RedisClient directly.
func TestMockRedisClient(t *testing.T) {
	c := newMockRedisClient()
	if err := c.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := c.HSet("h", "f", "v"); err != nil {
		t.Fatalf("HSet: %v", err)
	}
	v, ok, err := c.HGet("h", "f")
	if err != nil || !ok || v != "v" {
		t.Fatalf("HGet = %q %v %v, want v true nil", v, ok, err)
	}
	all, err := c.HGetAll("h")
	if err != nil || len(all) != 1 || all["f"] != "v" {
		t.Fatalf("HGetAll = %v %v", all, err)
	}
	exists, err := c.Exists("h")
	if err != nil || !exists {
		t.Fatalf("Exists = %v %v, want true nil", exists, err)
	}
	n, err := c.HDel("h", "f")
	if err != nil || n != 1 {
		t.Fatalf("HDel = %d %v, want 1 nil", n, err)
	}
	all, _ = c.HGetAll("h")
	if len(all) != 0 {
		t.Errorf("after HDel, HGetAll = %v, want empty", all)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// --- helpers ---

func mustOpen(t *testing.T) db.DBConn {
	t.Helper()
	conn, err := NewRedisDriver().Open("redis://127.0.0.1:6379/0")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return conn
}

func seedSubscribers(t *testing.T, conn db.DBConn) {
	t.Helper()
	rows := [][]string{
		{"alice@example.com", "alice", "example.com"},
		{"bob@example.com", "bob", "example.com"},
		{"carol@test", "carol", "test"},
	}
	keys := []db.DBKey{
		{Name: "id", Type: db.DBValString},
		{Name: "user", Type: db.DBValString},
		{Name: "domain", Type: db.DBValString},
	}
	for _, r := range rows {
		vals := []db.DBValue{
			db.NewStringValue(r[0]),
			db.NewStringValue(r[1]),
			db.NewStringValue(r[2]),
		}
		if err := conn.Insert("subscriber", keys, vals); err != nil {
			t.Fatalf("seed Insert: %v", err)
		}
	}
}
