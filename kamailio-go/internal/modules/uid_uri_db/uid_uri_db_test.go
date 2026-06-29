// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the UIDURIDB module.
 */

package uid_uri_db

import (
	"sync"
	"testing"
)

func TestCheckAddRemove(t *testing.T) {
	m := New()
	m.AddURI("sip:alice@example.com", "did-1")
	m.AddURI("sip:bob@example.com", "did-2")
	if !m.CheckURI("sip:alice@example.com") {
		t.Errorf("CheckURI(alice) = false, want true")
	}
	if !m.CheckURI("sip:bob@example.com") {
		t.Errorf("CheckURI(bob) = false, want true")
	}
	if m.CheckURI("sip:nobody@example.com") {
		t.Errorf("CheckURI(nobody) = true, want false")
	}
	if m.CheckURI("") {
		t.Errorf("CheckURI(empty) = true, want false")
	}
	if !m.RemoveURI("sip:alice@example.com") {
		t.Errorf("RemoveURI(alice) = false, want true")
	}
	if m.CheckURI("sip:alice@example.com") {
		t.Errorf("CheckURI(alice) after remove = true, want false")
	}
	if m.RemoveURI("sip:alice@example.com") {
		t.Errorf("RemoveURI(alice) twice = true, want false")
	}
	if m.RemoveURI("sip:nobody@example.com") {
		t.Errorf("RemoveURI(nobody) = true, want false")
	}
}

func TestOverwriteAndEmpty(t *testing.T) {
	m := New()
	m.AddURI("sip:carol@example.com", "did-1")
	m.AddURI("sip:carol@example.com", "did-2")
	// The same uri should only appear once.
	count := 0
	for range m.uris {
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 uri after overwrite, got %d", count)
	}
	// Empty uri is ignored.
	m.AddURI("", "did")
	if m.CheckURI("") {
		t.Errorf("CheckURI(empty) = true, want false")
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
			uri := "sip:u" + itoa(i) + "@example.com"
			m.AddURI(uri, "did")
			m.CheckURI(uri)
			m.RemoveURI(uri)
		}(i)
	}
	wg.Wait()
	// All URIs were removed.
	for i := 0; i < goroutines; i++ {
		uri := "sip:u" + itoa(i) + "@example.com"
		if m.CheckURI(uri) {
			t.Errorf("CheckURI(%s) = true after remove", uri)
		}
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultUIDURIDB()
	if d == nil {
		t.Fatal("DefaultUIDURIDB() = nil")
	}
	if d != DefaultUIDURIDB() {
		t.Fatal("DefaultUIDURIDB() returned different instances")
	}
	d.AddURI("sip:default@example.com", "did")
	if !d.CheckURI("sip:default@example.com") {
		t.Fatal("default add/check failed")
	}
	Init()
	if DefaultUIDURIDB().CheckURI("sip:default@example.com") {
		t.Errorf("CheckURI after re-Init should be false")
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
