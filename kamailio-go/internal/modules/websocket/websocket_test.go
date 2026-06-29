// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the WebSocket module.
 */

package websocket

import (
	"sync"
	"testing"
)

func TestInitConfig(t *testing.T) {
	m := New()
	if err := m.Init(&WSConfig{
		ListenAddr: "0.0.0.0:5060",
		Path:       "/ws",
		Origins:    []string{"example.com"},
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if m.cfg == nil {
		t.Fatalf("cfg not set after Init()")
	}
	if m.cfg.ListenAddr != "0.0.0.0:5060" {
		t.Errorf("ListenAddr = %q", m.cfg.ListenAddr)
	}
	if m.cfg.Path != "/ws" {
		t.Errorf("Path = %q", m.cfg.Path)
	}
	if len(m.cfg.Origins) != 1 || m.cfg.Origins[0] != "example.com" {
		t.Errorf("Origins = %v", m.cfg.Origins)
	}

	// nil config is accepted.
	if err := (&WSModule{}).Init(nil); err != nil {
		t.Errorf("Init(nil) error = %v", err)
	}
}

func TestHandleConnectionAndCount(t *testing.T) {
	m := New()

	c1 := m.HandleConnection("conn1", "192.0.2.1:1234")
	if c1 == nil {
		t.Fatalf("HandleConnection() returned nil")
	}
	if c1.ID != "conn1" {
		t.Errorf("ID = %q, want conn1", c1.ID)
	}
	if c1.RemoteAddr != "192.0.2.1:1234" {
		t.Errorf("RemoteAddr = %q", c1.RemoteAddr)
	}
	if c1.Subprotocol != "sip" {
		t.Errorf("Subprotocol = %q, want sip", c1.Subprotocol)
	}
	if c1.Closed {
		t.Errorf("new connection should not be Closed")
	}

	m.HandleConnection("conn2", "192.0.2.2:1234")
	if got := m.Count(); got != 2 {
		t.Errorf("Count() = %d, want 2", got)
	}

	// Re-adding the same id replaces the existing connection (count stays).
	m.HandleConnection("conn1", "192.0.2.3:1234")
	if got := m.Count(); got != 2 {
		t.Errorf("Count() after re-add = %d, want 2", got)
	}
	if c := m.Connections(); len(c) != 2 {
		t.Errorf("Connections() = %d, want 2", len(c))
	}
}

func TestSendTo(t *testing.T) {
	m := New()
	m.HandleConnection("c1", "192.0.2.1:1234")

	if err := m.SendTo("c1", []byte("hello")); err != nil {
		t.Fatalf("SendTo() error = %v", err)
	}
	pending := m.DrainOutbox("c1")
	if len(pending) != 1 {
		t.Fatalf("DrainOutbox() = %d messages, want 1", len(pending))
	}
	if string(pending[0]) != "hello" {
		t.Errorf("pending[0] = %q, want hello", pending[0])
	}
	// DrainOutbox clears the queue.
	if p := m.DrainOutbox("c1"); len(p) != 0 {
		t.Errorf("DrainOutbox() after drain = %d, want 0", len(p))
	}

	// Unknown connection -> error.
	if err := m.SendTo("nope", []byte("x")); err == nil {
		t.Errorf("SendTo(unknown) should error")
	}
}

func TestBroadcast(t *testing.T) {
	m := New()

	// No connections -> error.
	if err := m.Broadcast([]byte("hi")); err == nil {
		t.Errorf("Broadcast() with no connections should error")
	}

	m.HandleConnection("c1", "192.0.2.1:1")
	m.HandleConnection("c2", "192.0.2.2:1")
	m.HandleConnection("c3", "192.0.2.3:1")

	if err := m.Broadcast([]byte("ping")); err != nil {
		t.Fatalf("Broadcast() error = %v", err)
	}
	for _, id := range []string{"c1", "c2", "c3"} {
		if p := m.DrainOutbox(id); len(p) != 1 || string(p[0]) != "ping" {
			t.Errorf("conn %s pending = %v, want [ping]", id, p)
		}
	}
}

func TestCloseAndCloseAll(t *testing.T) {
	m := New()
	m.HandleConnection("c1", "192.0.2.1:1")
	m.HandleConnection("c2", "192.0.2.2:1")

	if err := m.Close("c1"); err != nil {
		t.Fatalf("Close(c1) error = %v", err)
	}
	if got := m.Count(); got != 1 {
		t.Errorf("Count() after close = %d, want 1", got)
	}
	// Closing again -> error (already removed).
	if err := m.Close("c1"); err == nil {
		t.Errorf("Close(c1) twice should error")
	}
	// Unknown -> error.
	if err := m.Close("nope"); err == nil {
		t.Errorf("Close(unknown) should error")
	}

	m.CloseAll()
	if got := m.Count(); got != 0 {
		t.Errorf("Count() after CloseAll = %d, want 0", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultWS()
	if d == nil {
		t.Fatalf("DefaultWS() returned nil")
	}
	if d != DefaultWS() {
		t.Fatalf("DefaultWS() returned different instances after Init()")
	}

	// Re-init resets state.
	d.HandleConnection("x", "1.2.3.4:1")
	if got := d.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}
	Init()
	if got := DefaultWS().Count(); got != 0 {
		t.Errorf("Count() after re-Init = %d, want 0", got)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultWS()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			id := "c" + itoa(i)
			shared.HandleConnection(id, "1.2.3.4:1")
			shared.SendTo(id, []byte("m"))
			shared.Broadcast([]byte("b"))
			shared.Count()
			shared.Connections()
			shared.DrainOutbox(id)
			shared.Close(id)
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != 0 {
		t.Errorf("Count() after concurrent = %d, want 0", got)
	}
}

// itoa is a tiny local int->string helper to avoid pulling strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
