// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - auth_diameter module tests.
 */

package auth_diameter

import (
	"sync"
	"testing"
)

func TestInitAndAuthenticate(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("new module should not be connected")
	}
	m.Init("diameter://example.com:3868")
	if !m.IsConnected() {
		t.Fatal("module should be connected after Init")
	}
	m.AddCredential("alice", "secret")

	ok, err := m.Authenticate("alice", "secret")
	if err != nil {
		t.Fatalf("Authenticate error: %v", err)
	}
	if !ok {
		t.Errorf("Authenticate should accept valid credentials")
	}
	ok, err = m.Authenticate("alice", "wrong")
	if err != nil {
		t.Fatalf("Authenticate error: %v", err)
	}
	if ok {
		t.Errorf("Authenticate should reject wrong password")
	}
	ok, _ = m.Authenticate("bob", "secret")
	if ok {
		t.Errorf("Authenticate should reject unknown user")
	}
}

func TestNotConnectedError(t *testing.T) {
	m := New()
	m.AddCredential("alice", "secret")
	_, err := m.Authenticate("alice", "secret")
	if err == nil {
		t.Errorf("Authenticate should error when not connected")
	}
	m.Init("")
	if m.IsConnected() {
		t.Errorf("Init with empty server should leave module disconnected")
	}
}

func TestEdgeCases(t *testing.T) {
	m := New()
	m.Init("srv")
	if _, err := m.Authenticate("", "pw"); err == nil {
		t.Errorf("Authenticate with empty user should error")
	}
	if _, err := m.Authenticate("alice", ""); err == nil {
		t.Errorf("Authenticate with empty password should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultAuthDiameter()
	b := DefaultAuthDiameter()
	if a != b {
		t.Fatal("DefaultAuthDiameter should return the same instance")
	}
	a.Init("srv")
	a.AddCredential("u", "p")
	if !a.IsConnected() {
		t.Fatal("default should be connected after Init")
	}
	Init()
	c := DefaultAuthDiameter()
	if c == a {
		t.Fatal("package Init should reset the default instance")
	}
	if c.IsConnected() {
		t.Errorf("reset default should not be connected")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	m.Init("srv")
	m.AddCredential("alice", "pw")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Authenticate("alice", "pw")
			_ = m.IsConnected()
		}()
	}
	wg.Wait()
}
