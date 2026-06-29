// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - db_oracle module tests.
 *
 * These tests do NOT require a running Oracle server. They exercise config
 * validation, DSN building, driver construction, registration, the mock
 * version accessor and the in-memory mock store.
 */

package db_oracle

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// TestOracleConfig verifies defaults, validation and DSN construction.
func TestOracleConfig(t *testing.T) {
	cfg := DefaultOracleConfig()
	if cfg.Host != "localhost" {
		t.Errorf("default host = %q, want localhost", cfg.Host)
	}
	if cfg.Port != DefaultOraclePort {
		t.Errorf("default port = %d, want %d", cfg.Port, DefaultOraclePort)
	}
	if cfg.SID != "ORCL" {
		t.Errorf("default sid = %q, want ORCL", cfg.SID)
	}
	if cfg.MaxOpenConns != DefaultMaxOpenConns {
		t.Errorf("default maxOpenConns = %d, want %d", cfg.MaxOpenConns, DefaultMaxOpenConns)
	}

	cfg.User = "alice"
	cfg.Password = "pw"
	cfg.Host = "ora.example.com"
	cfg.Port = 1522
	cfg.SID = "KAM"
	dsn := cfg.DSN()
	want := "alice/pw@ora.example.com:1522/KAM"
	if dsn != want {
		t.Errorf("DSN() = %q, want %q", dsn, want)
	}

	tests := []struct {
		name string
		cfg  *OracleConfig
		want string
	}{
		{"nil config", nil, "nil"},
		{"empty host", &OracleConfig{Port: 1521, SID: "s", User: "u"}, "host"},
		{"zero port", &OracleConfig{Host: "h", Port: 0, SID: "s", User: "u"}, "port"},
		{"oversized port", &OracleConfig{Host: "h", Port: 70000, SID: "s", User: "u"}, "port"},
		{"empty sid", &OracleConfig{Host: "h", Port: 1521, User: "u"}, "sid"},
		{"empty user", &OracleConfig{Host: "h", Port: 1521, SID: "s"}, "user"},
		{"negative conns", &OracleConfig{Host: "h", Port: 1521, SID: "s", User: "u", MaxOpenConns: -1}, "maxOpenConns"},
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
	d := NewOracleDriver()
	if d == nil {
		t.Fatal("NewOracleDriver returned nil")
	}
	if d.Name() != "oracle" {
		t.Errorf("Name() = %q, want oracle", d.Name())
	}
	if got := db.GetDriver("oracle"); got == nil {
		t.Error("oracle driver not registered")
	}
	found := false
	for _, name := range db.RegisteredDrivers() {
		if name == "oracle" {
			found = true
			break
		}
	}
	if !found {
		t.Error("oracle not found in RegisteredDrivers()")
	}
}

// TestConnect exercises Connect success and failure paths.
func TestConnect(t *testing.T) {
	conn := &OracleConn{}
	if err := conn.Connect(nil); err == nil {
		t.Fatal("expected error for nil config")
	}
	if err := conn.Connect(&OracleConfig{}); err == nil {
		t.Fatal("expected error for invalid config")
	}
	if err := conn.Connect(DefaultOracleConfig()); err != nil {
		t.Fatalf("Connect valid config: %v", err)
	}
}

// TestGetDSN verifies GetDSN returns the Oracle connection string.
func TestGetDSN(t *testing.T) {
	conn := &OracleConn{}
	if got := conn.GetDSN(); got != "" {
		t.Errorf("GetDSN before connect = %q, want empty", got)
	}
	cfg := DefaultOracleConfig()
	cfg.User = "alice"
	cfg.Password = "pw"
	cfg.Host = "ora.host"
	cfg.Port = 1522
	cfg.SID = "KAM"
	if err := conn.Connect(cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := conn.GetDSN(); !strings.Contains(got, "alice/pw@ora.host:1522/KAM") {
		t.Errorf("GetDSN = %q, want alice/pw@ora.host:1522/KAM", got)
	}
}

// TestGetVersion verifies the mock version accessor.
func TestGetVersion(t *testing.T) {
	conn := &OracleConn{}
	if err := conn.Connect(DefaultOracleConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	v, err := conn.GetVersion()
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v == "" {
		t.Error("GetVersion returned empty string")
	}
	if !strings.Contains(v, "Oracle") {
		t.Errorf("version = %q, want Oracle substring", v)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := conn.GetVersion(); err == nil {
		t.Error("GetVersion after close: expected error")
	}
}

// TestPingClose verifies Ping/Close behaviour.
func TestPingClose(t *testing.T) {
	conn := &OracleConn{}
	if err := conn.Connect(DefaultOracleConfig()); err != nil {
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
	conn := &OracleConn{}
	if err := conn.Connect(DefaultOracleConfig()); err != nil {
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
	conn := &OracleConn{}
	if err := conn.Connect(DefaultOracleConfig()); err != nil {
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

// TestParseOracleURL verifies URL parsing.
func TestParseOracleURL(t *testing.T) {
	cfg, err := parseOracleURL("oracle://alice:pw@ora.host:1522/KAM")
	if err != nil {
		t.Fatalf("parseOracleURL: %v", err)
	}
	if cfg.User != "alice" {
		t.Errorf("user = %q, want alice", cfg.User)
	}
	if cfg.Password != "pw" {
		t.Errorf("password = %q, want pw", cfg.Password)
	}
	if cfg.Host != "ora.host" {
		t.Errorf("host = %q, want ora.host", cfg.Host)
	}
	if cfg.Port != 1522 {
		t.Errorf("port = %d, want 1522", cfg.Port)
	}
	if cfg.SID != "KAM" {
		t.Errorf("sid = %q, want KAM", cfg.SID)
	}
}

// TestConcurrentAccess exercises the connection under -race.
func TestConcurrentAccess(t *testing.T) {
	conn := &OracleConn{}
	if err := conn.Connect(DefaultOracleConfig()); err != nil {
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
