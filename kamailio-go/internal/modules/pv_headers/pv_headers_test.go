// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - PVHeaders module tests.
 */

package pv_headers

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var testInvite = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Content-Length: 0\r\n\r\n")

func mustParse(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

func TestSetGetRemoveCount(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	m.Set(msg, "X-Custom", "value1")
	m.Set(msg, "X-Other", "value2")
	if got := m.Count(msg); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}
	if got := m.Get(msg, "X-Custom"); got != "value1" {
		t.Fatalf("Get = %q, want value1", got)
	}
	if !m.Remove(msg, "X-Custom") {
		t.Fatal("expected Remove true")
	}
	if got := m.Count(msg); got != 1 {
		t.Fatalf("Count = %d, want 1", got)
	}
	if got := m.Get(msg, "X-Custom"); got != "" {
		t.Fatalf("Get after remove = %q, want empty", got)
	}
}

func TestGetRemoveAbsent(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	if got := m.Get(msg, "missing"); got != "" {
		t.Fatalf("Get(missing) = %q, want empty", got)
	}
	if m.Remove(msg, "missing") {
		t.Fatal("expected Remove false for missing")
	}
	if m.Remove(msg, "") {
		t.Fatal("expected Remove false for empty name")
	}
}

func TestNilMessage(t *testing.T) {
	m := New()
	m.Set(nil, "k", "v")
	if got := m.Get(nil, "k"); got != "" {
		t.Fatalf("Get(nil) = %q, want empty", got)
	}
	if m.Remove(nil, "k") {
		t.Fatal("expected Remove(nil) false")
	}
	if m.Count(nil) != 0 {
		t.Fatalf("Count(nil) = %d, want 0", m.Count(nil))
	}
}

func TestPurge(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	m.Set(msg, "a", "1")
	m.Set(msg, "b", "2")
	m.Purge(msg)
	if got := m.Count(msg); got != 0 {
		t.Fatalf("Count after Purge = %d, want 0", got)
	}
}

func TestPerMessageIsolation(t *testing.T) {
	m := New()
	msg1 := mustParse(t, testInvite)
	msg2 := mustParse(t, testInvite)
	m.Set(msg1, "k", "v1")
	m.Set(msg2, "k", "v2")
	if got := m.Get(msg1, "k"); got != "v1" {
		t.Fatalf("msg1 Get = %q, want v1", got)
	}
	if got := m.Get(msg2, "k"); got != "v2" {
		t.Fatalf("msg2 Get = %q, want v2", got)
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	msg := mustParse(t, testInvite)
	Set(msg, "gk", "gv")
	if got := Get(msg, "gk"); got != "gv" {
		t.Fatalf("global Get = %q, want gv", got)
	}
	if Count(msg) != 1 {
		t.Fatalf("global Count = %d, want 1", Count(msg))
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Set(msg, "k", "v")
			_ = m.Get(msg, "k")
			_ = m.Count(msg)
		}(i)
	}
	wg.Wait()
}
