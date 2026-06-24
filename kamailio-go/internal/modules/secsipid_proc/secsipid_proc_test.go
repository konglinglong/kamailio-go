// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - SecSIPIDProc module tests.
 */

package secsipid_proc

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func writeKey(t *testing.T) string {
	t.Helper()
	return writeKeyContent(t, "test-secret-key", "key.pem")
}

// writeKeyContent writes content to a uniquely named file in a temp dir.
func writeKeyContent(t *testing.T, content, name string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func TestInitSignVerify(t *testing.T) {
	m := New()
	if m.IsInitialized() {
		t.Fatal("expected not initialized")
	}
	keyPath := writeKey(t)
	if err := m.Init(keyPath); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsInitialized() {
		t.Fatal("expected initialized after Init")
	}
	tok, err := m.Sign("hello-payload")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token")
	}
	ok, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("expected Verify true")
	}
}

func TestInitErrors(t *testing.T) {
	m := New()
	if err := m.Init(""); err == nil {
		t.Fatal("expected error for empty key path")
	}
	if err := m.Init("/nonexistent/key.pem"); err == nil {
		t.Fatal("expected error for missing key file")
	}
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := m.Init(empty); err == nil {
		t.Fatal("expected error for empty key file")
	}
}

func TestVerifyInvalid(t *testing.T) {
	m := New()
	m.Init(writeKey(t))
	if _, err := m.Verify(""); err == nil {
		t.Fatal("expected error for empty token")
	}
	if _, err := m.Verify("no-dot-here"); err == nil {
		t.Fatal("expected error for malformed token")
	}
	ok, _ := m.Verify("aaa.bbb")
	if ok {
		t.Fatal("expected Verify false for bad signature")
	}
	// Tampered token: re-sign with a different module using a different key.
	m2 := New()
	m2.Init(writeKeyContent(t, "a-different-secret-key", "other.pem"))
	tok, _ := m2.Sign("payload")
	ok, _ = m.Verify(tok)
	if ok {
		t.Fatal("expected Verify false for token signed with different key")
	}
}

func TestSignEmptyPayload(t *testing.T) {
	m := New()
	m.Init(writeKey(t))
	if _, err := m.Sign(""); err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestGlobalFunctions(t *testing.T) {
	keyPath := writeKey(t)
	if err := Init(keyPath); err != nil {
		t.Fatalf("global Init: %v", err)
	}
	tok, err := Sign("global-payload")
	if err != nil {
		t.Fatalf("global Sign: %v", err)
	}
	ok, err := Verify(tok)
	if err != nil || !ok {
		t.Fatalf("global Verify: %v, %v", ok, err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.Init(writeKey(t))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, _ := m.Sign("p")
			_, _ = m.Verify(tok)
			_ = m.IsInitialized()
		}()
	}
	wg.Wait()
}
