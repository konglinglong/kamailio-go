// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the cfg_db module.
 */

package cfg_db

import (
	"sync"
	"testing"
)

func TestStoreLoadDelete(t *testing.T) {
	m := New()

	if err := m.Store("timeout", "30"); err != nil {
		t.Fatalf("Store error: %v", err)
	}
	v, err := m.Load("timeout")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if v != "30" {
		t.Errorf("Load(timeout) = %q, want %q", v, "30")
	}

	if err := m.Delete("timeout"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if _, err := m.Load("timeout"); err == nil {
		t.Errorf("Load after delete should error")
	}
	if err := m.Delete("timeout"); err == nil {
		t.Errorf("Delete twice should error")
	}
}

func TestStoreEmptyKey(t *testing.T) {
	m := New()
	if err := m.Store("", "v"); err == nil {
		t.Errorf("Store(\"\", ...) should error")
	}
}

func TestListAndCount(t *testing.T) {
	m := New()
	m.Store("a", "1")
	m.Store("b", "2")
	m.Store("c", "3")

	if got := m.Count(); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}
	list := m.List()
	if len(list) != 3 {
		t.Fatalf("List() = %d entries, want 3", len(list))
	}
	if list["a"] != "1" || list["b"] != "2" || list["c"] != "3" {
		t.Errorf("List() = %v", list)
	}
	// Mutating the returned map must not affect the store.
	list["a"] = "mutated"
	v, _ := m.Load("a")
	if v != "1" {
		t.Errorf("Load(a) after mutating list = %q, want %q", v, "1")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultCfgDB()
	if d == nil {
		t.Fatalf("DefaultCfgDB() returned nil")
	}
	if d != DefaultCfgDB() {
		t.Fatalf("DefaultCfgDB() returned different instances")
	}
	if err := Store("pkg", "val"); err != nil {
		t.Fatalf("package Store error: %v", err)
	}
	v, err := Load("pkg")
	if err != nil || v != "val" {
		t.Errorf("package Load(pkg) = %q,%v", v, err)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultCfgDB()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			k := itoa(i)
			shared.Store(k, "v")
			shared.Load(k)
			shared.List()
			shared.Delete(k)
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != 0 {
		t.Errorf("Count() after concurrent = %d, want 0", got)
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
