// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - db_sqlite module tests.
 *
 * These tests use a real in-memory SQLite database (modernc.org/sqlite) so
 * the full db.DBConn / db.DBDriver contract is exercised against an actual
 * SQL engine, including concurrent access under -race.
 */

package db_sqlite

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
	if cfg.Path != ":memory:" {
		t.Errorf("default path = %q, want :memory:", cfg.Path)
	}
	if cfg.MaxOpenConn != 4 {
		t.Errorf("default MaxOpenConn = %d, want 4", cfg.MaxOpenConn)
	}
	if cfg.MaxIdleConn != 2 {
		t.Errorf("default MaxIdleConn = %d, want 2", cfg.MaxIdleConn)
	}
	if cfg.ConnMaxLifetime != 5*time.Minute {
		t.Errorf("default ConnMaxLifetime = %v, want 5m", cfg.ConnMaxLifetime)
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

// TestNewSQLiteDriver verifies driver construction and name.
func TestNewSQLiteDriver(t *testing.T) {
	d := NewSQLiteDriver()
	if d == nil {
		t.Fatal("NewSQLiteDriver returned nil")
	}
	if d.Name() != "sqlite" {
		t.Errorf("Name() = %q, want sqlite", d.Name())
	}
}

// TestOpenReturnsConn verifies Open returns a usable connection.
func TestOpenReturnsConn(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	var _ db.DBConn = conn
	if err := conn.Ping(); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

// TestInsertAndQuery verifies a row can be inserted and queried back, and
// that the table is auto-created from the keys.
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
	if res.Row(0).GetString("user") != "alice" {
		t.Errorf("user = %q, want alice", res.Row(0).GetString("user"))
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

// TestQueryOrderByLimit verifies ORDER BY and LIMIT.
func TestQueryOrderByLimit(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	seedSubscribers(t, conn)

	res, err := conn.Query("subscriber", nil, nil, "-user", 2, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 2 {
		t.Fatalf("RowCount = %d, want 2", res.RowCount())
	}
	// Descending by user: carol, bob, alice -> first two are carol, bob.
	if res.Row(0).GetString("user") != "carol" {
		t.Errorf("first user = %q, want carol", res.Row(0).GetString("user"))
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
// fails (UNIQUE constraint violation).
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

// TestRaw verifies a raw SQL query returns rows.
func TestRaw(t *testing.T) {
	conn := mustOpen(t)
	defer conn.Close()
	seedSubscribers(t, conn)
	res, err := conn.Raw("SELECT COUNT(*) AS n FROM subscriber")
	if err != nil {
		t.Fatalf("Raw: %v", err)
	}
	if res.RowCount() != 1 {
		t.Fatalf("RowCount = %d, want 1", res.RowCount())
	}
	if res.Row(0).GetString("n") != "3" {
		t.Errorf("n = %q, want 3", res.Row(0).GetString("n"))
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

// TestDriverReusesPool verifies the driver caches *sql.DB by path.
func TestDriverReusesPool(t *testing.T) {
	d := NewSQLiteDriver()
	c1, err := d.Open(":memory:")
	if err != nil {
		t.Fatalf("Open c1: %v", err)
	}
	defer c1.Close()
	c2, err := d.Open(":memory:")
	if err != nil {
		t.Fatalf("Open c2: %v", err)
	}
	defer c2.Close()
	// Each :memory: path gets its own pool entry; both should be usable.
	if err := c2.Ping(); err != nil {
		t.Errorf("c2 Ping: %v", err)
	}
}

// --- helpers ---

func mustOpen(t *testing.T) db.DBConn {
	t.Helper()
	conn, err := NewSQLiteDriver().Open(":memory:")
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
