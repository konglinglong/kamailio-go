// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the TMX (TM extensions) module.
 */

package tmx

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// inviteReq is a minimal INVITE used to exercise request-side helpers.
const inviteReq = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"From: Alice <sip:alice@atlanta.com>;tag=1928301774\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

// ringingReply is a 180 Ringing reply used to exercise reply-side helpers.
const ringingReply = "SIP/2.0 180 Ringing\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"To: Bob <sip:bob@biloxi.com>;tag=abc\r\n" +
	"From: Alice <sip:alice@atlanta.com>;tag=1928301774\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

func mustParse(t *testing.T, raw string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

func TestRouteTypePredicates(t *testing.T) {
	m := NewTMXModule()

	// Fresh module has no route context.
	if m.TIsFailureRoute() {
		t.Error("TIsFailureRoute() = true on fresh module, want false")
	}
	if m.TIsBranchRoute() {
		t.Error("TIsBranchRoute() = true on fresh module, want false")
	}
	if m.TIsRequestRoute() {
		t.Error("TIsRequestRoute() = true on fresh module, want false")
	}

	m.SetRouteType(RouteFailure)
	if !m.TIsFailureRoute() {
		t.Error("TIsFailureRoute() = false after SetRouteType(Failure), want true")
	}
	if m.TIsRequestRoute() {
		t.Error("TIsRequestRoute() = true in failure route, want false")
	}

	m.SetRouteType(RouteBranch)
	if !m.TIsBranchRoute() {
		t.Error("TIsBranchRoute() = false after SetRouteType(Branch), want true")
	}
	if m.TIsFailureRoute() {
		t.Error("TIsFailureRoute() = true in branch route, want false")
	}

	m.SetRouteType(RouteRequest)
	if !m.TIsRequestRoute() {
		t.Error("TIsRequestRoute() = false after SetRouteType(Request), want true")
	}

	m.SetRouteType(RouteOnReply)
	if !m.TIsReplyRoute() {
		t.Error("TIsReplyRoute() = false after SetRouteType(OnReply), want true")
	}
}

func TestTReplyCode(t *testing.T) {
	m := NewTMXModule()

	// No reply, no recorded status -> 0.
	if got := m.TReplyCode(nil); got != 0 {
		t.Errorf("TReplyCode(nil) = %d, want 0", got)
	}

	// Request route with a recorded UAS status uses the recorded status.
	m.SetRouteType(RouteRequest)
	m.SetStatus(100)
	req := mustParse(t, inviteReq)
	if got := m.TReplyCode(req); got != 100 {
		t.Errorf("TReplyCode(request) = %d, want 100", got)
	}

	// Onreply route with a reply message uses the reply's own status code.
	m.SetRouteType(RouteOnReply)
	reply := mustParse(t, ringingReply)
	if got := m.TReplyCode(reply); got != 180 {
		t.Errorf("TReplyCode(reply) = %d, want 180", got)
	}

	// A reply outside an explicit onreply route still reports its code.
	m.SetRouteType(RouteFailure)
	if got := m.TReplyCode(reply); got != 180 {
		t.Errorf("TReplyCode(reply in failure route) = %d, want 180", got)
	}
}

func TestTReplyReasonAndRequestMethod(t *testing.T) {
	m := NewTMXModule()

	reply := mustParse(t, ringingReply)
	if got := m.TReplyReason(reply); got != "Ringing" {
		t.Errorf("TReplyReason(reply) = %q, want %q", got, "Ringing")
	}
	// No reply -> empty reason.
	if got := m.TReplyReason(nil); got != "" {
		t.Errorf("TReplyReason(nil) = %q, want empty", got)
	}

	req := mustParse(t, inviteReq)
	if got := m.TRequestMethod(req); got != "INVITE" {
		t.Errorf("TRequestMethod(request) = %q, want %q", got, "INVITE")
	}
	// A reply has no request method.
	if got := m.TRequestMethod(reply); got != "" {
		t.Errorf("TRequestMethod(reply) = %q, want empty", got)
	}
}

func TestBranchIndexCountAndStatus(t *testing.T) {
	m := NewTMXModule()

	// Fresh module reports an undefined branch index.
	if got := m.TBranchIndex(); got != BranchUndefined {
		t.Errorf("TBranchIndex() = %d, want BranchUndefined(%d)", got, BranchUndefined)
	}
	if got := m.TBranchCount(); got != 0 {
		t.Errorf("TBranchCount() = %d, want 0", got)
	}
	if got := m.TGetStatus(); got != 0 {
		t.Errorf("TGetStatus() = %d, want 0", got)
	}

	m.SetBranchIndex(2)
	m.SetBranchCount(3)
	m.SetStatus(200)

	if got := m.TBranchIndex(); got != 2 {
		t.Errorf("TBranchIndex() = %d, want 2", got)
	}
	if got := m.TBranchCount(); got != 3 {
		t.Errorf("TBranchCount() = %d, want 3", got)
	}
	if got := m.TGetStatus(); got != 200 {
		t.Errorf("TGetStatus() = %d, want 200", got)
	}
}

func TestTIsLocal(t *testing.T) {
	m := NewTMXModule()
	req := mustParse(t, inviteReq)

	if m.TIsLocal(req) {
		t.Error("TIsLocal() = true on fresh module, want false")
	}

	m.SetLocal(true)
	if !m.TIsLocal(req) {
		t.Error("TIsLocal() = false after SetLocal(true), want true")
	}

	m.SetLocal(false)
	if m.TIsLocal(req) {
		t.Error("TIsLocal() = true after SetLocal(false), want false")
	}
}

func TestRouteTypeNameAndString(t *testing.T) {
	m := NewTMXModule()

	if got := m.RouteTypeName(); got != "UNDEFINED" {
		t.Errorf("RouteTypeName() = %q, want %q", got, "UNDEFINED")
	}

	m.SetRouteType(RouteFailure)
	if got := m.RouteTypeName(); got != "FAILURE_ROUTE" {
		t.Errorf("RouteTypeName() = %q, want %q", got, "FAILURE_ROUTE")
	}

	m.SetBranchIndex(1)
	m.SetBranchCount(2)
	m.SetStatus(500)
	m.SetLocal(true)
	s := m.String()
	for _, want := range []string{"FAILURE_ROUTE", "branch=1", "count=2", "status=500", "local=true"} {
		if !contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
}

func TestDefaultAndInit(t *testing.T) {
	// DefaultTMX returns a usable instance.
	d := DefaultTMX()
	if d == nil {
		t.Fatal("DefaultTMX() = nil")
	}
	d.SetRouteType(RouteRequest)
	if !d.TIsRequestRoute() {
		t.Error("DefaultTMX did not retain SetRouteType")
	}

	// Init resets the default instance.
	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	d2 := DefaultTMX()
	if d2.TIsRequestRoute() {
		t.Error("DefaultTMX still in request route after Init()")
	}
}

func TestConcurrentAccess(t *testing.T) {
	// Exercise the read/write paths under -race to confirm the mutex
	// protects the route context.
	m := NewTMXModule()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.SetRouteType(RouteType(i % 4))
			_ = m.TIsFailureRoute()
			_ = m.TBranchIndex()
			_ = m.TGetStatus()
			_ = m.String()
		}(i)
	}
	wg.Wait()
}

// contains is a tiny local helper to avoid importing strings.
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
