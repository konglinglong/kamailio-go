// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - pua_rpc module tests.
 */

package pua_rpc

import (
	"sync"
	"testing"
)

func TestInitAndSend(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("new module should not be connected")
	}
	m.Init("rpc://localhost:9090")
	if !m.IsConnected() {
		t.Fatal("module should be connected after Init")
	}
	if err := m.SendPublish("alice", "body"); err != nil {
		t.Fatalf("SendPublish error: %v", err)
	}
	if err := m.SendSubscribe("alice"); err != nil {
		t.Fatalf("SendSubscribe error: %v", err)
	}
	if got := m.PendingCount(); got != 2 {
		t.Errorf("PendingCount = %d, want 2", got)
	}
}

func TestNotConnectedErrors(t *testing.T) {
	m := New()
	if err := m.SendPublish("alice", "x"); err == nil {
		t.Errorf("SendPublish should error when not connected")
	}
	if err := m.SendSubscribe("alice"); err == nil {
		t.Errorf("SendSubscribe should error when not connected")
	}
	m.Init("rpc://x")
	if err := m.SendPublish("", "x"); err == nil {
		t.Errorf("SendPublish with empty user should error")
	}
	if err := m.SendSubscribe(""); err == nil {
		t.Errorf("SendSubscribe with empty user should error")
	}
}

func TestInitEmptyAddr(t *testing.T) {
	m := New()
	m.Init("rpc://x")
	if !m.IsConnected() {
		t.Fatal("should be connected")
	}
	m.Init("")
	if m.IsConnected() {
		t.Errorf("Init with empty addr should disconnect")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultPUARPC()
	b := DefaultPUARPC()
	if a != b {
		t.Fatal("DefaultPUARPC should return the same instance")
	}
	a.Init("rpc://x")
	a.SendPublish("u", "b")
	Init()
	c := DefaultPUARPC()
	if c == a {
		t.Fatal("package Init should reset the default instance")
	}
	if c.IsConnected() {
		t.Errorf("reset default should not be connected")
	}
	if c.PendingCount() != 0 {
		t.Errorf("reset default should have no requests")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	m.Init("rpc://x")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.SendPublish("u", "b")
			_ = m.SendSubscribe("u")
			_ = m.PendingCount()
		}()
	}
	wg.Wait()
}
