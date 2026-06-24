// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - PUsrloc module tests.
 */

package p_usrloc

import (
	"sync"
	"testing"
)

func TestStoreLoadDelete(t *testing.T) {
	m := New()
	m.Store("sip:alice@example.com", "sip:alice@10.0.0.1:5060")
	c, ok := m.Load("sip:alice@example.com")
	if !ok || c != "sip:alice@10.0.0.1:5060" {
		t.Fatalf("Load = %q, %v", c, ok)
	}
	if m.Count() != 1 {
		t.Fatalf("Count = %d, want 1", m.Count())
	}
	if !m.Delete("sip:alice@example.com") {
		t.Fatal("expected Delete true")
	}
	if _, ok := m.Load("sip:alice@example.com"); ok {
		t.Fatal("expected Load false after Delete")
	}
	if m.Count() != 0 {
		t.Fatalf("Count = %d, want 0", m.Count())
	}
}

func TestStoreReplaces(t *testing.T) {
	m := New()
	m.Store("aor", "c1")
	m.Store("aor", "c2")
	c, _ := m.Load("aor")
	if c != "c2" {
		t.Fatalf("Load = %q, want c2 (replaced)", c)
	}
	if m.Count() != 1 {
		t.Fatalf("Count = %d, want 1", m.Count())
	}
}

func TestLoadDeleteUnknown(t *testing.T) {
	m := New()
	if _, ok := m.Load("missing"); ok {
		t.Fatal("expected Load false for missing")
	}
	if m.Delete("missing") {
		t.Fatal("expected Delete false for missing")
	}
}

func TestStoreIgnoresEmptyAOR(t *testing.T) {
	m := New()
	m.Store("", "contact")
	if m.Count() != 0 {
		t.Fatalf("Count = %d, want 0 (empty AOR ignored)", m.Count())
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	Store("sip:bob@example.com", "sip:bob@10.0.0.2")
	c, ok := Load("sip:bob@example.com")
	if !ok || c != "sip:bob@10.0.0.2" {
		t.Fatalf("global Load = %q, %v", c, ok)
	}
	if Count() < 1 {
		t.Fatalf("global Count = %d, want >=1", Count())
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Store("aor", "c")
			_, _ = m.Load("aor")
			_ = m.Count()
		}(i)
	}
	wg.Wait()
}
