// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the UAC redirect (uac_redirect) module.
 */

package uac_redirect

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// buildReply builds a SIP reply message with the given status code and
// Contact header body.
func buildReply(statusCode uint16, contactBody string) *parser.SIPMsg {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type:  parser.MsgReply,
		Flags: parser.FLINEFlagProtoSIP,
		Reply: &parser.ReplyLine{
			StatusCode: statusCode,
		},
	}
	if contactBody != "" {
		h := msg.AddHeader("Contact", contactBody)
		msg.Contact = h
	}
	return msg
}

func TestIsRedirect(t *testing.T) {
	m := New()

	redirects := []uint16{300, 301, 302, 305, 399}
	for _, code := range redirects {
		if !m.IsRedirect(buildReply(code, "")) {
			t.Errorf("IsRedirect(%d) = false, want true", code)
		}
	}
	nonRedirects := []uint16{200, 404, 480, 299, 400, 500}
	for _, code := range nonRedirects {
		if m.IsRedirect(buildReply(code, "")) {
			t.Errorf("IsRedirect(%d) = true, want false", code)
		}
	}
	// nil msg -> false.
	if m.IsRedirect(nil) {
		t.Errorf("IsRedirect(nil) = true, want false")
	}
}

func TestGetRedirectType(t *testing.T) {
	m := New()

	cases := []uint16{301, 302, 305}
	for _, code := range cases {
		if got := m.GetRedirectType(buildReply(code, "")); got != int(code) {
			t.Errorf("GetRedirectType(%d) = %d, want %d", code, got, code)
		}
	}
	// Non-redirect -> 0.
	if got := m.GetRedirectType(buildReply(200, "")); got != 0 {
		t.Errorf("GetRedirectType(200) = %d, want 0", got)
	}
}

func TestProcessRedirect(t *testing.T) {
	m := New()

	msg := buildReply(302, "<sip:bob@192.0.2.1:5060>;q=0.9, <sip:bob@192.0.2.2:5060>;q=0.7, <sip:bob@192.0.2.3:5060>;q=1.0")
	entries := m.ProcessRedirect(msg)
	if got := m.Count(entries); got != 3 {
		t.Fatalf("Count() = %d, want 3", got)
	}
	// ProcessRedirect returns entries sorted by descending q-value, so the
	// best (q=1.0) must come first. q-values are parsed as float32 (matching
	// Kamailio's qvalue_t), so compare with a tolerance.
	if !approxEqual(entries[0].Q, 1.0) {
		t.Errorf("entries[0].Q = %v, want 1.0", entries[0].Q)
	}
	if !approxEqual(entries[1].Q, 0.9) {
		t.Errorf("entries[1].Q = %v, want 0.9", entries[1].Q)
	}
	if !approxEqual(entries[2].Q, 0.7) {
		t.Errorf("entries[2].Q = %v, want 0.7", entries[2].Q)
	}

	// Each entry exposes its contact URI.
	if entries[0].Contact == "" {
		t.Errorf("entries[0].Contact is empty")
	}

	// Non-redirect reply -> empty slice.
	empty := m.ProcessRedirect(buildReply(200, "<sip:x@x>"))
	if len(empty) != 0 {
		t.Errorf("ProcessRedirect(200) = %d, want 0", len(empty))
	}
	// Redirect without Contact -> empty slice.
	empty2 := m.ProcessRedirect(buildReply(302, ""))
	if len(empty2) != 0 {
		t.Errorf("ProcessRedirect(no-contact) = %d, want 0", len(empty2))
	}
	// nil msg -> empty slice.
	if got := m.ProcessRedirect(nil); len(got) != 0 {
		t.Errorf("ProcessRedirect(nil) = %d, want 0", len(got))
	}
}

func TestSelectBest(t *testing.T) {
	m := New()

	entries := []*RedirectEntry{
		{Contact: "sip:a@x", Q: 0.3},
		{Contact: "sip:b@x", Q: 0.9},
		{Contact: "sip:c@x", Q: 0.5},
	}
	best := m.SelectBest(entries)
	if best == nil {
		t.Fatalf("SelectBest() returned nil")
	}
	if best.Contact != "sip:b@x" {
		t.Errorf("SelectBest() = %q, want sip:b@x", best.Contact)
	}

	// Empty slice -> nil.
	if m.SelectBest(nil) != nil {
		t.Errorf("SelectBest(nil) should return nil")
	}
	if m.SelectBest([]*RedirectEntry{}) != nil {
		t.Errorf("SelectBest(empty) should return nil")
	}

	// Ties broken in favour of the earlier entry.
	tie := []*RedirectEntry{
		{Contact: "sip:first@x", Q: 0.5},
		{Contact: "sip:second@x", Q: 0.5},
	}
	if got := m.SelectBest(tie); got.Contact != "sip:first@x" {
		t.Errorf("SelectBest(tie) = %q, want sip:first@x", got.Contact)
	}
}

func TestSortByQ(t *testing.T) {
	m := New()

	entries := []*RedirectEntry{
		{Contact: "sip:a@x", Q: 0.1},
		{Contact: "sip:b@x", Q: 0.9},
		{Contact: "sip:c@x", Q: 0.5},
		{Contact: "sip:d@x", Q: 0.5},
	}
	sorted := m.SortByQ(entries)
	if len(sorted) != 4 {
		t.Fatalf("SortByQ() len = %d, want 4", len(sorted))
	}
	// Descending order.
	if sorted[0].Q != 0.9 || sorted[1].Q != 0.5 || sorted[2].Q != 0.5 || sorted[3].Q != 0.1 {
		t.Errorf("SortByQ() order = %v %v %v %v", sorted[0].Q, sorted[1].Q, sorted[2].Q, sorted[3].Q)
	}
	// Stable: the two q=0.5 entries keep their relative order.
	if sorted[1].Contact != "sip:c@x" || sorted[2].Contact != "sip:d@x" {
		t.Errorf("SortByQ() not stable: %q then %q", sorted[1].Contact, sorted[2].Contact)
	}
	// Original slice is not mutated.
	if entries[0].Contact != "sip:a@x" {
		t.Errorf("SortByQ() mutated the input slice")
	}

	// Empty / nil input.
	if got := m.SortByQ(nil); len(got) != 0 {
		t.Errorf("SortByQ(nil) = %d, want 0", len(got))
	}
}

func TestCount(t *testing.T) {
	m := New()

	if got := m.Count(nil); got != 0 {
		t.Errorf("Count(nil) = %d, want 0", got)
	}
	entries := []*RedirectEntry{{}, {}, {}}
	if got := m.Count(entries); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}
}

func TestProcessRedirectExpires(t *testing.T) {
	m := New()

	msg := buildReply(302, "<sip:bob@192.0.2.1:5060>;q=1.0;expires=3600")
	entries := m.ProcessRedirect(msg)
	if len(entries) != 1 {
		t.Fatalf("ProcessRedirect() = %d, want 1", len(entries))
	}
	if entries[0].Expires != 3600 {
		t.Errorf("Expires = %d, want 3600", entries[0].Expires)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultUACRedirect()
	if d == nil {
		t.Fatalf("DefaultUACRedirect() returned nil")
	}
	if d != DefaultUACRedirect() {
		t.Fatalf("DefaultUACRedirect() returned different instances after Init()")
	}

	// Default module processes a redirect.
	msg := buildReply(302, "<sip:x@x>;q=1.0")
	entries := DefaultUACRedirect().ProcessRedirect(msg)
	if len(entries) != 1 {
		t.Errorf("default ProcessRedirect() = %d, want 1", len(entries))
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultUACRedirect()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			msg := buildReply(302, "<sip:a@x>;q=0.3, <sip:b@x>;q=0.9, <sip:c@x>;q=0.5")
			entries := shared.ProcessRedirect(msg)
			shared.Count(entries)
			shared.SelectBest(entries)
			shared.SortByQ(entries)
			shared.IsRedirect(msg)
			shared.GetRedirectType(msg)
		}()
	}
	wg.Wait()
}

// approxEqual reports whether a and b are equal within a small tolerance,
// used to compare q-values that are parsed as float32 (Kamailio's
// qvalue_t) and then widened to float64.
func approxEqual(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-5
}
