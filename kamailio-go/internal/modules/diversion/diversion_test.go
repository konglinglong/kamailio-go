// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Diversion module tests.
 */

package diversion

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

func TestAddGetCount(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	if m.Count(msg) != 0 {
		t.Fatal("expected 0 diversions initially")
	}
	n := m.AddDiversion(msg, "sip:orig1@example.com;reason=unconditional")
	if n != 1 {
		t.Fatalf("AddDiversion returned %d, want 1", n)
	}
	m.AddDiversion(msg, "sip:orig2@example.com;reason=unconditional")
	if m.Count(msg) != 2 {
		t.Fatalf("Count = %d, want 2", m.Count(msg))
	}
	got := m.GetDiversion(msg)
	if len(got) != 2 || got[0] != "sip:orig1@example.com;reason=unconditional" {
		t.Fatalf("GetDiversion = %v", got)
	}
}

func TestRemoveDiversion(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	m.AddDiversion(msg, "sip:orig1@example.com")
	m.AddDiversion(msg, "sip:orig2@example.com")
	n := m.RemoveDiversion(msg)
	if n != 2 {
		t.Fatalf("RemoveDiversion = %d, want 2", n)
	}
	if m.Count(msg) != 0 {
		t.Fatalf("Count after remove = %d, want 0", m.Count(msg))
	}
	// Removing again returns 0.
	if m.RemoveDiversion(msg) != 0 {
		t.Fatal("expected 0 removed on empty")
	}
}

func TestNilMessage(t *testing.T) {
	m := New()
	if m.AddDiversion(nil, "uri") != 0 {
		t.Fatal("expected 0 for nil AddDiversion")
	}
	if m.RemoveDiversion(nil) != 0 {
		t.Fatal("expected 0 for nil RemoveDiversion")
	}
	if m.GetDiversion(nil) != nil {
		t.Fatal("expected nil for nil GetDiversion")
	}
	if m.Count(nil) != 0 {
		t.Fatal("expected 0 for nil Count")
	}
}

func TestGetDiversionEmpty(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	if got := m.GetDiversion(msg); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	msg := mustParse(t, testInvite)
	if AddDiversion(msg, "sip:g@example.com") != 1 {
		t.Fatal("expected global AddDiversion 1")
	}
	if Count(msg) != 1 {
		t.Fatalf("global Count = %d, want 1", Count(msg))
	}
	if len(GetDiversion(msg)) != 1 {
		t.Fatal("expected global GetDiversion len 1")
	}
	if RemoveDiversion(msg) != 1 {
		t.Fatal("expected global RemoveDiversion 1")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.AddDiversion(msg, "sip:orig@example.com")
			_ = m.Count(msg)
			_ = m.GetDiversion(msg)
		}()
	}
	wg.Wait()
	if m.Count(msg) != 20 {
		t.Fatalf("Count = %d, want 20", m.Count(msg))
	}
}
