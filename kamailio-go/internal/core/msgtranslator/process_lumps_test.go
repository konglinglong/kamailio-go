// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Tests for process_lumps.go
 */

package msgtranslator

import (
	"strings"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

func makeBuf(s string) []byte { return []byte(s) }

func TestProcessLumps_NoLumps(t *testing.T) {
	buf := makeBuf("hello world")
	out, err := ProcessLumps(buf, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello world" {
		t.Fatalf("expected original, got %q", out)
	}
	// Verify the output is a copy, not the original slice.
	out[0] = 'X'
	if buf[0] != 'h' {
		t.Fatal("ProcessLumps returned the original buffer instead of a copy")
	}
}

func TestProcessLumps_EmptyList(t *testing.T) {
	buf := makeBuf("hello")
	out, err := ProcessLumps(buf, &parser.LumpList{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello" {
		t.Fatalf("expected hello, got %q", out)
	}
}

func TestProcessLumps_InsertBefore(t *testing.T) {
	// Insert "XYZ" before offset 6 in "hello world"
	buf := makeBuf("hello world")
	ll := &parser.LumpList{}
	ll.Append(parser.AddLump(6, []byte("XYZ"), true))
	out, err := ProcessLumps(buf, ll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello XYZworld" {
		t.Fatalf("expected 'hello XYZworld', got %q", out)
	}
}

func TestProcessLumps_InsertAfter(t *testing.T) {
	// Insert "XYZ" after offset 4 (i.e. at offset 5) in "hello world"
	buf := makeBuf("hello world")
	ll := &parser.LumpList{}
	ll.Append(parser.AddLump(5, []byte("XYZ"), false))
	out, err := ProcessLumps(buf, ll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Insert at offset 5 means insert before "world" word boundary
	// Actually since IsAfter is set, semantics in our impl: pure insert at offset.
	want := "helloXYZ world"
	if string(out) != want {
		t.Fatalf("expected %q, got %q", want, out)
	}
}

func TestProcessLumps_Delete(t *testing.T) {
	// Delete "world" from "hello world" (offset 6, len 5)
	buf := makeBuf("hello world")
	ll := &parser.LumpList{}
	ll.Append(parser.DeleteLump(6, 5))
	out, err := ProcessLumps(buf, ll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello " {
		t.Fatalf("expected 'hello ', got %q", out)
	}
}

func TestProcessLumps_Replace(t *testing.T) {
	// Replace "world" with "Go" in "hello world"
	buf := makeBuf("hello world")
	ll := &parser.LumpList{}
	ll.Append(parser.ReplaceLump(6, 5, []byte("Go")))
	out, err := ProcessLumps(buf, ll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello Go" {
		t.Fatalf("expected 'hello Go', got %q", out)
	}
}

func TestProcessLumps_MultipleOrdered(t *testing.T) {
	// Multiple lumps out of order should be applied by offset.
	buf := makeBuf("abcdefghij")
	ll := &parser.LumpList{}
	// Replace "cd" (offset 2, len 2) with "CD"
	ll.Append(parser.ReplaceLump(2, 2, []byte("CD")))
	// Delete "f" at offset 5
	ll.Append(parser.DeleteLump(5, 1))
	// Insert "X" before "h" at offset 7
	ll.Append(parser.AddLump(7, []byte("X"), true))
	out, err := ProcessLumps(buf, ll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Walk: a b -> replace cd with CD -> e -> delete f -> g -> X -> hij
	want := "abCDegXhij"
	if string(out) != want {
		t.Fatalf("expected %q, got %q", want, out)
	}
}

func TestProcessLumps_SIPMessageDeleteHeader(t *testing.T) {
	raw := "INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1\r\n" +
		"From: <sip:alice@example.com>\r\n" +
		"To: <sip:bob@example.com>\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Find the From header and create a delete lump for it.
	var fromHdr *parser.HdrField
	for _, h := range msg.Headers {
		if strings.EqualFold(h.Name.String(), "From") {
			fromHdr = h
			break
		}
	}
	if fromHdr == nil {
		t.Fatal("From header not found")
	}

	ll := &parser.LumpList{}
	del := parser.DeleteLump(fromHdr.Offset, fromHdr.Len)
	ll.Append(del)

	out, err := ProcessLumps(msg.Buf[:msg.Len], ll)
	if err != nil {
		t.Fatalf("ProcessLumps failed: %v", err)
	}
	if strings.Contains(string(out), "From:") {
		t.Fatalf("From header should be removed, got:\n%s", out)
	}
	if !strings.Contains(string(out), "Via:") {
		t.Fatal("Via header should still be present")
	}
}

func TestProcessLumps_SIPMessageReplaceHeader(t *testing.T) {
	raw := "INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1\r\n" +
		"To: <sip:bob@example.com>\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Find the To header and replace its body.
	var toHdr *parser.HdrField
	for _, h := range msg.Headers {
		if strings.EqualFold(h.Name.String(), "To") {
			toHdr = h
			break
		}
	}
	if toHdr == nil {
		t.Fatal("To header not found")
	}

	ll := &parser.LumpList{}
	newVal := []byte("To: <sip:carol@example.com>\r\n")
	rep := parser.ReplaceLump(toHdr.Offset, toHdr.Len, newVal)
	ll.Append(rep)

	out, err := ProcessLumps(msg.Buf[:msg.Len], ll)
	if err != nil {
		t.Fatalf("ProcessLumps failed: %v", err)
	}
	if !strings.Contains(string(out), "sip:carol@example.com") {
		t.Fatalf("expected new To header, got:\n%s", out)
	}
	// The old To body must be gone; check specifically the To header line
	// (not the R-URI in the request line, which also contains the host).
	if strings.Contains(string(out), "To: <sip:bob@example.com>") {
		t.Fatalf("old To body should be removed, got:\n%s", out)
	}
}

func TestProcessLumps_SIPMessageAddHeader(t *testing.T) {
	raw := "INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Find insertion point: end of last header.
	var lastHdr *parser.HdrField
	for _, h := range msg.Headers {
		lastHdr = h
	}
	if lastHdr == nil {
		t.Fatal("no headers found")
	}
	offset := lastHdr.Offset + lastHdr.Len

	ll := &parser.LumpList{}
	ll.Append(parser.AddLump(offset, []byte("P-Header: test\r\n"), false))

	out, err := ProcessLumps(msg.Buf[:msg.Len], ll)
	if err != nil {
		t.Fatalf("ProcessLumps failed: %v", err)
	}
	if !strings.Contains(string(out), "P-Header: test") {
		t.Fatalf("P-Header should be present, got:\n%s", out)
	}
}

func TestProcessLumps_BodyModification(t *testing.T) {
	// Test body replacement using the body lumps list.
	raw := "INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1\r\n" +
		"Content-Length: 13\r\n" +
		"\r\n" +
		"v=0\r\no=root"
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Find body offset.
	var bodyOffset int
	for _, h := range msg.Headers {
		bodyOffset = h.Offset + h.Len
	}

	ll := &parser.LumpList{}
	newBody := []byte("v=0\r\no=alice")
	ll.Append(parser.ReplaceLump(bodyOffset, msg.Len-bodyOffset, newBody))

	out, err := ProcessLumps(msg.Buf[:msg.Len], ll)
	if err != nil {
		t.Fatalf("ProcessLumps failed: %v", err)
	}
	if !strings.Contains(string(out), "o=alice") {
		t.Fatalf("new body should be present, got:\n%s", out)
	}
	if strings.Contains(string(out), "o=root") {
		t.Fatalf("old body should be removed, got:\n%s", out)
	}
}

func TestProcessMsgLumps_AllLists(t *testing.T) {
	buf := makeBuf("Hello, World!")
	// Use the head list to insert a prefix, the AddRM list to replace "World" with "Go".
	headList := &parser.LumpList{}
	headList.Append(parser.AddLump(0, []byte("SIP: "), true))

	addRmList := &parser.LumpList{}
	addRmList.Append(parser.ReplaceLump(7, 5, []byte("Go")))

	ml := &parser.MsgLumps{
		HeadLumps: *headList,
		AddRM:     *addRmList,
	}
	out, err := ProcessMsgLumps(buf, ml)
	if err != nil {
		t.Fatalf("ProcessMsgLumps failed: %v", err)
	}
	// After head: "SIP: Hello, World!"
	// After addRm replace: "SIP: Hello, Go!"
	want := "SIP: Hello, Go!"
	if string(out) != want {
		t.Fatalf("expected %q, got %q", want, out)
	}
}

func TestProcessMsgLumps_Nil(t *testing.T) {
	buf := makeBuf("hello")
	out, err := ProcessMsgLumps(buf, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello" {
		t.Fatalf("expected hello, got %q", out)
	}
}

func TestApplyLumps_NilMsg(t *testing.T) {
	_, err := ApplyLumps(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil message")
	}
}

func TestApplyLumps_NoBuffer(t *testing.T) {
	msg := &parser.SIPMsg{}
	_, err := ApplyLumps(msg, nil)
	if err == nil {
		t.Fatal("expected error for message with no buffer")
	}
}

func TestApplyLumps_WithMsgBuffer(t *testing.T) {
	raw := "INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	var lastHdr *parser.HdrField
	for _, h := range msg.Headers {
		lastHdr = h
	}
	offset := lastHdr.Offset + lastHdr.Len

	addRm := &parser.LumpList{}
	addRm.Append(parser.AddLump(offset, []byte("X-Custom: yes\r\n"), false))

	ml := &parser.MsgLumps{
		AddRM: *addRm,
	}
	out, err := ApplyLumps(msg, ml)
	if err != nil {
		t.Fatalf("ApplyLumps failed: %v", err)
	}
	if !strings.Contains(string(out), "X-Custom: yes") {
		t.Fatalf("expected X-Custom header, got:\n%s", out)
	}
}

func TestProcessLumps_InvalidOffset(t *testing.T) {
	buf := makeBuf("hi")
	ll := &parser.LumpList{}
	ll.Append(parser.AddLump(100, []byte("X"), false))
	_, err := ProcessLumps(buf, ll)
	if err != ErrInvalidLumpOffset {
		t.Fatalf("expected ErrInvalidLumpOffset, got %v", err)
	}
}

func TestProcessLumps_ConcurrentSafe(t *testing.T) {
	// ProcessLumps must be safe for concurrent reads on the same lump list.
	buf := makeBuf("hello world")
	ll := &parser.LumpList{}
	ll.Append(parser.ReplaceLump(6, 5, []byte("Go")))

	done := make(chan struct{})
	const N = 50
	for i := 0; i < N; i++ {
		go func() {
			out, err := ProcessLumps(buf, ll)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if string(out) != "hello Go" {
				t.Errorf("expected 'hello Go', got %q", out)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < N; i++ {
		<-done
	}
}

func TestProcessLumps_BeforeAfterSubLumps(t *testing.T) {
	// Test that Before/After sub-lumps are emitted around the anchor.
	buf := makeBuf("hello")
	// Main lump: insert "MAIN" at offset 5 (end of "hello").
	main := parser.AddLump(5, []byte("MAIN"), false)
	// Before sub-lump: insert "BEFORE_" before main.
	main.InsertBefore(parser.AddLump(5, []byte("BEFORE_"), true))
	// After sub-lump: insert "_AFTER" after main.
	main.InsertAfter(parser.AddLump(5, []byte("_AFTER"), false))

	ll := &parser.LumpList{}
	ll.Append(main)

	out, err := ProcessLumps(buf, ll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "helloBEFORE_MAIN_AFTER"
	if string(out) != want {
		t.Fatalf("expected %q, got %q", want, out)
	}
}
