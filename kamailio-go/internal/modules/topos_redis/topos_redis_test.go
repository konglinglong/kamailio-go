// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the ToposRedis module.
 */

package topos_redis

import (
	"bytes"
	"sync"
	"testing"
)

func TestNotConnectedByDefault(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("IsConnected() = true before Init")
	}
	if err := m.Store("c", "t", []byte("x")); err == nil {
		t.Errorf("Store() before Init should error")
	}
	if _, err := m.Retrieve("c", "t"); err == nil {
		t.Errorf("Retrieve() before Init should error")
	}
	if m.Delete("c") {
		t.Errorf("Delete() before Init should return false")
	}
}

func TestStoreRetrieveDelete(t *testing.T) {
	m := New()
	m.Init("127.0.0.1:6379")
	if !m.IsConnected() {
		t.Fatal("IsConnected() = false after Init")
	}
	data := []byte("dialog-state")
	if err := m.Store("call-1", "ftag-1", data); err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	got, err := m.Retrieve("call-1", "ftag-1")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Retrieve() = %q, want %q", got, data)
	}
	// Mutating the returned slice must not affect the stored record.
	got[0] = 'X'
	got2, _ := m.Retrieve("call-1", "ftag-1")
	if got2[0] != 'd' {
		t.Errorf("stored record was mutated by caller")
	}
	// Unknown -> error.
	if _, err := m.Retrieve("nope", "nope"); err == nil {
		t.Errorf("Retrieve(unknown) should error")
	}
	// Empty call-id -> error.
	if err := m.Store("", "t", data); err == nil {
		t.Errorf("Store(empty call-id) should error")
	}
	// Delete by call-id removes all matching records.
	m.Store("call-1", "t2", []byte("a2"))
	if !m.Delete("call-1") {
		t.Errorf("Delete(call-1) = false, want true")
	}
	if _, err := m.Retrieve("call-1", "ftag-1"); err == nil {
		t.Errorf("Retrieve() after delete should error")
	}
	if m.Delete("call-1") {
		t.Errorf("Delete(call-1) twice = true, want false")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	m.Init("127.0.0.1:6379")
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			callID := "c" + itoa(i)
			_ = m.Store(callID, "t", []byte("data"))
			_, _ = m.Retrieve(callID, "t")
			m.IsConnected()
			m.Delete(callID)
		}(i)
	}
	wg.Wait()
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultToposRedis()
	if d == nil {
		t.Fatal("DefaultToposRedis() = nil")
	}
	if d != DefaultToposRedis() {
		t.Fatal("DefaultToposRedis() returned different instances")
	}
	// Default starts disconnected.
	if d.IsConnected() {
		t.Errorf("default should start disconnected")
	}
	d.Init("127.0.0.1:6379")
	if !d.IsConnected() {
		t.Errorf("default should be connected after Init(addr)")
	}
	// Package Init() resets to disconnected.
	Init()
	if DefaultToposRedis().IsConnected() {
		t.Errorf("Init() should reset to disconnected")
	}
}

// itoa is a tiny local int->string helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
