// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - mqtt module tests.
 *
 * These tests exercise the in-memory pub/sub simulation (including
 * wildcard matching and retained messages); no real MQTT broker is
 * required.
 */

package mqtt

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newConnected(t *testing.T) *MQTTModule {
	t.Helper()
	m := New()
	if err := m.Init(&MQTTConfig{
		Broker:   "tcp://127.0.0.1:1883",
		ClientID: "c1",
		QoS:      1,
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
	if err := m.Init(&MQTTConfig{Broker: "tcp://127.0.0.1:1883"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsConnected() {
		t.Fatal("expected connected after Init with broker")
	}
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init(nil): %v", err)
	}
	if m.IsConnected() {
		t.Fatal("expected disconnected after nil config")
	}
	if err := m.Init(&MQTTConfig{}); err != nil {
		t.Fatalf("Init(empty): %v", err)
	}
	if m.IsConnected() {
		t.Fatal("expected disconnected for empty broker")
	}
}

func TestPublishSubscribe(t *testing.T) {
	m := newConnected(t)

	var got []*MQTTMessage
	var mu sync.Mutex
	if err := m.Subscribe("sensors/temp", func(msg *MQTTMessage) {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := m.Publish("sensors/temp", []byte("23.5"), 1, false); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})

	if string(got[0].Payload) != "23.5" {
		t.Errorf("payload = %q", got[0].Payload)
	}
	if got[0].Topic != "sensors/temp" {
		t.Errorf("topic = %q", got[0].Topic)
	}
	if got[0].QoS != 1 {
		t.Errorf("qos = %d, want 1", got[0].QoS)
	}

	topics := m.SubscribedTopics()
	if len(topics) != 1 || topics[0] != "sensors/temp" {
		t.Errorf("subscribed topics = %v", topics)
	}
}

func TestWildcardMatching(t *testing.T) {
	cases := []struct {
		filter string
		topic  string
		want   bool
	}{
		{"a/+/c", "a/b/c", true},
		{"a/+/c", "a/b/d", false},
		{"a/#", "a/b/c", true},
		{"a/#", "a", true},
		{"#", "anything/here", true},
		{"a/b/c", "a/b/c", true},
		{"a/b/c", "a/b/d", false},
		{"+/b/c", "a/b/c", true},
		{"a/+", "a/b", true},
		{"a/+", "a/b/c", false},
		{"a/b/#", "a/b", true},
		{"a/b/#", "a/b/c/d", true},
		{"a/b/#", "a/c/b", false},
	}
	for _, c := range cases {
		if got := topicMatches(c.filter, c.topic); got != c.want {
			t.Errorf("topicMatches(%q, %q) = %v, want %v", c.filter, c.topic, got, c.want)
		}
	}
}

func TestWildcardDelivery(t *testing.T) {
	m := newConnected(t)

	var plus, hash atomic.Int64
	m.Subscribe("home/+/temp", func(*MQTTMessage) { plus.Add(1) })
	m.Subscribe("home/#", func(*MQTTMessage) { hash.Add(1) })

	m.Publish("home/kitchen/temp", []byte("x"), 0, false)
	m.Publish("home/living/light", []byte("y"), 0, false)
	m.Publish("office/temp", []byte("z"), 0, false)

	waitFor(t, func() bool { return plus.Load() == 1 && hash.Load() == 2 })

	if plus.Load() != 1 {
		t.Errorf("+ filter received = %d, want 1", plus.Load())
	}
	if hash.Load() != 2 {
		t.Errorf("# filter received = %d, want 2", hash.Load())
	}
}

func TestRetainedMessages(t *testing.T) {
	m := newConnected(t)

	// Publish a retained message before any subscriber.
	if err := m.Publish("cfg/version", []byte("1.2.3"), 1, true); err != nil {
		t.Fatalf("Publish retained: %v", err)
	}

	var got atomic.Int64
	if err := m.Subscribe("cfg/version", func(msg *MQTTMessage) {
		if string(msg.Payload) == "1.2.3" && msg.Retained {
			got.Add(1)
		}
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	waitFor(t, func() bool { return got.Load() == 1 })

	// A wildcard subscriber also receives the retained message.
	var got2 atomic.Int64
	m.Subscribe("cfg/+", func(msg *MQTTMessage) {
		if string(msg.Payload) == "1.2.3" {
			got2.Add(1)
		}
	})
	waitFor(t, func() bool { return got2.Load() == 1 })

	// A zero-length retained payload clears the retained message.
	m.Publish("cfg/version", []byte{}, 1, true)
	m.Subscribe("cfg/version", func(*MQTTMessage) {})
	// No new retained delivery should occur; the cleared retained message
	// is simply gone. Re-publish a fresh retained value to confirm.
	m.Publish("cfg/version", []byte("2.0.0"), 1, true)
	var got3 atomic.Int64
	m.Subscribe("cfg/version", func(msg *MQTTMessage) {
		if string(msg.Payload) == "2.0.0" {
			got3.Add(1)
		}
	})
	waitFor(t, func() bool { return got3.Load() == 1 })
}

func TestUnsubscribe(t *testing.T) {
	m := newConnected(t)
	m.Subscribe("a", func(*MQTTMessage) {})
	m.Subscribe("b", func(*MQTTMessage) {})
	if len(m.SubscribedTopics()) != 2 {
		t.Fatalf("subscribed = %d, want 2", len(m.SubscribedTopics()))
	}
	if err := m.Unsubscribe("a"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	topics := m.SubscribedTopics()
	if len(topics) != 1 || topics[0] != "b" {
		t.Errorf("after unsubscribe topics = %v", topics)
	}
	// Unsubscribing an unknown topic is not an error.
	if err := m.Unsubscribe("nope"); err != nil {
		t.Errorf("Unsubscribe unknown: %v", err)
	}
}

func TestErrors(t *testing.T) {
	m := newConnected(t)

	if err := m.Publish("", []byte("x"), 0, false); err == nil {
		t.Error("expected error for empty topic")
	}
	if err := m.Subscribe("", func(*MQTTMessage) {}); err == nil {
		t.Error("expected error for empty filter")
	}
	if err := m.Subscribe("a", nil); err == nil {
		t.Error("expected error for nil handler")
	}

	// Not connected -> errors.
	m2 := New()
	m2.Init(nil)
	if err := m2.Publish("a", []byte("x"), 0, false); err == nil {
		t.Error("expected Publish error when not connected")
	}
	if err := m2.Subscribe("a", func(*MQTTMessage) {}); err == nil {
		t.Error("expected Subscribe error when not connected")
	}
	if err := m2.Unsubscribe("a"); err == nil {
		t.Error("expected Unsubscribe error when not connected")
	}
}

func TestClose(t *testing.T) {
	m := newConnected(t)
	m.Subscribe("a", func(*MQTTMessage) {})
	m.Publish("a", []byte("ret"), 0, true)

	m.Close()
	if m.IsConnected() {
		t.Error("expected disconnected after Close")
	}
	if err := m.Publish("a", []byte("x"), 0, false); err == nil {
		t.Error("expected Publish error after close")
	}
	if err := m.Subscribe("a", func(*MQTTMessage) {}); err == nil {
		t.Error("expected Subscribe error after close")
	}
	if err := m.Unsubscribe("a"); err == nil {
		t.Error("expected Unsubscribe error after close")
	}
	if len(m.SubscribedTopics()) != 0 {
		t.Errorf("subscribed topics after close = %v", m.SubscribedTopics())
	}
	// Close is idempotent.
	m.Close()
}

func TestConcurrentAccess(t *testing.T) {
	m := newConnected(t)
	var received atomic.Int64
	m.Subscribe("c/#", func(*MQTTMessage) { received.Add(1) })

	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Publish("c/x", []byte("p"), 0, false)
		}()
	}
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.SubscribedTopics()
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

func TestDefaultMQTTAndInit(t *testing.T) {
	if DefaultMQTT() == nil {
		t.Fatal("DefaultMQTT() nil")
	}
	Init()
	d1 := DefaultMQTT()
	d2 := DefaultMQTT()
	if d1 != d2 {
		t.Fatal("DefaultMQTT returned different instances")
	}
	if err := d1.Init(&MQTTConfig{Broker: "tcp://127.0.0.1:1883"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !d2.IsConnected() {
		t.Error("expected default to share state")
	}
	Init()
	if DefaultMQTT().IsConnected() {
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
