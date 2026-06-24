// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - NDBMongo module tests.
 */

package ndb_mongodb

import (
	"sync"
	"testing"
)

func TestInsertFind(t *testing.T) {
	m := New()
	m.Init("mongodb://localhost:27017")
	if err := m.Insert("db", "users", map[string]interface{}{"name": "alice", "age": 30}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := m.Insert("db", "users", map[string]interface{}{"name": "bob", "age": 25}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if got := m.Count("db", "users"); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}
	all, err := m.Find("db", "users", nil)
	if err != nil {
		t.Fatalf("Find all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("Find all = %d, want 2", len(all))
	}
	filtered, err := m.Find("db", "users", map[string]interface{}{"name": "alice"})
	if err != nil {
		t.Fatalf("Find filtered: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("Find filtered = %d, want 1", len(filtered))
	}
}

func TestErrors(t *testing.T) {
	m := New()
	if err := m.Insert("db", "c", map[string]interface{}{"k": "v"}); err == nil {
		t.Fatal("expected error when not connected")
	}
	if _, err := m.Find("db", "c", nil); err == nil {
		t.Fatal("expected error when not connected")
	}
	m.Init("uri")
	if err := m.Insert("", "c", nil); err == nil {
		t.Fatal("expected error for empty db")
	}
	if err := m.Insert("db", "", nil); err == nil {
		t.Fatal("expected error for empty collection")
	}
}

func TestClose(t *testing.T) {
	m := New()
	m.Init("uri")
	m.Insert("db", "c", map[string]interface{}{"k": "v"})
	m.Close()
	if m.IsConnected() {
		t.Fatal("expected not connected after Close")
	}
	if err := m.Insert("db", "c", nil); err == nil {
		t.Fatal("expected error when inserting after Close")
	}
}

func TestFindCopyIsolation(t *testing.T) {
	m := New()
	m.Init("uri")
	m.Insert("db", "c", map[string]interface{}{"k": "v"})
	res, _ := m.Find("db", "c", nil)
	d := res[0].(map[string]interface{})
	d["k"] = "mutated"
	again, _ := m.Find("db", "c", nil)
	if again[0].(map[string]interface{})["k"] != "v" {
		t.Fatal("expected isolation from Find copy")
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init("global-uri")
	if !IsConnected() {
		t.Fatal("expected global connected")
	}
	if err := Insert("db", "c", map[string]interface{}{"k": "v"}); err != nil {
		t.Fatalf("global Insert: %v", err)
	}
	res, err := Find("db", "c", nil)
	if err != nil || len(res) != 1 {
		t.Fatalf("global Find: %v, %d", err, len(res))
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.Init("uri")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Insert("db", "c", map[string]interface{}{"k": "v"})
			_, _ = m.Find("db", "c", nil)
			_ = m.Count("db", "c")
		}()
	}
	wg.Wait()
	if m.Count("db", "c") != 20 {
		t.Fatalf("Count = %d, want 20", m.Count("db", "c"))
	}
}
