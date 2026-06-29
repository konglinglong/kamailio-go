// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the kemix module.
 */

package kemix

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

const invite = "INVITE sip:alice@example.com:5060;transport=tcp SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@atlanta.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"X-Custom: hello-world\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

func parseRaw(t *testing.T, raw string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

func TestGetURI(t *testing.T) {
	m := New()
	msg := parseRaw(t, invite)

	if got := m.GetURI(msg, "user"); got != "alice" {
		t.Errorf("GetURI(user) = %q, want %q", got, "alice")
	}
	if got := m.GetURI(msg, "host"); got != "example.com" {
		t.Errorf("GetURI(host) = %q, want %q", got, "example.com")
	}
	if got := m.GetURI(msg, "port"); got != "5060" {
		t.Errorf("GetURI(port) = %q, want %q", got, "5060")
	}
	if got := m.GetURI(msg, "ruri"); got != "sip:alice@example.com:5060;transport=tcp" {
		t.Errorf("GetURI(ruri) = %q, want %q", got, "sip:alice@example.com:5060;transport=tcp")
	}
}

func TestSetURI(t *testing.T) {
	m := New()
	msg := parseRaw(t, invite)

	m.SetURI(msg, "user", "bob")
	if got := m.GetURI(msg, "user"); got != "bob" {
		t.Errorf("GetURI(user) after SetURI = %q, want %q", got, "bob")
	}

	m.SetURI(msg, "host", "newhost.com")
	if got := m.GetURI(msg, "host"); got != "newhost.com" {
		t.Errorf("GetURI(host) after SetURI = %q, want %q", got, "newhost.com")
	}

	m.SetURI(msg, "port", "5061")
	if got := m.GetURI(msg, "port"); got != "5061" {
		t.Errorf("GetURI(port) after SetURI = %q, want %q", got, "5061")
	}

	// Full URI reflects all changes.
	if got := m.GetURI(msg, ""); got != "sip:bob@newhost.com:5061;transport=tcp" {
		t.Errorf("GetURI(full) after SetURI = %q, want %q", got, "sip:bob@newhost.com:5061;transport=tcp")
	}
}

func TestGetHeader(t *testing.T) {
	m := New()
	msg := parseRaw(t, invite)

	if got := m.GetHeader(msg, "X-Custom"); got != "hello-world" {
		t.Errorf("GetHeader(X-Custom) = %q, want %q", got, "hello-world")
	}
	// Case-insensitive.
	if got := m.GetHeader(msg, "x-custom"); got != "hello-world" {
		t.Errorf("GetHeader(x-custom) = %q, want %q", got, "hello-world")
	}
	if got := m.GetHeader(msg, "Call-ID"); got != "a84b4c76e66710@pc33.atlanta.com" {
		t.Errorf("GetHeader(Call-ID) = %q", got)
	}
	if got := m.GetHeader(msg, "Missing"); got != "" {
		t.Errorf("GetHeader(Missing) = %q, want empty", got)
	}
}

func TestNilMsg(t *testing.T) {
	m := New()
	if got := m.GetURI(nil, "user"); got != "" {
		t.Errorf("GetURI(nil) = %q, want empty", got)
	}
	m.SetURI(nil, "user", "x")
	if got := m.GetHeader(nil, "X"); got != "" {
		t.Errorf("GetHeader(nil) = %q, want empty", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultKEMIX()
	if d == nil {
		t.Fatalf("DefaultKEMIX() returned nil")
	}
	msg := parseRaw(t, invite)
	if got := GetURI(msg, "user"); got != "alice" {
		t.Errorf("package GetURI(user) = %q, want %q", got, "alice")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultKEMIX()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			msg := parseRaw(t, invite)
			shared.GetURI(msg, "user")
			shared.GetHeader(msg, "X-Custom")
		}()
	}
	wg.Wait()
}
