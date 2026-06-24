// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - NDBRedis module tests.
 */

package ndb_redis

import (
	"sync"
	"testing"
)

func TestSetGetDel(t *testing.T) {
	m := New()
	m.Init("localhost:6379")
	if err := m.Set("k1", "v1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := m.Get("k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "v1" {
		t.Fatalf("Get = %q, want v1", v)
	}
	if !m.Exists("k1") {
		t.Fatal("expected Exists true")
	}
	if err := m.Del("k1"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	if m.Exists("k1") {
		t.Fatal("expected Exists false after Del")
	}
	v, _ = m.Get("k1")
	if v != "" {
		t.Fatalf("Get after Del = %q, want empty", v)
	}
}

func TestErrors(t *testing.T) {
	m := New()
	if err := m.Set("k", "v"); err == nil {
		t.Fatal("expected error when not connected")
	}
	if _, err := m.Get("k"); err == nil {
		t.Fatal("expected error when not connected")
	}
	if err := m.Del("k"); err == nil {
		t.Fatal("expected error when not connected")
	}
	m.Init("addr")
	if err := m.Set("", "v"); err == nil {
		t.Fatal("expected error for empty key")
	}
	if _, err := m.Get(""); err == nil {
		t.Fatal("expected error for empty key")
	}
	if err := m.Del(""); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestClose(t *testing.T) {
	m := New()
	m.Init("addr")
	m.Set("k", "v")
	m.Close()
	if m.IsConnected() {
		t.Fatal("expected not connected after Close")
	}
	if err := m.Set("k", "v"); err == nil {
		t.Fatal("expected error when setting after Close")
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init("global-addr")
	if !IsConnected() {
		t.Fatal("expected global connected")
	}
	if err := Set("gk", "gv"); err != nil {
		t.Fatalf("global Set: %v", err)
	}
	v, err := Get("gk")
	if err != nil || v != "gv" {
		t.Fatalf("global Get: %v, %q", err, v)
	}
	if err := Del("gk"); err != nil {
		t.Fatalf("global Del: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.Init("addr")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = m.Set("k", "v")
			_, _ = m.Get("k")
			_ = m.Exists("k")
		}(i)
	}
	wg.Wait()
}
