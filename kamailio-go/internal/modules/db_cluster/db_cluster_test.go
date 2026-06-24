// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the db_cluster module.
 */

package db_cluster

import (
	"sync"
	"testing"
)

func TestAddGetRemoveNode(t *testing.T) {
	m := New()

	m.AddNode("db1", "mysql", "mysql://h1:3306")
	m.AddNode("db2", "postgres", "postgres://h2:5432")

	if got := m.NodeCount(); got != 2 {
		t.Errorf("NodeCount() = %d, want 2", got)
	}

	url, ok := m.GetNode("db1")
	if !ok {
		t.Fatalf("GetNode(db1) not found")
	}
	if url != "mysql://h1:3306" {
		t.Errorf("GetNode(db1) = %q, want %q", url, "mysql://h1:3306")
	}

	if _, ok := m.GetNode("nope"); ok {
		t.Errorf("GetNode(nope) should return false")
	}

	if !m.RemoveNode("db1") {
		t.Errorf("RemoveNode(db1) returned false")
	}
	if m.RemoveNode("db1") {
		t.Errorf("RemoveNode(db1) twice should return false")
	}
	if got := m.NodeCount(); got != 1 {
		t.Errorf("NodeCount() after remove = %d, want 1", got)
	}
}

func TestExecute(t *testing.T) {
	m := New()

	// No nodes -> error.
	if _, err := m.Execute("SELECT 1"); err == nil {
		t.Errorf("Execute with no nodes should error")
	}

	m.AddNode("db1", "mysql", "mysql://h1:3306")
	res, err := m.Execute("SELECT 1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res != "SELECT 1@mysql://h1:3306" {
		t.Errorf("Execute = %v, want %q", res, "SELECT 1@mysql://h1:3306")
	}
}

func TestAddNodeOverwrite(t *testing.T) {
	m := New()
	m.AddNode("n", "mysql", "url1")
	m.AddNode("n", "postgres", "url2")
	if got := m.NodeCount(); got != 1 {
		t.Errorf("NodeCount() after overwrite = %d, want 1", got)
	}
	url, _ := m.GetNode("n")
	if url != "url2" {
		t.Errorf("GetNode(n) = %q, want %q", url, "url2")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultDBCluster()
	if d == nil {
		t.Fatalf("DefaultDBCluster() returned nil")
	}
	if d != DefaultDBCluster() {
		t.Fatalf("DefaultDBCluster() returned different instances")
	}
	AddNode("pkg", "mysql", "mysql://p")
	if got := NodeCount(); got != 1 {
		t.Errorf("package NodeCount() = %d, want 1", got)
	}
	res, err := Execute("Q")
	if err != nil || res == nil {
		t.Errorf("package Execute = %v,%v", res, err)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultDBCluster()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := "n" + itoa(i)
			shared.AddNode(name, "mysql", "url")
			shared.GetNode(name)
			shared.Execute("Q")
			shared.NodeCount()
		}(i)
	}
	wg.Wait()
	if got := shared.NodeCount(); got != n {
		t.Errorf("NodeCount() after concurrent = %d, want %d", got, n)
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
