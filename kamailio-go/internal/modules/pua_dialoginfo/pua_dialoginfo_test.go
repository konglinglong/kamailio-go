// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - pua_dialoginfo module tests.
 */

package pua_dialoginfo

import (
	"sync"
	"testing"
)

func TestPublishAndGet(t *testing.T) {
	m := New()
	if got := m.GetDialogInfo("alice"); got != "" {
		t.Errorf("GetDialogInfo unknown = %q, want empty", got)
	}
	if err := m.PublishDialogInfo("alice", "<dialog/>"); err != nil {
		t.Fatalf("PublishDialogInfo error: %v", err)
	}
	if got := m.GetDialogInfo("alice"); got != "<dialog/>" {
		t.Errorf("GetDialogInfo = %q, want <dialog/>", got)
	}
	if err := m.PublishDialogInfo("", "x"); err == nil {
		t.Errorf("PublishDialogInfo with empty user should error")
	}
}

func TestSubscribe(t *testing.T) {
	m := New()
	if m.IsSubscribed("bob") {
		t.Errorf("IsSubscribed should be false before subscribe")
	}
	if err := m.SubscribeDialogInfo("bob"); err != nil {
		t.Fatalf("SubscribeDialogInfo error: %v", err)
	}
	if !m.IsSubscribed("bob") {
		t.Errorf("IsSubscribed should be true after subscribe")
	}
	if err := m.SubscribeDialogInfo(""); err == nil {
		t.Errorf("SubscribeDialogInfo with empty user should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultPUADialogInfo()
	b := DefaultPUADialogInfo()
	if a != b {
		t.Fatal("DefaultPUADialogInfo should return the same instance")
	}
	a.PublishDialogInfo("u", "info")
	Init()
	c := DefaultPUADialogInfo()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
	if c.GetDialogInfo("u") != "" {
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
			_ = m.PublishDialogInfo(u, "info")
			_ = m.SubscribeDialogInfo(u)
			_ = m.GetDialogInfo(u)
		}()
	}
	wg.Wait()
}
