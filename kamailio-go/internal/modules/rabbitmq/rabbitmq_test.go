// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - rabbitmq module tests.
 *
 * These tests exercise the in-memory exchange/queue/binding simulation;
 * no real RabbitMQ broker is required.
 */

package rabbitmq

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newConnected(t *testing.T) *RabbitMQModule {
	t.Helper()
	m := New()
	if err := m.Init(&RabbitMQConfig{
		URL:        "amqp://guest:guest@127.0.0.1:5672/",
		Exchange:   "ex",
		Queue:      "q",
		RoutingKey: "rk",
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
	if err := m.Init(&RabbitMQConfig{URL: "amqp://x"}); err != nil {
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
	if err := m.Init(&RabbitMQConfig{}); err != nil {
		t.Fatalf("Init(empty): %v", err)
	}
	if m.IsConnected() {
		t.Fatal("expected disconnected for empty URL")
	}
}

func TestDeclareBindPublishConsume(t *testing.T) {
	m := newConnected(t)

	if err := m.DeclareQueue("orders", true); err != nil {
		t.Fatalf("DeclareQueue: %v", err)
	}
	if err := m.BindQueue("orders", "ex", "rk"); err != nil {
		t.Fatalf("BindQueue: %v", err)
	}

	var got []*RabbitMQMessage
	var mu sync.Mutex
	if err := m.Consume("orders", func(msg *RabbitMQMessage) {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	if err := m.Publish(&RabbitMQMessage{
		Exchange:   "ex",
		RoutingKey: "rk",
		Body:       []byte("hello"),
		Headers:    map[string]interface{}{"k": "v"},
	}); err != nil {
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
	if got[0].Headers["k"] != "v" {
		t.Errorf("headers not preserved: %v", got[0].Headers)
	}
	if got[0].Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
	if msgs := m.QueueMessages("orders"); len(msgs) != 1 {
		t.Errorf("queued messages = %d, want 1", len(msgs))
	}
}

func TestRoutingMultipleQueues(t *testing.T) {
	m := newConnected(t)
	m.DeclareQueue("q1", false)
	m.DeclareQueue("q2", false)
	m.BindQueue("q1", "ex", "rk")
	m.BindQueue("q2", "ex", "rk")

	var c1, c2 atomic.Int64
	m.Consume("q1", func(*RabbitMQMessage) { c1.Add(1) })
	m.Consume("q2", func(*RabbitMQMessage) { c2.Add(1) })

	if err := m.Publish(&RabbitMQMessage{Exchange: "ex", RoutingKey: "rk", Body: []byte("x")}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	waitFor(t, func() bool { return c1.Load() == 1 && c2.Load() == 1 })

	// A non-matching routing key delivers to neither queue.
	if err := m.Publish(&RabbitMQMessage{Exchange: "ex", RoutingKey: "other", Body: []byte("y")}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if c1.Load() != 1 || c2.Load() != 1 {
		t.Errorf("non-matching routing key should not deliver: c1=%d c2=%d", c1.Load(), c2.Load())
	}
	if bs := m.Bindings(); len(bs) != 2 {
		t.Errorf("bindings = %d, want 2", len(bs))
	}
}

func TestPublishAsync(t *testing.T) {
	m := newConnected(t)
	m.DeclareQueue("q", true)
	m.BindQueue("q", "ex", "rk")

	var got atomic.Int64
	var cb atomic.Int64
	m.Consume("q", func(*RabbitMQMessage) { got.Add(1) })

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		m.PublishAsync(&RabbitMQMessage{Exchange: "ex", RoutingKey: "rk", Body: []byte("p")}, func(err error) {
			defer wg.Done()
			if err != nil {
				t.Errorf("callback err: %v", err)
			}
			cb.Add(1)
		})
	}
	wg.Wait()

	if got.Load() != 10 {
		t.Errorf("delivered = %d, want 10", got.Load())
	}
	if cb.Load() != 10 {
		t.Errorf("callbacks = %d, want 10", cb.Load())
	}
	if msgs := m.QueueMessages("q"); len(msgs) != 10 {
		t.Errorf("queued = %d, want 10", len(msgs))
	}
}

func TestErrors(t *testing.T) {
	m := newConnected(t)

	// Empty queue name.
	if err := m.DeclareQueue("", true); err == nil {
		t.Error("expected error for empty queue name")
	}
	// Bind to undeclared queue.
	if err := m.BindQueue("missing", "ex", "rk"); err == nil {
		t.Error("expected error binding undeclared queue")
	}
	// Consume from undeclared queue.
	if err := m.Consume("missing", func(*RabbitMQMessage) {}); err == nil {
		t.Error("expected error consuming undeclared queue")
	}
	// nil handler.
	m.DeclareQueue("q", false)
	if err := m.Consume("q", nil); err == nil {
		t.Error("expected error for nil handler")
	}
	// nil message.
	if err := m.Publish(nil); err == nil {
		t.Error("expected error for nil message")
	}

	// Not connected -> errors.
	m2 := New()
	m2.Init(nil)
	if err := m2.DeclareQueue("q", false); err == nil {
		t.Error("expected DeclareQueue error when not connected")
	}
	if err := m2.BindQueue("q", "ex", "rk"); err == nil {
		t.Error("expected BindQueue error when not connected")
	}
	if err := m2.Publish(&RabbitMQMessage{Exchange: "ex", RoutingKey: "rk", Body: []byte("x")}); err == nil {
		t.Error("expected Publish error when not connected")
	}
	if err := m2.Consume("q", func(*RabbitMQMessage) {}); err == nil {
		t.Error("expected Consume error when not connected")
	}
}

func TestClose(t *testing.T) {
	m := newConnected(t)
	m.DeclareQueue("q", true)
	m.BindQueue("q", "ex", "rk")
	m.Consume("q", func(*RabbitMQMessage) {})

	// Fire an async publish and close; Close must drain it.
	m.PublishAsync(&RabbitMQMessage{Exchange: "ex", RoutingKey: "rk", Body: []byte("x")}, nil)
	m.Close()

	if m.IsConnected() {
		t.Error("expected disconnected after Close")
	}
	if err := m.Publish(&RabbitMQMessage{Exchange: "ex", RoutingKey: "rk", Body: []byte("x")}); err == nil {
		t.Error("expected Publish error after close")
	}
	if err := m.DeclareQueue("q2", false); err == nil {
		t.Error("expected DeclareQueue error after close")
	}
	// Close is idempotent.
	m.Close()
}

func TestConcurrentAccess(t *testing.T) {
	m := newConnected(t)
	m.DeclareQueue("q", true)
	m.BindQueue("q", "ex", "rk")

	var received atomic.Int64
	m.Consume("q", func(*RabbitMQMessage) { received.Add(1) })

	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Publish(&RabbitMQMessage{Exchange: "ex", RoutingKey: "rk", Body: []byte("x")})
		}()
	}
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.QueueMessages("q")
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
	if msgs := m.QueueMessages("q"); len(msgs) != goroutines {
		t.Errorf("queued = %d, want %d", len(msgs), goroutines)
	}
}

func TestDefaultRabbitMQAndInit(t *testing.T) {
	if DefaultRabbitMQ() == nil {
		t.Fatal("DefaultRabbitMQ() nil")
	}
	Init()
	d1 := DefaultRabbitMQ()
	d2 := DefaultRabbitMQ()
	if d1 != d2 {
		t.Fatal("DefaultRabbitMQ returned different instances")
	}
	if err := d1.Init(&RabbitMQConfig{URL: "amqp://x"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !d2.IsConnected() {
		t.Error("expected default to share state")
	}
	Init()
	if DefaultRabbitMQ().IsConnected() {
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
