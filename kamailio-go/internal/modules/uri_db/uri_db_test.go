// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the URIDB module.
 */

package uri_db

import (
	"sync"
	"testing"
)

func TestCheckAndDoesExist(t *testing.T) {
	m := New()
	m.AddURI("sip:alice@example.com", "example.com")
	m.AddURI("sip:bob@test.org", "test.org")
	if !m.CheckURI("sip:alice@example.com") {
		t.Errorf("CheckURI(alice) = false, want true")
	}
	if !m.DoesURIExist("sip:alice@example.com") {
		t.Errorf("DoesURIExist(alice) = false, want true")
	}
	if m.CheckURI("sip:nobody@example.com") {
		t.Errorf("CheckURI(nobody) = true, want false")
	}
	if m.DoesURIExist("sip:nobody@example.com") {
		t.Errorf("DoesURIExist(nobody) = true, want false")
	}
	if m.CheckURI("") {
		t.Errorf("CheckURI(empty) = true, want false")
	}
}

func TestGetDomain(t *testing.T) {
	m := New()
	m.AddURI("sip:alice@example.com", "example.com")
	m.AddURI("sip:bob@test.org", "test.org")
	domain, ok := m.GetDomain("sip:alice@example.com")
	if !ok {
		t.Fatal("GetDomain(alice) ok = false, want true")
	}
	if domain != "example.com" {
		t.Errorf("GetDomain(alice) = %q, want example.com", domain)
	}
	domain, ok = m.GetDomain("sip:nobody@example.com")
	if ok {
		t.Errorf("GetDomain(nobody) ok = true, want false")
	}
	if domain != "" {
		t.Errorf("GetDomain(nobody) = %q, want empty", domain)
	}
	// Overwrite domain.
	m.AddURI("sip:alice@example.com", "new.com")
	if d, _ := m.GetDomain("sip:alice@example.com"); d != "new.com" {
		t.Errorf("GetDomain(alice) after overwrite = %q, want new.com", d)
	}
}

func TestRemoveAndConcurrent(t *testing.T) {
	m := New()
	m.AddURI("sip:alice@example.com", "example.com")
	if !m.RemoveURI("sip:alice@example.com") {
		t.Errorf("RemoveURI(alice) = false, want true")
	}
	if m.CheckURI("sip:alice@example.com") {
		t.Errorf("CheckURI(alice) after remove = true, want false")
	}
	if m.RemoveURI("sip:alice@example.com") {
		t.Errorf("RemoveURI(alice) twice = true, want false")
	}

	// Concurrent access.
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			uri := "sip:u" + itoa(i) + "@example.com"
			m.AddURI(uri, "example.com")
			m.CheckURI(uri)
			m.DoesURIExist(uri)
			_, _ = m.GetDomain(uri)
			m.RemoveURI(uri)
		}(i)
	}
	wg.Wait()
	for i := 0; i < goroutines; i++ {
		uri := "sip:u" + itoa(i) + "@example.com"
		if m.CheckURI(uri) {
			t.Errorf("CheckURI(%s) = true after remove", uri)
		}
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultURIDB()
	if d == nil {
		t.Fatal("DefaultURIDB() = nil")
	}
	if d != DefaultURIDB() {
		t.Fatal("DefaultURIDB() returned different instances")
	}
	d.AddURI("sip:default@example.com", "example.com")
	if !d.CheckURI("sip:default@example.com") {
		t.Fatal("default add/check failed")
	}
	Init()
	if DefaultURIDB().CheckURI("sip:default@example.com") {
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
