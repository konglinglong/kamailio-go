// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - tls_wolfssl module tests.
 */

package tls_wolfssl

import (
	"io"
	"net"
	"sync"
	"testing"
)

// pipePair returns a connected pair of net.Conns (a server side and a
// client side) backed by net.Pipe.
func pipePair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	c1, c2 := net.Pipe()
	return c1, c2
}

// echoHandshake reads one byte from conn and writes it back.
func echoHandshake(t *testing.T, conn net.Conn) {
	t.Helper()
	buf := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Errorf("server read: %v", err)
		return
	}
	if _, err := conn.Write(buf); err != nil {
		t.Errorf("server write: %v", err)
	}
}

func TestInitAndIsEnabled(t *testing.T) {
	m := New()
	if m.IsEnabled() {
		t.Fatal("new module should be disabled")
	}
	m.Init("server.pem", "server.key")
	if !m.IsEnabled() {
		t.Fatal("module should be enabled after Init")
	}
	m.Init("", "key")
	if m.IsEnabled() {
		t.Errorf("module should be disabled when certFile empty")
	}
	m.Init("cert", "")
	if m.IsEnabled() {
		t.Errorf("module should be disabled when keyFile empty")
	}
}

func TestHandshake(t *testing.T) {
	m := New()
	m.Init("cert.pem", "key.pem")
	client, server := pipePair(t)
	defer client.Close()
	defer server.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		echoHandshake(t, server)
	}()
	if err := m.Handshake(client); err != nil {
		t.Fatalf("Handshake error: %v", err)
	}
	wg.Wait()
}

func TestHandshakeErrors(t *testing.T) {
	m := New()
	// Not enabled.
	client, server := pipePair(t)
	defer client.Close()
	defer server.Close()
	if err := m.Handshake(client); err == nil {
		t.Errorf("Handshake should error when disabled")
	}
	m.Init("cert", "key")
	// Nil connection.
	if err := m.Handshake(nil); err == nil {
		t.Errorf("Handshake with nil conn should error")
	}
	// Closed connection.
	closed, _ := net.Pipe()
	closed.Close()
	if err := m.Handshake(closed); err == nil {
		t.Errorf("Handshake with closed conn should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultTLSWolfSSL()
	b := DefaultTLSWolfSSL()
	if a != b {
		t.Fatal("DefaultTLSWolfSSL should return the same instance")
	}
	a.Init("c", "k")
	if !a.IsEnabled() {
		t.Fatal("default should be enabled")
	}
	Init()
	c := DefaultTLSWolfSSL()
	if c == a {
		t.Fatal("package Init should reset the default instance")
	}
	if c.IsEnabled() {
		t.Errorf("reset default should be disabled")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	m.Init("c", "k")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, server := pipePair(t)
			defer client.Close()
			defer server.Close()
			go echoHandshake(t, server)
			_ = m.Handshake(client)
		}()
	}
	wg.Wait()
}
