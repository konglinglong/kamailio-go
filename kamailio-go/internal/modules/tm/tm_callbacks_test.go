// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the TM callback registry (TMCB_*).
 */
package tm

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

func mustParseT(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	return msg
}

// makeInvite builds an INVITE request with the given Call-ID and Via
// branch. The request is suitable for feeding into Manager.HandleRequest.
func makeInvite(t *testing.T, callID, branch string) *parser.SIPMsg {
	t.Helper()
	raw := []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP pc33.example.com;branch=" + branch + "\r\n" +
		"Max-Forwards: 70\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag\r\n" +
		"To: Bob <sip:bob@example.com>\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	return mustParseT(t, raw)
}

// makeReply builds a SIP reply matching the given request's Via branch
// and Call-ID with the supplied status code, reason and To-tag.
func makeReply(t *testing.T, req *parser.SIPMsg, code int, reason, toTag string) *parser.SIPMsg {
	t.Helper()
	branch := ""
	if req != nil && req.Via1 != nil && req.Via1.Branch != nil {
		branch = req.Via1.Branch.Value.String()
	}
	callID := ""
	if req != nil && req.CallID != nil {
		callID = req.CallID.Body.String()
	}
	raw := []byte("SIP/2.0 " + strconv.Itoa(code) + " " + reason + "\r\n" +
		"Via: SIP/2.0/UDP pc33.example.com;branch=" + branch + "\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag\r\n" +
		"To: Bob <sip:bob@example.com>;tag=" + toTag + "\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	return mustParseT(t, raw)
}

// noopCB is a callback that does nothing - useful for registration
// accounting tests where the body doesn't matter.
func noopCB(*Cell, int, *parser.SIPMsg, interface{}) {}

// --- Registry unit tests (no Manager needed) ---

func TestTMCBType_String(t *testing.T) {
	cases := map[TMCBType]string{
		0:                                              "none",
		TMCBOnReply:                                    "on_reply",
		TMCBOnFailure:                                  "on_failure",
		TMCBOnReply | TMCBOnFailure:                    "on_reply|on_failure",
		TMCBRequestIn | TMCBRequestOut | TMCBOnBranch:  "request_in|request_out|on_branch",
	}
	for mask, want := range cases {
		if got := mask.String(); got != want {
			t.Errorf("mask=%x: got %q, want %q", mask, got, want)
		}
	}
}

func TestCallbackRegistry_RegisterUnregister(t *testing.T) {
	r := NewCallbackRegistry()
	if r.Count() != 0 {
		t.Fatalf("initial Count = %d, want 0", r.Count())
	}
	h1 := r.Register(TMCBOnReply, noopCB, nil)
	h2 := r.Register(TMCBOnFailure, noopCB, nil)
	if h1 == h2 {
		t.Fatal("handles should be distinct")
	}
	if r.Count() != 2 {
		t.Errorf("after 2 registers Count = %d, want 2", r.Count())
	}
	r.Unregister(h1)
	if r.Count() != 1 {
		t.Errorf("after unregister h1 Count = %d, want 1", r.Count())
	}
	// Unregistering twice is a no-op.
	r.Unregister(h1)
	if r.Count() != 1 {
		t.Errorf("after double unregister Count = %d, want 1", r.Count())
	}
	// Unknown handle is a no-op.
	r.Unregister(999)
	if r.Count() != 1 {
		t.Errorf("after unknown unregister Count = %d, want 1", r.Count())
	}
	r.Unregister(h2)
	if r.Count() != 0 {
		t.Errorf("after unregister h2 Count = %d, want 0", r.Count())
	}
}

func TestCallbackRegistry_RegisterPanicsOnNil(t *testing.T) {
	r := NewCallbackRegistry()
	defer func() {
		if recover() == nil {
			t.Error("expected panic on nil callback")
		}
	}()
	r.Register(TMCBOnReply, nil, nil)
}

func TestCallbackRegistry_RegisterPanicsOnEmptyMask(t *testing.T) {
	r := NewCallbackRegistry()
	defer func() {
		if recover() == nil {
			t.Error("expected panic on empty mask")
		}
	}()
	r.Register(0, noopCB, nil)
}

func TestCallbackRegistry_HasType(t *testing.T) {
	r := NewCallbackRegistry()
	if r.HasType(TMCBOnReply) {
		t.Error("empty registry should not have any type")
	}
	r.Register(TMCBOnReply|TMCBOnFailure, noopCB, nil)
	if !r.HasType(TMCBOnReply) {
		t.Error("expected HasType(OnReply) true")
	}
	if !r.HasType(TMCBOnFailure) {
		t.Error("expected HasType(OnFailure) true")
	}
	if r.HasType(TMCBRequestIn) {
		t.Error("expected HasType(RequestIn) false")
	}
}

func TestCallbackRegistry_InvokeFiltersByMask(t *testing.T) {
	r := NewCallbackRegistry()
	var fired int32
	r.Register(TMCBOnReply, func(*Cell, int, *parser.SIPMsg, interface{}) {
		atomic.AddInt32(&fired, 1)
	}, nil)
	r.Register(TMCBOnFailure, func(*Cell, int, *parser.SIPMsg, interface{}) {
		atomic.AddInt32(&fired, 10)
	}, nil)
	r.Register(TMCBOnReply|TMCBOnFailure, func(*Cell, int, *parser.SIPMsg, interface{}) {
		atomic.AddInt32(&fired, 100)
	}, nil)

	// Fire OnReply: should hit callbacks 1 and 3 (not 2).
	r.Invoke(TMCBOnReply, nil, -1, nil)
	if got := atomic.LoadInt32(&fired); got != 101 {
		t.Errorf("OnReply fire: fired=%d, want 101", got)
	}

	// Fire OnFailure: should hit callbacks 2 and 3.
	atomic.StoreInt32(&fired, 0)
	r.Invoke(TMCBOnFailure, nil, -1, nil)
	if got := atomic.LoadInt32(&fired); got != 110 {
		t.Errorf("OnFailure fire: fired=%d, want 110", got)
	}

	// Fire RequestIn: nobody is interested.
	atomic.StoreInt32(&fired, 0)
	r.Invoke(TMCBRequestIn, nil, -1, nil)
	if got := atomic.LoadInt32(&fired); got != 0 {
		t.Errorf("RequestIn fire: fired=%d, want 0", got)
	}
}

func TestCallbackRegistry_InvokePassesArgs(t *testing.T) {
	r := NewCallbackRegistry()
	cell := &Cell{CallIDVal: str.Mk("call-x")}
	r.Register(TMCBOnReply, func(c *Cell, b int, m *parser.SIPMsg, d interface{}) {
		if c != cell {
			t.Error("cell mismatch")
		}
		if b != 7 {
			t.Errorf("branch = %d, want 7", b)
		}
		if d != "payload" {
			t.Errorf("data = %v, want payload", d)
		}
	}, "payload")
	r.Invoke(TMCBOnReply, cell, 7, nil)
}

func TestCallbackRegistry_InvokeRecoversPanic(t *testing.T) {
	r := NewCallbackRegistry()
	r.Register(TMCBOnReply, func(*Cell, int, *parser.SIPMsg, interface{}) {
		panic("boom")
	}, nil)
	// A second callback should still run after the panicking one.
	var secondRan bool
	r.Register(TMCBOnReply, func(*Cell, int, *parser.SIPMsg, interface{}) {
		secondRan = true
	}, nil)
	// Must not panic.
	r.Invoke(TMCBOnReply, nil, -1, nil)
	if !secondRan {
		t.Error("second callback should still run after first panicked")
	}
}

func TestCallbackRegistry_InvokeNoLockHeld(t *testing.T) {
	// A callback that re-registers must not deadlock (the registry
	// lock is released before user callbacks run).
	r := NewCallbackRegistry()
	done := make(chan struct{})
	r.Register(TMCBOnReply, func(*Cell, int, *parser.SIPMsg, interface{}) {
		r.Register(TMCBOnFailure, func(*Cell, int, *parser.SIPMsg, interface{}) {
			close(done)
		}, nil)
		r.Invoke(TMCBOnFailure, nil, -1, nil)
	}, nil)
	r.Invoke(TMCBOnReply, nil, -1, nil)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("re-entrant register/invoke timed out")
	}
}

// --- Manager integration tests ---

func TestManager_NoRegistryAllocatedLazily(t *testing.T) {
	mgr := NewManager(64)
	// No registry until first access.
	mgr.mutex.RLock()
	pre := mgr.tmcb
	mgr.mutex.RUnlock()
	if pre != nil {
		t.Error("registry should not be allocated until first access")
	}
	// Accessing it allocates.
	reg := mgr.TMCallbackRegistry()
	if reg == nil {
		t.Fatal("registry should be non-nil after TMCallbackRegistry()")
	}
	// Subsequent access returns the same instance.
	if mgr.TMCallbackRegistry() != reg {
		t.Error("registry should be stable across accesses")
	}
}

func TestManager_RegisterTMCallback(t *testing.T) {
	mgr := NewManager(64)
	var fired int32
	h := mgr.RegisterTMCallback(TMCBOnReply, func(*Cell, int, *parser.SIPMsg, interface{}) {
		atomic.AddInt32(&fired, 1)
	}, nil)
	if h <= 0 {
		t.Fatalf("handle = %d, want > 0", h)
	}
	mgr.invokeTMCBs(TMCBOnReply, nil, -1, nil)
	if got := atomic.LoadInt32(&fired); got != 1 {
		t.Errorf("fired = %d, want 1", got)
	}
	mgr.UnregisterTMCallback(h)
	atomic.StoreInt32(&fired, 0)
	mgr.invokeTMCBs(TMCBOnReply, nil, -1, nil)
	if got := atomic.LoadInt32(&fired); got != 0 {
		t.Errorf("after unregister fired = %d, want 0", got)
	}
}

func TestManager_InvokeNoOpWhenNoRegistry(t *testing.T) {
	// A fresh Manager has no registry; invokeTMCBs must be a no-op
	// without panicking.
	mgr := NewManager(64)
	mgr.invokeTMCBs(TMCBOnReply, nil, -1, nil)
}

func TestManager_HandleRequestFiresRequestIn(t *testing.T) {
	mgr := NewManagerWithTimers(64)
	var got atomic.Value // *Cell
	mgr.RegisterTMCallback(TMCBRequestIn, func(cell *Cell, branch int, msg *parser.SIPMsg, data interface{}) {
		if branch != -1 {
			t.Errorf("branch = %d, want -1", branch)
		}
		if data != "ctx" {
			t.Errorf("data = %v, want ctx", data)
		}
		got.Store(cell)
	}, "ctx")

	req := makeInvite(t, "call-reqin", "z9hG4bK-reqin")
	cell, err := mgr.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	c := got.Load()
	if c == nil {
		t.Fatal("TMCBRequestIn did not fire")
	}
	if c.(*Cell) != cell {
		t.Error("callback received wrong cell")
	}
}

func TestManager_HandleResponseFiresReplyAndFailure(t *testing.T) {
	mgr := NewManagerWithTimers(64)
	req := makeInvite(t, "call-rep", "z9hG4bK-rep")
	cell, err := mgr.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if _, err := mgr.AddBranch(cell, 0); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}

	var mu sync.Mutex
	var replies []int
	var failures []int
	var branchFailures []int
	var responsesIn []int
	mgr.RegisterTMCallback(TMCBOnReply, func(c *Cell, b int, m *parser.SIPMsg, d interface{}) {
		mu.Lock()
		defer mu.Unlock()
		replies = append(replies, int(m.StatusCode()))
	}, nil)
	mgr.RegisterTMCallback(TMCBOnFailure, func(c *Cell, b int, m *parser.SIPMsg, d interface{}) {
		mu.Lock()
		defer mu.Unlock()
		failures = append(failures, int(m.StatusCode()))
	}, nil)
	mgr.RegisterTMCallback(TMCBOnBranchFailure, func(c *Cell, b int, m *parser.SIPMsg, d interface{}) {
		mu.Lock()
		defer mu.Unlock()
		branchFailures = append(branchFailures, int(m.StatusCode()))
	}, nil)
	mgr.RegisterTMCallback(TMCBResponseIn, func(c *Cell, b int, m *parser.SIPMsg, d interface{}) {
		mu.Lock()
		defer mu.Unlock()
		responsesIn = append(responsesIn, int(m.StatusCode()))
	}, nil)

	// 180 provisional: OnReply + ResponseIn only.
	if _, _, err := mgr.HandleResponse(makeReply(t, req, 180, "Ringing", "ttag1")); err != nil {
		t.Fatalf("HandleResponse(180): %v", err)
	}
	mu.Lock()
	if len(replies) != 1 || replies[0] != 180 {
		t.Errorf("after 180: replies=%v, want [180]", replies)
	}
	if len(responsesIn) != 1 {
		t.Errorf("after 180: responsesIn=%v, want one", responsesIn)
	}
	if len(failures) != 0 || len(branchFailures) != 0 {
		t.Errorf("after 180: failures/branchFailures should be empty: %v / %v", failures, branchFailures)
	}
	mu.Unlock()

	// 500 final non-2xx: OnReply + ResponseIn + OnFailure + OnBranchFailure.
	if _, _, err := mgr.HandleResponse(makeReply(t, req, 500, "Server Error", "ttag1")); err != nil {
		t.Fatalf("HandleResponse(500): %v", err)
	}
	mu.Lock()
	if len(replies) != 2 {
		t.Errorf("after 500: replies=%v, want two", replies)
	}
	if len(failures) != 1 || failures[0] != 500 {
		t.Errorf("after 500: failures=%v, want [500]", failures)
	}
	if len(branchFailures) != 1 || branchFailures[0] != 500 {
		t.Errorf("after 500: branchFailures=%v, want [500]", branchFailures)
	}
	if len(responsesIn) != 2 {
		t.Errorf("after 500: responsesIn=%v, want two", responsesIn)
	}
	mu.Unlock()
}

func TestManager_HandleResponse2xxFiresReplyOnly(t *testing.T) {
	mgr := NewManagerWithTimers(64)
	req := makeInvite(t, "call-2xx", "z9hG4bK-2xx")
	cell, err := mgr.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if _, err := mgr.AddBranch(cell, 0); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}

	var failures int32
	var replies int32
	mgr.RegisterTMCallback(TMCBOnFailure, func(*Cell, int, *parser.SIPMsg, interface{}) {
		atomic.AddInt32(&failures, 1)
	}, nil)
	mgr.RegisterTMCallback(TMCBOnReply, func(*Cell, int, *parser.SIPMsg, interface{}) {
		atomic.AddInt32(&replies, 1)
	}, nil)

	if _, _, err := mgr.HandleResponse(makeReply(t, req, 200, "OK", "ttag2")); err != nil {
		t.Fatalf("HandleResponse(200): %v", err)
	}
	if got := atomic.LoadInt32(&failures); got != 0 {
		t.Errorf("OnFailure fired %d times for 200, want 0", got)
	}
	if got := atomic.LoadInt32(&replies); got != 1 {
		t.Errorf("OnReply fired %d times for 200, want 1", got)
	}
}

func TestManager_RelayRequestFiresOnBranchAndRequestOut(t *testing.T) {
	mgr := NewManagerWithTimers(64)
	req := makeInvite(t, "call-relay", "z9hG4bK-relay")
	if _, err := mgr.HandleRequest(req); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	var onBranchBranch int32 = -99
	var requestOutBranch int32 = -99
	mgr.RegisterTMCallback(TMCBOnBranch, func(c *Cell, b int, m *parser.SIPMsg, d interface{}) {
		atomic.StoreInt32(&onBranchBranch, int32(b))
	}, nil)
	mgr.RegisterTMCallback(TMCBRequestOut, func(c *Cell, b int, m *parser.SIPMsg, d interface{}) {
		atomic.StoreInt32(&requestOutBranch, int32(b))
	}, nil)

	_, branch, err := mgr.RelayRequest(req, "10.0.0.2", 5060)
	if err != nil {
		t.Fatalf("RelayRequest: %v", err)
	}
	if branch < 0 {
		t.Fatalf("branch = %d, want >= 0", branch)
	}
	if got := atomic.LoadInt32(&onBranchBranch); got != int32(branch) {
		t.Errorf("OnBranch branch = %d, want %d", got, branch)
	}
	if got := atomic.LoadInt32(&requestOutBranch); got != int32(branch) {
		t.Errorf("RequestOut branch = %d, want %d", got, branch)
	}
}

func TestManager_RelayRequestOrderBranchBeforeRequestOut(t *testing.T) {
	mgr := NewManagerWithTimers(64)
	req := makeInvite(t, "call-order", "z9hG4bK-order")
	if _, err := mgr.HandleRequest(req); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	var order []string
	var mu sync.Mutex
	mgr.RegisterTMCallback(TMCBOnBranch, func(*Cell, int, *parser.SIPMsg, interface{}) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "on_branch")
	}, nil)
	mgr.RegisterTMCallback(TMCBRequestOut, func(*Cell, int, *parser.SIPMsg, interface{}) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "request_out")
	}, nil)

	if _, _, err := mgr.RelayRequest(req, "10.0.0.3", 5060); err != nil {
		t.Fatalf("RelayRequest: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "on_branch" || order[1] != "request_out" {
		t.Errorf("order = %v, want [on_branch request_out]", order)
	}
}

func TestManager_CallbacksCoexistWithRegistry(t *testing.T) {
	// The legacy single-slot RouteCallbacks and the new registry must
	// both fire.
	mgr := NewManagerWithTimers(64)
	req := makeInvite(t, "call-coexist", "z9hG4bK-coexist")
	cell, err := mgr.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if _, err := mgr.AddBranch(cell, 0); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}

	var legacy, registry int32
	mgr.SetCallbacks(RouteCallbacks{
		OnReply: func(*Cell, int, *parser.SIPMsg) {
			atomic.AddInt32(&legacy, 1)
		},
	})
	mgr.RegisterTMCallback(TMCBOnReply, func(*Cell, int, *parser.SIPMsg, interface{}) {
		atomic.AddInt32(&registry, 1)
	}, nil)

	if _, _, err := mgr.HandleResponse(makeReply(t, req, 180, "Ringing", "t")); err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if got := atomic.LoadInt32(&legacy); got != 1 {
		t.Errorf("legacy OnReply fired %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&registry); got != 1 {
		t.Errorf("registry OnReply fired %d times, want 1", got)
	}
}
