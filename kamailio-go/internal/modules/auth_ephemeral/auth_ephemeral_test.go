// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the auth_ephemeral module.
 */

package auth_ephemeral

import (
	"strings"
	"sync"
	"testing"
)

func TestGenerateAndValidate(t *testing.T) {
	m := New()

	tok, err := m.Generate("alice")
	if err != nil {
		t.Fatalf("Generate(alice) error: %v", err)
	}
	if tok == "" {
		t.Fatalf("Generate(alice) returned empty token")
	}
	if !strings.HasPrefix(tok, "alice:") {
		t.Errorf("token = %q, want prefix %q", tok, "alice:")
	}
	if !m.Validate("alice", tok) {
		t.Errorf("Validate(alice, tok) = false, want true")
	}
	if m.Validate("alice", "wrong") {
		t.Errorf("Validate(alice, wrong) = true, want false")
	}
	if m.Validate("bob", tok) {
		t.Errorf("Validate(bob, tok) = true, want false")
	}
}

func TestGenerateEmptyUser(t *testing.T) {
	m := New()
	if _, err := m.Generate(""); err == nil {
		t.Errorf("Generate(\"\") should return an error")
	}
}

func TestRevoke(t *testing.T) {
	m := New()

	tok, _ := m.Generate("carol")
	if !m.Validate("carol", tok) {
		t.Fatalf("Validate before revoke failed")
	}
	m.Revoke("carol")
	if m.Validate("carol", tok) {
		t.Errorf("Validate after revoke should return false")
	}
	// Revoking again is a no-op.
	m.Revoke("carol")
	if got := m.Count(); got != 0 {
		t.Errorf("Count() = %d, want 0", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultAuthEphemeral()
	if d == nil {
		t.Fatalf("DefaultAuthEphemeral() returned nil")
	}
	if d != DefaultAuthEphemeral() {
		t.Fatalf("DefaultAuthEphemeral() returned different instances")
	}
	tok, err := Generate("dave")
	if err != nil {
		t.Fatalf("package Generate error: %v", err)
	}
	if !Validate("dave", tok) {
		t.Errorf("package Validate(dave) = false")
	}
	Revoke("dave")
	if Validate("dave", tok) {
		t.Errorf("package Validate after Revoke = true")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultAuthEphemeral()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			u := itoa(i)
			tok, _ := shared.Generate(u)
			shared.Validate(u, tok)
			shared.Revoke(u)
			shared.Count()
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != 0 {
		t.Errorf("Count() after concurrent = %d, want 0", got)
	}
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
