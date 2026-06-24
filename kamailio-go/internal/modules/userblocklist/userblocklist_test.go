// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - UserBlocklist module tests.
 */

package userblocklist

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var testInvite = []byte("INVITE sip:15551234567@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776asdhds\r\n" +
	"Max-Forwards: 70\r\n" +
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

// TestAddIsBlocked verifies insertion and blocking by exact user and prefix.
func TestAddIsBlocked(t *testing.T) {
	m := New()
	m.Add("15551234567", "example.com", "", "banned user", 5*time.Minute)

	if !m.IsBlocked("15551234567", "example.com", "15551234567") {
		t.Fatal("expected blocked for matching user")
	}
	if m.IsBlocked("15559999999", "example.com", "15559999999") {
		t.Fatal("expected not blocked for different user")
	}
	if m.IsBlocked("15551234567", "other.com", "15551234567") {
		t.Fatal("expected not blocked for different domain")
	}
}

// TestIsBlocked_Prefix verifies prefix-based blocking.
func TestIsBlocked_Prefix(t *testing.T) {
	m := New()
	// Block the entire 1555 prefix for this user/domain.
	m.Add("15551234567", "example.com", "1555", "prefix ban", 5*time.Minute)

	if !m.IsBlocked("15551234567", "example.com", "15551234567") {
		t.Fatal("expected blocked: number starts with 1555")
	}
	if m.IsBlocked("15551234567", "example.com", "19991234567") {
		t.Fatal("expected not blocked: number does not start with 1555")
	}
	// Empty prefix entry blocks everything for the user/domain.
	m2 := New()
	m2.Add("user", "dom", "", "all", 5*time.Minute)
	if !m2.IsBlocked("user", "dom", "anything") {
		t.Fatal("expected wildcard block")
	}
}

// TestRemove verifies entry removal.
func TestRemove(t *testing.T) {
	m := New()
	m.Add("user", "dom", "1555", "x", 5*time.Minute)
	m.Add("user", "dom", "1999", "y", 5*time.Minute)
	if !m.Remove("user", "dom", "1555") {
		t.Fatal("expected Remove true")
	}
	if m.IsBlocked("user", "dom", "15551234") {
		t.Fatal("expected not blocked after remove")
	}
	if !m.IsBlocked("user", "dom", "19991234") {
		t.Fatal("expected other entry still blocked")
	}
	if m.Remove("user", "dom", "1555") {
		t.Fatal("expected Remove false for already removed")
	}
	if m.Remove("nouser", "nodom", "") {
		t.Fatal("expected Remove false for unknown user")
	}
}

// TestCheck verifies R-URI based blocking.
func TestCheck(t *testing.T) {
	m := New()
	m.Add("15551234567", "example.com", "", "banned", 5*time.Minute)

	msg := mustParse(t, testInvite) // R-URI sip:15551234567@example.com
	blocked, reason := m.Check(msg)
	if !blocked {
		t.Fatal("expected blocked")
	}
	if reason != "banned" {
		t.Fatalf("unexpected reason: %q", reason)
	}

	// A non-blocked user is allowed.
	m2 := New()
	blocked, _ = m2.Check(msg)
	if blocked {
		t.Fatal("expected not blocked for empty blocklist")
	}
}

// TestCheck_NilAndInvalid verifies nil/invalid message handling.
func TestCheck_NilAndInvalid(t *testing.T) {
	m := New()
	if blocked, _ := m.Check(nil); blocked {
		t.Fatal("expected not blocked for nil msg")
	}
	// A message whose R-URI has no user part.
	msg := mustParse(t, []byte("INVITE sip:@example.com SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP x;branch=z9hG4bK1\r\n"+
		"From: <sip:a@b>;tag=1\r\n"+
		"To: <sip:b@c>\r\n"+
		"Call-ID: c\r\n"+
		"CSeq: 1 INVITE\r\n"+
		"Content-Length: 0\r\n\r\n"))
	blocked, reason := m.Check(msg)
	if blocked {
		t.Fatalf("expected not blocked for empty user, reason: %s", reason)
	}
}

// TestGetListCountClear verifies retrieval, listing, counting and clearing.
func TestGetListCountClear(t *testing.T) {
	m := New()
	m.Add("alice", "a.com", "1", "x", 5*time.Minute)
	m.Add("alice", "a.com", "2", "y", 5*time.Minute)
	m.Add("bob", "b.com", "", "z", 5*time.Minute)

	if m.Count() != 3 {
		t.Fatalf("expected count 3, got %d", m.Count())
	}
	got := m.Get("alice", "a.com")
	if len(got) != 2 {
		t.Fatalf("expected 2 entries for alice, got %d", len(got))
	}
	// Mutating a returned entry must not affect the module.
	got[0].Reason = "mutated"
	if m.Get("alice", "a.com")[0].Reason == "mutated" {
		t.Fatal("expected isolation from Get copy")
	}

	list := m.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(list))
	}
	// Sorted by username, domain, prefix.
	if list[0].Username != "alice" || list[0].Prefix != "1" {
		t.Fatalf("unexpected first entry: %+v", list[0])
	}
	if list[2].Username != "bob" {
		t.Fatalf("unexpected third entry: %+v", list[2])
	}
	m.Clear()
	if m.Count() != 0 {
		t.Fatalf("expected count 0 after Clear, got %d", m.Count())
	}
	if m.Get("alice", "a.com") != nil {
		t.Fatal("expected nil after Clear")
	}
}

// TestExpiryAndCleanup verifies TTL expiry and Cleanup.
func TestExpiryAndCleanup(t *testing.T) {
	m := New()
	m.Add("u1", "d.com", "", "temp", 50*time.Millisecond)
	m.Add("u2", "d.com", "", "perm", 0)

	if !m.IsBlocked("u1", "d.com", "123") {
		t.Fatal("expected blocked before expiry")
	}
	time.Sleep(80 * time.Millisecond)
	if m.IsBlocked("u1", "d.com", "123") {
		t.Fatal("expected not blocked after expiry")
	}
	if !m.IsBlocked("u2", "d.com", "123") {
		t.Fatal("expected permanent entry still blocked")
	}
	if m.Count() != 2 {
		t.Fatalf("expected count 2 before cleanup, got %d", m.Count())
	}
	m.Cleanup()
	if m.Count() != 1 {
		t.Fatalf("expected count 1 after cleanup, got %d", m.Count())
	}
}

// TestLoadFromCSV verifies CSV loading.
func TestLoadFromCSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "blocks.csv")
	content := "username,domain,prefix,reason,ttl\n" +
		"15551234567,example.com,,banned,0\n" +
		"spammer,evil.com,1999,range,3600\n" +
		",nodomain,,emptyuser,0\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := New()
	if err := m.LoadFromCSV(p); err != nil {
		t.Fatalf("LoadFromCSV: %v", err)
	}
	// The empty-username row is skipped.
	if m.Count() != 2 {
		t.Fatalf("expected 2 entries, got %d", m.Count())
	}
	if !m.IsBlocked("15551234567", "example.com", "15551234567") {
		t.Fatal("expected wildcard block from CSV")
	}
	if !m.IsBlocked("spammer", "evil.com", "19991234") {
		t.Fatal("expected prefix block from CSV")
	}
	if m.IsBlocked("spammer", "evil.com", "20001234") {
		t.Fatal("expected not blocked for non-matching prefix")
	}
}

// TestLoadFromCSV_Errors verifies error handling.
func TestLoadFromCSV_Errors(t *testing.T) {
	m := New()
	if err := m.LoadFromCSV(""); err == nil {
		t.Fatal("expected error for empty path")
	}
	if err := m.LoadFromCSV("/nonexistent/path/file.csv"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	Init()
	m := DefaultUserBlocklist()
	if m == nil {
		t.Fatal("expected non-nil default module")
	}
	Add("globaluser", "global.com", "", "global ban", 5*time.Minute)
	if !IsBlocked("globaluser", "global.com", "anything") {
		t.Fatal("expected blocked via global API")
	}
	msg := mustParse(t, testInvite)
	blocked, _ := Check(msg)
	if blocked {
		t.Fatal("expected not blocked for unrelated R-URI via global API")
	}
}

// TestConcurrentAccess exercises the module under the race detector.
func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user := "user" + string(rune('0'+i%10))
			m.Add(user, "d.com", "1", "x", 5*time.Minute)
			m.IsBlocked(user, "d.com", "1234")
			m.Get(user, "d.com")
			m.List()
			_ = m.Count()
		}(i)
	}
	wg.Wait()
}
