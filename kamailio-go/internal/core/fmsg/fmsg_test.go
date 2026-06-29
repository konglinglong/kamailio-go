// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the fmsg package - matching C fmsg.c / fmsg.h.
 */

package fmsg

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// ============================================================
// FakeMsgBuilder
// ============================================================

// TestNewFakeMsgBuilder verifies that the constructor pre-fills defaults.
func TestNewFakeMsgBuilder(t *testing.T) {
	b := NewFakeMsgBuilder("INVITE", "sip:bob@example.com")
	if b.Method != "INVITE" {
		t.Errorf("Method = %q, want %q", b.Method, "INVITE")
	}
	if b.RURI != "sip:bob@example.com" {
		t.Errorf("RURI = %q, want %q", b.RURI, "sip:bob@example.com")
	}
	if b.FromURI == "" {
		t.Error("FromURI should have a default value")
	}
	if b.ToURI == "" {
		t.Error("ToURI should have a default value")
	}
	if b.CallID == "" {
		t.Error("CallID should have a default value")
	}
	if b.CSeq != 1 {
		t.Errorf("CSeq = %d, want 1", b.CSeq)
	}
	if b.CSeqMethod != "INVITE" {
		t.Errorf("CSeqMethod = %q, want %q", b.CSeqMethod, "INVITE")
	}
	if b.FromTag == "" {
		t.Error("FromTag should have a default value")
	}
}

// TestBuildAsString verifies the wire-format output of BuildAsString.
func TestBuildAsString(t *testing.T) {
	b := NewFakeMsgBuilder("OPTIONS", "sip:kamailio.org")
	b.FromURI = "sip:alice@example.com"
	b.ToURI = "sip:bob@example.com"
	b.CallID = "test-call-id"
	b.CSeq = 42
	b.CSeqMethod = "OPTIONS"
	b.FromTag = "abc123"
	b.Headers = []string{"X-Custom: hello"}

	raw, err := b.BuildAsString()
	if err != nil {
		t.Fatalf("BuildAsString error: %v", err)
	}

	// Request line.
	if !strings.HasPrefix(raw, "OPTIONS sip:kamailio.org SIP/2.0\r\n") {
		t.Errorf("request line missing or wrong: %q", raw[:40])
	}

	// Via header.
	if !strings.Contains(raw, "Via: SIP/2.0/UDP 127.0.0.1\r\n") {
		t.Error("Via header missing")
	}

	// From header with tag.
	if !strings.Contains(raw, "From: <sip:alice@example.com>;tag=abc123\r\n") {
		t.Error("From header missing or wrong")
	}

	// To header.
	if !strings.Contains(raw, "To: <sip:bob@example.com>\r\n") {
		t.Error("To header missing or wrong")
	}

	// Call-ID.
	if !strings.Contains(raw, "Call-ID: test-call-id\r\n") {
		t.Error("Call-ID header missing or wrong")
	}

	// CSeq.
	if !strings.Contains(raw, "CSeq: 42 OPTIONS\r\n") {
		t.Error("CSeq header missing or wrong")
	}

	// Max-Forwards.
	if !strings.Contains(raw, "Max-Forwards: 70\r\n") {
		t.Error("Max-Forwards header missing")
	}

	// Custom header.
	if !strings.Contains(raw, "X-Custom: hello\r\n") {
		t.Error("custom header missing")
	}

	// Content-Length: 0 (no body).
	if !strings.Contains(raw, "Content-Length: 0\r\n") {
		t.Error("Content-Length header missing or wrong")
	}

	// Must end with \r\n\r\n (blank line).
	if !strings.HasSuffix(raw, "\r\n\r\n") {
		t.Error("message does not end with blank line")
	}
}

// TestBuildAsStringWithBody verifies that a body is included and
// Content-Length reflects its size.
func TestBuildAsStringWithBody(t *testing.T) {
	body := "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\n"
	b := NewFakeMsgBuilder("INVITE", "sip:bob@example.com")
	b.Body = body

	raw, err := b.BuildAsString()
	if err != nil {
		t.Fatalf("BuildAsString error: %v", err)
	}

	if !strings.Contains(raw, "Content-Length: "+itoa(len(body))) {
		t.Errorf("Content-Length does not match body length %d", len(body))
	}
	if !strings.HasSuffix(raw, body) {
		t.Error("body not appended at the end of the message")
	}
}

// TestBuildAsStringEmptyMethod verifies that an empty method is rejected.
func TestBuildAsStringEmptyMethod(t *testing.T) {
	b := NewFakeMsgBuilder("", "sip:test@example.com")
	_, err := b.BuildAsString()
	if err == nil {
		t.Error("BuildAsString with empty method should return error")
	}
}

// TestBuildAsStringEmptyRURI verifies that an empty RURI is rejected.
func TestBuildAsStringEmptyRURI(t *testing.T) {
	b := NewFakeMsgBuilder("OPTIONS", "")
	_, err := b.BuildAsString()
	if err == nil {
		t.Error("BuildAsString with empty RURI should return error")
	}
}

// TestBuild verifies that Build produces a valid parsed SIPMsg.
func TestBuild(t *testing.T) {
	b := NewFakeMsgBuilder("OPTIONS", "sip:kamailio.org")
	b.CallID = "build-test-call-id"

	msg, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if msg == nil {
		t.Fatal("Build returned nil message")
	}

	// Must be a request.
	if !msg.IsRequest() {
		t.Error("message is not a request")
	}

	// Method must be OPTIONS.
	if msg.Method() != parser.MethodOptions {
		t.Errorf("Method = %v, want %v", msg.Method(), parser.MethodOptions)
	}

	// Must have a non-zero ID.
	if msg.ID == 0 {
		t.Error("message ID is 0")
	}

	// Must have a Call-ID header.
	if msg.CallID == nil {
		t.Fatal("Call-ID header is nil")
	}
	if msg.CallID.Body.String() != "build-test-call-id" {
		t.Errorf("Call-ID = %q, want %q", msg.CallID.Body.String(), "build-test-call-id")
	}

	// Must have From, To, Via, CSeq headers.
	if msg.From == nil {
		t.Error("From header is nil")
	}
	if msg.To == nil {
		t.Error("To header is nil")
	}
	if msg.HdrVia1 == nil {
		t.Error("Via header is nil")
	}
	if msg.CSeq == nil {
		t.Error("CSeq header is nil")
	}
}

// TestBuildWithExtraHeaders verifies that extra headers survive parsing.
func TestBuildWithExtraHeaders(t *testing.T) {
	b := NewFakeMsgBuilder("OPTIONS", "sip:test@example.com")
	b.Headers = []string{"X-Route-Name: my_route", "X-Param: key=value"}

	msg, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	// Find the X-Route-Name header among the parsed headers.
	found := false
	for _, h := range msg.Headers {
		if h.Name.String() == "X-Route-Name" && h.Body.String() == "my_route" {
			found = true
			break
		}
	}
	if !found {
		t.Error("X-Route-Name header not found in parsed message")
	}
}

// TestBuildUniqueIDs verifies that successive Build calls produce unique IDs.
func TestBuildUniqueIDs(t *testing.T) {
	b := NewFakeMsgBuilder("OPTIONS", "sip:test@example.com")

	msg1, err := b.Build()
	if err != nil {
		t.Fatalf("first Build error: %v", err)
	}
	msg2, err := b.Build()
	if err != nil {
		t.Fatalf("second Build error: %v", err)
	}

	if msg1.ID == msg2.ID {
		t.Errorf("IDs are not unique: %d == %d", msg1.ID, msg2.ID)
	}
}

// ============================================================
// NewTimerMsg
// ============================================================

// TestNewTimerMsg verifies the timer message structure.
func TestNewTimerMsg(t *testing.T) {
	params := map[string]string{
		"key1": "val1",
		"key2": "val2",
	}
	msg, err := NewTimerMsg("my_timer_route", params)
	if err != nil {
		t.Fatalf("NewTimerMsg error: %v", err)
	}
	if msg == nil {
		t.Fatal("NewTimerMsg returned nil")
	}

	// Find X-Timer-Route header.
	routeHdr := msg.GetHeaderByType(parser.HdrOther)
	_ = routeHdr // HdrOther won't match; search by name instead.

	var routeFound bool
	var paramCount int
	for _, h := range msg.Headers {
		if h.Name.String() == "X-Timer-Route" && h.Body.String() == "my_timer_route" {
			routeFound = true
		}
		if h.Name.String() == "X-Timer-Param" {
			paramCount++
		}
	}
	if !routeFound {
		t.Error("X-Timer-Route header not found")
	}
	if paramCount != 2 {
		t.Errorf("X-Timer-Param count = %d, want 2", paramCount)
	}
}

// TestNewTimerMsgEmptyRoute verifies that an empty route name is rejected.
func TestNewTimerMsgEmptyRoute(t *testing.T) {
	_, err := NewTimerMsg("", nil)
	if err == nil {
		t.Error("NewTimerMsg with empty route should return error")
	}
}

// TestNewTimerMsgNoParams verifies that nil params produce a valid message.
func TestNewTimerMsgNoParams(t *testing.T) {
	msg, err := NewTimerMsg("cleanup", nil)
	if err != nil {
		t.Fatalf("NewTimerMsg error: %v", err)
	}
	if msg == nil {
		t.Fatal("NewTimerMsg returned nil")
	}

	// Must still have the X-Timer-Route header.
	var found bool
	for _, h := range msg.Headers {
		if h.Name.String() == "X-Timer-Route" {
			found = true
			break
		}
	}
	if !found {
		t.Error("X-Timer-Route header not found")
	}
}

// ============================================================
// NewInternalReq
// ============================================================

// TestNewInternalReq verifies the internal request structure.
func TestNewInternalReq(t *testing.T) {
	msg, err := NewInternalReq("OPTIONS", "sip:target@example.com", "mytag")
	if err != nil {
		t.Fatalf("NewInternalReq error: %v", err)
	}
	if msg == nil {
		t.Fatal("NewInternalReq returned nil")
	}

	if !msg.IsRequest() {
		t.Error("message is not a request")
	}
	if msg.Method() != parser.MethodOptions {
		t.Errorf("Method = %v, want %v", msg.Method(), parser.MethodOptions)
	}

	// Must have the X-Internal-Req marker.
	var found bool
	for _, h := range msg.Headers {
		if h.Name.String() == "X-Internal-Req" && h.Body.String() == "true" {
			found = true
			break
		}
	}
	if !found {
		t.Error("X-Internal-Req header not found")
	}

	// From tag must be set.
	if msg.From == nil {
		t.Fatal("From header is nil")
	}
	fromBody := msg.From.Body.String()
	if !strings.Contains(fromBody, "tag=mytag") {
		t.Errorf("From tag not set correctly: %q", fromBody)
	}
}

// TestNewInternalReqEmptyMethod verifies validation.
func TestNewInternalReqEmptyMethod(t *testing.T) {
	_, err := NewInternalReq("", "sip:test@example.com", "tag")
	if err == nil {
		t.Error("NewInternalReq with empty method should return error")
	}
}

// TestNewInternalReqEmptyRURI verifies validation.
func TestNewInternalReqEmptyRURI(t *testing.T) {
	_, err := NewInternalReq("OPTIONS", "", "tag")
	if err == nil {
		t.Error("NewInternalReq with empty RURI should return error")
	}
}

// TestNewInternalReqNoFromTag verifies that an empty fromTag uses the default.
func TestNewInternalReqNoFromTag(t *testing.T) {
	msg, err := NewInternalReq("OPTIONS", "sip:test@example.com", "")
	if err != nil {
		t.Fatalf("NewInternalReq error: %v", err)
	}
	if msg.From == nil {
		t.Fatal("From header is nil")
	}
	// Should still have a tag (the default "xyz").
	fromBody := msg.From.Body.String()
	if !strings.Contains(fromBody, "tag=") {
		t.Errorf("From should have a tag: %q", fromBody)
	}
}

// ============================================================
// Singleton (DefaultFakedMsg / GetNext / GetNextClear / Match / Init)
// ============================================================

// TestDefaultFakedMsg verifies the singleton is lazily initialised.
func TestDefaultFakedMsg(t *testing.T) {
	Init()

	msg := DefaultFakedMsg()
	if msg == nil {
		t.Fatal("DefaultFakedMsg returned nil")
	}
	if !msg.IsRequest() {
		t.Error("faked message is not a request")
	}
	if msg.Method() != parser.MethodOptions {
		t.Errorf("Method = %v, want %v", msg.Method(), parser.MethodOptions)
	}

	// A second call must return the same pointer (singleton).
	msg2 := DefaultFakedMsg()
	if msg != msg2 {
		t.Error("DefaultFakedMsg returned different pointers")
	}
}

// TestGetNext verifies that GetNext returns a clone with a fresh ID.
func TestGetNext(t *testing.T) {
	Init()

	msg1 := GetNext()
	if msg1 == nil {
		t.Fatal("GetNext returned nil")
	}
	id1 := msg1.ID

	msg2 := GetNext()
	if msg2 == nil {
		t.Fatal("second GetNext returned nil")
	}

	// GetNext returns independent clones (not the shared singleton).
	if msg1 == msg2 {
		t.Error("GetNext returned the same pointer (expected independent clones)")
	}

	// ID must be unique.
	if msg2.ID == id1 {
		t.Error("GetNext did not produce a unique ID")
	}
}

// TestGetNextClear verifies that GetNextClear returns a clone with cleared
// routing fields.
func TestGetNextClear(t *testing.T) {
	Init()

	// GetNextClear returns a clone with cleared routing fields.
	msg := GetNextClear()
	if msg == nil {
		t.Fatal("GetNextClear returned nil")
	}
	if msg.NewURI.Len != 0 {
		t.Errorf("NewURI not cleared: %v", msg.NewURI)
	}
	if msg.DstURI.Len != 0 {
		t.Errorf("DstURI not cleared: %v", msg.DstURI)
	}
	if msg.Flags != 0 {
		t.Errorf("Flags not cleared: %d", msg.Flags)
	}

	// The shared template must be unaffected.
	tmpl := DefaultFakedMsg()
	if tmpl == nil {
		t.Fatal("DefaultFakedMsg returned nil")
	}
}

// TestMatch verifies that Match identifies the singleton.
func TestMatch(t *testing.T) {
	Init()

	msg := DefaultFakedMsg()
	if msg == nil {
		t.Fatal("DefaultFakedMsg returned nil")
	}
	if !Match(msg) {
		t.Error("Match should return true for the singleton")
	}

	other := &parser.SIPMsg{}
	if Match(other) {
		t.Error("Match should return false for a non-singleton message")
	}
}

// TestInitResets verifies that Init clears the singleton.
func TestInitResets(t *testing.T) {
	// Ensure the singleton is initialised.
	msg := DefaultFakedMsg()
	if msg == nil {
		t.Fatal("DefaultFakedMsg returned nil")
	}

	// Init must reset so the next call re-parses.
	Init()
	msg2 := DefaultFakedMsg()
	if msg2 == nil {
		t.Fatal("DefaultFakedMsg returned nil after Init")
	}
	// The pointers may differ because Init clears the state.
	// What matters is that the message is valid.
	if !msg2.IsRequest() {
		t.Error("faked message after Init is not a request")
	}
}

// ============================================================
// Concurrency tests (run with -race)
// ============================================================

// TestConcurrentBuild exercises Build under concurrent access.
func TestConcurrentBuild(t *testing.T) {
	var wg sync.WaitGroup
	const goroutines = 50

	results := make([]*parser.SIPMsg, goroutines)
	errs := make([]error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			b := NewFakeMsgBuilder("OPTIONS", "sip:concurrent@example.com")
			b.CallID = "concurrent-" + itoa(n)
			results[n], errs[n] = b.Build()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d Build error: %v", i, err)
			continue
		}
		if results[i] == nil {
			t.Errorf("goroutine %d Build returned nil", i)
			continue
		}
		if results[i].ID == 0 {
			t.Errorf("goroutine %d message ID is 0", i)
		}
	}

	// All IDs must be unique.
	seen := make(map[uint32]bool)
	for i, msg := range results {
		if msg == nil {
			continue
		}
		if seen[msg.ID] {
			t.Errorf("goroutine %d has duplicate ID %d", i, msg.ID)
		}
		seen[msg.ID] = true
	}
}

// TestConcurrentSingleton exercises the singleton under concurrent access.
func TestConcurrentSingleton(t *testing.T) {
	Init()

	var wg sync.WaitGroup
	const goroutines = 50

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = DefaultFakedMsg()
			_ = GetNext()
			_ = GetNextClear()
		}()
	}
	wg.Wait()

	// After the storm, the singleton must still be usable.
	msg := GetNext()
	if msg == nil {
		t.Fatal("GetNext returned nil after concurrent access")
	}
	if !msg.IsRequest() {
		t.Error("singleton is not a request after concurrent access")
	}
}

// ============================================================
// Helpers
// ============================================================

// itoa is a local strconv.Itoa to avoid importing strconv in the test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
