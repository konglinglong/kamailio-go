// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - db_flatstore module tests.
 *
 * These tests use temporary directories; no external resources required.
 */

package db_flatstore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// TestWriteRowReadTable verifies the append/read round-trip.
func TestWriteRowReadTable(t *testing.T) {
	dir := t.TempDir()
	conn := &FlatConn{cfg: &FlatConfig{Path: dir, Delimiter: "|", Suffix: ".log"}, files: make(map[string]bool)}

	rows, err := conn.ReadTable("acc")
	if err != nil {
		t.Fatalf("ReadTable empty: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("empty rows = %v, want none", rows)
	}

	if err := conn.WriteRow("acc", []string{"1", "alice", "bob"}); err != nil {
		t.Fatalf("WriteRow: %v", err)
	}
	if err := conn.WriteRow("acc", []string{"2", "carol", "dave"}); err != nil {
		t.Fatalf("WriteRow 2: %v", err)
	}

	rows, err = conn.ReadTable("acc")
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][1] != "alice" {
		t.Errorf("rows[0] = %v, want alice at idx 1", rows[0])
	}

	// File on disk uses the configured delimiter and suffix.
	data, err := os.ReadFile(filepath.Join(dir, "acc.log"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "1|alice|bob") {
		t.Errorf("file content = %q, want delimited row", string(data))
	}
}

// TestFlush verifies Flush is a no-op returning no error.
func TestFlush(t *testing.T) {
	conn := &FlatConn{cfg: DefaultFlatConfig(), files: make(map[string]bool)}
	if err := conn.Flush(); err != nil {
		t.Errorf("Flush: %v", err)
	}
}

// TestRotate verifies the current log file is renamed on rotation.
func TestRotate(t *testing.T) {
	dir := t.TempDir()
	conn := &FlatConn{cfg: &FlatConfig{Path: dir, Delimiter: "|", Suffix: ".log"}, files: make(map[string]bool)}

	if err := conn.WriteRow("log", []string{"a", "b"}); err != nil {
		t.Fatalf("WriteRow: %v", err)
	}
	orig := filepath.Join(dir, "log.log")
	if _, err := os.Stat(orig); err != nil {
		t.Fatalf("orig file missing: %v", err)
	}

	if err := conn.Rotate(); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if _, err := os.Stat(orig); !os.IsNotExist(err) {
		t.Errorf("orig file still exists after rotate: %v", err)
	}

	// A rotated file should exist.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "log.log.") {
			found = true
			break
		}
	}
	if !found {
		t.Error("no rotated file found")
	}

	// Subsequent writes start a new file.
	if err := conn.WriteRow("log", []string{"c", "d"}); err != nil {
		t.Fatalf("WriteRow after rotate: %v", err)
	}
	rows, err := conn.ReadTable("log")
	if err != nil {
		t.Fatalf("ReadTable after rotate: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("rows after rotate = %d, want 1", len(rows))
	}
}

// TestNewDriver verifies driver construction, name and registration.
func TestNewDriver(t *testing.T) {
	d := NewFlatDriver()
	if d == nil {
		t.Fatal("NewFlatDriver returned nil")
	}
	if d.Name() != "flatstore" {
		t.Errorf("Name() = %q, want flatstore", d.Name())
	}
	if got := db.GetDriver("flatstore"); got == nil {
		t.Error("flatstore driver not registered")
	}
	found := false
	for _, name := range db.RegisteredDrivers() {
		if name == "flatstore" {
			found = true
			break
		}
	}
	if !found {
		t.Error("flatstore not found in RegisteredDrivers()")
	}
}

// TestFlatConfig verifies defaults and validation.
func TestFlatConfig(t *testing.T) {
	cfg := DefaultFlatConfig()
	if cfg.Delimiter != DefaultFlatDelimiter {
		t.Errorf("default delimiter = %q, want %q", cfg.Delimiter, DefaultFlatDelimiter)
	}
	if cfg.Suffix != DefaultFlatSuffix {
		t.Errorf("default suffix = %q, want %q", cfg.Suffix, DefaultFlatSuffix)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid config: %v", err)
	}
	if err := (&FlatConfig{Delimiter: ""}).Validate(); err == nil {
		t.Error("expected error for empty delimiter")
	}
	if err := (&FlatConfig{Delimiter: "|", Suffix: ""}).Validate(); err == nil {
		t.Error("expected error for empty suffix")
	}
}

// TestFlatConnInsertQuery verifies the db.DBConn interface via FlatConn.
func TestFlatConnInsertQuery(t *testing.T) {
	dir := t.TempDir()
	conn := &FlatConn{cfg: &FlatConfig{Path: dir, Delimiter: "|", Suffix: ".log"}, files: make(map[string]bool)}

	keys := []db.DBKey{{Name: "id", Type: db.DBValString}, {Name: "from", Type: db.DBValString}}
	if err := conn.Insert("acc", keys, []db.DBValue{
		{Type: db.DBValString, StrVal: "1"},
		{Type: db.DBValString, StrVal: "alice"},
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := conn.Insert("acc", keys, []db.DBValue{
		{Type: db.DBValString, StrVal: "2"},
		{Type: db.DBValString, StrVal: "bob"},
	}); err != nil {
		t.Fatalf("Insert 2: %v", err)
	}

	res, err := conn.Query("acc", nil, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 2 {
		t.Fatalf("RowCount = %d, want 2", res.RowCount())
	}
	if got := res.Row(0).GetString("c1"); got != "alice" {
		t.Errorf("c1 = %q, want alice", got)
	}

	// Replace behaves like Insert for an append-only store.
	if err := conn.Replace("acc", keys, []db.DBValue{
		{Type: db.DBValString, StrVal: "3"},
		{Type: db.DBValString, StrVal: "carol"},
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	res, _ = conn.Query("acc", nil, nil, "", 0, 0)
	if res.RowCount() != 3 {
		t.Errorf("after replace RowCount = %d, want 3", res.RowCount())
	}

	// Update/Delete are no-ops for an append-only store.
	n, err := conn.Update("acc", nil, nil, nil)
	if err != nil || n != 0 {
		t.Errorf("Update = (%d, %v), want (0, nil)", n, err)
	}
	d, err := conn.Delete("acc", nil)
	if err != nil || d != 0 {
		t.Errorf("Delete = (%d, %v), want (0, nil)", d, err)
	}
}

// TestPingClose verifies Ping/Close behaviour.
func TestPingClose(t *testing.T) {
	conn := &FlatConn{cfg: DefaultFlatConfig(), files: make(map[string]bool)}
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

// TestConcurrentWrite exercises the connection under -race.
func TestConcurrentWrite(t *testing.T) {
	dir := t.TempDir()
	conn := &FlatConn{cfg: &FlatConfig{Path: dir, Delimiter: "|", Suffix: ".log"}, files: make(map[string]bool)}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = conn.WriteRow("acc", []string{"x", "y"})
			_, _ = conn.ReadTable("acc")
		}(i)
	}
	wg.Wait()
	rows, err := conn.ReadTable("acc")
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	if len(rows) != 20 {
		t.Errorf("rows = %d, want 20", len(rows))
	}
}
