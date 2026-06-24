// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipTrace module tests - tracing SIP messages.
 */
package siptrace

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var inviteBytes = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776asdhds\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: call-1234@10.0.0.1\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@10.0.0.1>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

var replyBytes = []byte("SIP/2.0 200 OK\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776asdhds\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>;tag=9876\r\n" +
	"Call-ID: call-1234@10.0.0.1\r\n" +
	"CSeq: 314159 INVITE\r\n" +
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

func TestTraceRequest(t *testing.T) {
	m := NewSipTraceModule()
	msg := mustParseMsg(t, inviteBytes)

	entry := m.Trace(msg, "10.0.0.1", "10.0.0.2")
	if entry == nil {
		t.Fatal("expected non-nil trace entry")
	}
	if entry.Type != TraceRequest {
		t.Errorf("Type = %q, want %q", entry.Type, TraceRequest)
	}
	if entry.Method != "INVITE" {
		t.Errorf("Method = %q, want INVITE", entry.Method)
	}
	if entry.Status != 0 {
		t.Errorf("Status = %d, want 0", entry.Status)
	}
	if entry.CallID != "call-1234@10.0.0.1" {
		t.Errorf("CallID = %q, want call-1234@10.0.0.1", entry.CallID)
	}
	if entry.SrcIP != "10.0.0.1" || entry.DstIP != "10.0.0.2" {
		t.Errorf("SrcIP/DstIP = %q/%q", entry.SrcIP, entry.DstIP)
	}
	if entry.Payload == "" {
		t.Error("expected non-empty payload")
	}
	if entry.ID <= 0 {
		t.Errorf("ID = %d, want > 0", entry.ID)
	}
}

func TestTraceReply(t *testing.T) {
	m := NewSipTraceModule()
	msg := mustParseMsg(t, replyBytes)

	entry := m.TraceReply(msg, "10.0.0.2", "10.0.0.1")
	if entry == nil {
		t.Fatal("expected non-nil trace entry")
	}
	if entry.Type != TraceReply {
		t.Errorf("Type = %q, want %q", entry.Type, TraceReply)
	}
	if entry.Status != 200 {
		t.Errorf("Status = %d, want 200", entry.Status)
	}
	if entry.Method != "" {
		t.Errorf("Method = %q, want empty", entry.Method)
	}
}

func TestGetByCallID(t *testing.T) {
	m := NewSipTraceModule()
	req := mustParseMsg(t, inviteBytes)
	rpl := mustParseMsg(t, replyBytes)

	m.TraceRequest(req, "10.0.0.1", "10.0.0.2")
	m.TraceReply(rpl, "10.0.0.2", "10.0.0.1")

	entries := m.GetByCallID("call-1234@10.0.0.1")
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Type != TraceRequest {
		t.Errorf("entries[0].Type = %q, want req", entries[0].Type)
	}
	if entries[1].Type != TraceReply {
		t.Errorf("entries[1].Type = %q, want rpl", entries[1].Type)
	}

	none := m.GetByCallID("nonexistent")
	if len(none) != 0 {
		t.Errorf("expected 0 entries for unknown call-id, got %d", len(none))
	}
}

func TestGetTraceAndCount(t *testing.T) {
	m := NewSipTraceModule()
	msg := mustParseMsg(t, inviteBytes)

	entry := m.Trace(msg, "10.0.0.1", "10.0.0.2")
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
	got := m.GetTrace(entry.ID)
	if got != entry {
		t.Errorf("GetTrace returned %p, want %p", got, entry)
	}
	if m.GetTrace(99999) != nil {
		t.Error("expected nil for unknown id")
	}
}

func TestListAndClear(t *testing.T) {
	m := NewSipTraceModule()
	msg := mustParseMsg(t, inviteBytes)
	m.Trace(msg, "a", "b")
	m.Trace(msg, "a", "b")

	if got := m.List(); len(got) != 2 {
		t.Errorf("len(List()) = %d, want 2", len(got))
	}
	m.Clear()
	if m.Count() != 0 {
		t.Errorf("Count after Clear = %d, want 0", m.Count())
	}
	if got := m.List(); len(got) != 0 {
		t.Errorf("len(List()) after Clear = %d, want 0", len(got))
	}
	if entries := m.GetByCallID("call-1234@10.0.0.1"); len(entries) != 0 {
		t.Errorf("GetByCallID after Clear = %d, want 0", len(entries))
	}
}

func TestSetEnabled(t *testing.T) {
	m := NewSipTraceModule()
	if !m.IsEnabled() {
		t.Error("expected enabled by default")
	}
	m.SetEnabled(false)
	if m.IsEnabled() {
		t.Error("expected disabled after SetEnabled(false)")
	}
	msg := mustParseMsg(t, inviteBytes)
	if entry := m.Trace(msg, "a", "b"); entry != nil {
		t.Errorf("Trace when disabled returned %v, want nil", entry)
	}
	if m.Count() != 0 {
		t.Errorf("Count when disabled = %d, want 0", m.Count())
	}
	m.SetEnabled(true)
	if entry := m.Trace(msg, "a", "b"); entry == nil {
		t.Error("Trace when re-enabled returned nil")
	}
}

func TestNilMessage(t *testing.T) {
	m := NewSipTraceModule()
	if entry := m.Trace(nil, "a", "b"); entry != nil {
		t.Errorf("Trace(nil) returned %v, want nil", entry)
	}
	if entry := m.TraceRequest(nil, "a", "b"); entry != nil {
		t.Errorf("TraceRequest(nil) returned %v, want nil", entry)
	}
	if entry := m.TraceReply(nil, "a", "b"); entry != nil {
		t.Errorf("TraceReply(nil) returned %v, want nil", entry)
	}
}

func TestConcurrentTrace(t *testing.T) {
	m := NewSipTraceModule()
	msg := mustParseMsg(t, inviteBytes)
	const goroutines = 50
	const perG = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.Trace(msg, "10.0.0.1", "10.0.0.2")
			}
		}()
	}
	wg.Wait()
	want := int64(goroutines * perG)
	if got := int64(m.Count()); got != want {
		t.Errorf("Count = %d, want %d", got, want)
	}
}

func TestDefaultSipTraceAndInit(t *testing.T) {
	Init()
	d1 := DefaultSipTrace()
	d2 := DefaultSipTrace()
	if d1 != d2 {
		t.Error("DefaultSipTrace returned different instances")
	}
	d1.Clear()
	msg := mustParseMsg(t, inviteBytes)
	d1.Trace(msg, "a", "b")
	if d2.Count() != 1 {
		t.Errorf("Count after trace via default = %d, want 1", d2.Count())
	}
	// Init resets the singleton.
	Init()
	if DefaultSipTrace().Count() != 0 {
		t.Error("expected reset after Init()")
	}
}
