// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - influxdbc module tests.
 */

package influxdbc

import (
	"sync"
	"testing"
)

func TestInitWriteQuery(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("new module should not be connected")
	}
	m.Init("http://localhost:8086", "telegraf")
	if !m.IsConnected() {
		t.Fatal("module should be connected after Init")
	}
	if err := m.Write("cpu", map[string]string{"host": "s1"}, map[string]string{"value": "0.42"}); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := m.Write("mem", map[string]string{"host": "s1"}, map[string]string{"used": "1024"}); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	rows, err := m.Query("cpu")
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Query cpu rows = %d, want 1", len(rows))
	}
	if rows[0][0] != "cpu" {
		t.Errorf("row[0] = %q, want cpu", rows[0][0])
	}
	all, _ := m.Query("")
	if len(all) != 2 {
		t.Errorf("Query '' rows = %d, want 2", len(all))
	}
}

func TestWriteErrors(t *testing.T) {
	m := New()
	// Not connected.
	if err := m.Write("cpu", nil, map[string]string{"v": "1"}); err == nil {
		t.Errorf("Write should error when not connected")
	}
	m.Init("http://x", "db")
	// Empty measurement.
	if err := m.Write("", nil, nil); err == nil {
		t.Errorf("Write should error for empty measurement")
	}
}

func TestQueryNotConnected(t *testing.T) {
	m := New()
	if _, err := m.Query(""); err == nil {
		t.Errorf("Query should error when not connected")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultInfluxDBC()
	b := DefaultInfluxDBC()
	if a != b {
		t.Fatal("DefaultInfluxDBC should return the same instance")
	}
	a.Init("addr", "db")
	if !a.IsConnected() {
		t.Fatal("default should be connected")
	}
	Init()
	c := DefaultInfluxDBC()
	if c == a {
		t.Fatal("package Init should reset the default instance")
	}
	if c.IsConnected() {
		t.Errorf("reset default should not be connected")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	m.Init("addr", "db")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Write("cpu", map[string]string{"h": "s"}, map[string]string{"v": "1"})
			_, _ = m.Query("cpu")
		}()
	}
	wg.Wait()
}
