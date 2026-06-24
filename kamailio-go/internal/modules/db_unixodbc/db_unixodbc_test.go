// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - db_unixodbc module tests.
 *
 * These tests do NOT require a running ODBC data source. They exercise
 * config validation, DSN building, driver construction, registration and
 * the in-memory mock store.
 */

package db_unixodbc

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// TestODBCConfig verifies defaults and validation.
func TestODBCConfig(t *testing.T) {
	cfg := DefaultODBCConfig()
	if cfg.DSN != "kamailio" {
		t.Errorf("default dsn = %q, want kamailio", cfg.DSN)
	}
	if cfg.MaxOpenConns != DefaultMaxOpenConns {
		t.Errorf("default maxOpenConns = %d, want %d", cfg.MaxOpenConns, DefaultMaxOpenConns)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid config: %v", err)
	}

	tests := []struct {
		name string
		cfg  *ODBCConfig
		want string
	}{
		{"nil config", nil, "nil"},
		{"empty dsn", &ODBCConfig{User: "u"}, "dsn"},
		{"empty user", &ODBCConfig{DSN: "d"}, "user"},
		{"negative conns", &ODBCConfig{DSN: "d", User: "u", MaxOpenConns: -1}, "maxOpenConns"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

// TestNewDriver verifies driver construction, name and registration.
func TestNewDriver(t *testing.T) {
	d := NewODBCDriver()
	if d == nil {
		t.Fatal("NewODBCDriver returned nil")
	}
	if d.Name() != "unixodbc" {
		t.Errorf("Name() = %q, want unixodbc", d.Name())
	}
	if got := db.GetDriver("unixodbc"); got == nil {
		t.Error("unixodbc driver not registered")
	}
	found := false
	for _, name := range db.RegisteredDrivers() {
		if name == "unixodbc" {
			found = true
			break
		}
	}
	if !found {
		t.Error("unixodbc not found in RegisteredDrivers()")
	}
}

// TestConnect exercises Connect success and failure paths.
func TestConnect(t *testing.T) {
	conn := &ODBCConn{}
	if err := conn.Connect(nil); err == nil {
		t.Fatal("expected error for nil config")
	}
	if err := conn.Connect(&ODBCConfig{}); err == nil {
		t.Fatal("expected error for invalid config")
	}
	if err := conn.Connect(DefaultODBCConfig()); err != nil {
		t.Fatalf("Connect valid config: %v", err)
	}
}

// TestGetDSN verifies GetDSN returns the configured DSN.
func TestGetDSN(t *testing.T) {
	conn := &ODBCConn{}
	if got := conn.GetDSN(); got != "" {
		t.Errorf("GetDSN before connect = %q, want empty", got)
	}
	cfg := DefaultODBCConfig()
	cfg.DSN = "mydsn"
	if err := conn.Connect(cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := conn.GetDSN(); got != "mydsn" {
		t.Errorf("GetDSN = %q, want mydsn", got)
	}
}

// TestPingClose verifies Ping/Close behaviour.
func TestPingClose(t *testing.T) {
	conn := &ODBCConn{}
	if err := conn.Connect(DefaultODBCConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := conn.Ping(); err != nil {
		t.Errorf("Ping before close: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := conn.Ping(); err == nil {
		t.Error("Ping after close: expected error")
	}
}

// TestInsertQuery verifies an insert followed by a query returns the row.
func TestInsertQuery(t *testing.T) {
	conn := &ODBCConn{}
	if err := conn.Connect(DefaultODBCConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	keys := []db.DBKey{
		{Name: "id", Type: db.DBValInt},
		{Name: "user", Type: db.DBValString},
	}
	if err := conn.Insert("location", keys, []db.DBValue{
		{Type: db.DBValInt, IntVal: 1},
		{Type: db.DBValString, StrVal: "alice"},
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	res, err := conn.Query("location", nil, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 1 {
		t.Fatalf("RowCount = %d, want 1", res.RowCount())
	}
	if got := res.Row(0).GetString("user"); got != "alice" {
		t.Errorf("user = %q, want alice", got)
	}
}

// TestUpdateDelete verifies update and delete operations.
func TestUpdateDelete(t *testing.T) {
	conn := &ODBCConn{}
	if err := conn.Connect(DefaultODBCConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	keys := []db.DBKey{{Name: "id", Type: db.DBValInt}, {Name: "v", Type: db.DBValString}}
	conn.Insert("t", keys, []db.DBValue{{Type: db.DBValInt, IntVal: 1}, {Type: db.DBValString, StrVal: "a"}})
	conn.Insert("t", keys, []db.DBValue{{Type: db.DBValInt, IntVal: 2}, {Type: db.DBValString, StrVal: "b"}})

	n, err := conn.Update("t",
		[]db.DBKey{{Name: "v", Type: db.DBValString}},
		[]db.DBValue{{Type: db.DBValString, StrVal: "updated"}},
		[]db.DBCondition{{Key: "id", Op: "=", Value: db.DBValue{Type: db.DBValInt, IntVal: 2}}})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if n != 1 {
		t.Errorf("Update affected = %d, want 1", n)
	}
	deleted, err := conn.Delete("t", []db.DBCondition{{Key: "id", Op: "=", Value: db.DBValue{Type: db.DBValInt, IntVal: 1}}})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Delete = %d, want 1", deleted)
	}
	res, _ := conn.Query("t", nil, nil, "", 0, 0)
	if res.RowCount() != 1 {
		t.Errorf("after delete RowCount = %d, want 1", res.RowCount())
	}
}

// TestParseODBCURL verifies URL parsing.
func TestParseODBCURL(t *testing.T) {
	cfg, err := parseODBCURL("unixodbc://alice:pw@mydsn")
	if err != nil {
		t.Fatalf("parseODBCURL: %v", err)
	}
	if cfg.User != "alice" {
		t.Errorf("user = %q, want alice", cfg.User)
	}
	if cfg.Password != "pw" {
		t.Errorf("password = %q, want pw", cfg.Password)
	}
	if cfg.DSN != "mydsn" {
		t.Errorf("dsn = %q, want mydsn", cfg.DSN)
	}
	// Raw DSN passthrough.
	cfg2, _ := parseODBCURL("rawdsn")
	if cfg2.DSN != "rawdsn" {
		t.Errorf("raw dsn = %q, want rawdsn", cfg2.DSN)
	}
}

// TestConcurrentAccess exercises the connection under -race.
func TestConcurrentAccess(t *testing.T) {
	conn := &ODBCConn{}
	if err := conn.Connect(DefaultODBCConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	keys := []db.DBKey{{Name: "id", Type: db.DBValInt}}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = conn.Insert("c", keys, []db.DBValue{{Type: db.DBValInt, IntVal: int64(n)}})
			_, _ = conn.Query("c", nil, nil, "", 0, 0)
		}(i)
	}
	wg.Wait()
	res, err := conn.Query("c", nil, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 20 {
		t.Errorf("RowCount = %d, want 20", res.RowCount())
	}
}
