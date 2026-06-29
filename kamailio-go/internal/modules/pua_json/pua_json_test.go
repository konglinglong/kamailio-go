// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - pua_json module tests.
 */

package pua_json

import (
	"sync"
	"testing"
)

func TestPublishAndGet(t *testing.T) {
	m := New()
	if got := m.GetJSON("alice"); got != "" {
		t.Errorf("GetJSON unknown = %q, want empty", got)
	}
	payload := `{"state":"busy","activity":"meeting"}`
	if err := m.PublishJSON("alice", payload); err != nil {
		t.Fatalf("PublishJSON error: %v", err)
	}
	if got := m.GetJSON("alice"); got != payload {
		t.Errorf("GetJSON = %q, want %q", got, payload)
	}
	if err := m.PublishJSON("", "x"); err == nil {
		t.Errorf("PublishJSON with empty user should error")
	}
}

func TestSubscribe(t *testing.T) {
	m := New()
	if m.IsSubscribed("bob") {
		t.Errorf("IsSubscribed should be false before subscribe")
	}
	if err := m.SubscribeJSON("bob"); err != nil {
		t.Fatalf("SubscribeJSON error: %v", err)
	}
	if !m.IsSubscribed("bob") {
		t.Errorf("IsSubscribed should be true after subscribe")
	}
	if err := m.SubscribeJSON(""); err == nil {
		t.Errorf("SubscribeJSON with empty user should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultPUAJSON()
	b := DefaultPUAJSON()
	if a != b {
		t.Fatal("DefaultPUAJSON should return the same instance")
	}
	a.PublishJSON("u", "{}")
	Init()
	c := DefaultPUAJSON()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
	if c.GetJSON("u") != "" {
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
			_ = m.PublishJSON(u, "{}")
			_ = m.SubscribeJSON(u)
			_ = m.GetJSON(u)
		}()
	}
	wg.Wait()
}
