// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the async module.
 */

package async

import (
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

func TestRoute(t *testing.T) {
	m := New()
	called := false
	m.Register("RELAY", func(msg *parser.SIPMsg) int {
		called = true
		return 1
	})

	if rc := m.Route("RELAY", nil); rc != 1 {
		t.Errorf("Route(RELAY) = %d, want 1", rc)
	}
	if !called {
		t.Errorf("Route handler was not invoked")
	}

	// Unknown route returns -1.
	if rc := m.Route("NOPE", nil); rc != -1 {
		t.Errorf("Route(NOPE) = %d, want -1", rc)
	}
}

func TestSleep(t *testing.T) {
	m := New()
	start := time.Now()
	m.Sleep(15)
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Errorf("Sleep(15) elapsed %v, want >= 10ms", elapsed)
	}
	// Non-positive values return immediately.
	m.Sleep(0)
	m.Sleep(-5)
}

func TestIsReady(t *testing.T) {
	m := New()
	if !m.IsReady() {
		t.Errorf("IsReady() = false, want true on new module")
	}
	m.SetReady(false)
	if m.IsReady() {
		t.Errorf("IsReady() = true after SetReady(false)")
	}
	m.SetReady(true)
	if !m.IsReady() {
		t.Errorf("IsReady() = false after SetReady(true)")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultAsync()
	if d == nil {
		t.Fatalf("DefaultAsync() returned nil")
	}
	if d != DefaultAsync() {
		t.Fatalf("DefaultAsync() returned different instances")
	}
	Register("pkg", func(msg *parser.SIPMsg) int { return 42 })
	if rc := Route("pkg", nil); rc != 42 {
		t.Errorf("package Route(pkg) = %d, want 42", rc)
	}
	if !IsReady() {
		t.Errorf("package IsReady() = false")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultAsync()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			shared.Register(itoa(i), func(msg *parser.SIPMsg) int { return i })
			shared.Route(itoa(i), nil)
			shared.IsReady()
		}(i)
	}
	wg.Wait()
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
