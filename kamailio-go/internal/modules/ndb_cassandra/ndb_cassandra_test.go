// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - NDBCassandra module tests.
 */

package ndb_cassandra

import (
	"sync"
	"testing"
)

func TestInitQueryExecute(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("expected not connected before Init")
	}
	m.Init("host1:9042,host2:9042")
	if !m.IsConnected() {
		t.Fatal("expected connected after Init")
	}
	res, err := m.Query("SELECT * FROM ks.users WHERE id = 1")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	r, ok := res.(Result)
	if !ok {
		t.Fatalf("expected Result, got %T", res)
	}
	if len(r.Rows) != 1 || r.Rows[0]["cql"] != "SELECT * FROM ks.users WHERE id = 1" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if err := m.Execute("INSERT INTO ks.users (id) VALUES (1)"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(m.Queries()) != 1 || len(m.Executed()) != 1 {
		t.Fatalf("unexpected buffer sizes: queries=%d executed=%d", len(m.Queries()), len(m.Executed()))
	}
}

func TestErrors(t *testing.T) {
	m := New()
	if _, err := m.Query("SELECT 1"); err == nil {
		t.Fatal("expected error when not connected")
	}
	if err := m.Execute("INSERT"); err == nil {
		t.Fatal("expected error when not connected")
	}
	m.Init("host")
	if _, err := m.Query(""); err == nil {
		t.Fatal("expected error for empty cql")
	}
	if err := m.Execute("   "); err == nil {
		t.Fatal("expected error for whitespace cql")
	}
}

func TestClose(t *testing.T) {
	m := New()
	m.Init("host")
	m.Query("SELECT 1")
	m.Close()
	if m.IsConnected() {
		t.Fatal("expected not connected after Close")
	}
	if _, err := m.Query("SELECT 1"); err == nil {
		t.Fatal("expected error when querying after Close")
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init("global-host")
	if !IsConnected() {
		t.Fatal("expected global connected")
	}
	if _, err := Query("SELECT 1"); err != nil {
		t.Fatalf("global Query: %v", err)
	}
	if err := Execute("INSERT"); err != nil {
		t.Fatalf("global Execute: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.Init("host")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Query("SELECT 1")
			_ = m.Execute("INSERT")
			_ = m.Queries()
			_ = m.Executed()
		}()
	}
	wg.Wait()
}
