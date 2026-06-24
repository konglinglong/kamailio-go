// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - SCTP module tests.
 */

package sctp

import (
	"sync"
	"testing"
	"time"
)

func TestInitSendReceive(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("expected not connected before Init")
	}
	if err := m.Init("10.0.0.1:5060"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsConnected() {
		t.Fatal("expected connected after Init")
	}
	if m.Addr() != "10.0.0.1:5060" {
		t.Fatalf("Addr = %q", m.Addr())
	}
	if err := m.Send([]byte("hello")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	rx := m.Receive()
	if rx == nil {
		t.Fatal("expected non-nil receive channel")
	}
	select {
	case got := <-rx:
		if string(got) != "hello" {
			t.Fatalf("received %q, want hello", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for received data")
	}
}

func TestInitErrors(t *testing.T) {
	m := New()
	if err := m.Init(""); err == nil {
		t.Fatal("expected error for empty address")
	}
	if err := m.Send([]byte("x")); err == nil {
		t.Fatal("expected error when not connected")
	}
	m.Init("addr:5060")
	if err := m.Send(nil); err == nil {
		t.Fatal("expected error for empty data")
	}
	if err := m.Send([]byte{}); err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestClose(t *testing.T) {
	m := New()
	m.Init("addr:5060")
	m.Send([]byte("x"))
	m.Close()
	if m.IsConnected() {
		t.Fatal("expected not connected after Close")
	}
	if err := m.Send([]byte("x")); err == nil {
		t.Fatal("expected error when sending after Close")
	}
	if m.Receive() != nil {
		t.Fatal("expected nil receive channel after Close")
	}
	// Close again should be a no-op.
	m.Close()
}

func TestReceiveNilWhenNotConnected(t *testing.T) {
	m := New()
	if m.Receive() != nil {
		t.Fatal("expected nil receive channel when not connected")
	}
}

func TestGlobalFunctions(t *testing.T) {
	if err := Init("global:5060"); err != nil {
		t.Fatalf("global Init: %v", err)
	}
	if !IsConnected() {
		t.Fatal("expected global connected")
	}
	if err := Send([]byte("g")); err != nil {
		t.Fatalf("global Send: %v", err)
	}
	select {
	case <-Receive():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for global receive")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.Init("addr:5060")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Send([]byte("x"))
			_ = m.IsConnected()
			_ = m.Receive()
		}()
	}
	wg.Wait()
}
