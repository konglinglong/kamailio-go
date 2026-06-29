// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - crypto module tests.
 */

package crypto

import (
	"sync"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	m := New()
	plain := []byte("hello kamailio")
	key := "super-secret-key"
	ct, err := m.Encrypt(plain, key)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	if string(ct) == string(plain) {
		t.Error("ciphertext should differ from plaintext")
	}
	pt, err := m.Decrypt(ct, key)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if string(pt) != string(plain) {
		t.Errorf("round-trip mismatch: got %q want %q", pt, plain)
	}
	// Wrong key fails.
	if _, err := m.Decrypt(ct, "other-key"); err == nil {
		t.Errorf("Decrypt with wrong key should fail")
	}
}

func TestEncryptErrors(t *testing.T) {
	m := New()
	if _, err := m.Encrypt([]byte("x"), ""); err == nil {
		t.Errorf("Encrypt with empty key should error")
	}
	if _, err := m.Encrypt(nil, "k"); err == nil {
		t.Errorf("Encrypt with empty data should error")
	}
	if _, err := m.Decrypt(nil, "k"); err == nil {
		t.Errorf("Decrypt with empty data should error")
	}
	if _, err := m.Decrypt([]byte("short"), "k"); err == nil {
		t.Errorf("Decrypt with too-short data should error")
	}
}

func TestHashSignVerify(t *testing.T) {
	m := New()
	data := []byte("payload")
	h := m.Hash(data)
	if h == "" || len(h) != 64 {
		t.Errorf("Hash returned %q (len %d)", h, len(h))
	}
	if m.Hash(data) != h {
		t.Errorf("Hash should be deterministic")
	}
	if m.Hash([]byte("other")) == h {
		t.Errorf("Hash collision for different data")
	}
	sig, err := m.Sign(data, "key")
	if err != nil {
		t.Fatalf("Sign error: %v", err)
	}
	if !m.Verify(data, sig, "key") {
		t.Errorf("Verify should accept valid signature")
	}
	if m.Verify(data, sig, "wrong") {
		t.Errorf("Verify should reject wrong key")
	}
	if m.Verify([]byte("other"), sig, "key") {
		t.Errorf("Verify should reject different data")
	}
	if m.Verify(data, nil, "key") {
		t.Errorf("Verify should reject empty signature")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultCrypto()
	b := DefaultCrypto()
	if a != b {
		t.Fatal("DefaultCrypto should return the same instance")
	}
	if a.Hash([]byte("x")) == "" {
		t.Error("default Hash returned empty")
	}
	Init()
	c := DefaultCrypto()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ct, _ := m.Encrypt([]byte("data"), "k")
			_, _ = m.Decrypt(ct, "k")
			_ = m.Hash([]byte("x"))
			sig, _ := m.Sign([]byte("x"), "k")
			_ = m.Verify([]byte("x"), sig, "k")
		}()
	}
	wg.Wait()
}
