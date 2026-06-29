// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the smsops module - SMS body parsing and number handling.
 */
package smsops

import (
	"strings"
	"sync"
	"testing"
)

func TestParseBody(t *testing.T) {
	m := New()
	body := "From: alice\r\nTo: bob\r\nCoding: 0\r\n\r\nHello there"
	msg, err := m.ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody: %v", err)
	}
	if msg.From != "alice" {
		t.Errorf("From = %q", msg.From)
	}
	if msg.To != "bob" {
		t.Errorf("To = %q", msg.To)
	}
	if msg.Coding != 0 {
		t.Errorf("Coding = %d", msg.Coding)
	}
	if msg.Body != "Hello there" {
		t.Errorf("Body = %q", msg.Body)
	}
}

func TestBuildBodyRoundTrip(t *testing.T) {
	m := New()
	orig := &SMSMessage{From: "alice", To: "bob", Body: "Hi", Coding: 0}
	body := m.BuildBody(orig)
	if !strings.Contains(body, "From: alice") {
		t.Errorf("body missing From: %s", body)
	}
	if !strings.Contains(body, "To: bob") {
		t.Errorf("body missing To: %s", body)
	}
	if !strings.Contains(body, "Hi") {
		t.Errorf("body missing text: %s", body)
	}
	// Round-trip.
	msg, err := m.ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody: %v", err)
	}
	if msg.From != orig.From || msg.To != orig.To || msg.Body != orig.Body {
		t.Errorf("round-trip = %+v", msg)
	}
}

func TestExtractText(t *testing.T) {
	m := New()
	body := "From: a\r\n\r\nthe text"
	if got := m.ExtractText(body); got != "the text" {
		t.Errorf("ExtractText = %q", got)
	}
	if got := m.ExtractText(""); got != "" {
		t.Errorf("ExtractText(empty) = %q", got)
	}
}

func TestValidateNumber(t *testing.T) {
	m := New()
	valid := []string{"+12025551234", "12025551234", "+447911123456"}
	invalid := []string{"", "abc", "12", "+", strings.Repeat("1", 20)}
	for _, n := range valid {
		if !m.ValidateNumber(n) {
			t.Errorf("ValidateNumber(%q) = false, want true", n)
		}
	}
	for _, n := range invalid {
		if m.ValidateNumber(n) {
			t.Errorf("ValidateNumber(%q) = true, want false", n)
		}
	}
}

func TestNormalizeNumber(t *testing.T) {
	m := New()
	if got := m.NormalizeNumber("2025551234", "1"); got != "+12025551234" {
		t.Errorf("NormalizeNumber = %q", got)
	}
	if got := m.NormalizeNumber("+12025551234", "1"); got != "+12025551234" {
		t.Errorf("NormalizeNumber intl = %q", got)
	}
	if got := m.NormalizeNumber("(202) 555-1234", "1"); got != "+12025551234" {
		t.Errorf("NormalizeNumber formatted = %q", got)
	}
	if got := m.NormalizeNumber("2025551234", ""); got != "2025551234" {
		t.Errorf("NormalizeNumber no cc = %q", got)
	}
}

func TestParseBodyEmpty(t *testing.T) {
	m := New()
	if _, err := m.ParseBody(""); err == nil {
		t.Error("ParseBody(empty) should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultSMSOps()
	d2 := DefaultSMSOps()
	if d1 != d2 {
		t.Error("DefaultSMSOps should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	body := "From: a\r\nTo: b\r\n\r\nhi"
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.ParseBody(body)
			_ = m.BuildBody(&SMSMessage{From: "a", Body: "x"})
			_ = m.ExtractText(body)
			_ = m.ValidateNumber("+12025551234")
			_ = m.NormalizeNumber("2025551234", "1")
		}()
	}
	wg.Wait()
}
