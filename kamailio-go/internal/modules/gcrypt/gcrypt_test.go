// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - gcrypt module tests.
 */

package gcrypt

import (
	"sync"
	"testing"
)

func TestAES256RoundTrip(t *testing.T) {
	m := New()
	plain := []byte("the quick brown fox")
	key := "passphrase"
	ct, err := m.AES256Encrypt(plain, key)
	if err != nil {
		t.Fatalf("AES256Encrypt error: %v", err)
	}
	if string(ct) == string(plain) {
		t.Error("ciphertext should differ from plaintext")
	}
	pt, err := m.AES256Decrypt(ct, key)
	if err != nil {
		t.Fatalf("AES256Decrypt error: %v", err)
	}
	if string(pt) != string(plain) {
		t.Errorf("round-trip mismatch: got %q want %q", pt, plain)
	}
	if _, err := m.AES256Decrypt(ct, "other"); err == nil {
		t.Errorf("AES256Decrypt with wrong key should fail")
	}
}

func TestAESErrors(t *testing.T) {
	m := New()
	if _, err := m.AES256Encrypt([]byte("x"), ""); err == nil {
		t.Errorf("AES256Encrypt with empty key should error")
	}
	if _, err := m.AES256Encrypt(nil, "k"); err == nil {
		t.Errorf("AES256Encrypt with empty data should error")
	}
	if _, err := m.AES256Decrypt(nil, "k"); err == nil {
		t.Errorf("AES256Decrypt with empty data should error")
	}
	if _, err := m.AES256Decrypt([]byte("x"), "k"); err == nil {
		t.Errorf("AES256Decrypt with too-short data should error")
	}
}

func TestSHA256AndHMAC(t *testing.T) {
	m := New()
	data := []byte("message")
	h := m.SHA256(data)
	if h == "" || len(h) != 64 {
		t.Errorf("SHA256 returned %q (len %d)", h, len(h))
	}
	if m.SHA256(data) != h {
		t.Errorf("SHA256 should be deterministic")
	}
	if m.SHA256([]byte("other")) == h {
		t.Errorf("SHA256 collision for different data")
	}
	mac := m.HMAC(data, "key")
	if mac == "" || len(mac) != 64 {
		t.Errorf("HMAC returned %q (len %d)", mac, len(mac))
	}
	if m.HMAC(data, "key") != mac {
		t.Errorf("HMAC should be deterministic")
	}
	if m.HMAC(data, "other") == mac {
		t.Errorf("HMAC should differ for different key")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultGCrypt()
	b := DefaultGCrypt()
	if a != b {
		t.Fatal("DefaultGCrypt should return the same instance")
	}
	if a.SHA256([]byte("x")) == "" {
		t.Error("default SHA256 returned empty")
	}
	Init()
	c := DefaultGCrypt()
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
			ct, _ := m.AES256Encrypt([]byte("d"), "k")
			_, _ = m.AES256Decrypt(ct, "k")
			_ = m.SHA256([]byte("x"))
			_ = m.HMAC([]byte("x"), "k")
		}()
	}
	wg.Wait()
}
