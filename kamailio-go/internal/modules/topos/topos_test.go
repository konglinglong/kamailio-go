// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Topos module.
 */

package topos

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// addHeader appends a header to msg and wires up the matching quick-access
// pointer, mirroring what the full parser does.
func addHeader(msg *parser.SIPMsg, name, value string) *parser.HdrField {
	h := msg.AddHeader(name, value)
	switch h.Type {
	case parser.HdrFrom:
		msg.From = h
	case parser.HdrTo:
		msg.To = h
	case parser.HdrCallID:
		msg.CallID = h
	}
	return h
}

// newRequestMsg builds a SIP request message with the given headers and RURI.
func newRequestMsg(callID, from, to, ruri string) *parser.SIPMsg {
	msg := &parser.SIPMsg{}
	addHeader(msg, "Call-ID", callID)
	addHeader(msg, "From", from)
	addHeader(msg, "To", to)
	if ruri != "" {
		msg.FirstLine = &parser.MsgStart{Req: &parser.RequestLine{URI: str.Mk(ruri)}}
	}
	return msg
}

func TestRecord(t *testing.T) {
	m := New()

	msg := newRequestMsg(
		"call-1@example.com",
		"<sip:alice@example.com>;tag=fromtag1",
		"<sip:bob@example.com>;tag=totag1",
		"sip:bob@example.com",
	)

	rec := m.Record(msg, "downstream")
	if rec == nil {
		t.Fatalf("Record() returned nil")
	}
	if rec.CallID != "call-1@example.com" {
		t.Errorf("CallID = %q, want %q", rec.CallID, "call-1@example.com")
	}
	if rec.FromTag != "fromtag1" {
		t.Errorf("FromTag = %q, want %q", rec.FromTag, "fromtag1")
	}
	if rec.ToTag != "totag1" {
		t.Errorf("ToTag = %q, want %q", rec.ToTag, "totag1")
	}
	if rec.Direction != "downstream" {
		t.Errorf("Direction = %q, want %q", rec.Direction, "downstream")
	}
	if rec.OriginalFrom == "" {
		t.Errorf("OriginalFrom should be captured")
	}
	if rec.OriginalTo == "" {
		t.Errorf("OriginalTo should be captured")
	}
	if rec.OriginalRURI == "" {
		t.Errorf("OriginalRURI should be captured")
	}
	// Hidden values must differ from the originals.
	if rec.HiddenFrom == rec.OriginalFrom {
		t.Errorf("HiddenFrom should differ from OriginalFrom")
	}
	if rec.HiddenTo == rec.OriginalTo {
		t.Errorf("HiddenTo should differ from OriginalTo")
	}
	if rec.HiddenRURI == rec.OriginalRURI {
		t.Errorf("HiddenRURI should differ from OriginalRURI")
	}
	// The message must now carry the hidden values.
	if msg.From.Body.String() == rec.OriginalFrom {
		t.Errorf("From header should be replaced with hidden value")
	}
	if msg.To.Body.String() == rec.OriginalTo {
		t.Errorf("To header should be replaced with hidden value")
	}

	// nil message -> nil record.
	if rec := m.Record(nil, "downstream"); rec != nil {
		t.Errorf("Record(nil) should return nil")
	}
}

func TestRestore(t *testing.T) {
	m := New()

	origFrom := "<sip:alice@example.com>;tag=fromtag1"
	origTo := "<sip:bob@example.com>;tag=totag1"
	msg := newRequestMsg("call-2@example.com", origFrom, origTo, "sip:bob@example.com")

	m.Record(msg, "downstream")
	// After Record the From/To are hidden.
	if msg.From.Body.String() == origFrom {
		t.Fatalf("Record did not hide the From header")
	}

	rec, err := m.Restore(msg)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if rec == nil {
		t.Fatalf("Restore() returned nil record")
	}
	// After Restore the originals must be back.
	if got := msg.From.Body.String(); got != origFrom {
		t.Errorf("From after restore = %q, want %q", got, origFrom)
	}
	if got := msg.To.Body.String(); got != origTo {
		t.Errorf("To after restore = %q, want %q", got, origTo)
	}

	// Restore on an unknown dialog -> error.
	unknown := newRequestMsg("unknown@example.com", "<sip:x@y>;tag=nope", "<sip:z@w>", "")
	if _, err := m.Restore(unknown); err == nil {
		t.Errorf("Restore() on unknown dialog should error")
	}
	// nil message -> error.
	if _, err := m.Restore(nil); err == nil {
		t.Errorf("Restore(nil) should error")
	}
}

func TestGetRecord(t *testing.T) {
	m := New()

	msg := newRequestMsg(
		"call-3@example.com",
		"<sip:alice@example.com>;tag=ftag3",
		"<sip:bob@example.com>;tag=ttag3",
		"sip:bob@example.com",
	)
	m.Record(msg, "upstream")

	rec := m.GetRecord("call-3@example.com", "ftag3")
	if rec == nil {
		t.Fatalf("GetRecord() returned nil")
	}
	if rec.CallID != "call-3@example.com" {
		t.Errorf("GetRecord().CallID = %q", rec.CallID)
	}
	if rec.FromTag != "ftag3" {
		t.Errorf("GetRecord().FromTag = %q", rec.FromTag)
	}

	// Unknown -> nil.
	if rec := m.GetRecord("nope", "nope"); rec != nil {
		t.Errorf("GetRecord(unknown) should return nil, got %+v", rec)
	}
}

func TestDeleteRecordAndCount(t *testing.T) {
	m := New()

	msg1 := newRequestMsg("call-a@example.com", "<sip:a@b>;tag=t1", "<sip:c@d>;tag=t2", "sip:c@d")
	m.Record(msg1, "downstream")
	msg2 := newRequestMsg("call-b@example.com", "<sip:a@b>;tag=t3", "<sip:c@d>;tag=t4", "sip:c@d")
	m.Record(msg2, "downstream")

	if got := m.Count(); got != 2 {
		t.Fatalf("Count() = %d, want 2", got)
	}

	m.DeleteRecord("call-a@example.com")
	if got := m.Count(); got != 1 {
		t.Errorf("Count() after delete = %d, want 1", got)
	}
	if rec := m.GetRecord("call-a@example.com", "t1"); rec != nil {
		t.Errorf("GetRecord() after delete should return nil")
	}
	// The other record is untouched.
	if rec := m.GetRecord("call-b@example.com", "t3"); rec == nil {
		t.Errorf("GetRecord(call-b) should still exist")
	}

	// Deleting an unknown call-id is a no-op.
	m.DeleteRecord("does-not-exist")
	if got := m.Count(); got != 1 {
		t.Errorf("Count() after deleting unknown = %d, want 1", got)
	}
}

func TestCleanupExpired(t *testing.T) {
	m := New()

	// Record a dialog, then artificially age it.
	msg := newRequestMsg("expired@example.com", "<sip:a@b>;tag=old", "<sip:c@d>;tag=t", "sip:c@d")
	rec := m.Record(msg, "downstream")
	rec.createdAt = time.Now().Add(-2 * time.Hour)

	// Record a fresh dialog.
	msg2 := newRequestMsg("fresh@example.com", "<sip:a@b>;tag=new", "<sip:c@d>;tag=t2", "sip:c@d")
	m.Record(msg2, "downstream")

	if got := m.Count(); got != 2 {
		t.Fatalf("Count() = %d, want 2 before cleanup", got)
	}

	m.CleanupExpired()
	if got := m.Count(); got != 1 {
		t.Errorf("Count() after cleanup = %d, want 1", got)
	}
	if rec := m.GetRecord("expired@example.com", "old"); rec != nil {
		t.Errorf("expired record should have been cleaned up")
	}
	if rec := m.GetRecord("fresh@example.com", "new"); rec == nil {
		t.Errorf("fresh record should still exist")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultTopos()
	if d == nil {
		t.Fatalf("DefaultTopos() returned nil")
	}
	if d != DefaultTopos() {
		t.Fatalf("DefaultTopos() returned different instances after Init()")
	}

	// Package-level wrappers delegate to the default module.
	Init()
	msg := newRequestMsg("default@example.com", "<sip:a@b>;tag=dt", "<sip:c@d>;tag=dt2", "sip:c@d")
	if rec := Record(msg, "downstream"); rec == nil {
		t.Fatalf("package Record() returned nil")
	}
	if got := Count(); got != 1 {
		t.Errorf("package Count() = %d, want 1", got)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultTopos()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			msg := newRequestMsg(
				"c"+itoa(i)+"@example.com",
				"<sip:a@b>;tag=t"+itoa(i),
				"<sip:c@d>;tag=u"+itoa(i),
				"sip:c@d",
			)
			shared.Record(msg, "downstream")
			shared.GetRecord("c"+itoa(i)+"@example.com", "t"+itoa(i))
			shared.Count()
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != goroutines {
		t.Errorf("Count() after concurrent records = %d, want %d", got, goroutines)
	}
}

// itoa is a tiny local int->string helper to avoid pulling strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// satisfy unused-import checks for strings (used in assertions).
var _ = strings.Contains
