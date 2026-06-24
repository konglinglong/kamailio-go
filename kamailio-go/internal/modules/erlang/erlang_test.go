// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the erlang module.
 */

package erlang

import (
	"sync"
	"testing"
)

func TestInitAndIsConnected(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Errorf("IsConnected() = true before Init")
	}
	m.Init("erlang@localhost")
	if !m.IsConnected() {
		t.Errorf("IsConnected() = false after Init")
	}
}

func TestSendReceive(t *testing.T) {
	m := New()
	m.Init("erlang@localhost")

	if err := m.Send("node1", "hello"); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	m.Send("node2", "world")

	if got := m.Pending(); got != 2 {
		t.Errorf("Pending() = %d, want 2", got)
	}

	msg := m.Receive()
	if msg != "node1:hello" {
		t.Errorf("Receive() = %q, want %q", msg, "node1:hello")
	}
	msg = m.Receive()
	if msg != "node2:world" {
		t.Errorf("Receive() = %q, want %q", msg, "node2:world")
	}
	if msg := m.Receive(); msg != "" {
		t.Errorf("Receive() on empty mailbox = %q, want empty", msg)
	}
}

func TestSendErrors(t *testing.T) {
	m := New()
	// Not connected.
	if err := m.Send("node", "msg"); err == nil {
		t.Errorf("Send when disconnected should error")
	}
	m.Init("erlang@localhost")
	// Empty message.
	if err := m.Send("node", ""); err == nil {
		t.Errorf("Send(empty) should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init("pkg@localhost")
	d := DefaultErlang()
	if d == nil {
		t.Fatalf("DefaultErlang() returned nil")
	}
	if !IsConnected() {
		t.Errorf("package IsConnected() = false")
	}
	if err := Send("n", "m"); err != nil {
		t.Fatalf("package Send error: %v", err)
	}
	if got := Receive(); got != "n:m" {
		t.Errorf("package Receive() = %q, want %q", got, "n:m")
	}
}

func TestConcurrent(t *testing.T) {
	Init("c@localhost")
	shared := DefaultErlang()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			shared.Send("n", itoa(i))
			shared.Receive()
		}(i)
	}
	wg.Wait()
	if got := shared.Pending(); got != 0 {
		t.Errorf("Pending() after concurrent = %d, want 0", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
