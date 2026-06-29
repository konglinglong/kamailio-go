// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - MiscRadius module tests.
 */

package misc_radius

import (
	"sync"
	"testing"
)

func TestInitAndSend(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("expected not connected before Init")
	}
	m.Init("radius.example.com:1812", "s3cr3t")
	if !m.IsConnected() {
		t.Fatal("expected connected after Init")
	}
	if err := m.Send("alice", map[string]string{"NAS-IP-Address": "10.0.0.1"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := m.Count(); got != 1 {
		t.Fatalf("Count = %d, want 1", got)
	}
	reqs := m.RequestsForUser("alice")
	if len(reqs) != 1 || reqs[0].Attrs["NAS-IP-Address"] != "10.0.0.1" {
		t.Fatalf("unexpected requests: %+v", reqs)
	}
}

func TestSendErrors(t *testing.T) {
	m := New()
	if err := m.Send("alice", nil); err == nil {
		t.Fatal("expected error when not connected")
	}
	m.Init("srv", "secret")
	if err := m.Send("", map[string]string{"a": "b"}); err == nil {
		t.Fatal("expected error for empty user")
	}
}

func TestClose(t *testing.T) {
	m := New()
	m.Init("srv", "secret")
	m.Send("alice", map[string]string{"k": "v"})
	m.Close()
	if m.IsConnected() {
		t.Fatal("expected not connected after Close")
	}
	if err := m.Send("alice", nil); err == nil {
		t.Fatal("expected error when sending after Close")
	}
}

func TestRequestsCopyIsolation(t *testing.T) {
	m := New()
	m.Init("srv", "secret")
	m.Send("alice", map[string]string{"k": "v"})
	reqs := m.Requests()
	reqs[0].Attrs["k"] = "mutated"
	again := m.RequestsForUser("alice")
	if again[0].Attrs["k"] != "v" {
		t.Fatalf("expected isolation from Requests copy, got %q", again[0].Attrs["k"])
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init("global-srv", "global-secret")
	if !IsConnected() {
		t.Fatal("expected global connected")
	}
	if err := Send("bob", map[string]string{"x": "1"}); err != nil {
		t.Fatalf("global Send: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.Init("srv", "secret")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = m.Send("user", map[string]string{"i": "v"})
			_ = m.Requests()
			_ = m.Count()
		}(i)
	}
	wg.Wait()
	if m.Count() != 20 {
		t.Fatalf("Count = %d, want 20", m.Count())
	}
}
