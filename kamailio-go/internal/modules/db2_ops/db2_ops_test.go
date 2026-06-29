// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the db2_ops module.
 */

package db2_ops

import (
	"sync"
	"testing"
)

func TestInsertQuery(t *testing.T) {
	m := New()

	if err := m.Insert("users", map[string]string{"id": "1", "name": "alice"}); err != nil {
		t.Fatalf("Insert error: %v", err)
	}
	m.Insert("users", map[string]string{"id": "2", "name": "bob"})
	m.Insert("users", map[string]string{"id": "3", "name": "alice"})

	// Query all.
	rows, err := m.Query("users", nil)
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("Query(all) = %d rows, want 3", len(rows))
	}

	// Query with conditions.
	rows, _ = m.Query("users", map[string]string{"name": "alice"})
	if len(rows) != 2 {
		t.Errorf("Query(name=alice) = %d rows, want 2", len(rows))
	}

	// Unknown table.
	if _, err := m.Query("nope", nil); err == nil {
		t.Errorf("Query(unknown) should error")
	}
}

func TestUpdate(t *testing.T) {
	m := New()
	m.Insert("t", map[string]string{"id": "1", "v": "a"})
	m.Insert("t", map[string]string{"id": "2", "v": "a"})

	n, err := m.Update("t", map[string]string{"v": "a"}, map[string]string{"v": "b"})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if n != 2 {
		t.Errorf("Update returned %d, want 2", n)
	}
	rows, _ := m.Query("t", map[string]string{"v": "b"})
	if len(rows) != 2 {
		t.Errorf("Query(v=b) = %d, want 2", len(rows))
	}
}

func TestDelete(t *testing.T) {
	m := New()
	m.Insert("t", map[string]string{"id": "1", "g": "x"})
	m.Insert("t", map[string]string{"id": "2", "g": "x"})
	m.Insert("t", map[string]string{"id": "3", "g": "y"})

	n, err := m.Delete("t", map[string]string{"g": "x"})
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if n != 2 {
		t.Errorf("Delete returned %d, want 2", n)
	}
	rows, _ := m.Query("t", nil)
	if len(rows) != 1 {
		t.Errorf("Query after delete = %d, want 1", len(rows))
	}
}

func TestInsertEmptyTable(t *testing.T) {
	m := New()
	if err := m.Insert("", map[string]string{"a": "b"}); err == nil {
		t.Errorf("Insert(\"\") should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultDB2Ops()
	if d == nil {
		t.Fatalf("DefaultDB2Ops() returned nil")
	}
	if err := Insert("pkg", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("package Insert error: %v", err)
	}
	rows, err := Query("pkg", map[string]string{"k": "v"})
	if err != nil || len(rows) != 1 {
		t.Errorf("package Query = %d,%v", len(rows), err)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultDB2Ops()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			shared.Insert("c", map[string]string{"id": itoa(i)})
			shared.Query("c", map[string]string{"id": itoa(i)})
		}(i)
	}
	wg.Wait()
	rows, _ := shared.Query("c", nil)
	if len(rows) != n {
		t.Errorf("Query after concurrent = %d, want %d", len(rows), n)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
