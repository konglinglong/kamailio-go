// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - kafka module tests.
 *
 * These tests exercise the in-memory broker simulation; no real Kafka
 * cluster is required.
 */

package kafka

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newConnected(t *testing.T) *KafkaModule {
	t.Helper()
	m := New()
	if err := m.Init(&KafkaConfig{
		Brokers: []string{"127.0.0.1:9092"},
		Topic:   "test",
		GroupID: "g1",
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
	if err := m.Init(&KafkaConfig{Brokers: []string{"127.0.0.1:9092"}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsConnected() {
		t.Fatal("expected connected after Init with brokers")
	}
	// Nil config resets to disconnected.
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init(nil): %v", err)
	}
	if m.IsConnected() {
		t.Fatal("expected disconnected after nil config")
	}
	// Empty brokers leaves the module disconnected.
	if err := m.Init(&KafkaConfig{}); err != nil {
		t.Fatalf("Init(empty): %v", err)
	}
	if m.IsConnected() {
		t.Fatal("expected disconnected for empty brokers")
	}
}

func TestProduceAndConsume(t *testing.T) {
	m := newConnected(t)

	var got []*KafkaMessage
	var mu sync.Mutex
	if err := m.Consume("orders", func(msg *KafkaMessage) {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	if err := m.Produce(&KafkaMessage{
		Topic:   "orders",
		Key:     "k1",
		Value:   []byte("hello"),
		Headers: map[string]string{"h": "v"},
	}); err != nil {
		t.Fatalf("Produce: %v", err)
	}

	// Messages produced before a consumer registered are still logged.
	if err := m.Produce(&KafkaMessage{Topic: "orders", Value: []byte("second")}); err != nil {
		t.Fatalf("Produce: %v", err)
	}

	// The consumer should have received both messages.
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 2
	})

	if string(got[0].Value) != "hello" || got[0].Key != "k1" {
		t.Errorf("first message = %+v", got[0])
	}
	if got[0].Headers["h"] != "v" {
		t.Errorf("headers not preserved: %v", got[0].Headers)
	}
	if got[0].Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}

	// The topic log should hold both messages.
	if msgs := m.Messages("orders"); len(msgs) != 2 {
		t.Errorf("logged messages = %d, want 2", len(msgs))
	}
}

func TestProduceErrors(t *testing.T) {
	m := newConnected(t)

	if err := m.Produce(nil); err == nil {
		t.Error("expected error for nil message")
	}
	if err := m.Produce(&KafkaMessage{Topic: "", Value: []byte("x")}); err == nil {
		t.Error("expected error for empty topic")
	}

	// Not connected -> error.
	m2 := New()
	m2.Init(nil)
	if err := m2.Produce(&KafkaMessage{Topic: "t", Value: []byte("x")}); err == nil {
		t.Error("expected error when not connected")
	}
	if err := m2.Consume("t", func(*KafkaMessage) {}); err == nil {
		t.Error("expected Consume error when not connected")
	}
	// nil handler -> error.
	if err := m.Consume("t", nil); err == nil {
		t.Error("expected error for nil handler")
	}
}

func TestProduceAsync(t *testing.T) {
	m := newConnected(t)

	var got atomic.Int64
	var cb atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		m.ProduceAsync(&KafkaMessage{Topic: "async", Value: []byte("p")}, func(err error) {
			defer wg.Done()
			if err != nil {
				t.Errorf("callback err: %v", err)
			}
			cb.Add(1)
			got.Add(1)
		})
	}

	// While in flight, PendingCount should be >= 0 and eventually 0.
	wg.Wait()
	if got.Load() != 10 {
		t.Errorf("callbacks fired = %d, want 10", got.Load())
	}
	if cb.Load() != 10 {
		t.Errorf("callback count = %d, want 10", cb.Load())
	}
	if m.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0 after drain", m.PendingCount())
	}
	if msgs := m.Messages("async"); len(msgs) != 10 {
		t.Errorf("logged messages = %d, want 10", len(msgs))
	}
}

func TestClose(t *testing.T) {
	m := newConnected(t)
	m.Consume("t", func(*KafkaMessage) {})

	// Fire an async produce and close; Close must drain it.
	m.ProduceAsync(&KafkaMessage{Topic: "t", Value: []byte("x")}, nil)
	m.Close()

	if m.IsConnected() {
		t.Error("expected disconnected after Close")
	}
	// Produce after close fails.
	if err := m.Produce(&KafkaMessage{Topic: "t", Value: []byte("x")}); err == nil {
		t.Error("expected Produce error after close")
	}
	if err := m.Consume("t", func(*KafkaMessage) {}); err == nil {
		t.Error("expected Consume error after close")
	}
	// Close is idempotent.
	m.Close()
}

func TestConcurrentAccess(t *testing.T) {
	m := newConnected(t)
	var received atomic.Int64
	m.Consume("c", func(*KafkaMessage) {
		received.Add(1)
	})

	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Produce(&KafkaMessage{Topic: "c", Value: []byte("x")})
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

func TestDefaultKafkaAndInit(t *testing.T) {
	if DefaultKafka() == nil {
		t.Fatal("DefaultKafka() nil")
	}
	Init()
	d1 := DefaultKafka()
	d2 := DefaultKafka()
	if d1 != d2 {
		t.Fatal("DefaultKafka returned different instances")
	}
	if err := d1.Init(&KafkaConfig{Brokers: []string{"127.0.0.1:9092"}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !d2.IsConnected() {
		t.Error("expected default to share state")
	}
	Init()
	if DefaultKafka().IsConnected() {
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
