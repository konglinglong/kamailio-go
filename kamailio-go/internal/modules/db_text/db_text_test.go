// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - db_text module tests.
 *
 * These tests use temporary files and directories; no external resources
 * are required.
 */

package db_text

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// writeFile writes content to a temp file and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestLoadSaveTable verifies round-tripping a table through a temp file.
func TestLoadSaveTable(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "users.txt", "id,user\n1,alice\n2,bob\n")

	tbl, err := LoadTable(src)
	if err != nil {
		t.Fatalf("LoadTable: %v", err)
	}
	if len(tbl.Headers) != 2 || tbl.Headers[0] != "id" || tbl.Headers[1] != "user" {
		t.Errorf("headers = %v, want [id user]", tbl.Headers)
	}
	if len(tbl.Rows) != 2 {
		t.Fatalf("rows = %v, want 2", tbl.Rows)
	}
	if tbl.Rows[0][1] != "alice" {
		t.Errorf("rows[0] = %v, want alice at idx 1", tbl.Rows[0])
	}

	dst := filepath.Join(dir, "users_copy.txt")
	if err := SaveTable(tbl, dst); err != nil {
		t.Fatalf("SaveTable: %v", err)
	}
	again, err := LoadTable(dst)
	if err != nil {
		t.Fatalf("LoadTable copy: %v", err)
	}
	if len(again.Rows) != 2 {
		t.Errorf("copy rows = %d, want 2", len(again.Rows))
	}
}

// TestQuery verifies filtering rows by conditions.
func TestQuery(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "t.txt", "id,user\n1,alice\n2,bob\n3,alice\n")

	rows, err := Query(p, map[string]string{"user": "alice"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r[1] != "alice" {
			t.Errorf("row = %v, want user alice", r)
		}
	}

	all, err := Query(p, nil)
	if err != nil {
		t.Fatalf("Query all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all rows = %d, want 3", len(all))
	}
}

// TestInsertRow verifies appending rows to a table file.
func TestInsertRow(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "t.txt", "id,user\n")

	if err := InsertRow(p, []string{"1", "alice"}); err != nil {
		t.Fatalf("InsertRow: %v", err)
	}
	if err := InsertRow(p, []string{"2", "bob"}); err != nil {
		t.Fatalf("InsertRow 2: %v", err)
	}
	rows, err := Query(p, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][0] != "1" || rows[0][1] != "alice" {
		t.Errorf("rows[0] = %v", rows[0])
	}
}

// TestDeleteRow verifies removing rows by conditions.
func TestDeleteRow(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "t.txt", "id,user\n1,alice\n2,bob\n3,alice\n")

	n, err := DeleteRow(p, map[string]string{"user": "alice"})
	if err != nil {
		t.Fatalf("DeleteRow: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted = %d, want 2", n)
	}
	rows, err := Query(p, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("remaining rows = %d, want 1", len(rows))
	}
	if rows[0][1] != "bob" {
		t.Errorf("remaining row = %v, want bob", rows[0])
	}
}

// TestNewDriver verifies driver construction, name and registration.
func TestNewDriver(t *testing.T) {
	d := NewTextDriver()
	if d == nil {
		t.Fatal("NewTextDriver returned nil")
	}
	if d.Name() != "text" {
		t.Errorf("Name() = %q, want text", d.Name())
	}
	if got := db.GetDriver("text"); got == nil {
		t.Error("text driver not registered")
	}
	found := false
	for _, name := range db.RegisteredDrivers() {
		if name == "text" {
			found = true
			break
		}
	}
	if !found {
		t.Error("text not found in RegisteredDrivers()")
	}
}

// TestTextConfig verifies defaults and validation.
func TestTextConfig(t *testing.T) {
	cfg := DefaultTextConfig()
	if cfg.Delimiter != DefaultDelimiter {
		t.Errorf("default delimiter = %q, want %q", cfg.Delimiter, DefaultDelimiter)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid config: %v", err)
	}
	bad := &TextConfig{Delimiter: ""}
	if err := bad.Validate(); err == nil {
		t.Error("expected error for empty delimiter")
	}
}

// TestTextConnInsertQuery verifies the db.DBConn interface via TextConn.
func TestTextConnInsertQuery(t *testing.T) {
	dir := t.TempDir()
	conn := &TextConn{cfg: &TextConfig{DBPath: dir, Delimiter: DefaultDelimiter}}

	keys := []db.DBKey{
		{Name: "id", Type: db.DBValString},
		{Name: "user", Type: db.DBValString},
	}
	if err := conn.Insert("loc", keys, []db.DBValue{
		{Type: db.DBValString, StrVal: "1"},
		{Type: db.DBValString, StrVal: "alice"},
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := conn.Insert("loc", keys, []db.DBValue{
		{Type: db.DBValString, StrVal: "2"},
		{Type: db.DBValString, StrVal: "bob"},
	}); err != nil {
		t.Fatalf("Insert 2: %v", err)
	}

	res, err := conn.Query("loc", nil, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 2 {
		t.Fatalf("RowCount = %d, want 2", res.RowCount())
	}
	if got := res.Row(0).GetString("user"); got != "alice" {
		t.Errorf("user = %q, want alice", got)
	}

	// Filtered query.
	res, err = conn.Query("loc", nil, []db.DBCondition{
		{Key: "user", Op: "=", Value: db.DBValue{Type: db.DBValString, StrVal: "bob"}},
	}, "", 0, 0)
	if err != nil {
		t.Fatalf("Query filtered: %v", err)
	}
	if res.RowCount() != 1 {
		t.Fatalf("filtered RowCount = %d, want 1", res.RowCount())
	}
	if got := res.Row(0).GetString("id"); got != "2" {
		t.Errorf("id = %q, want 2", got)
	}
}

// TestTextConnUpdateDelete verifies Update and Delete via TextConn.
func TestTextConnUpdateDelete(t *testing.T) {
	dir := t.TempDir()
	conn := &TextConn{cfg: &TextConfig{DBPath: dir, Delimiter: DefaultDelimiter}}
	keys := []db.DBKey{{Name: "id", Type: db.DBValString}, {Name: "v", Type: db.DBValString}}
	conn.Insert("t", keys, []db.DBValue{{Type: db.DBValString, StrVal: "1"}, {Type: db.DBValString, StrVal: "a"}})
	conn.Insert("t", keys, []db.DBValue{{Type: db.DBValString, StrVal: "2"}, {Type: db.DBValString, StrVal: "b"}})

	n, err := conn.Update("t",
		[]db.DBKey{{Name: "v", Type: db.DBValString}},
		[]db.DBValue{{Type: db.DBValString, StrVal: "z"}},
		[]db.DBCondition{{Key: "id", Op: "=", Value: db.DBValue{Type: db.DBValString, StrVal: "2"}}})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if n != 1 {
		t.Errorf("Update affected = %d, want 1", n)
	}
	res, _ := conn.Query("t", nil, []db.DBCondition{{Key: "id", Op: "=", Value: db.DBValue{Type: db.DBValString, StrVal: "2"}}}, "", 0, 0)
	if res.Row(0).GetString("v") != "z" {
		t.Errorf("v = %q, want z", res.Row(0).GetString("v"))
	}

	deleted, err := conn.Delete("t", []db.DBCondition{{Key: "id", Op: "=", Value: db.DBValue{Type: db.DBValString, StrVal: "1"}}})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Delete = %d, want 1", deleted)
	}
	res, _ = conn.Query("t", nil, nil, "", 0, 0)
	if res.RowCount() != 1 {
		t.Errorf("after delete RowCount = %d, want 1", res.RowCount())
	}
}

// TestTextConnReplace verifies upsert behaviour via TextConn.
func TestTextConnReplace(t *testing.T) {
	dir := t.TempDir()
	conn := &TextConn{cfg: &TextConfig{DBPath: dir, Delimiter: DefaultDelimiter}}
	keys := []db.DBKey{{Name: "id", Type: db.DBValString}, {Name: "v", Type: db.DBValString}}
	if err := conn.Replace("t", keys, []db.DBValue{{Type: db.DBValString, StrVal: "1"}, {Type: db.DBValString, StrVal: "a"}}); err != nil {
		t.Fatalf("Replace insert: %v", err)
	}
	if err := conn.Replace("t", keys, []db.DBValue{{Type: db.DBValString, StrVal: "1"}, {Type: db.DBValString, StrVal: "b"}}); err != nil {
		t.Fatalf("Replace update: %v", err)
	}
	res, _ := conn.Query("t", nil, nil, "", 0, 0)
	if res.RowCount() != 1 {
		t.Errorf("after replace RowCount = %d, want 1", res.RowCount())
	}
	if res.Row(0).GetString("v") != "b" {
		t.Errorf("v = %q, want b", res.Row(0).GetString("v"))
	}
}

// TestCustomDelimiter verifies a non-default delimiter is honoured.
func TestCustomDelimiter(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "t.txt", "id:user\n1:alice\n")
	rows, err := queryDelim(p, ":", nil)
	if err != nil {
		t.Fatalf("queryDelim: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if strings.Join(rows[0], ":") != "1:alice" {
		t.Errorf("row = %v", rows[0])
	}
}
