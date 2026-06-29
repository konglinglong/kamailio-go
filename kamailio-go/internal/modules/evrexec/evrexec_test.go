// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the evrexec module.
 */

package evrexec

import (
	"sync"
	"testing"
)

func TestRegisterExecuteUnregister(t *testing.T) {
	m := New()

	m.Register("startup", "/bin/echo hello")
	if err := m.Execute("startup"); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got := m.ExecutedCount(); got != 1 {
		t.Errorf("ExecutedCount() = %d, want 1", got)
	}

	// Unknown event.
	if err := m.Execute("nope"); err == nil {
		t.Errorf("Execute(unknown) should error")
	}

	if !m.Unregister("startup") {
		t.Errorf("Unregister(startup) returned false")
	}
	if m.Unregister("startup") {
		t.Errorf("Unregister twice should return false")
	}
	if err := m.Execute("startup"); err == nil {
		t.Errorf("Execute after unregister should error")
	}
}

func TestRegisterOverwrite(t *testing.T) {
	m := New()
	m.Register("e", "cmd1")
	m.Register("e", "cmd2")
	list := m.List()
	if list["e"] != "cmd2" {
		t.Errorf("List()[e] = %q, want %q", list["e"], "cmd2")
	}
	if len(list) != 1 {
		t.Errorf("List() = %d entries, want 1", len(list))
	}
}

func TestList(t *testing.T) {
	m := New()
	m.Register("a", "1")
	m.Register("b", "2")
	list := m.List()
	if len(list) != 2 {
		t.Fatalf("List() = %d entries, want 2", len(list))
	}
	if list["a"] != "1" || list["b"] != "2" {
		t.Errorf("List() = %v", list)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultEVRExec()
	if d == nil {
		t.Fatalf("DefaultEVRExec() returned nil")
	}
	Register("pkg", "cmd")
	if err := Execute("pkg"); err != nil {
		t.Fatalf("package Execute error: %v", err)
	}
	if !Unregister("pkg") {
		t.Errorf("package Unregister(pkg) = false")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultEVRExec()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			e := itoa(i)
			shared.Register(e, "cmd")
			shared.Execute(e)
			shared.List()
			shared.Unregister(e)
		}(i)
	}
	wg.Wait()
	if got := shared.ExecutedCount(); got != n {
		t.Errorf("ExecutedCount() = %d, want %d", got, n)
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
