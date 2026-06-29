// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Record-Route (rr) module.
 */

package rr

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// addHeader appends a header to msg and wires up the matching quick-access
// pointer, mirroring what the full parser does. This keeps the tests free of
// any dependency on the wire-format parser while still exercising the same
// code paths production code uses.
func addHeader(msg *parser.SIPMsg, name, value string) *parser.HdrField {
	h := msg.AddHeader(name, value)
	switch h.Type {
	case parser.HdrFrom:
		msg.From = h
	case parser.HdrTo:
		msg.To = h
	case parser.HdrRoute:
		if msg.Route == nil {
			msg.Route = h
		}
	case parser.HdrRecordRoute:
		if msg.RecordRoute == nil {
			msg.RecordRoute = h
		}
	}
	return h
}

func TestRecordRoute(t *testing.T) {
	rr := NewRRModule()
	msg := &parser.SIPMsg{}

	params := &RecordRouteParams{
		Username:     "proxy",
		Domain:       "example.com",
		Port:         "5060",
		Transport:    "udp",
		LR:           true,
		CustomParams: "ftag=abc",
	}
	val := rr.RecordRoute(msg, params)

	// URI params (lr, transport, custom) are emitted as rr-params after '>'.
	want := "<sip:proxy@example.com:5060>;lr;transport=udp;ftag=abc"
	if val != want {
		t.Fatalf("RecordRoute() = %q, want %q", val, want)
	}

	rrs := msg.GetAllHeadersByType(parser.HdrRecordRoute)
	if len(rrs) != 1 {
		t.Fatalf("expected 1 Record-Route header, got %d", len(rrs))
	}
	if rrs[0].Body.String() != want {
		t.Errorf("header body = %q, want %q", rrs[0].Body.String(), want)
	}
	if msg.RecordRoute == nil || msg.RecordRoute != rrs[0] {
		t.Errorf("msg.RecordRoute quick ref not wired to the new header")
	}
	if got := rr.Stats().RecordRoutesAdded.Load(); got != 1 {
		t.Errorf("RecordRoutesAdded = %d, want 1", got)
	}

	// nil params fall back to defaults (localhost, loose routing).
	msg2 := &parser.SIPMsg{}
	val2 := rr.RecordRoute(msg2, nil)
	if val2 != "<sip:localhost>;lr" {
		t.Errorf("RecordRoute(nil) = %q, want %q", val2, "<sip:localhost>;lr")
	}
	if got := rr.Stats().RecordRoutesAdded.Load(); got != 2 {
		t.Errorf("RecordRoutesAdded = %d, want 2", got)
	}

	// nil message is a no-op.
	if rr.RecordRoute(nil, params) != "" {
		t.Errorf("RecordRoute(nil msg) should return empty string")
	}
}

func TestRecordRoutePreset(t *testing.T) {
	rr := NewRRModule()
	msg := &parser.SIPMsg{}

	// Bare URI gets wrapped in angle brackets.
	val := rr.RecordRoutePreset(msg, "sip:proxy.example.com:5061;lr")
	want := "<sip:proxy.example.com:5061;lr>"
	if val != want {
		t.Fatalf("RecordRoutePreset() = %q, want %q", val, want)
	}

	rrs := msg.GetAllHeadersByType(parser.HdrRecordRoute)
	if len(rrs) != 1 {
		t.Fatalf("expected 1 Record-Route header, got %d", len(rrs))
	}
	if rrs[0].Body.String() != want {
		t.Errorf("header body = %q, want %q", rrs[0].Body.String(), want)
	}
	if got := rr.Stats().RecordRoutesAdded.Load(); got != 1 {
		t.Errorf("RecordRoutesAdded = %d, want 1", got)
	}

	// Already bracketed URI is left untouched.
	msg2 := &parser.SIPMsg{}
	val2 := rr.RecordRoutePreset(msg2, "<sip:other.example.com;lr>")
	if val2 != "<sip:other.example.com;lr>" {
		t.Errorf("RecordRoutePreset(bracketed) = %q, want unchanged", val2)
	}

	// Empty/nil inputs are no-ops.
	if rr.RecordRoutePreset(&parser.SIPMsg{}, "") != "" {
		t.Errorf("RecordRoutePreset(empty) should return empty string")
	}
	if rr.RecordRoutePreset(nil, "sip:x") != "" {
		t.Errorf("RecordRoutePreset(nil msg) should return empty string")
	}
}

func TestLooseRoute(t *testing.T) {
	rr := NewRRModule()

	// Route with ;lr -> loose routing.
	looseMsg := &parser.SIPMsg{}
	addHeader(looseMsg, "Route", "<sip:proxy.example.com>;lr")
	isLoose, uri := rr.LooseRoute(looseMsg)
	if !isLoose {
		t.Errorf("LooseRoute() isLoose = false, want true for ;lr route")
	}
	if uri != "<sip:proxy.example.com>;lr" {
		t.Errorf("LooseRoute() uri = %q, want %q", uri, "<sip:proxy.example.com>;lr")
	}
	if got := rr.Stats().LooseRoutesDetected.Load(); got != 1 {
		t.Errorf("LooseRoutesDetected = %d, want 1", got)
	}

	// Route without ;lr -> strict routing.
	strictMsg := &parser.SIPMsg{}
	addHeader(strictMsg, "Route", "<sip:proxy.example.com>")
	isLoose2, uri2 := rr.LooseRoute(strictMsg)
	if isLoose2 {
		t.Errorf("LooseRoute() isLoose = true, want false for strict route")
	}
	if uri2 != "<sip:proxy.example.com>" {
		t.Errorf("LooseRoute() uri = %q, want %q", uri2, "<sip:proxy.example.com>")
	}
	// Strict routing must not bump the loose-route counter.
	if got := rr.Stats().LooseRoutesDetected.Load(); got != 1 {
		t.Errorf("LooseRoutesDetected = %d, want 1", got)
	}

	// No Route header at all.
	noneMsg := &parser.SIPMsg{}
	isLoose3, uri3 := rr.LooseRoute(noneMsg)
	if isLoose3 {
		t.Errorf("LooseRoute() isLoose = true, want false with no Route header")
	}
	if uri3 != "" {
		t.Errorf("LooseRoute() uri = %q, want empty", uri3)
	}

	// nil message.
	if isLoose, _ := rr.LooseRoute(nil); isLoose {
		t.Errorf("LooseRoute(nil) should return false")
	}

	// Package-level wrapper delegates to the default module.
	Init()
	gLoose, gURI := LooseRoute(looseMsg)
	if !gLoose {
		t.Errorf("package LooseRoute() = false, want true")
	}
	if gURI != "<sip:proxy.example.com>;lr" {
		t.Errorf("package LooseRoute() uri = %q, want %q", gURI, "<sip:proxy.example.com>;lr")
	}
}

func TestRemoveRecordRoute(t *testing.T) {
	rr := NewRRModule()

	// Two Record-Route headers.
	msg := &parser.SIPMsg{}
	addHeader(msg, "Record-Route", "<sip:a.example.com>;lr")
	addHeader(msg, "Record-Route", "<sip:b.example.com>;lr")
	if msg.RecordRoute == nil {
		t.Fatalf("RecordRoute quick ref should be set before removal")
	}

	n := rr.RemoveRecordRoute(msg)
	if n != 2 {
		t.Fatalf("RemoveRecordRoute() = %d, want 2", n)
	}
	if got := msg.CountHeadersByType(parser.HdrRecordRoute); got != 0 {
		t.Errorf("remaining Record-Route headers = %d, want 0", got)
	}
	if msg.RecordRoute != nil {
		t.Errorf("msg.RecordRoute quick ref should be cleared after removal")
	}

	// No Record-Route headers -> 0 removed.
	emptyMsg := &parser.SIPMsg{}
	if n := rr.RemoveRecordRoute(emptyMsg); n != 0 {
		t.Errorf("RemoveRecordRoute() on empty msg = %d, want 0", n)
	}

	// nil message.
	if n := rr.RemoveRecordRoute(nil); n != 0 {
		t.Errorf("RemoveRecordRoute(nil) = %d, want 0", n)
	}
}

func TestAddRRParam(t *testing.T) {
	rr := NewRRModule()

	msg := &parser.SIPMsg{}
	addHeader(msg, "Record-Route", "<sip:proxy.example.com>;lr")

	if ret := rr.AddRRParam(msg, "ftag=abc123"); ret != 0 {
		t.Fatalf("AddRRParam() = %d, want 0", ret)
	}

	rrs := msg.GetAllHeadersByType(parser.HdrRecordRoute)
	if len(rrs) != 1 {
		t.Fatalf("expected 1 Record-Route header, got %d", len(rrs))
	}
	want := "<sip:proxy.example.com>;lr;ftag=abc123"
	if got := rrs[0].Body.String(); got != want {
		t.Errorf("after AddRRParam body = %q, want %q", got, want)
	}

	// The appended parameter is discoverable via CheckRouteParam.
	if !rr.CheckRouteParam(msg, "ftag=abc123") {
		t.Errorf("CheckRouteParam(ftag=abc123) = false, want true")
	}
	if !rr.CheckRouteParam(msg, "ftag") {
		t.Errorf("CheckRouteParam(ftag) = false, want true")
	}

	// Appending to the last header when several are present.
	msg2 := &parser.SIPMsg{}
	addHeader(msg2, "Record-Route", "<sip:first.example.com>;lr")
	addHeader(msg2, "Record-Route", "<sip:second.example.com>;lr")
	if ret := rr.AddRRParam(msg2, "r2=on"); ret != 0 {
		t.Fatalf("AddRRParam() = %d, want 0", ret)
	}
	all := msg2.GetAllHeadersByType(parser.HdrRecordRoute)
	if got := all[1].Body.String(); got != "<sip:second.example.com>;lr;r2=on" {
		t.Errorf("second header body = %q, want %q", got, "<sip:second.example.com>;lr;r2=on")
	}
	if got := all[0].Body.String(); got != "<sip:first.example.com>;lr" {
		t.Errorf("first header should be unchanged, got %q", got)
	}

	// No Record-Route header -> -1.
	if ret := rr.AddRRParam(&parser.SIPMsg{}, "x=y"); ret != -1 {
		t.Errorf("AddRRParam() without RR = %d, want -1", ret)
	}
	// Empty param -> -1.
	if ret := rr.AddRRParam(msg, ""); ret != -1 {
		t.Errorf("AddRRParam(empty) = %d, want -1", ret)
	}
	// nil message -> -1.
	if ret := rr.AddRRParam(nil, "x=y"); ret != -1 {
		t.Errorf("AddRRParam(nil) = %d, want -1", ret)
	}
}

func TestIsDirection(t *testing.T) {
	rr := NewRRModule()

	// Downstream: From tag matches the ftag stored on Record-Route.
	downMsg := &parser.SIPMsg{}
	addHeader(downMsg, "From", "<sip:alice@example.com>;tag=abc123")
	addHeader(downMsg, "Record-Route", "<sip:proxy.example.com>;lr;ftag=abc123")

	if !rr.IsDirection(downMsg, "downstream") {
		t.Errorf("IsDirection(downstream) = false, want true (tags match)")
	}
	if rr.IsDirection(downMsg, "upstream") {
		t.Errorf("IsDirection(upstream) = true, want false (tags match)")
	}

	// Upstream: From tag differs from the stored ftag.
	upMsg := &parser.SIPMsg{}
	addHeader(upMsg, "From", "<sip:bob@example.com>;tag=xyz789")
	addHeader(upMsg, "Record-Route", "<sip:proxy.example.com>;lr;ftag=abc123")

	if rr.IsDirection(upMsg, "downstream") {
		t.Errorf("IsDirection(downstream) = true, want false (tags differ)")
	}
	if !rr.IsDirection(upMsg, "upstream") {
		t.Errorf("IsDirection(upstream) = false, want true (tags differ)")
	}

	// Case-insensitive direction argument.
	if !rr.IsDirection(downMsg, "Downstream") {
		t.Errorf("IsDirection(Downstream) = false, want true (case-insensitive)")
	}
	if !rr.IsDirection(upMsg, "UPSTREAM") {
		t.Errorf("IsDirection(UPSTREAM) = false, want true (case-insensitive)")
	}

	// Unknown direction -> false.
	if rr.IsDirection(downMsg, "sideways") {
		t.Errorf("IsDirection(sideways) = true, want false")
	}

	// No ftag on Record-Route -> cannot determine direction.
	noFtagMsg := &parser.SIPMsg{}
	addHeader(noFtagMsg, "From", "<sip:alice@example.com>;tag=abc123")
	addHeader(noFtagMsg, "Record-Route", "<sip:proxy.example.com>;lr")
	if rr.IsDirection(noFtagMsg, "downstream") {
		t.Errorf("IsDirection(downstream) without ftag = true, want false")
	}
	if rr.IsDirection(noFtagMsg, "upstream") {
		t.Errorf("IsDirection(upstream) without ftag = true, want false")
	}

	// No Record-Route at all -> false.
	noRRMsg := &parser.SIPMsg{}
	addHeader(noRRMsg, "From", "<sip:alice@example.com>;tag=abc123")
	if rr.IsDirection(noRRMsg, "downstream") {
		t.Errorf("IsDirection without Record-Route = true, want false")
	}

	// nil message.
	if rr.IsDirection(nil, "downstream") {
		t.Errorf("IsDirection(nil) = true, want false")
	}
}

func TestStats(t *testing.T) {
	// Reset the process-wide singleton so the test is deterministic.
	Init()
	rr := NewRRModule()

	msg := &parser.SIPMsg{}
	rr.RecordRoute(msg, &RecordRouteParams{Domain: "a.example.com", LR: true})
	rr.RecordRoute(msg, &RecordRouteParams{Domain: "b.example.com", LR: true})

	stats := rr.Stats()
	if got := stats.RecordRoutesAdded.Load(); got != 2 {
		t.Errorf("RecordRoutesAdded = %d, want 2", got)
	}
	if got := stats.LooseRoutesDetected.Load(); got != 0 {
		t.Errorf("LooseRoutesDetected = %d, want 0", got)
	}

	// Loose-route detection bumps the second counter.
	looseMsg := &parser.SIPMsg{}
	addHeader(looseMsg, "Route", "<sip:proxy.example.com>;lr")
	rr.LooseRoute(looseMsg)
	if got := rr.Stats().LooseRoutesDetected.Load(); got != 1 {
		t.Errorf("LooseRoutesDetected = %d, want 1", got)
	}

	// The returned pointer aliases the live counters.
	stats.RecordRoutesAdded.Add(0) // no-op, just confirms aliasing is usable
	if got := rr.Stats().RecordRoutesAdded.Load(); got != 2 {
		t.Errorf("aliased stats RecordRoutesAdded = %d, want 2", got)
	}

	// Global functions operate on the process-wide singleton.
	Init()
	g := DefaultRR()
	if g == nil {
		t.Fatalf("DefaultRR() returned nil")
	}
	if g != DefaultRR() {
		t.Fatalf("DefaultRR() returned different instances after Init()")
	}
	val := RecordRoute(&parser.SIPMsg{}, &RecordRouteParams{Domain: "g.example.com", LR: true})
	if val != "<sip:g.example.com>;lr" {
		t.Errorf("package RecordRoute() = %q, want %q", val, "<sip:g.example.com>;lr")
	}
	if got := DefaultRR().Stats().RecordRoutesAdded.Load(); got != 1 {
		t.Errorf("global RecordRoutesAdded = %d, want 1", got)
	}

	// Exercise the atomic counters under concurrency to validate -race safety.
	Init()
	shared := DefaultRR()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			m := &parser.SIPMsg{}
			shared.RecordRoute(m, &RecordRouteParams{
				Domain:    fmt.Sprintf("h%d.example.com", i),
				LR:        true,
				Transport: "udp",
			})
			lm := &parser.SIPMsg{}
			addHeader(lm, "Route", "<sip:proxy.example.com>;lr")
			shared.LooseRoute(lm)
		}(i)
	}
	wg.Wait()
	if got := shared.Stats().RecordRoutesAdded.Load(); got != goroutines {
		t.Errorf("concurrent RecordRoutesAdded = %d, want %d", got, goroutines)
	}
	if got := shared.Stats().LooseRoutesDetected.Load(); got != goroutines {
		t.Errorf("concurrent LooseRoutesDetected = %d, want %d", got, goroutines)
	}
}
