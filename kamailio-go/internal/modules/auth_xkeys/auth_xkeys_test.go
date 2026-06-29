// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the auth_xkeys module.
 */

package auth_xkeys

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

const inviteWithKey = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@atlanta.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"X-Auth-Key: secret123\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

const inviteNoKey = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@atlanta.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
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

func TestSetGetRemoveKey(t *testing.T) {
	m := New()

	m.SetKey("svc1", "secret123")
	k, ok := m.GetKey("svc1")
	if !ok {
		t.Fatalf("GetKey(svc1) not found")
	}
	if k != "secret123" {
		t.Errorf("GetKey(svc1) = %q, want %q", k, "secret123")
	}

	// Unknown key.
	if _, ok := m.GetKey("nope"); ok {
		t.Errorf("GetKey(nope) should return false")
	}

	if !m.RemoveKey("svc1") {
		t.Errorf("RemoveKey(svc1) returned false")
	}
	if m.RemoveKey("svc1") {
		t.Errorf("RemoveKey(svc1) twice should return false")
	}
}

func TestValidate(t *testing.T) {
	m := New()
	m.SetKey("svc1", "secret123")

	msg := parseRaw(t, inviteWithKey)
	if !m.Validate(msg, "svc1") {
		t.Errorf("Validate(with key) = false, want true")
	}

	// Wrong key name registered -> false.
	m.SetKey("svc1", "different")
	if m.Validate(msg, "svc1") {
		t.Errorf("Validate(wrong key) = true, want false")
	}

	// Restore correct key.
	m.SetKey("svc1", "secret123")
	if !m.Validate(msg, "svc1") {
		t.Errorf("Validate(restored) = false, want true")
	}

	// Message without the header.
	msgNoKey := parseRaw(t, inviteNoKey)
	if m.Validate(msgNoKey, "svc1") {
		t.Errorf("Validate(no header) = true, want false")
	}

	// Unknown key name.
	if m.Validate(msg, "unknown") {
		t.Errorf("Validate(unknown keyname) = true, want false")
	}
}

func TestValidateNilMsg(t *testing.T) {
	m := New()
	m.SetKey("svc1", "secret123")
	if m.Validate(nil, "svc1") {
		t.Errorf("Validate(nil) = true, want false")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultAuthXKeys()
	if d == nil {
		t.Fatalf("DefaultAuthXKeys() returned nil")
	}
	if d != DefaultAuthXKeys() {
		t.Fatalf("DefaultAuthXKeys() returned different instances")
	}
	SetKey("pkg", "abc")
	k, ok := GetKey("pkg")
	if !ok || k != "abc" {
		t.Errorf("package GetKey(pkg) = %q,%v", k, ok)
	}
	if !RemoveKey("pkg") {
		t.Errorf("package RemoveKey(pkg) = false")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultAuthXKeys()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := itoa(i)
			shared.SetKey(name, "k"+itoa(i))
			shared.GetKey(name)
			shared.RemoveKey(name)
			shared.Count()
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
