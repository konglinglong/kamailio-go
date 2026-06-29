// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the jsonrpcc module.
 */

package jsonrpcc

import (
	"sync"
	"testing"
)

func TestInitAndIsConnected(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Errorf("IsConnected() = true before Init")
	}
	m.Init("127.0.0.1:8080")
	if !m.IsConnected() {
		t.Errorf("IsConnected() = false after Init")
	}
}

func TestCallAndNotify(t *testing.T) {
	m := New()
	m.Init("127.0.0.1:8080")

	res, err := m.Call("sum", []int{1, 2})
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	obj, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("Call result is not an object")
	}
	if obj["method"] != "sum" {
		t.Errorf("Call().method = %v, want %q", obj["method"], "sum")
	}
	if obj["jsonrpc"] != "2.0" {
		t.Errorf("Call().jsonrpc = %v, want %q", obj["jsonrpc"], "2.0")
	}

	if err := m.Notify("update", "data"); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if err := m.Notify("delete", 42); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	notes := m.Notifications()
	if len(notes) != 2 {
		t.Fatalf("Notifications() = %d, want 2", len(notes))
	}
	if notes[0].Method != "update" || notes[1].Method != "delete" {
		t.Errorf("Notifications = %v %v", notes[0].Method, notes[1].Method)
	}
}

func TestNotConnectedErrors(t *testing.T) {
	m := New()
	if _, err := m.Call("m", nil); err == nil {
		t.Errorf("Call when not connected should error")
	}
	if err := m.Notify("m", nil); err == nil {
		t.Errorf("Notify when not connected should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init("pkg:8080")
	d := DefaultJSONRPCC()
	if d == nil {
		t.Fatalf("DefaultJSONRPCC() returned nil")
	}
	res, err := Call("pkg", nil)
	if err != nil || res == nil {
		t.Errorf("package Call = %v,%v", res, err)
	}
	if err := Notify("n", nil); err != nil {
		t.Fatalf("package Notify error: %v", err)
	}
}

func TestConcurrent(t *testing.T) {
	Init("c:8080")
	shared := DefaultJSONRPCC()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			shared.Call("m", itoa(i))
			shared.Notify("n", itoa(i))
		}(i)
	}
	wg.Wait()
	if got := shared.CallCount(); got != n {
		t.Errorf("CallCount() = %d, want %d", got, n)
	}
	if got := len(shared.Notifications()); got != n {
		t.Errorf("Notifications() = %d, want %d", got, n)
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
