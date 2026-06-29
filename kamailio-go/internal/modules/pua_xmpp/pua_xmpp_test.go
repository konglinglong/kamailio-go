// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - pua_xmpp module tests.
 */

package pua_xmpp

import (
	"sync"
	"testing"
)

func TestPublishAndSubscribe(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("new module should be disconnected")
	}
	m.Connect()
	if !m.IsConnected() {
		t.Fatal("module should be connected after Connect")
	}
	if err := m.PublishXMPP("alice", "hello"); err != nil {
		t.Fatalf("PublishXMPP error: %v", err)
	}
	if got := m.GetMessage("alice"); got != "hello" {
		t.Errorf("GetMessage = %q, want hello", got)
	}
	if err := m.SubscribeXMPP("alice"); err != nil {
		t.Fatalf("SubscribeXMPP error: %v", err)
	}
}

func TestNotConnectedErrors(t *testing.T) {
	m := New()
	if err := m.PublishXMPP("alice", "x"); err == nil {
		t.Errorf("PublishXMPP should error when disconnected")
	}
	if err := m.SubscribeXMPP("alice"); err == nil {
		t.Errorf("SubscribeXMPP should error when disconnected")
	}
	m.Connect()
	if err := m.PublishXMPP("", "x"); err == nil {
		t.Errorf("PublishXMPP with empty user should error")
	}
	if err := m.SubscribeXMPP(""); err == nil {
		t.Errorf("SubscribeXMPP with empty user should error")
	}
	m.Disconnect()
	if m.IsConnected() {
		t.Errorf("module should be disconnected after Disconnect")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultPUAXmpp()
	b := DefaultPUAXmpp()
	if a != b {
		t.Fatal("DefaultPUAXmpp should return the same instance")
	}
	a.Connect()
	if !a.IsConnected() {
		t.Fatal("default should be connected")
	}
	Init()
	c := DefaultPUAXmpp()
	if c == a {
		t.Fatal("package Init should reset the default instance")
	}
	if c.IsConnected() {
		t.Errorf("reset default should be disconnected")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	m.Connect()
	var wg sync.WaitGroup
	users := []string{"u1", "u2", "u3"}
	for _, u := range users {
		wg.Add(1)
		u := u
		go func() {
			defer wg.Done()
			_ = m.PublishXMPP(u, "msg")
			_ = m.SubscribeXMPP(u)
			_ = m.GetMessage(u)
		}()
	}
	wg.Wait()
}
