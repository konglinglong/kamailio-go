// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - pua_reginfo module tests.
 */

package pua_reginfo

import (
	"sync"
	"testing"
)

func TestPublishAndGet(t *testing.T) {
	m := New()
	if got := m.GetRegInfo("alice"); got != "" {
		t.Errorf("GetRegInfo unknown = %q, want empty", got)
	}
	if err := m.PublishRegInfo("alice", "<reg/>"); err != nil {
		t.Fatalf("PublishRegInfo error: %v", err)
	}
	if got := m.GetRegInfo("alice"); got != "<reg/>" {
		t.Errorf("GetRegInfo = %q, want <reg/>", got)
	}
	if err := m.PublishRegInfo("", "x"); err == nil {
		t.Errorf("PublishRegInfo with empty user should error")
	}
}

func TestSubscribe(t *testing.T) {
	m := New()
	if m.IsSubscribed("bob") {
		t.Errorf("IsSubscribed should be false before subscribe")
	}
	if err := m.SubscribeRegInfo("bob"); err != nil {
		t.Fatalf("SubscribeRegInfo error: %v", err)
	}
	if !m.IsSubscribed("bob") {
		t.Errorf("IsSubscribed should be true after subscribe")
	}
	if err := m.SubscribeRegInfo(""); err == nil {
		t.Errorf("SubscribeRegInfo with empty user should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultPUARegInfo()
	b := DefaultPUARegInfo()
	if a != b {
		t.Fatal("DefaultPUARegInfo should return the same instance")
	}
	a.PublishRegInfo("u", "info")
	Init()
	c := DefaultPUARegInfo()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
	if c.GetRegInfo("u") != "" {
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
			_ = m.PublishRegInfo(u, "info")
			_ = m.SubscribeRegInfo(u)
			_ = m.GetRegInfo(u)
		}()
	}
	wg.Wait()
}
