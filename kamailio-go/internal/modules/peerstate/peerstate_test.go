// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - peerstate module tests.
 */

package peerstate

import (
	"sync"
	"testing"
)

func TestSetGetState(t *testing.T) {
	m := New()
	if got := m.GetState("p1"); got != "" {
		t.Errorf("GetState unknown = %q, want empty", got)
	}
	m.SetState("p1", "up")
	if got := m.GetState("p1"); got != "up" {
		t.Errorf("GetState = %q, want up", got)
	}
	m.SetState("p1", "down")
	if got := m.GetState("p1"); got != "down" {
		t.Errorf("GetState = %q, want down", got)
	}
	// Empty peer is ignored.
	m.SetState("", "up")
	if got := m.GetState(""); got != "" {
		t.Errorf("GetState empty = %q, want empty", got)
	}
}

func TestIsAlive(t *testing.T) {
	m := New()
	m.SetState("alive", "up")
	m.SetState("dead", "down")
	if !m.IsAlive("alive") {
		t.Errorf("IsAlive should be true for up peer")
	}
	if m.IsAlive("dead") {
		t.Errorf("IsAlive should be false for down peer")
	}
	if m.IsAlive("unknown") {
		t.Errorf("IsAlive should be false for unknown peer")
	}
}

func TestList(t *testing.T) {
	m := New()
	m.SetState("a", "up")
	m.SetState("b", "down")
	list := m.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	if list["a"] != "up" || list["b"] != "down" {
		t.Errorf("List = %v", list)
	}
	// Mutating the snapshot must not affect the module.
	list["c"] = "up"
	if m.GetState("c") != "" {
		t.Errorf("mutating List snapshot affected module state")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultPeerState()
	b := DefaultPeerState()
	if a != b {
		t.Fatal("DefaultPeerState should return the same instance")
	}
	a.SetState("p", "up")
	Init()
	c := DefaultPeerState()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
	if c.GetState("p") != "" {
		t.Errorf("reset default should have no peers")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	peers := []string{"p1", "p2", "p3", "p4"}
	for _, p := range peers {
		wg.Add(1)
		p := p
		go func() {
			defer wg.Done()
			m.SetState(p, "up")
			_ = m.IsAlive(p)
			_ = m.GetState(p)
			_ = m.List()
		}()
	}
	wg.Wait()
}
