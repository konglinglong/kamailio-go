// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - auth_arnacon module tests.
 */

package auth_arnacon

import (
	"sync"
	"testing"
)

func TestGenerateAndAuthenticate(t *testing.T) {
	m := New()
	tok := m.GenerateToken("alice")
	if tok == "" {
		t.Fatal("GenerateToken returned empty string")
	}
	if !m.Authenticate("alice", tok) {
		t.Errorf("Authenticate should accept the issued token")
	}
	// Wrong token rejected.
	if m.Authenticate("alice", "bogus") {
		t.Errorf("Authenticate should reject wrong token")
	}
	// Wrong user rejected.
	if m.Authenticate("bob", tok) {
		t.Errorf("Authenticate should reject wrong user")
	}
}

func TestRevokeToken(t *testing.T) {
	m := New()
	tok := m.GenerateToken("carol")
	if !m.Authenticate("carol", tok) {
		t.Fatal("token should be valid before revoke")
	}
	m.RevokeToken("carol")
	if m.Authenticate("carol", tok) {
		t.Errorf("token should be invalid after revoke")
	}
	// Revoking a missing token is a no-op.
	m.RevokeToken("nobody")
	// Re-issue after revoke works.
	tok2 := m.GenerateToken("carol")
	if tok2 == tok {
		t.Errorf("re-issued token should differ")
	}
	if !m.Authenticate("carol", tok2) {
		t.Errorf("re-issued token should authenticate")
	}
}

func TestEdgeCases(t *testing.T) {
	m := New()
	if m.GenerateToken("") != "" {
		t.Errorf("GenerateToken with empty user should return empty")
	}
	if m.Authenticate("", "tok") {
		t.Errorf("Authenticate with empty user should fail")
	}
	if m.Authenticate("alice", "") {
		t.Errorf("Authenticate with empty token should fail")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultAuthArnacon()
	b := DefaultAuthArnacon()
	if a != b {
		t.Fatal("DefaultAuthArnacon should return the same instance")
	}
	tok := a.GenerateToken("default-user")
	if !a.Authenticate("default-user", tok) {
		t.Errorf("default instance should authenticate")
	}
	Init()
	c := DefaultAuthArnacon()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	users := []string{"u1", "u2", "u3", "u4", "u5"}
	for _, u := range users {
		wg.Add(1)
		u := u
		go func() {
			defer wg.Done()
			tok := m.GenerateToken(u)
			_ = m.Authenticate(u, tok)
			m.RevokeToken(u)
		}()
	}
	wg.Wait()
}
