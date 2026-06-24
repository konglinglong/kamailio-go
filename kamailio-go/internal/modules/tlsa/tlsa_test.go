// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - tlsa module tests.
 */

package tlsa

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"
)

func TestGenerate(t *testing.T) {
	m := New()
	cert := []byte("fake-cert-der-bytes")
	rec, err := m.Generate(cert)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if rec.Usage != DefaultUsage {
		t.Errorf("Usage = %d, want %d", rec.Usage, DefaultUsage)
	}
	if rec.MatchingType != DefaultMatchingType {
		t.Errorf("MatchingType = %d, want %d", rec.MatchingType, DefaultMatchingType)
	}
	sum := sha256.Sum256(cert)
	want := hex.EncodeToString(sum[:])
	if rec.Certificate != want {
		t.Errorf("Certificate = %q, want %q", rec.Certificate, want)
	}
	if _, err := m.Generate(nil); err == nil {
		t.Errorf("Generate with empty cert should error")
	}
}

func TestStoreAndVerify(t *testing.T) {
	m := New()
	cert := []byte("another-cert")
	rec, _ := m.Generate(cert)
	m.Store("example.com", *rec)
	if !m.Verify("example.com", cert) {
		t.Errorf("Verify should accept matching cert")
	}
	if m.Verify("example.com", []byte("other-cert")) {
		t.Errorf("Verify should reject non-matching cert")
	}
	if m.Verify("other.com", cert) {
		t.Errorf("Verify should reject unknown domain")
	}
	if m.Verify("example.com", nil) {
		t.Errorf("Verify with empty cert should fail")
	}
	if m.Verify("", cert) {
		t.Errorf("Verify with empty domain should fail")
	}
}

func TestMultipleRecords(t *testing.T) {
	m := New()
	c1 := []byte("cert1")
	c2 := []byte("cert2")
	r1, _ := m.Generate(c1)
	r2, _ := m.Generate(c2)
	m.Store("multi.com", *r1)
	m.Store("multi.com", *r2)
	if !m.Verify("multi.com", c1) {
		t.Errorf("Verify should accept cert1")
	}
	if !m.Verify("multi.com", c2) {
		t.Errorf("Verify should accept cert2")
	}
	if m.Verify("multi.com", []byte("cert3")) {
		t.Errorf("Verify should reject cert3")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultTLSA()
	b := DefaultTLSA()
	if a != b {
		t.Fatal("DefaultTLSA should return the same instance")
	}
	rec, _ := a.Generate([]byte("c"))
	a.Store("d", *rec)
	Init()
	c := DefaultTLSA()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
	if c.Verify("d", []byte("c")) {
		t.Errorf("reset default should have no records")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	rec, _ := m.Generate([]byte("c"))
	m.Store("d", *rec)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Verify("d", []byte("c"))
			r, _ := m.Generate([]byte("x"))
			m.Store("d2", *r)
		}()
	}
	wg.Wait()
}
