// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the UIDGFlags module.
 */

package uid_gflags

import (
	"sync"
	"testing"
)

func TestSetGetIsSet(t *testing.T) {
	m := New()
	if m.IsSet("foo") {
		t.Errorf("IsSet(foo) = true before Set")
	}
	if m.Get("foo") != 0 {
		t.Errorf("Get(foo) = %d, want 0 before Set", m.Get("foo"))
	}
	m.Set("foo", 42)
	if !m.IsSet("foo") {
		t.Errorf("IsSet(foo) = false after Set")
	}
	if m.Get("foo") != 42 {
		t.Errorf("Get(foo) = %d, want 42", m.Get("foo"))
	}
	// Overwrite.
	m.Set("foo", 7)
	if m.Get("foo") != 7 {
		t.Errorf("Get(foo) = %d, want 7 (overwrite)", m.Get("foo"))
	}
	// Negative and zero values are valid.
	m.Set("zero", 0)
	if !m.IsSet("zero") {
		t.Errorf("IsSet(zero) = false, want true (0 is a valid value)")
	}
	m.Set("neg", -5)
	if m.Get("neg") != -5 {
		t.Errorf("Get(neg) = %d, want -5", m.Get("neg"))
	}
	// Empty name is ignored.
	m.Set("", 1)
	if m.IsSet("") {
		t.Errorf("IsSet(empty) = true, want false")
	}
}

func TestList(t *testing.T) {
	m := New()
	if got := m.List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty", got)
	}
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)
	got := m.List()
	if len(got) != 3 {
		t.Fatalf("List() = %v, want 3 entries", got)
	}
	if got["a"] != 1 || got["b"] != 2 || got["c"] != 3 {
		t.Errorf("List() = %v, want a=1,b=2,c=3", got)
	}
	// Mutating the returned map must not affect the store.
	got["a"] = 999
	if m.Get("a") != 1 {
		t.Errorf("store was mutated by caller: Get(a) = %d, want 1", m.Get("a"))
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			name := "f" + itoa(i)
			m.Set(name, i)
			m.Get(name)
			m.IsSet(name)
			m.List()
		}(i)
	}
	wg.Wait()
	if len(m.List()) != goroutines {
		t.Errorf("List() after concurrent = %d entries, want %d", len(m.List()), goroutines)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultUIDGFlags()
	if d == nil {
		t.Fatal("DefaultUIDGFlags() = nil")
	}
	if d != DefaultUIDGFlags() {
		t.Fatal("DefaultUIDGFlags() returned different instances")
	}
	d.Set("default", 1)
	if !d.IsSet("default") {
		t.Fatal("default set/isSet failed")
	}
	Init()
	if DefaultUIDGFlags().IsSet("default") {
		t.Errorf("IsSet after re-Init should be false")
	}
}

// itoa is a tiny local int->string helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
