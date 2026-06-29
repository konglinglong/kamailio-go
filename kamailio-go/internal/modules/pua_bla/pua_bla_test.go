// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - pua_bla module tests.
 */

package pua_bla

import (
	"sync"
	"testing"
)

func TestPublishAndGetState(t *testing.T) {
	m := New()
	if got := m.GetBLAState("alice"); got != "" {
		t.Errorf("GetBLAState unknown = %q, want empty", got)
	}
	if err := m.PublishBLA("alice", "busy"); err != nil {
		t.Fatalf("PublishBLA error: %v", err)
	}
	if got := m.GetBLAState("alice"); got != "busy" {
		t.Errorf("GetBLAState = %q, want busy", got)
	}
	if err := m.PublishBLA("", "x"); err == nil {
		t.Errorf("PublishBLA with empty user should error")
	}
}

func TestSubscribe(t *testing.T) {
	m := New()
	if m.IsSubscribed("bob") {
		t.Errorf("IsSubscribed should be false before subscribe")
	}
	if err := m.SubscribeBLA("bob"); err != nil {
		t.Fatalf("SubscribeBLA error: %v", err)
	}
	if !m.IsSubscribed("bob") {
		t.Errorf("IsSubscribed should be true after subscribe")
	}
	if err := m.SubscribeBLA(""); err == nil {
		t.Errorf("SubscribeBLA with empty user should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultPUABLA()
	b := DefaultPUABLA()
	if a != b {
		t.Fatal("DefaultPUABLA should return the same instance")
	}
	a.PublishBLA("u", "idle")
	Init()
	c := DefaultPUABLA()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
	if c.GetBLAState("u") != "" {
		t.Errorf("reset default should have no state")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	users := []string{"u1", "u2", "u3"}
	for _, u := range users {
		wg.Add(1)
		u := u
		go func() {
			defer wg.Done()
			_ = m.PublishBLA(u, "busy")
			_ = m.SubscribeBLA(u)
			_ = m.GetBLAState(u)
		}()
	}
	wg.Wait()
}
