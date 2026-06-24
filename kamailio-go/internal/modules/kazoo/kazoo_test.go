// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - kazoo module tests.
 */

package kazoo

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestInitPublishSubscribe(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("new module should not be connected")
	}
	m.Init("amqp://guest:guest@localhost:5672")
	if !m.IsConnected() {
		t.Fatal("module should be connected after Init")
	}
	var got []byte
	var mu sync.Mutex
	m.Subscribe("events", func(b []byte) {
		mu.Lock()
		got = b
		mu.Unlock()
	})
	if err := m.Publish("", "events", []byte("hello")); err != nil {
		t.Fatalf("Publish error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if string(got) != "hello" {
		t.Errorf("handler got %q, want hello", got)
	}
}

func TestPublishErrors(t *testing.T) {
	m := New()
	// Not connected.
	if err := m.Publish("", "q", []byte("x")); err == nil {
		t.Errorf("Publish should error when not connected")
	}
	m.Init("amqp://x")
	// Empty exchange and routing key.
	if err := m.Publish("", "", []byte("x")); err == nil {
		t.Errorf("Publish should error for empty exchange and routing key")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	m := New()
	m.Init("amqp://x")
	var count int32
	for i := 0; i < 3; i++ {
		m.Subscribe("topic", func(b []byte) {
			atomic.AddInt32(&count, 1)
		})
	}
	if err := m.Publish("", "topic", []byte("msg")); err != nil {
		t.Fatalf("Publish error: %v", err)
	}
	if got := atomic.LoadInt32(&count); got != 3 {
		t.Errorf("handler count = %d, want 3", got)
	}
	// Subscribe with empty queue or nil handler is a no-op.
	m.Subscribe("", func(b []byte) {})
	m.Subscribe("topic", nil)
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultKazoo()
	b := DefaultKazoo()
	if a != b {
		t.Fatal("DefaultKazoo should return the same instance")
	}
	a.Init("amqp://x")
	if !a.IsConnected() {
		t.Fatal("default should be connected")
	}
	Init()
	c := DefaultKazoo()
	if c == a {
		t.Fatal("package Init should reset the default instance")
	}
	if c.IsConnected() {
		t.Errorf("reset default should not be connected")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	m.Init("amqp://x")
	var received int32
	m.Subscribe("q", func(b []byte) {
		atomic.AddInt32(&received, 1)
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Publish("", "q", []byte("x"))
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&received); got != 20 {
		t.Errorf("received = %d, want 20", got)
	}
}
