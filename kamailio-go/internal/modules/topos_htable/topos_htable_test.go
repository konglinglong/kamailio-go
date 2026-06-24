// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the ToposHTable module.
 */

package topos_htable

import (
	"bytes"
	"sync"
	"testing"
)

func TestStoreRetrieve(t *testing.T) {
	m := New()
	data := []byte("dialog-state")
	if err := m.Store("call-1", "ftag-1", data); err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	if m.Count() != 1 {
		t.Errorf("Count() = %d, want 1", m.Count())
	}
	got, err := m.Retrieve("call-1", "ftag-1")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Retrieve() = %q, want %q", got, data)
	}
	// Mutating the returned slice must not affect the stored record.
	got[0] = 'X'
	got2, _ := m.Retrieve("call-1", "ftag-1")
	if got2[0] != 'd' {
		t.Errorf("stored record was mutated by caller")
	}
	// Unknown -> error.
	if _, err := m.Retrieve("nope", "nope"); err == nil {
		t.Errorf("Retrieve(unknown) should error")
	}
	// Empty call-id -> error.
	if err := m.Store("", "tag", data); err == nil {
		t.Errorf("Store(empty call-id) should error")
	}
}

func TestDeleteAndCount(t *testing.T) {
	m := New()
	m.Store("call-a", "t1", []byte("a1"))
	m.Store("call-a", "t2", []byte("a2"))
	m.Store("call-b", "t1", []byte("b1"))
	if m.Count() != 3 {
		t.Fatalf("Count() = %d, want 3", m.Count())
	}
	if !m.Delete("call-a") {
		t.Errorf("Delete(call-a) = false, want true")
	}
	if m.Count() != 1 {
		t.Errorf("Count() after delete = %d, want 1", m.Count())
	}
	if _, err := m.Retrieve("call-a", "t1"); err == nil {
		t.Errorf("Retrieve(call-a) after delete should error")
	}
	if got, _ := m.Retrieve("call-b", "t1"); string(got) != "b1" {
		t.Errorf("call-b should survive, got %q", got)
	}
	if m.Delete("call-a") {
		t.Errorf("Delete(call-a) twice = true, want false")
	}
	if m.Delete("") {
		t.Errorf("Delete(empty) = true, want false")
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
			callID := "c" + itoa(i)
			_ = m.Store(callID, "t", []byte("data"))
			_, _ = m.Retrieve(callID, "t")
			m.Count()
			m.Delete(callID)
		}(i)
	}
	wg.Wait()
	if m.Count() != 0 {
		t.Errorf("Count() after concurrent = %d, want 0", m.Count())
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultToposHTable()
	if d == nil {
		t.Fatal("DefaultToposHTable() = nil")
	}
	if d != DefaultToposHTable() {
		t.Fatal("DefaultToposHTable() returned different instances")
	}
	_ = d.Store("default", "t", []byte("x"))
	if d.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", d.Count())
	}
	Init()
	if DefaultToposHTable().Count() != 0 {
		t.Errorf("Count() after re-Init = %d, want 0", DefaultToposHTable().Count())
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
