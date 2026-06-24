// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - dmq_usrloc module tests.
 */

package dmq_usrloc

import (
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/modules/dmq"
)

// waitFor polls cond until it returns true or the deadline elapses.
func waitFor(t *testing.T, deadline time.Duration, cond func() bool, msg string) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting: %s", msg)
}

func TestInit(t *testing.T) {
	m := New()
	m.Init(&DMQUsrlocConfig{SyncInterval: 0, Enable: true})
	if !m.enabled {
		t.Errorf("should be enabled")
	}
	if m.syncInterval != DefaultSyncInterval {
		t.Errorf("zero interval should default, got %v", m.syncInterval)
	}
}

func TestSyncContact(t *testing.T) {
	m := New()
	m.Init(&DMQUsrlocConfig{Enable: true})
	// Broadcast needs at least one connected peer.
	m.DMQ().AddPeer("10.0.0.1", 5060)

	if err := m.SyncContact("alice@example.com", "sip:alice@10.0.0.1", "add"); err != nil {
		t.Fatalf("SyncContact error: %v", err)
	}
	if m.PendingSyncs() != 1 {
		t.Errorf("pending = %d, want 1", m.PendingSyncs())
	}
	// The message should be sitting on the DMQ receive channel.
	select {
	case msg := <-m.DMQ().Receive():
		if msg.Type != SyncMessageType {
			t.Errorf("type = %q, want %q", msg.Type, SyncMessageType)
		}
		if msg.Body != "alice@example.com|sip:alice@10.0.0.1|add" {
			t.Errorf("body = %q", msg.Body)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for DMQ message")
	}
}

func TestSyncContactDisabled(t *testing.T) {
	m := New()
	m.Init(&DMQUsrlocConfig{Enable: false})
	if err := m.SyncContact("a", "b", "add"); err == nil {
		t.Errorf("expected error when disabled")
	}
}

func TestReceiveSync(t *testing.T) {
	m := New()
	m.Init(&DMQUsrlocConfig{Enable: true})

	msg := &dmq.DMQMessage{
		Type:     SyncMessageType,
		Body:     "bob@example.com|sip:bob@10.0.0.2|update",
		FromPeer: "10.0.0.2:5060",
	}
	if err := m.ReceiveSync(msg); err != nil {
		t.Fatalf("ReceiveSync error: %v", err)
	}
	if m.ProcessedCount() != 1 {
		t.Errorf("processed = %d, want 1", m.ProcessedCount())
	}

	// Invalid message type.
	if err := m.ReceiveSync(&dmq.DMQMessage{Type: "other", Body: "x|y|z"}); err == nil {
		t.Errorf("expected error for wrong type")
	}
	// Malformed body.
	if err := m.ReceiveSync(&dmq.DMQMessage{Type: SyncMessageType, Body: "no-pipes"}); err == nil {
		t.Errorf("expected error for malformed body")
	}
	// Nil message.
	if err := m.ReceiveSync(nil); err == nil {
		t.Errorf("expected error for nil message")
	}
}

func TestStartStopEndToEnd(t *testing.T) {
	m := New()
	m.Init(&DMQUsrlocConfig{SyncInterval: 20 * time.Millisecond, Enable: true})
	m.DMQ().AddPeer("10.0.0.1", 5060)

	m.Start()
	// The periodic ticker broadcasts syncs; the drainer applies them.
	waitFor(t, time.Second, func() bool { return m.ProcessedCount() > 0 }, "processed > 0")
	m.Stop()

	if m.ProcessedCount() < 1 {
		t.Errorf("processed = %d, want >= 1", m.ProcessedCount())
	}
}

func TestStartDrainsPending(t *testing.T) {
	m := New()
	m.Init(&DMQUsrlocConfig{SyncInterval: time.Hour, Enable: true})
	m.DMQ().AddPeer("10.0.0.1", 5060)

	// Queue a sync before starting the drainer.
	if err := m.SyncContact("alice@example.com", "sip:alice@10.0.0.1", "add"); err != nil {
		t.Fatalf("SyncContact error: %v", err)
	}
	if m.PendingSyncs() != 1 {
		t.Fatalf("pending = %d, want 1", m.PendingSyncs())
	}
	m.Start()
	// The drainer should apply the queued sync, clearing pending.
	waitFor(t, time.Second, func() bool { return m.PendingSyncs() == 0 }, "pending == 0")
	waitFor(t, time.Second, func() bool { return m.ProcessedCount() == 1 }, "processed == 1")
	m.Stop()
}

func TestStopWhenNotRunning(t *testing.T) {
	m := New()
	// Stop without Start must be a safe no-op.
	m.Stop()
	m.Stop()
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultDMQUsrloc() == nil {
		t.Fatalf("DefaultDMQUsrloc() nil")
	}
	Init()
	if DefaultDMQUsrloc() == nil {
		t.Fatalf("DefaultDMQUsrloc() nil after Init")
	}
}
