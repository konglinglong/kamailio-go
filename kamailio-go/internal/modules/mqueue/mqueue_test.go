// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - mqueue module tests.
 */

package mqueue

import (
	"sync"
	"testing"
)

func TestPushPopOrder(t *testing.T) {
	m := New()
	if n := m.Push("q", "a"); n != 1 {
		t.Errorf("Push 1 size = %d, want 1", n)
	}
	m.Push("q", "b")
	m.Push("q", "c")
	if m.Size("q") != 3 {
		t.Errorf("Size = %d, want 3", m.Size("q"))
	}
	if msg, ok := m.Pop("q"); !ok || msg != "a" {
		t.Errorf("Pop 1 = %q,%v, want a,true", msg, ok)
	}
	if msg, ok := m.Pop("q"); !ok || msg != "b" {
		t.Errorf("Pop 2 = %q,%v, want b,true", msg, ok)
	}
	if msg, ok := m.Pop("q"); !ok || msg != "c" {
		t.Errorf("Pop 3 = %q,%v, want c,true", msg, ok)
	}
	if _, ok := m.Pop("q"); ok {
		t.Error("Pop on empty should return false")
	}
}

func TestListAndClear(t *testing.T) {
	m := New()
	m.Push("q1", "x")
	m.Push("q2", "y")
	names := m.List()
	if len(names) != 2 {
		t.Fatalf("List len = %d, want 2", len(names))
	}
	m.Clear("q1")
	if m.Size("q1") != 0 {
		t.Error("Size after Clear should be 0")
	}
	if len(m.List()) != 1 {
		t.Errorf("List len after clear = %d, want 1", len(m.List()))
	}
}

func TestConcurrentPushPop(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Push("q", "msg")
		}()
	}
	wg.Wait()
	if m.Size("q") != 100 {
		t.Errorf("Size = %d, want 100", m.Size("q"))
	}
	count := 0
	for {
		if _, ok := m.Pop("q"); !ok {
			break
		}
		count++
	}
	if count != 100 {
		t.Errorf("popped %d, want 100", count)
	}
}
