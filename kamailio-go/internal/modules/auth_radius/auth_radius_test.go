// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - auth_radius module tests.
 */

package auth_radius

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestInit(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("expected disconnected before Init")
	}
	if err := m.Init("radius.example.com:1812", "shared", 2*time.Second, 3); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsConnected() {
		t.Fatal("expected connected after Init")
	}
	if got := m.Server(); got != "radius.example.com:1812" {
		t.Errorf("Server = %q, want radius.example.com:1812", got)
	}
	if got := m.Secret(); got != "shared" {
		t.Errorf("Secret = %q, want shared", got)
	}
	if got := m.Timeout(); got != 2*time.Second {
		t.Errorf("Timeout = %v, want 2s", got)
	}
	if got := m.Retries(); got != 3 {
		t.Errorf("Retries = %d, want 3", got)
	}
}

func TestInitEmptyServer(t *testing.T) {
	m := New()
	if err := m.Init("", "shared", 0, 0); err == nil {
		t.Fatal("expected error for empty server")
	}
	if m.IsConnected() {
		t.Fatal("expected disconnected when Init fails")
	}
}

func TestInitInvalidRetries(t *testing.T) {
	m := New()
	if err := m.Init("radius.example.com:1812", "shared", 0, -1); err == nil {
		t.Fatal("expected error for negative retries")
	}
}

func TestRadiusAuthPAP(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	mc.SetUser("alice", "secret123", nil)
	m.SetClient(mc)
	if err := m.Init("radius.example.com:1812", "shared", time.Second, 1); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ok, err := m.RadiusAuth("alice", "secret123")
	if err != nil {
		t.Fatalf("RadiusAuth: %v", err)
	}
	if !ok {
		t.Fatal("expected auth success for alice")
	}
}

func TestRadiusAuthPAPWrongPassword(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	mc.SetUser("alice", "secret123", nil)
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	ok, err := m.RadiusAuth("alice", "wrong")
	if err != nil {
		t.Fatalf("RadiusAuth: %v", err)
	}
	if ok {
		t.Fatal("expected auth failure for wrong password")
	}
}

func TestRadiusAuthUnknownUser(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	ok, err := m.RadiusAuth("ghost", "whatever")
	if err != nil {
		t.Fatalf("RadiusAuth: %v", err)
	}
	if ok {
		t.Fatal("expected auth failure for unknown user")
	}
}

func TestRadiusAuthNotConnected(t *testing.T) {
	m := New()
	if _, err := m.RadiusAuth("alice", "secret"); err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestRadiusAuthClientError(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	mc.SetUser("alice", "secret123", nil)
	mc.failNext = errors.New("network down")
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	if _, err := m.RadiusAuth("alice", "secret123"); err == nil {
		t.Fatal("expected error when client fails")
	}
}

func TestRadiusAuthDigest(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	mc.SetDigestUser("bob", "example.com", "response-hash", nil)
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	ok, err := m.RadiusAuthDigest("bob", "example.com", "sip:example.com", "nonce", "response-hash", "REGISTER")
	if err != nil {
		t.Fatalf("RadiusAuthDigest: %v", err)
	}
	if !ok {
		t.Fatal("expected digest auth success for bob")
	}
}

func TestRadiusAuthDigestWrongResponse(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	mc.SetDigestUser("bob", "example.com", "response-hash", nil)
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	ok, err := m.RadiusAuthDigest("bob", "example.com", "sip:example.com", "nonce", "wrong-response", "REGISTER")
	if err != nil {
		t.Fatalf("RadiusAuthDigest: %v", err)
	}
	if ok {
		t.Fatal("expected digest auth failure for wrong response")
	}
}

func TestRadiusAuthDigestNotConnected(t *testing.T) {
	m := New()
	if _, err := m.RadiusAuthDigest("bob", "example.com", "sip:example.com", "nonce", "response", "REGISTER"); err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestCheckUserExists(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	mc.SetUser("alice", "secret123", map[string]string{"Filter-Id": "10"})
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	exists, err := m.CheckUserExists("alice")
	if err != nil {
		t.Fatalf("CheckUserExists: %v", err)
	}
	if !exists {
		t.Fatal("expected alice to exist")
	}
	exists, err = m.CheckUserExists("nobody")
	if err != nil {
		t.Fatalf("CheckUserExists: %v", err)
	}
	if exists {
		t.Fatal("expected nobody to not exist")
	}
}

func TestCheckUserExistsNotConnected(t *testing.T) {
	m := New()
	if _, err := m.CheckUserExists("alice"); err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestGetAttributes(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	attrs := map[string]string{"Filter-Id": "10", "SIP-AVP": "alice@example.com"}
	mc.SetUser("alice", "secret123", attrs)
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	got, err := m.GetAttributes("alice")
	if err != nil {
		t.Fatalf("GetAttributes: %v", err)
	}
	if len(got) != len(attrs) {
		t.Fatalf("got %d attrs, want %d", len(got), len(attrs))
	}
	for k, v := range attrs {
		if got[k] != v {
			t.Errorf("attr %q = %q, want %q", k, got[k], v)
		}
	}
}

func TestGetAttributesNoUser(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	got, err := m.GetAttributes("ghost")
	if err != nil {
		t.Fatalf("GetAttributes: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty attrs, got %v", got)
	}
}

func TestGetAttributesNotConnected(t *testing.T) {
	m := New()
	if _, err := m.GetAttributes("alice"); err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestTestConnection(t *testing.T) {
	m := New()
	if err := m.TestConnection(); err == nil {
		t.Fatal("expected error when not connected")
	}
	mc := newMockRadiusClient()
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	if err := m.TestConnection(); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
}

func TestTestConnectionClientError(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	mc.failNext = errors.New("network down")
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	if err := m.TestConnection(); err == nil {
		t.Fatal("expected error when client fails")
	}
}

func TestAppendRealmToUsername(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	m.SetAppendRealmToUsername(true)
	// Realm is derived from the RADIUS server host: "radius.example.com".
	mc.SetUser("bob@radius.example.com", "secret123", nil)
	ok, err := m.RadiusAuth("bob", "secret123")
	if err != nil {
		t.Fatalf("RadiusAuth: %v", err)
	}
	if !ok {
		t.Fatal("expected auth success with realm appended")
	}
	// Without the flag, "bob" alone should fail since the stored user is
	// "bob@radius.example.com".
	m.SetAppendRealmToUsername(false)
	ok, err = m.RadiusAuth("bob", "secret123")
	if err != nil {
		t.Fatalf("RadiusAuth: %v", err)
	}
	if ok {
		t.Fatal("expected auth failure without realm appended")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	mc := newMockRadiusClient()
	mc.SetUser("alice", "secret123", map[string]string{"Filter-Id": "10"})
	m.SetClient(mc)
	_ = m.Init("radius.example.com:1812", "shared", time.Second, 1)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = m.RadiusAuth("alice", "secret123")
			_, _ = m.CheckUserExists("alice")
			_, _ = m.GetAttributes("alice")
			_ = m.TestConnection()
		}(i)
	}
	wg.Wait()
}

func TestDefaultSingleton(t *testing.T) {
	a := DefaultAuthRadius()
	b := DefaultAuthRadius()
	if a != b {
		t.Fatal("DefaultAuthRadius should return the same instance")
	}
}

func TestPackageInitResets(t *testing.T) {
	_ = Init("radius.example.com:1812", "shared", time.Second, 1)
	if !IsConnected() {
		t.Fatal("expected connected after package Init")
	}
	if _, err := RadiusAuth("nobody", "x"); err != nil {
		t.Fatalf("RadiusAuth: %v", err)
	}
}
