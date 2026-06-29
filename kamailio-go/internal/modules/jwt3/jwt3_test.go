// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - jwt3 module tests.
 */

package jwt3

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"sync"
	"testing"
)

// generateKeyPair returns PEM-encoded RSA private and public keys.
func generateKeyPair(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	privDER := x509.MarshalPKCS1PrivateKey(key)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER})
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return string(privPEM), string(pubPEM)
}

func TestSignVerifyDecode(t *testing.T) {
	m := New()
	priv, pub := generateKeyPair(t)
	claims := map[string]interface{}{"sub": "bob", "role": "user"}
	tok, err := m.Sign(claims, priv)
	if err != nil {
		t.Fatalf("Sign error: %v", err)
	}
	if tok == "" {
		t.Fatal("Sign returned empty token")
	}
	ok, err := m.Verify(tok, pub)
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
	if decoded["sub"] != "bob" {
		t.Errorf("Decode sub = %v, want bob", decoded["sub"])
	}
}

func TestVerifyRejects(t *testing.T) {
	m := New()
	priv, pub := generateKeyPair(t)
	_, otherPub := generateKeyPair(t)
	tok, _ := m.Sign(map[string]interface{}{"x": 1}, priv)
	// Wrong public key.
	if ok, _ := m.Verify(tok, otherPub); ok {
		t.Errorf("Verify should reject token signed by different key")
	}
	// Tampered signature.
	parts := splitToken(t, tok)
	tampered := parts[0] + "." + parts[1] + ".AAAA"
	if ok, _ := m.Verify(tampered, pub); ok {
		t.Errorf("Verify should reject tampered signature")
	}
	// Malformed token.
	if _, err := m.Verify("not-a-jwt", pub); err == nil {
		t.Errorf("Verify malformed token should error")
	}
	// Invalid public key PEM.
	if _, err := m.Verify(tok, "not-a-pem"); err == nil {
		t.Errorf("Verify with invalid PEM should error")
	}
}

func TestSignErrors(t *testing.T) {
	m := New()
	if _, err := m.Sign(map[string]interface{}{"x": 1}, "not-a-pem"); err == nil {
		t.Errorf("Sign with invalid PEM should error")
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
	a := DefaultJWT3()
	b := DefaultJWT3()
	if a != b {
		t.Fatal("DefaultJWT3 should return the same instance")
	}
	Init()
	c := DefaultJWT3()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	priv, pub := generateKeyPair(t)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, _ := m.Sign(map[string]interface{}{"x": 1}, priv)
			_, _ = m.Verify(tok, pub)
			_, _ = m.Decode(tok)
		}()
	}
	wg.Wait()
}

// splitToken splits a JWT into its three parts.
func splitToken(t *testing.T, tok string) []string {
	t.Helper()
	parts := splitDot(tok)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	return parts
}

// splitDot splits s on '.'.
func splitDot(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
