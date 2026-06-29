// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - auth_web3 module tests.
 */

package auth_web3

import (
	"sync"
	"testing"
)

func TestChallengeAndVerify(t *testing.T) {
	m := New()
	addr := "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb1"

	ch := m.GetChallenge(addr)
	if ch == "" {
		t.Fatal("GetChallenge returned empty string")
	}
	if !m.ValidateChallenge(addr, ch) {
		t.Errorf("ValidateChallenge should accept the issued challenge")
	}
	// Wrong challenge rejected.
	if m.ValidateChallenge(addr, "bogus") {
		t.Errorf("ValidateChallenge should reject wrong challenge")
	}

	sig := m.SignChallenge(addr)
	if sig == "" {
		t.Fatal("SignChallenge returned empty string")
	}
	if !m.Verify(addr, sig) {
		t.Errorf("Verify should accept the correct signature")
	}
	// Challenge is consumed after a successful verify.
	if m.Verify(addr, sig) {
		t.Errorf("Verify should fail after challenge consumed")
	}
}

func TestVerifyEdgeCases(t *testing.T) {
	m := New()
	if m.Verify("", "sig") {
		t.Errorf("Verify with empty address should fail")
	}
	if m.Verify("0xabc", "") {
		t.Errorf("Verify with empty signature should fail")
	}
	if m.Verify("0xabc", "deadbeef") {
		t.Errorf("Verify with no challenge should fail")
	}
	if m.GetChallenge("") != "" {
		t.Errorf("GetChallenge with empty user should return empty")
	}
	if m.ValidateChallenge("", "x") {
		t.Errorf("ValidateChallenge with empty user should fail")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultAuthWeb3()
	b := DefaultAuthWeb3()
	if a != b {
		t.Fatal("DefaultAuthWeb3 should return the same instance")
	}
	addr := "0xdefault"
	ch := a.GetChallenge(addr)
	if ch == "" {
		t.Fatal("default GetChallenge returned empty")
	}
	// Re-init resets state.
	Init()
	c := DefaultAuthWeb3()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	addrs := []string{"0x1", "0x2", "0x3", "0x4", "0x5"}
	for _, addr := range addrs {
		wg.Add(1)
		addr := addr
		go func() {
			defer wg.Done()
			_ = m.GetChallenge(addr)
			sig := m.SignChallenge(addr)
			_ = m.Verify(addr, sig)
			_ = m.ValidateChallenge(addr, "x")
		}()
	}
	wg.Wait()
}
