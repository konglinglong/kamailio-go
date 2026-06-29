// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SL module tests - stateless SIP replies.
 */
package sl

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var testInviteBytes = []byte("INVITE sip:user@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776asdhds\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@pc33.example.com>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

func TestSendReply(t *testing.T) {
	sl := NewSLModule()
	msg := mustParseMsg(t, testInviteBytes)

	if err := sl.SendReply(msg, 200, "OK"); err != nil {
		t.Fatalf("SendReply failed: %v", err)
	}

	stats := sl.Stats()
	if got := stats.RepliesSent.Load(); got != 1 {
		t.Errorf("expected RepliesSent=1, got %d", got)
	}
	if got := stats.RepliesError.Load(); got != 0 {
		t.Errorf("expected RepliesError=0, got %d", got)
	}

	buf := sl.LastBuffer()
	if buf == nil {
		t.Fatal("expected non-nil last buffer")
	}
	if !strings.HasPrefix(string(buf), "SIP/2.0 200 OK\r\n") {
		t.Errorf("expected buffer to start with status line, got: %q", string(buf))
	}
	// A 2xx reply must carry a generated to-tag (C: code >= 180).
	if !strings.Contains(string(buf), ";tag=") {
		t.Errorf("expected buffer to contain a to-tag, got: %q", string(buf))
	}

	reply := sl.LastReply()
	if reply == nil {
		t.Fatal("expected non-nil last reply")
	}
	if reply.Code != 200 {
		t.Errorf("expected Code=200, got %d", reply.Code)
	}
	if reply.Reason != "OK" {
		t.Errorf("expected Reason=OK, got %s", reply.Reason)
	}
}

func TestSendReplyWithHeaders(t *testing.T) {
	sl := NewSLModule()
	msg := mustParseMsg(t, testInviteBytes)

	extra := []string{"X-Custom: value1", "X-Another: value2"}
	if err := sl.SendReplyWithHeaders(msg, 200, "OK", extra); err != nil {
		t.Fatalf("SendReplyWithHeaders failed: %v", err)
	}

	buf := string(sl.LastBuffer())
	if !strings.Contains(buf, "X-Custom: value1\r\n") {
		t.Errorf("expected buffer to contain X-Custom header, got: %q", buf)
	}
	if !strings.Contains(buf, "X-Another: value2\r\n") {
		t.Errorf("expected buffer to contain X-Another header, got: %q", buf)
	}

	reply := sl.LastReply()
	if reply == nil {
		t.Fatal("expected non-nil last reply")
	}
	if len(reply.Headers) != 2 {
		t.Errorf("expected 2 headers in reply, got %d", len(reply.Headers))
	}
}

func TestSendReplyWithBody(t *testing.T) {
	sl := NewSLModule()
	msg := mustParseMsg(t, testInviteBytes)

	body := "v=0\r\no=- 123456 2 IN IP4 127.0.0.1\r\n"
	if err := sl.SendReplyWithBody(msg, 200, "OK", body); err != nil {
		t.Fatalf("SendReplyWithBody failed: %v", err)
	}

	buf := string(sl.LastBuffer())
	if !strings.HasSuffix(buf, body) {
		t.Errorf("expected buffer to end with body, got: %q", buf)
	}
	wantCL := fmt.Sprintf("Content-Length: %d", len(body))
	if !strings.Contains(buf, wantCL) {
		t.Errorf("expected buffer to contain %q, got: %q", wantCL, buf)
	}
	// The stale Content-Length: 0 from the request must be gone.
	if strings.Contains(buf, "Content-Length: 0\r\n") {
		t.Errorf("expected stale Content-Length: 0 to be removed, got: %q", buf)
	}

	reply := sl.LastReply()
	if reply == nil {
		t.Fatal("expected non-nil last reply")
	}
	if reply.Body != body {
		t.Errorf("expected body %q, got %q", body, reply.Body)
	}
}

func TestGenTag(t *testing.T) {
	sl := NewSLModule()
	tag := sl.GenTag()
	if tag == "" {
		t.Fatal("expected non-empty tag")
	}
	// MD5 hex digest is 32 characters.
	if len(tag) != 32 {
		t.Errorf("expected tag length 32, got %d", len(tag))
	}
}

func TestStats(t *testing.T) {
	sl := NewSLModule()
	msg := mustParseMsg(t, testInviteBytes)

	for i := 0; i < 3; i++ {
		if err := sl.SendReply(msg, 200, "OK"); err != nil {
			t.Fatalf("SendReply failed: %v", err)
		}
	}
	// Invalid codes must be counted as errors.
	_ = sl.SendReply(msg, 99, "Bad")
	_ = sl.SendReply(msg, 700, "Bad")

	stats := sl.Stats()
	if got := stats.RepliesSent.Load(); got != 3 {
		t.Errorf("expected RepliesSent=3, got %d", got)
	}
	if got := stats.RepliesError.Load(); got != 2 {
		t.Errorf("expected RepliesError=2, got %d", got)
	}
}

func TestGenTagUniqueness(t *testing.T) {
	sl := NewSLModule()
	const n = 1000
	tags := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tags[i] = sl.GenTag()
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for _, tag := range tags {
		if tag == "" {
			t.Error("expected non-empty tag")
			continue
		}
		if seen[tag] {
			t.Errorf("duplicate tag: %s", tag)
		}
		seen[tag] = true
	}
	if len(seen) != n {
		t.Errorf("expected %d unique tags, got %d", n, len(seen))
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	sl := DefaultSL()
	if sl == nil {
		t.Fatal("expected non-nil default SLModule")
	}
	msg := mustParseMsg(t, testInviteBytes)
	if err := SendReply(msg, 200, "OK"); err != nil {
		t.Fatalf("global SendReply failed: %v", err)
	}
	stats := sl.Stats()
	if got := stats.RepliesSent.Load(); got < 1 {
		t.Errorf("expected RepliesSent>=1, got %d", got)
	}
}
