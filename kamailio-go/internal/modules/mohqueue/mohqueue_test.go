// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - mohqueue module tests.
 */

package mohqueue

import (
	"testing"
)

func TestEnqueueDequeue(t *testing.T) {
	m := New()
	m.AddQueue(&MOHQueue{Name: "moh1", URI: "sip:moh1@example.com", MaxCallers: 2})
	if !m.Enqueue("moh1", "call-1") {
		t.Fatal("Enqueue call-1 failed")
	}
	if !m.Enqueue("moh1", "call-2") {
		t.Fatal("Enqueue call-2 failed")
	}
	if m.Enqueue("moh1", "call-3") {
		t.Fatal("Enqueue call-3 should fail (capacity)")
	}
	if m.QueueSize("moh1") != 2 {
		t.Errorf("QueueSize = %d, want 2", m.QueueSize("moh1"))
	}
	if got := m.Dequeue("moh1"); got != "call-1" {
		t.Errorf("Dequeue = %q, want call-1", got)
	}
	if got := m.Dequeue("moh1"); got != "call-2" {
		t.Errorf("Dequeue = %q, want call-2", got)
	}
	if got := m.Dequeue("moh1"); got != "" {
		t.Errorf("Dequeue empty = %q, want empty", got)
	}
}

func TestUnknownQueue(t *testing.T) {
	m := New()
	if m.Enqueue("nope", "call") {
		t.Error("Enqueue to unknown queue should fail")
	}
	if got := m.Dequeue("nope"); got != "" {
		t.Errorf("Dequeue unknown = %q, want empty", got)
	}
	if m.QueueSize("nope") != 0 {
		t.Errorf("QueueSize unknown = %d, want 0", m.QueueSize("nope"))
	}
}

func TestRemoveQueue(t *testing.T) {
	m := New()
	m.AddQueue(&MOHQueue{Name: "moh2", URI: "sip:moh2@example.com", MaxCallers: 5})
	m.Enqueue("moh2", "call-a")
	m.Enqueue("moh2", "call-b")
	m.RemoveQueue("moh2")
	if m.QueueSize("moh2") != 0 {
		t.Error("QueueSize should be 0 after RemoveQueue")
	}
	if m.Enqueue("moh2", "call-c") {
		t.Error("Enqueue should fail after RemoveQueue")
	}
}

func TestUnlimitedCapacity(t *testing.T) {
	m := New()
	m.AddQueue(&MOHQueue{Name: "unlimited", URI: "sip:u@example.com", MaxCallers: 0})
	for i := 0; i < 100; i++ {
		if !m.Enqueue("unlimited", "c") {
			t.Fatalf("Enqueue %d failed", i)
		}
	}
	if m.QueueSize("unlimited") != 100 {
		t.Errorf("QueueSize = %d, want 100", m.QueueSize("unlimited"))
	}
}
