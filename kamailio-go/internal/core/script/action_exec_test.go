// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for extended action executor
 */

package script

import (
	"net"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

func TestDoActionForward(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActForward, Arg: "sip:new@host"}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.DstURI != "sip:new@host" {
		t.Errorf("expected DstURI 'sip:new@host', got '%s'", execCtx.DstURI)
	}
}

func TestDoActionDrop(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActDrop}
	ret := DoAction(ctx, a, nil, execCtx)
	if ret != 0 {
		t.Errorf("expected ret 0 for drop, got %d", ret)
	}
	if !execCtx.Drop {
		t.Error("expected Drop=true")
	}
}

func TestDoActionExit(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActExit}
	DoAction(ctx, a, nil, execCtx)
	if ctx.RunFlags&ExitRF == 0 {
		t.Error("expected ExitRF set")
	}
}

func TestDoActionSetFlag(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActSetFlag, ArgNum: 3}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.Flags&(1<<3) == 0 {
		t.Error("expected flag 3 set")
	}
}

func TestDoActionResetFlag(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	execCtx.Flags = 1 << 5
	a := &Action{Type: ActResetFlag, ArgNum: 5}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.Flags&(1<<5) != 0 {
		t.Error("expected flag 5 reset")
	}
}

func TestDoActionIsFlagSet(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	execCtx.Flags = 1 << 2
	a := &Action{Type: ActIsFlagSet, ArgNum: 2}
	DoAction(ctx, a, nil, execCtx)
	if ctx.LastRetCode != 1 {
		t.Error("expected retcode 1 for set flag")
	}

	a2 := &Action{Type: ActIsFlagSet, ArgNum: 3}
	DoAction(ctx, a2, nil, execCtx)
	if ctx.LastRetCode != -1 {
		t.Error("expected retcode -1 for unset flag")
	}
}

func TestDoActionSetHost(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	execCtx.RURI = "sip:alice@old.com"
	a := &Action{Type: ActSetHost, Arg: "new.com"}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.RURI != "sip:alice@new.com" {
		t.Errorf("expected 'sip:alice@new.com', got '%s'", execCtx.RURI)
	}
}

func TestDoActionSetUser(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	execCtx.RURI = "sip:old@host.com"
	a := &Action{Type: ActSetUser, Arg: "newuser"}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.RURI != "sip:newuser@host.com" {
		t.Errorf("expected 'sip:newuser@host.com', got '%s'", execCtx.RURI)
	}
}

func TestDoActionPrefix(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	execCtx.RURI = "sip:alice@host.com"
	a := &Action{Type: ActPrefix, Arg: "00"}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.RURI != "sip:00alice@host.com" {
		t.Errorf("expected 'sip:00alice@host.com', got '%s'", execCtx.RURI)
	}
}

func TestDoActionStrip(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	execCtx.RURI = "sip:00alice@host.com"
	a := &Action{Type: ActStrip, ArgNum: 2}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.RURI != "sip:alice@host.com" {
		t.Errorf("expected 'sip:alice@host.com', got '%s'", execCtx.RURI)
	}
}

func TestDoActionStripTail(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	execCtx.RURI = "sip:alice99@host.com"
	a := &Action{Type: ActStripTail, ArgNum: 2}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.RURI != "sip:alice@host.com" {
		t.Errorf("expected 'sip:alice@host.com', got '%s'", execCtx.RURI)
	}
}

func TestDoActionRevertURI(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type: parser.MsgRequest,
		Req: &parser.RequestLine{
			URI: str.Mk("sip:original@host.com"),
		},
	}
	execCtx := NewExecContext(msg, nil, "")
	execCtx.RURI = "sip:changed@other.com"
	a := &Action{Type: ActRevertURI}
	DoAction(ctx, a, msg, execCtx)
	if execCtx.RURI != "sip:original@host.com" {
		t.Errorf("expected 'sip:original@host.com', got '%s'", execCtx.RURI)
	}
}

func TestDoActionAppendBranch(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActAppendBranch, Arg: "sip:bob@host.com"}
	DoAction(ctx, a, nil, execCtx)
	if len(execCtx.Branches) != 1 || execCtx.Branches[0] != "sip:bob@host.com" {
		t.Error("append_branch failed")
	}
}

func TestDoActionRemoveBranch(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	execCtx.Branches = []string{"sip:a@h", "sip:b@h", "sip:c@h"}
	a := &Action{Type: ActRemoveBranch, ArgNum: 1}
	DoAction(ctx, a, nil, execCtx)
	if len(execCtx.Branches) != 2 || execCtx.Branches[0] != "sip:a@h" || execCtx.Branches[1] != "sip:c@h" {
		t.Error("remove_branch failed")
	}
}

func TestDoActionClearBranches(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	execCtx.Branches = []string{"sip:a@h", "sip:b@h"}
	a := &Action{Type: ActClearBranches}
	DoAction(ctx, a, nil, execCtx)
	if len(execCtx.Branches) != 0 {
		t.Error("clear_branches failed")
	}
}

func TestDoActionForwardTCP(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActForwardTCP, Arg: "sip:host.com"}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.DstURI != "sip:host.com" {
		t.Error("forward_tcp failed")
	}
	if execCtx.Vars["__forward_transport"] != "TCP" {
		t.Error("expected __forward_transport=TCP")
	}
}

func TestDoActionForceRPort(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActForceRPort}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.Vars["__force_rport"] != "1" {
		t.Error("force_rport failed")
	}
}

func TestDoActionSetAdvAddr(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActSetAdvAddr, Arg: "10.0.0.1"}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.Vars["__adv_addr"] != "10.0.0.1" {
		t.Error("set_adv_addr failed")
	}
}

func TestDoActionAssign(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActAssign, Arg: "myvar", Arg2: "value123"}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.Vars["myvar"] != "value123" {
		t.Error("assign failed")
	}
}

func TestDoActionAdd(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	execCtx.Vars["counter"] = "10"
	a := &Action{Type: ActAdd, Arg: "counter", Arg2: "5"}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.Vars["counter"] != "105" {
		t.Errorf("expected '105', got '%s'", execCtx.Vars["counter"])
	}
}

func TestRunTopRoute(t *testing.T) {
	script := &Script{
		Root: []*Action{
			{Type: ActSetVar, Arg: "test", Arg2: "hello"},
			{Type: ActDrop},
		},
	}
	execCtx, runCtx, err := RunTopRoute(script, nil, nil, "")
	if err != nil {
		t.Fatalf("RunTopRoute error: %v", err)
	}
	if !execCtx.Drop {
		t.Error("expected Drop=true")
	}
	if runCtx.RunFlags&DropRF == 0 {
		t.Error("expected DropRF set")
	}
	if execCtx.Vars["test"] != "hello" {
		t.Error("var not set")
	}
}

func TestRunTopRouteWithMsg(t *testing.T) {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type: parser.MsgRequest,
		Req: &parser.RequestLine{
			Method:  str.Mk("INVITE"),
			URI:     str.Mk("sip:bob@example.com"),
			Version: str.Mk("SIP/2.0"),
		},
	}
	script := &Script{
		Root: []*Action{
			{Type: ActSetRURI, Arg: "sip:alice@example.com"},
		},
	}
	execCtx, _, err := RunTopRoute(script, msg, &net.UDPAddr{}, "test")
	if err != nil {
		t.Fatalf("RunTopRoute error: %v", err)
	}
	if execCtx.RURI != "sip:alice@example.com" {
		t.Errorf("expected RURI 'sip:alice@example.com', got '%s'", execCtx.RURI)
	}
}

func TestDoActionWhile(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")

	// Test with false condition - body should not execute
	a := &Action{
		Type: ActWhile,
		Expr: &Expr{LeftStr: "0", Op: "==", Right: "1"}, // always false
		IfTrue: []*Action{
			{Type: ActSetVar, Arg: "iter", Arg2: "yes"},
		},
	}
	DoAction(ctx, a, nil, execCtx)
	if execCtx.Vars["iter"] == "yes" {
		t.Error("while loop body should not execute with false condition")
	}

	// Test with true condition and break
	ctx2 := &RunActCtx{}
	InitRunActCtx(ctx2)
	execCtx2 := NewExecContext(nil, nil, "")
	a2 := &Action{
		Type: ActWhile,
		Expr: &Expr{LeftStr: "1", Op: "==", Right: "1"}, // always true
		IfTrue: []*Action{
			{Type: ActSetVar, Arg: "iter", Arg2: "yes"},
			{Type: ActBreak},
		},
	}
	DoAction(ctx2, a2, nil, execCtx2)
	if execCtx2.Vars["iter"] != "yes" {
		t.Error("while loop body should have executed once before break")
	}
}

func TestDoActionBreak(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActBreak}
	DoAction(ctx, a, nil, execCtx)
	if ctx.RunFlags&BreakRF == 0 {
		t.Error("expected BreakRF set")
	}
}

func TestDoActionError(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActError}
	ret := DoAction(ctx, a, nil, execCtx)
	if ret != -1 {
		t.Errorf("expected ret -1 for error, got %d", ret)
	}
}

func TestDoActionModuleCall(t *testing.T) {
	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(nil, nil, "")
	a := &Action{Type: ActModule0}
	ret := DoAction(ctx, a, nil, execCtx)
	if ret != 1 {
		t.Errorf("expected ret 1 for module call, got %d", ret)
	}
	if ctx.LastRetCode != 1 {
		t.Error("expected LastRetCode=1")
	}
}

func TestReplacePort(t *testing.T) {
	tests := []struct {
		uri, port, expected string
	}{
		{"sip:alice@host:5060", "5061", "sip:alice@host:5061"},
		{"sip:alice@host", "5061", "sip:alice@host:5061"},
		{"sip:alice@host:5060;transport=tcp", "5061", "sip:alice@host:5061;transport=tcp"},
	}
	for _, tt := range tests {
		result := replacePort(tt.uri, tt.port)
		if result != tt.expected {
			t.Errorf("replacePort(%q, %q) = %q, expected %q", tt.uri, tt.port, result, tt.expected)
		}
	}
}
