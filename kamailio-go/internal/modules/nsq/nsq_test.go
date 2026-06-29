// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - nsq module tests.
 *
 * These tests exercise the in-memory topic/channel simulation; no real
 * nsqd instance is required.
 */

package nsq

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newConnected(t *testing.T) *NSQModule {
	t.Helper()
	m := New()
	if err := m.Init(&NSQConfig{
		NSQDAddr: "127.0.0.1:4150",
		Topic:    "test",
		Channel:  "ch",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return m
}

func TestInitAndConnected(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("expected not connected before Init")
	}
	if err := m.Init(&NSQConfig{NSQDAddr: "127.0.0.1:4150"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsConnected() {
		t.Fatal("expected connected after Init with nsqd addr")
	}
	// Lookupd-only config also connects.
	if err := m.Init(&NSQConfig{LookupDAddr: "127.0.0.1:4161"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsConnected() {
		t.Fatal("expected connected with lookupd addr")
	}
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init(nil): %v", err)
	}
	if m.IsConnected() {
		t.Fatal("expected disconnected after nil config")
	}
	if err := m.Init(&NSQConfig{}); err != nil {
		t.Fatalf("Init(empty): %v", err)
	}
	if m.IsConnected() {
		t.Fatal("expected disconnected for empty config")
	}
}

func TestPublishSubscribe(t *testing.T) {
	m := newConnected(t)

	var got []*NSQMessage
	var mu sync.Mutex
	if err := m.Subscribe("orders", "worker", func(msg *NSQMessage) {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := m.Publish("orders", []byte("hello")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})

	if string(got[0].Body) != "hello" {
		t.Errorf("body = %q", got[0].Body)
	}
	if got[0].Topic != "orders" {
		t.Errorf("topic = %q", got[0].Topic)
	}
	if got[0].Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
	if got[0].Attempts != 0 {
		t.Errorf("attempts = %d, want 0", got[0].Attempts)
	}
	if msgs := m.Messages("orders"); len(msgs) != 1 {
		t.Errorf("logged messages = %d, want 1", len(msgs))
	}
}

func TestMultipleChannelsPerTopic(t *testing.T) {
	m := newConnected(t)

	var ch1, ch2 atomic.Int64
	m.Subscribe("events", "channel1", func(*NSQMessage) { ch1.Add(1) })
	m.Subscribe("events", "channel2", func(*NSQMessage) { ch2.Add(1) })

	m.Publish("events", []byte("x"))
	waitFor(t, func() bool { return ch1.Load() == 1 && ch2.Load() == 1 })

	// Each channel receives its own copy.
	if ch1.Load() != 1 {
		t.Errorf("channel1 received = %d, want 1", ch1.Load())
	}
	if ch2.Load() != 1 {
		t.Errorf("channel2 received = %d, want 1", ch2.Load())
	}
	// Topic log holds a single message (one publish).
	if msgs := m.Messages("events"); len(msgs) != 1 {
		t.Errorf("logged = %d, want 1", len(msgs))
	}
	if subs := m.Subscriptions(); len(subs) != 2 {
		t.Errorf("subscriptions = %d, want 2", len(subs))
	}
}

func TestUnsubscribe(t *testing.T) {
	m := newConnected(t)
	var got atomic.Int64
	m.Subscribe("a", "ch", func(*NSQMessage) { got.Add(1) })

	m.Publish("a", []byte("1"))
	waitFor(t, func() bool { return got.Load() == 1 })

	if err := m.Unsubscribe("a", "ch"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	// After unsubscribe, publishes are not delivered.
	m.Publish("a", []byte("2"))
	time.Sleep(20 * time.Millisecond)
	if got.Load() != 1 {
		t.Errorf("received after unsubscribe = %d, want 1", got.Load())
	}
	// Unsubscribing an unknown pair is not an error.
	if err := m.Unsubscribe("nope", "nope"); err != nil {
		t.Errorf("Unsubscribe unknown: %v", err)
	}
}

func TestErrors(t *testing.T) {
	m := newConnected(t)

	if err := m.Publish("", []byte("x")); err == nil {
		t.Error("expected error for empty topic")
	}
	if err := m.Subscribe("", "ch", func(*NSQMessage) {}); err == nil {
		t.Error("expected error for empty topic")
	}
	if err := m.Subscribe("a", "", func(*NSQMessage) {}); err == nil {
		t.Error("expected error for empty channel")
	}
	if err := m.Subscribe("a", "ch", nil); err == nil {
		t.Error("expected error for nil handler")
	}

	// Not connected -> errors.
	m2 := New()
	m2.Init(nil)
	if err := m2.Publish("a", []byte("x")); err == nil {
		t.Error("expected Publish error when not connected")
	}
	if err := m2.Subscribe("a", "ch", func(*NSQMessage) {}); err == nil {
		t.Error("expected Subscribe error when not connected")
	}
	if err := m2.Unsubscribe("a", "ch"); err == nil {
		t.Error("expected Unsubscribe error when not connected")
	}
}

func TestClose(t *testing.T) {
	m := newConnected(t)
	m.Subscribe("a", "ch", func(*NSQMessage) {})

	m.Close()
	if m.IsConnected() {
		t.Error("expected disconnected after Close")
	}
	if err := m.Publish("a", []byte("x")); err == nil {
		t.Error("expected Publish error after close")
	}
	if err := m.Subscribe("a", "ch", func(*NSQMessage) {}); err == nil {
		t.Error("expected Subscribe error after close")
	}
	if err := m.Unsubscribe("a", "ch"); err == nil {
		t.Error("expected Unsubscribe error after close")
	}
	if len(m.Subscriptions()) != 0 {
		t.Errorf("subscriptions after close = %v", m.Subscriptions())
	}
	// Close is idempotent.
	m.Close()
}

func TestConcurrentAccess(t *testing.T) {
	m := newConnected(t)
	var received atomic.Int64
	m.Subscribe("c", "ch", func(*NSQMessage) { received.Add(1) })

	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Publish("c", []byte("p"))
		}()
	}
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Messages("c")
		}()
	}
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.IsConnected()
		}()
	}
	wg.Wait()

	if received.Load() != int64(goroutines) {
		t.Errorf("received = %d, want %d", received.Load(), goroutines)
	}
	if msgs := m.Messages("c"); len(msgs) != goroutines {
		t.Errorf("logged = %d, want %d", len(msgs), goroutines)
	}
}

func TestDefaultNSQAndInit(t *testing.T) {
	if DefaultNSQ() == nil {
		t.Fatal("DefaultNSQ() nil")
	}
	Init()
	d1 := DefaultNSQ()
	d2 := DefaultNSQ()
	if d1 != d2 {
		t.Fatal("DefaultNSQ returned different instances")
	}
	if err := d1.Init(&NSQConfig{NSQDAddr: "127.0.0.1:4150"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !d2.IsConnected() {
		t.Error("expected default to share state")
	}
	Init()
	if DefaultNSQ().IsConnected() {
		t.Error("expected reset after Init()")
	}
}

// waitFor polls cond until it returns true or the deadline elapses.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition never became true")
}
