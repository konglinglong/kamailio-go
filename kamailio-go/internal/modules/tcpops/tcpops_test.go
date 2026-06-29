// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the TCP operations (tcpops) module.
 */

package tcpops

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// addVia appends a Via header to msg and wires up the HdrVia1 quick ref.
func addVia(msg *parser.SIPMsg, body string) *parser.HdrField {
	h := msg.AddHeader("Via", body)
	if msg.HdrVia1 == nil {
		msg.HdrVia1 = h
	}
	return h
}

func TestAddAndGetConnection(t *testing.T) {
	m := New()

	c := m.AddConnection("192.0.2.1:5060", "198.51.100.1:40000")
	if c == nil {
		t.Fatalf("AddConnection() returned nil")
	}
	if c.ID <= 0 {
		t.Errorf("ID = %d, want > 0", c.ID)
	}
	if c.State != "established" {
		t.Errorf("State = %q, want established", c.State)
	}
	if c.LocalAddr != "192.0.2.1:5060" {
		t.Errorf("LocalAddr = %q", c.LocalAddr)
	}
	if c.RemoteAddr != "198.51.100.1:40000" {
		t.Errorf("RemoteAddr = %q", c.RemoteAddr)
	}

	if got := m.GetConnectionByID(c.ID); got != c {
		t.Errorf("GetConnectionByID() = %p, want %p", got, c)
	}
	if m.GetConnectionByID(99999) != nil {
		t.Errorf("GetConnectionByID(unknown) should return nil")
	}
}

func TestGetConnectionBySource(t *testing.T) {
	m := New()

	m.AddConnection("192.0.2.1:5060", "198.51.100.1:40000")
	c2 := m.AddConnection("192.0.2.1:5060", "198.51.100.2:40001")

	if got := m.GetConnectionBySource("198.51.100.2", 40001); got != c2 {
		t.Errorf("GetConnectionBySource() = %p, want %p", got, c2)
	}
	if m.GetConnectionBySource("198.51.100.9", 1) != nil {
		t.Errorf("GetConnectionBySource(unknown) should return nil")
	}
}

func TestSetLifetimeAndKeepalive(t *testing.T) {
	m := New()

	c := m.AddConnection("1.2.3.4:5060", "5.6.7.8:1")

	if !m.SetConnectionLifetime(c.ID, 120) {
		t.Fatalf("SetConnectionLifetime() = false, want true")
	}
	if got := m.GetConnectionByID(c.ID).Lifetime; got != 120 {
		t.Errorf("Lifetime = %d, want 120", got)
	}
	if m.SetConnectionLifetime(99999, 1) {
		t.Errorf("SetConnectionLifetime(unknown) = true, want false")
	}

	if !m.SetKeepalive(c.ID, true) {
		t.Fatalf("SetKeepalive() = false, want true")
	}
	if got := m.GetConnectionByID(c.ID).Keepalive; !got {
		t.Errorf("Keepalive = false, want true")
	}
	if !m.SetKeepalive(c.ID, false) {
		t.Errorf("SetKeepalive(false) = false, want true")
	}
	if m.GetConnectionByID(c.ID).Keepalive {
		t.Errorf("Keepalive = true, want false")
	}
	if m.SetKeepalive(99999, true) {
		t.Errorf("SetKeepalive(unknown) = true, want false")
	}
}

func TestCloseConnection(t *testing.T) {
	m := New()

	c := m.AddConnection("1.2.3.4:5060", "5.6.7.8:1")
	if m.ConnectionCount() != 1 {
		t.Errorf("ConnectionCount() = %d, want 1", m.ConnectionCount())
	}
	if !m.CloseConnection(c.ID) {
		t.Fatalf("CloseConnection() = false, want true")
	}
	if m.ConnectionCount() != 0 {
		t.Errorf("ConnectionCount() after close = %d, want 0", m.ConnectionCount())
	}
	if m.GetConnectionByID(c.ID) != nil {
		t.Errorf("GetConnectionByID() after close should return nil")
	}
	// Closing again -> false.
	if m.CloseConnection(c.ID) {
		t.Errorf("CloseConnection() twice = true, want false")
	}
}

func TestListConnections(t *testing.T) {
	m := New()
	m.AddConnection("a:1", "b:1")
	m.AddConnection("a:2", "b:2")
	m.AddConnection("a:3", "b:3")

	list := m.ListConnections()
	if len(list) != 3 {
		t.Fatalf("ListConnections() = %d, want 3", len(list))
	}
	// IDs are unique and ascending.
	seen := make(map[int]bool)
	for _, c := range list {
		if seen[c.ID] {
			t.Errorf("duplicate id %d in list", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestIsPersistent(t *testing.T) {
	m := New()

	// TCP Via -> persistent.
	msgTCP := &parser.SIPMsg{}
	addVia(msgTCP, "SIP/2.0/TCP 192.0.2.1:5060;branch=z9hG4bK1")
	if !m.IsPersistent(msgTCP) {
		t.Errorf("IsPersistent(TCP) = false, want true")
	}

	// TLS Via -> persistent.
	msgTLS := &parser.SIPMsg{}
	addVia(msgTLS, "SIP/2.0/TLS 192.0.2.1:5061;branch=z9hG4bK2")
	if !m.IsPersistent(msgTLS) {
		t.Errorf("IsPersistent(TLS) = false, want true")
	}

	// UDP Via -> not persistent.
	msgUDP := &parser.SIPMsg{}
	addVia(msgUDP, "SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK3")
	if m.IsPersistent(msgUDP) {
		t.Errorf("IsPersistent(UDP) = true, want false")
	}

	// Connection: keep-alive header -> persistent even on UDP.
	msgKeep := &parser.SIPMsg{}
	addVia(msgKeep, "SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK4")
	msgKeep.AddHeader("Connection", "keep-alive")
	if !m.IsPersistent(msgKeep) {
		t.Errorf("IsPersistent(Connection: keep-alive) = false, want true")
	}

	// No Via, no Connection header -> false.
	if m.IsPersistent(&parser.SIPMsg{}) {
		t.Errorf("IsPersistent(empty) = true, want false")
	}
	// nil msg -> false.
	if m.IsPersistent(nil) {
		t.Errorf("IsPersistent(nil) = true, want false")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultTCPOps()
	if d == nil {
		t.Fatalf("DefaultTCPOps() returned nil")
	}
	if d != DefaultTCPOps() {
		t.Fatalf("DefaultTCPOps() returned different instances after Init()")
	}

	// Re-init resets state.
	d.AddConnection("a:1", "b:1")
	if got := d.ConnectionCount(); got != 1 {
		t.Fatalf("ConnectionCount() = %d, want 1", got)
	}
	Init()
	if got := DefaultTCPOps().ConnectionCount(); got != 0 {
		t.Errorf("ConnectionCount() after re-Init = %d, want 0", got)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultTCPOps()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	ids := make([]int, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			c := shared.AddConnection("1.2.3.4:5060", "5.6.7.8:1")
			ids[i] = c.ID
			shared.GetConnectionByID(c.ID)
			shared.GetConnectionBySource("5.6.7.8", 1)
			shared.SetConnectionLifetime(c.ID, 60)
			shared.SetKeepalive(c.ID, true)
			shared.ConnectionCount()
			shared.ListConnections()
			shared.CloseConnection(c.ID)
		}(i)
	}
	wg.Wait()
	if got := shared.ConnectionCount(); got != 0 {
		t.Errorf("ConnectionCount() after concurrent = %d, want 0", got)
	}
}
