// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the dnssec module.
 */

package dnssec

import (
	"sync"
	"testing"
)

func TestVerify(t *testing.T) {
	m := New()

	if m.Verify("example.com") {
		t.Errorf("Verify before records = true, want false")
	}

	m.SetDNSKEY("example.com", []byte("key-data"))
	if m.Verify("example.com") {
		t.Errorf("Verify with only DNSKEY = true, want false")
	}

	m.SetRRSIG("example.com", []byte("sig-data"))
	if !m.Verify("example.com") {
		t.Errorf("Verify with both records = false, want true")
	}
}

func TestGetDNSKEYAndRRSIG(t *testing.T) {
	m := New()
	m.SetDNSKEY("a.com", []byte("KEY"))
	m.SetRRSIG("a.com", []byte("SIG"))

	key, err := m.GetDNSKEY("a.com")
	if err != nil {
		t.Fatalf("GetDNSKEY error: %v", err)
	}
	if string(key) != "KEY" {
		t.Errorf("GetDNSKEY = %q, want %q", key, "KEY")
	}

	sig, err := m.GetRRSIG("a.com")
	if err != nil {
		t.Fatalf("GetRRSIG error: %v", err)
	}
	if string(sig) != "SIG" {
		t.Errorf("GetRRSIG = %q, want %q", sig, "SIG")
	}

	if _, err := m.GetDNSKEY("missing.com"); err == nil {
		t.Errorf("GetDNSKEY(missing) should error")
	}
	if _, err := m.GetRRSIG("missing.com"); err == nil {
		t.Errorf("GetRRSIG(missing) should error")
	}
}

func TestGetReturnsCopy(t *testing.T) {
	m := New()
	orig := []byte("original")
	m.SetDNSKEY("x.com", orig)

	got, _ := m.GetDNSKEY("x.com")
	got[0] = 'X'
	again, _ := m.GetDNSKEY("x.com")
	if string(again) != "original" {
		t.Errorf("stored DNSKEY mutated via returned slice: %q", again)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultDNSSEC()
	if d == nil {
		t.Fatalf("DefaultDNSSEC() returned nil")
	}
	if d != DefaultDNSSEC() {
		t.Fatalf("DefaultDNSSEC() returned different instances")
	}
	d.SetDNSKEY("pkg.com", []byte("k"))
	d.SetRRSIG("pkg.com", []byte("s"))
	if !Verify("pkg.com") {
		t.Errorf("package Verify(pkg.com) = false")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultDNSSEC()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			dom := "d" + itoa(i) + ".com"
			shared.SetDNSKEY(dom, []byte("k"))
			shared.SetRRSIG(dom, []byte("s"))
			shared.Verify(dom)
			shared.GetDNSKEY(dom)
			shared.GetRRSIG(dom)
		}(i)
	}
	wg.Wait()
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
