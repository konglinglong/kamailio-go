// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - jwt module tests.
 */

package jwt

import (
	"sync"
	"testing"
)

func TestSignVerifyDecode(t *testing.T) {
	m := New()
	claims := map[string]interface{}{"sub": "alice", "role": "admin", "n": float64(42)}
	tok, err := m.Sign(claims, "secret")
	if err != nil {
		t.Fatalf("Sign error: %v", err)
	}
	if tok == "" {
		t.Fatal("Sign returned empty token")
	}
	ok, err := m.Verify(tok, "secret")
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if !ok {
		t.Errorf("Verify should accept valid token")
	}
	decoded, err := m.Decode(tok)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if decoded["sub"] != "alice" {
		t.Errorf("Decode sub = %v, want alice", decoded["sub"])
	}
	if decoded["role"] != "admin" {
		t.Errorf("Decode role = %v, want admin", decoded["role"])
	}
}

func TestVerifyRejects(t *testing.T) {
	m := New()
	tok, _ := m.Sign(map[string]interface{}{"sub": "x"}, "secret")
	if ok, _ := m.Verify(tok, "wrong"); ok {
		t.Errorf("Verify should reject wrong key")
	}
	if ok, _ := m.Verify("a.b.c", "secret"); ok {
		t.Errorf("Verify should reject invalid signature")
	}
	if _, err := m.Verify(tok, ""); err == nil {
		t.Errorf("Verify with empty key should error")
	}
	if _, err := m.Verify("not-a-jwt", "secret"); err == nil {
		t.Errorf("Verify with malformed token should error")
	}
	if _, err := m.Verify("a.b", "secret"); err == nil {
		t.Errorf("Verify with two-part token should error")
	}
}

func TestSignErrors(t *testing.T) {
	m := New()
	if _, err := m.Sign(map[string]interface{}{"x": 1}, ""); err == nil {
		t.Errorf("Sign with empty key should error")
	}
}

func TestDecodeErrors(t *testing.T) {
	m := New()
	if _, err := m.Decode("not-a-jwt"); err == nil {
		t.Errorf("Decode malformed token should error")
	}
	if _, err := m.Decode("a.b"); err == nil {
		t.Errorf("Decode two-part token should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultJWT()
	b := DefaultJWT()
	if a != b {
		t.Fatal("DefaultJWT should return the same instance")
	}
	tok, _ := a.Sign(map[string]interface{}{"k": "v"}, "k")
	if ok, _ := a.Verify(tok, "k"); !ok {
		t.Errorf("default instance should verify")
	}
	Init()
	c := DefaultJWT()
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
			tok, _ := m.Sign(map[string]interface{}{"x": 1}, "k")
			_, _ = m.Verify(tok, "k")
			_, _ = m.Decode(tok)
		}()
	}
	wg.Wait()
}
