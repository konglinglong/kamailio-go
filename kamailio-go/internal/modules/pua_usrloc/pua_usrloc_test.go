// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - pua_usrloc module tests.
 */

package pua_usrloc

import (
	"sync"
	"testing"
)

func TestContactAddedRemoved(t *testing.T) {
	m := New()
	m.OnContactAdded("sip:alice@example.com", "sip:alice@10.0.0.1")
	if got := m.GetContact("sip:alice@example.com"); got != "sip:alice@10.0.0.1" {
		t.Errorf("GetContact = %q", got)
	}
	subs := m.GetSubscribers()
	if len(subs) != 1 {
		t.Fatalf("GetSubscribers len = %d, want 1", len(subs))
	}
	m.OnContactRemoved("sip:alice@example.com")
	if got := m.GetContact("sip:alice@example.com"); got != "" {
		t.Errorf("GetContact after remove = %q, want empty", got)
	}
	if len(m.GetSubscribers()) != 0 {
		t.Errorf("GetSubscribers after remove should be empty")
	}
}

func TestEmptyAorIgnored(t *testing.T) {
	m := New()
	m.OnContactAdded("", "contact")
	if len(m.GetSubscribers()) != 0 {
		t.Errorf("empty aor should be ignored")
	}
	m.OnContactRemoved("") // no-op, no panic
}

func TestMultipleSubscribers(t *testing.T) {
	m := New()
	m.OnContactAdded("sip:a@example.com", "c1")
	m.OnContactAdded("sip:b@example.com", "c2")
	m.OnContactAdded("sip:c@example.com", "c3")
	subs := m.GetSubscribers()
	if len(subs) != 3 {
		t.Fatalf("GetSubscribers len = %d, want 3", len(subs))
	}
	// Re-adding the same aor updates the contact, not duplicates.
	m.OnContactAdded("sip:a@example.com", "c1-updated")
	if len(m.GetSubscribers()) != 3 {
		t.Errorf("re-add should not duplicate")
	}
	if got := m.GetContact("sip:a@example.com"); got != "c1-updated" {
		t.Errorf("GetContact = %q, want c1-updated", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultPUAUsrloc()
	b := DefaultPUAUsrloc()
	if a != b {
		t.Fatal("DefaultPUAUsrloc should return the same instance")
	}
	a.OnContactAdded("u", "c")
	Init()
	c := DefaultPUAUsrloc()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
	if len(c.GetSubscribers()) != 0 {
		t.Errorf("reset default should have no subscribers")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	aors := []string{"sip:a@x", "sip:b@x", "sip:c@x", "sip:d@x"}
	for _, aor := range aors {
		wg.Add(1)
		aor := aor
		go func() {
			defer wg.Done()
			m.OnContactAdded(aor, "contact")
			_ = m.GetSubscribers()
			m.OnContactRemoved(aor)
		}()
	}
	wg.Wait()
}
