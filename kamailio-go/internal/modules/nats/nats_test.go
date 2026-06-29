// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - nats module tests.
 *
 * These tests exercise the in-memory pub/sub and request/reply
 * simulation; no real NATS server is required.
 */

package nats

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newConnected(t *testing.T) *NATSModule {
	t.Helper()
	m := New()
	if err := m.Init(&NATSConfig{
		URL:      "nats://127.0.0.1:4222",
		Name:     "c1",
		Username: "u",
		Password: "p",
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
	if err := m.Init(&NATSConfig{URL: "nats://127.0.0.1:4222"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsConnected() {
		t.Fatal("expected connected after Init with URL")
	}
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init(nil): %v", err)
	}
	if m.IsConnected() {
		t.Fatal("expected disconnected after nil config")
	}
	if err := m.Init(&NATSConfig{}); err != nil {
		t.Fatalf("Init(empty): %v", err)
	}
	if m.IsConnected() {
		t.Fatal("expected disconnected for empty URL")
	}
}

func TestPublishSubscribe(t *testing.T) {
	m := newConnected(t)

	var got []*NATSMessage
	var mu sync.Mutex
	if err := m.Subscribe("events.created", func(msg *NATSMessage) {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := m.Publish("events.created", []byte("hello")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})

	if string(got[0].Data) != "hello" {
		t.Errorf("data = %q", got[0].Data)
	}
	if got[0].Subject != "events.created" {
		t.Errorf("subject = %q", got[0].Subject)
	}
}

func TestSubjectMatching(t *testing.T) {
	cases := []struct {
		filter  string
		subject string
		want    bool
	}{
		{"a.*.c", "a.b.c", true},
		{"a.*.c", "a.b.d", false},
		{"a.>", "a.b.c", true},
		{"a.>", "a", false},
		{">", "anything.here", true},
		{"a.b.c", "a.b.c", true},
		{"a.b.c", "a.b.d", false},
		{"*.b.c", "a.b.c", true},
		{"a.*", "a.b", true},
		{"a.*", "a.b.c", false},
		{"a.b.>", "a.b.c.d", true},
		{"a.b.>", "a.b", false},
		{"a.b.>", "a.c.b", false},
	}
	for _, c := range cases {
		if got := subjectMatches(c.filter, c.subject); got != c.want {
			t.Errorf("subjectMatches(%q, %q) = %v, want %v", c.filter, c.subject, got, c.want)
		}
	}
}

func TestWildcardDelivery(t *testing.T) {
	m := newConnected(t)

	var star, gt atomic.Int64
	m.Subscribe("logs.*.info", func(*NATSMessage) { star.Add(1) })
	m.Subscribe("logs.>", func(*NATSMessage) { gt.Add(1) })

	m.Publish("logs.app.info", []byte("x"))
	m.Publish("logs.app.error", []byte("y"))
	m.Publish("metrics.cpu", []byte("z"))

	waitFor(t, func() bool { return star.Load() == 1 && gt.Load() == 2 })

	if star.Load() != 1 {
		t.Errorf("* filter received = %d, want 1", star.Load())
	}
	if gt.Load() != 2 {
		t.Errorf("> filter received = %d, want 2", gt.Load())
	}
}

func TestRequestReply(t *testing.T) {
	m := newConnected(t)

	// Responder: echoes the request data back to the reply subject.
	if err := m.Subscribe("echo", func(msg *NATSMessage) {
		if msg.Reply != "" {
			_ = m.Publish(msg.Reply, msg.Data)
		}
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	data, err := m.Request("echo", []byte("ping"), time.Second)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if string(data) != "ping" {
		t.Errorf("reply = %q, want ping", data)
	}
}

func TestRequestTimeout(t *testing.T) {
	m := newConnected(t)
	// No responder registered -> the request times out.
	_, err := m.Request("nobody.home", []byte("x"), 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRequestErrors(t *testing.T) {
	m := newConnected(t)
	if _, err := m.Request("", []byte("x"), time.Second); err == nil {
		t.Error("expected error for empty subject")
	}
	if _, err := m.Request("s", []byte("x"), 0); err == nil {
		t.Error("expected error for non-positive timeout")
	}
}

func TestUnsubscribe(t *testing.T) {
	m := newConnected(t)
	var got atomic.Int64
	m.Subscribe("a", func(*NATSMessage) { got.Add(1) })

	m.Publish("a", []byte("1"))
	waitFor(t, func() bool { return got.Load() == 1 })

	if err := m.Unsubscribe("a"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	// After unsubscribe, publishes are not delivered.
	m.Publish("a", []byte("2"))
	time.Sleep(20 * time.Millisecond)
	if got.Load() != 1 {
		t.Errorf("received after unsubscribe = %d, want 1", got.Load())
	}
	// Unsubscribing an unknown subject is not an error.
	if err := m.Unsubscribe("nope"); err != nil {
		t.Errorf("Unsubscribe unknown: %v", err)
	}
}

func TestErrors(t *testing.T) {
	m := newConnected(t)

	if err := m.Publish("", []byte("x")); err == nil {
		t.Error("expected error for empty subject")
	}
	if err := m.Subscribe("", func(*NATSMessage) {}); err == nil {
		t.Error("expected error for empty filter")
	}
	if err := m.Subscribe("a", nil); err == nil {
		t.Error("expected error for nil handler")
	}

	// Not connected -> errors.
	m2 := New()
	m2.Init(nil)
	if err := m2.Publish("a", []byte("x")); err == nil {
		t.Error("expected Publish error when not connected")
	}
	if err := m2.Subscribe("a", func(*NATSMessage) {}); err == nil {
		t.Error("expected Subscribe error when not connected")
	}
	if err := m2.Unsubscribe("a"); err == nil {
		t.Error("expected Unsubscribe error when not connected")
	}
	if _, err := m2.Request("a", []byte("x"), time.Second); err == nil {
		t.Error("expected Request error when not connected")
	}
}

func TestClose(t *testing.T) {
	m := newConnected(t)
	m.Subscribe("a", func(*NATSMessage) {})

	m.Close()
	if m.IsConnected() {
		t.Error("expected disconnected after Close")
	}
	if err := m.Publish("a", []byte("x")); err == nil {
		t.Error("expected Publish error after close")
	}
	if err := m.Subscribe("a", func(*NATSMessage) {}); err == nil {
		t.Error("expected Subscribe error after close")
	}
	if err := m.Unsubscribe("a"); err == nil {
		t.Error("expected Unsubscribe error after close")
	}
	if len(m.SubscribedSubjects()) != 0 {
		t.Errorf("subjects after close = %v", m.SubscribedSubjects())
	}
	// Close is idempotent.
	m.Close()
}

func TestConcurrentAccess(t *testing.T) {
	m := newConnected(t)
	var received atomic.Int64
	m.Subscribe("c.>", func(*NATSMessage) { received.Add(1) })

	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Publish("c.x", []byte("p"))
		}()
	}
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.SubscribedSubjects()
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
}

func TestDefaultNATSAndInit(t *testing.T) {
	if DefaultNATS() == nil {
		t.Fatal("DefaultNATS() nil")
	}
	Init()
	d1 := DefaultNATS()
	d2 := DefaultNATS()
	if d1 != d2 {
		t.Fatal("DefaultNATS returned different instances")
	}
	if err := d1.Init(&NATSConfig{URL: "nats://127.0.0.1:4222"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !d2.IsConnected() {
		t.Error("expected default to share state")
	}
	Init()
	if DefaultNATS().IsConnected() {
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
