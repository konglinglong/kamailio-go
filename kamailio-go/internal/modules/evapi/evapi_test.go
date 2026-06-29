// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the evapi module.
 */

package evapi

import (
	"sync"
	"testing"
)

func TestInitAndIsRunning(t *testing.T) {
	m := New()
	if m.IsRunning() {
		t.Errorf("IsRunning() = true before Init")
	}
	m.Init("127.0.0.1:8488")
	if !m.IsRunning() {
		t.Errorf("IsRunning() = false after Init")
	}
}

func TestDispatch(t *testing.T) {
	m := New()
	m.Init("127.0.0.1:8488")

	var mu sync.Mutex
	got := make(map[string]string)
	m.Subscribe(func(event string, data []byte) {
		mu.Lock()
		defer mu.Unlock()
		got[event] = string(data)
	})
	m.Subscribe(func(event string, data []byte) {
		mu.Lock()
		defer mu.Unlock()
		got[event+"-2"] = string(data) + "!"
	})

	if err := m.Dispatch("evt", []byte("payload")); err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if got["evt"] != "payload" {
		t.Errorf("subscriber 1 got %q, want %q", got["evt"], "payload")
	}
	if got["evt-2"] != "payload!" {
		t.Errorf("subscriber 2 got %q, want %q", got["evt-2"], "payload!")
	}
}

func TestDispatchNotRunning(t *testing.T) {
	m := New()
	if err := m.Dispatch("evt", []byte("d")); err == nil {
		t.Errorf("Dispatch when not running should error")
	}
}

func TestSubscribeNil(t *testing.T) {
	m := New()
	m.Subscribe(nil)
	if got := m.SubscriberCount(); got != 0 {
		t.Errorf("SubscriberCount() = %d, want 0", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init("pkg:8488")
	d := DefaultEVAPI()
	if d == nil {
		t.Fatalf("DefaultEVAPI() returned nil")
	}
	if !IsRunning() {
		t.Errorf("package IsRunning() = false")
	}
	called := false
	Subscribe(func(event string, data []byte) { called = true })
	if err := Dispatch("e", []byte("d")); err != nil {
		t.Fatalf("package Dispatch error: %v", err)
	}
	if !called {
		t.Errorf("subscriber not called via package Dispatch")
	}
}

func TestConcurrent(t *testing.T) {
	Init("c:8488")
	shared := DefaultEVAPI()
	var mu sync.Mutex
	count := 0
	shared.Subscribe(func(event string, data []byte) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			shared.Dispatch("e", []byte(itoa(i)))
		}(i)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if count != n {
		t.Errorf("dispatch count = %d, want %d", count, n)
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
