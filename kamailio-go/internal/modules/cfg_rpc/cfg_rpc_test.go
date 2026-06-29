// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the cfg_rpc module.
 */

package cfg_rpc

import (
	"sync"
	"testing"
)

func TestGetSetReset(t *testing.T) {
	m := New()
	m.SetDefault("debug", "0")

	// Default value is returned when no override is set.
	v, err := m.Get("debug")
	if err != nil {
		t.Fatalf("Get(debug) error: %v", err)
	}
	if v != "0" {
		t.Errorf("Get(debug) = %q, want %q", v, "0")
	}

	// Override.
	if err := m.Set("debug", "1"); err != nil {
		t.Fatalf("Set error: %v", err)
	}
	v, _ = m.Get("debug")
	if v != "1" {
		t.Errorf("Get(debug) after set = %q, want %q", v, "1")
	}

	// Reset restores default.
	if err := m.Reset("debug"); err != nil {
		t.Fatalf("Reset error: %v", err)
	}
	v, _ = m.Get("debug")
	if v != "0" {
		t.Errorf("Get(debug) after reset = %q, want %q", v, "0")
	}
}

func TestGetUnknownAndSetEmpty(t *testing.T) {
	m := New()
	if _, err := m.Get("nope"); err == nil {
		t.Errorf("Get(unknown) should error")
	}
	if err := m.Set("", "v"); err == nil {
		t.Errorf("Set(\"\", ...) should error")
	}
	if err := m.Reset("nope"); err == nil {
		t.Errorf("Reset(unknown) should error")
	}
}

func TestList(t *testing.T) {
	m := New()
	m.SetDefault("a", "def-a")
	m.SetDefault("b", "def-b")
	m.Set("b", "override-b")
	m.Set("c", "val-c")

	list := m.List()
	if list["a"] != "def-a" {
		t.Errorf("List[a] = %q, want %q", list["a"], "def-a")
	}
	if list["b"] != "override-b" {
		t.Errorf("List[b] = %q, want %q", list["b"], "override-b")
	}
	if list["c"] != "val-c" {
		t.Errorf("List[c] = %q, want %q", list["c"], "val-c")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultCfgRPC()
	if d == nil {
		t.Fatalf("DefaultCfgRPC() returned nil")
	}
	if d != DefaultCfgRPC() {
		t.Fatalf("DefaultCfgRPC() returned different instances")
	}
	if err := Set("pkg", "v"); err != nil {
		t.Fatalf("package Set error: %v", err)
	}
	v, err := Get("pkg")
	if err != nil || v != "v" {
		t.Errorf("package Get(pkg) = %q,%v", v, err)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultCfgRPC()
	shared.SetDefault("k", "0")
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			shared.Set("k", itoa(i))
			shared.Get("k")
			shared.List()
			shared.Reset("k")
		}(i)
	}
	wg.Wait()
	v, _ := shared.Get("k")
	if v != "0" {
		t.Errorf("Get(k) after concurrent reset = %q, want %q", v, "0")
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
