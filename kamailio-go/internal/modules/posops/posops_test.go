// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - posops module tests.
 */

package posops

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

func newMsg(t *testing.T, raw string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	return msg
}

const inviteRaw = "INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP host;branch=z9hG4bK1\r\n" +
	"From: <sip:alice@example.com>;tag=1\r\n" +
	"To: <sip:bob@example.com>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

func TestSearch(t *testing.T) {
	m := New()
	msg := newMsg(t, inviteRaw)
	idx, ok := m.Search(msg, "bob", 0)
	if !ok {
		t.Fatal("Search(bob) not found")
	}
	if idx < 0 || string(msg.Buf[idx:idx+3]) != "bob" {
		t.Errorf("Search returned bad index %d", idx)
	}
	// Search starting past the first occurrence.
	idx2, ok := m.Search(msg, "example", idx+3)
	if !ok {
		t.Fatal("Search(example) not found")
	}
	if idx2 < idx {
		t.Errorf("second search index %d < first %d", idx2, idx)
	}
	if _, ok := m.Search(msg, "zzzznotfound", 0); ok {
		t.Error("Search(notfound) = true, want false")
	}
}

func TestReplace(t *testing.T) {
	m := New()
	msg := newMsg(t, inviteRaw)
	// "bob" appears twice: in the R-URI and in the To header.
	n := m.Replace(msg, "bob", "rob", 0)
	if n != 2 {
		t.Fatalf("Replace count = %d, want 2", n)
	}
	if !contains(string(msg.Buf), "rob@example.com") {
		t.Error("buffer does not contain rob@example.com")
	}
	if contains(string(msg.Buf), "bob@") {
		t.Error("buffer still contains bob@")
	}
}

func TestInsertAndAppend(t *testing.T) {
	m := New()
	msg := newMsg(t, inviteRaw)
	origLen := msg.Len
	newLen := m.Insert(msg, "X-Marker: yes\r\n", 0)
	if newLen <= origLen {
		t.Fatalf("Insert newLen = %d, orig = %d", newLen, origLen)
	}
	if string(msg.Buf[:13]) != "X-Marker: yes" {
		t.Errorf("buffer start = %q", string(msg.Buf[:13]))
	}
	appLen := m.Append(msg, "TRAILER")
	if appLen != newLen+len("TRAILER") {
		t.Errorf("Append len = %d, want %d", appLen, newLen+len("TRAILER"))
	}
	if string(msg.Buf[len(msg.Buf)-7:]) != "TRAILER" {
		t.Error("buffer does not end with TRAILER")
	}
}

func TestNilSafety(t *testing.T) {
	m := New()
	if _, ok := m.Search(nil, "x", 0); ok {
		t.Error("Search(nil) = true, want false")
	}
	if m.Replace(nil, "x", "y", 0) != 0 {
		t.Error("Replace(nil) != 0")
	}
	if m.Insert(nil, "x", 0) != 0 {
		t.Error("Insert(nil) != 0")
	}
	if m.Append(nil, "x") != 0 {
		t.Error("Append(nil) != 0")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
