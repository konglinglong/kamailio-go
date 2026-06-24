// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the janssonrpcc module.
 */

package janssonrpcc

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

func TestCall(t *testing.T) {
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
	if obj["result"] != "ok" {
		t.Errorf("Call().result = %v, want %q", obj["result"], "ok")
	}
	if got := m.CallCount(); got != 1 {
		t.Errorf("CallCount() = %d, want 1", got)
	}
}

func TestCallNotConnected(t *testing.T) {
	m := New()
	if _, err := m.Call("m", nil); err == nil {
		t.Errorf("Call when not connected should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init("pkg:8080")
	d := DefaultJanssonRPCC()
	if d == nil {
		t.Fatalf("DefaultJanssonRPCC() returned nil")
	}
	if !IsConnected() {
		t.Errorf("package IsConnected() = false")
	}
	res, err := Call("pkg", nil)
	if err != nil || res == nil {
		t.Errorf("package Call = %v,%v", res, err)
	}
}

func TestConcurrent(t *testing.T) {
	Init("c:8080")
	shared := DefaultJanssonRPCC()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			shared.Call("m", itoa(i))
			shared.IsConnected()
		}(i)
	}
	wg.Wait()
	if got := shared.CallCount(); got != n {
		t.Errorf("CallCount() = %d, want %d", got, n)
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
