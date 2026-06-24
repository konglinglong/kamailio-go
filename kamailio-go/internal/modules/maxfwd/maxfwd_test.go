// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - MaxFwd module tests.
 */

package maxfwd

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// A valid INVITE carrying a Max-Forwards header.
var testInvite = []byte("INVITE sip:user@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776asdhds\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@pc33.example.com>\r\n" +
	"Content-Type: application/sdp\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

// An INVITE without a Max-Forwards header.
var testNoMaxFwd = []byte("INVITE sip:user@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776asdhds\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@pc33.example.com>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func mustParse(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

// TestProcess verifies that Process decrements a present Max-Forwards
// header and signals "continue" (return 0).
func TestProcess(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)

	ret, err := m.Process(msg, 70)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if ret != 0 {
		t.Fatalf("expected return 0 (continue), got %d", ret)
	}
	if got := m.CheckMaxFwd(msg); got != 69 {
		t.Fatalf("expected Max-Forwards 69 after process, got %d", got)
	}
}

// TestProcess_MaxReached verifies that a zero Max-Forwards yields -1.
func TestProcess_MaxReached(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	m.SetMaxFwd(msg, 0)

	ret, err := m.Process(msg, 70)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if ret != -1 {
		t.Fatalf("expected return -1 (max reached), got %d", ret)
	}
}

// TestCheckMaxFwd verifies the value is read correctly.
func TestCheckMaxFwd(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	if got := m.CheckMaxFwd(msg); got != 70 {
		t.Fatalf("expected Max-Forwards 70, got %d", got)
	}
}

// TestDecrementMaxFwd verifies successive decrements.
func TestDecrementMaxFwd(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)

	if got := m.DecrementMaxFwd(msg); got != 69 {
		t.Fatalf("expected 69 after first decrement, got %d", got)
	}
	if got := m.DecrementMaxFwd(msg); got != 68 {
		t.Fatalf("expected 68 after second decrement, got %d", got)
	}
}

// TestDecrementMaxFwd_FloorAtZero verifies the value never goes negative.
func TestDecrementMaxFwd_FloorAtZero(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	m.SetMaxFwd(msg, 0)
	if got := m.DecrementMaxFwd(msg); got != 0 {
		t.Fatalf("expected 0 (floored), got %d", got)
	}
}

// TestSetMaxFwd verifies setting a value and range checking.
func TestSetMaxFwd(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)

	if got := m.SetMaxFwd(msg, 10); got != 10 {
		t.Fatalf("expected 10 after set, got %d", got)
	}
	if v := m.CheckMaxFwd(msg); v != 10 {
		t.Fatalf("expected CheckMaxFwd 10, got %d", v)
	}
	if got := m.SetMaxFwd(msg, 300); got != -1 {
		t.Fatalf("expected -1 for out-of-range value, got %d", got)
	}
	if got := m.SetMaxFwd(msg, -1); got != -1 {
		t.Fatalf("expected -1 for negative value, got %d", got)
	}
}

// TestIsZero verifies the zero detection.
func TestIsZero(t *testing.T) {
	m := New()
	msg := mustParse(t, testInvite)
	if m.IsZero(msg) {
		t.Fatal("expected IsZero false for value 70")
	}
	m.SetMaxFwd(msg, 0)
	if !m.IsZero(msg) {
		t.Fatal("expected IsZero true after setting 0")
	}
}

// TestMissingHeader verifies behaviour when the header is absent.
func TestMissingHeader(t *testing.T) {
	m := New()
	msg := mustParse(t, testNoMaxFwd)

	if got := m.CheckMaxFwd(msg); got != -1 {
		t.Fatalf("expected -1 for missing header, got %d", got)
	}
	ret, err := m.Process(msg, 64)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if ret != 1 {
		t.Fatalf("expected return 1 (header added), got %d", ret)
	}
	if got := m.CheckMaxFwd(msg); got != 64 {
		t.Fatalf("expected Max-Forwards 64 after add, got %d", got)
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	if got := DefaultMaxFwd(); got != DefaultMaxLimit {
		t.Fatalf("expected DefaultMaxFwd %d, got %d", DefaultMaxLimit, got)
	}
	Init()
	msg := mustParse(t, testInvite)
	if got := CheckMaxFwd(msg); got != 70 {
		t.Fatalf("expected global CheckMaxFwd 70, got %d", got)
	}
}
