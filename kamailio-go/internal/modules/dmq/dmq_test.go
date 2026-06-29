// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - dmq module tests.
 */

package dmq

import (
	"sync"
	"testing"
	"time"
)

func TestInitAndPeers(t *testing.T) {
	m := New()
	if err := m.Init(&DMQConfig{ListenAddr: "self", Peers: []string{"1.2.3.4:5060"}}); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	peers := m.Peers()
	if len(peers) != 1 {
		t.Fatalf("peers = %d, want 1", len(peers))
	}
	if !m.IsConnected("1.2.3.4:5060") {
		t.Errorf("peer should be connected")
	}
	if m.IsConnected("nope:1") {
		t.Errorf("unknown peer should not be connected")
	}
}

func TestAddRemovePeer(t *testing.T) {
	m := New()
	p := m.AddPeer("10.0.0.1", 5060)
	if p == nil {
		t.Fatalf("AddPeer returned nil")
	}
	if p.Address != "10.0.0.1" || p.Port != 5060 {
		t.Errorf("peer = %+v", p)
	}
	if !p.Connected {
		t.Errorf("new peer should be connected")
	}
	if !m.IsConnected("10.0.0.1:5060") {
		t.Errorf("peer should be connected")
	}
	// Re-adding the same peer returns the existing one (upsert).
	p2 := m.AddPeer("10.0.0.1", 5060)
	if p2 != p {
		t.Errorf("re-add should return same peer")
	}
	if !m.RemovePeer("10.0.0.1:5060") {
		t.Errorf("RemovePeer should return true for existing peer")
	}
	if m.RemovePeer("10.0.0.1:5060") {
		t.Errorf("RemovePeer should return false after removal")
	}
	if m.IsConnected("10.0.0.1:5060") {
		t.Errorf("removed peer should not be connected")
	}
}

func TestBroadcast(t *testing.T) {
	m := New()
	m.Init(&DMQConfig{ListenAddr: "self"})
	m.AddPeer("10.0.0.1", 5060)

	if err := m.Broadcast("notify", "hello"); err != nil {
		t.Fatalf("Broadcast error: %v", err)
	}
	select {
	case msg := <-m.Receive():
		if msg.Type != "notify" || msg.Body != "hello" {
			t.Errorf("msg = %+v", msg)
		}
		if msg.FromPeer != "self" {
			t.Errorf("FromPeer = %q, want self", msg.FromPeer)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for broadcast message")
	}
}

func TestBroadcastNoPeers(t *testing.T) {
	m := New()
	m.Init(&DMQConfig{ListenAddr: "self"})
	if err := m.Broadcast("notify", "hello"); err == nil {
		t.Errorf("expected error broadcasting with no peers")
	}
}

func TestSendToPeer(t *testing.T) {
	m := New()
	m.Init(&DMQConfig{ListenAddr: "self"})
	m.AddPeer("10.0.0.1", 5060)

	if err := m.SendToPeer("10.0.0.1:5060", "direct", "payload"); err != nil {
		t.Fatalf("SendToPeer error: %v", err)
	}
	select {
	case msg := <-m.Receive():
		if msg.Type != "direct" || msg.Body != "payload" {
			t.Errorf("msg = %+v", msg)
		}
		if msg.FromPeer != "10.0.0.1:5060" {
			t.Errorf("FromPeer = %q, want 10.0.0.1:5060", msg.FromPeer)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for message")
	}

	// Sending to an unknown peer fails.
	if err := m.SendToPeer("missing:5060", "x", "y"); err == nil {
		t.Errorf("expected error for unknown peer")
	}
}

func TestClose(t *testing.T) {
	m := New()
	m.AddPeer("10.0.0.1", 5060)
	m.Close()
	// After close, the receive channel is closed.
	if _, ok := <-m.Receive(); ok {
		t.Errorf("receive channel should be closed")
	}
	// Close is idempotent.
	m.Close()
	// Broadcast after close fails.
	if err := m.Broadcast("x", "y"); err == nil {
		t.Errorf("expected error broadcasting after close")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.Init(&DMQConfig{ListenAddr: "self"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.AddPeer("10.0.0.1", 5060+i)
		}(i)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Broadcast("t", "b")
		}()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Peers()
		}()
	}
	wg.Wait()
	// Drain the inbox so goroutines don't leak.
	for len(m.Receive()) > 0 {
		<-m.Receive()
	}
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultDMQ() == nil {
		t.Fatalf("DefaultDMQ() nil")
	}
	Init()
	if DefaultDMQ() == nil {
		t.Fatalf("DefaultDMQ() nil after Init")
	}
}
