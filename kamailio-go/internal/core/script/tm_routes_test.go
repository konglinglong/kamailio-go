// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for TM route-block parsing and dispatch: failure_route,
 * branch_route, onreply_route top-level blocks and the t_on_reply /
 * t_on_failure / t_on_branch script statements that bind them to a
 * transaction.
 */
package script

import (
	"errors"
	"testing"
)

// --- Parsing: top-level route blocks ---

func TestParse_FailureRouteBlock(t *testing.T) {
	sc := mustParse(t, `
		request_route { drop; }
		failure_route[MY_FAIL] {
			xlog("failure triggered");
		}
	`)
	if _, ok := sc.FailureRoutes["MY_FAIL"]; !ok {
		t.Errorf("expected failure_route MY_FAIL to be parsed; got map=%v", sc.FailureRoutes)
	}
	if len(sc.FailureRoutes["MY_FAIL"]) != 1 {
		t.Errorf("expected 1 action in MY_FAIL, got %d", len(sc.FailureRoutes["MY_FAIL"]))
	}
	if sc.FailureRoutes["MY_FAIL"][0].Type != ActLog {
		t.Errorf("expected first action to be ActLog, got %v", sc.FailureRoutes["MY_FAIL"][0].Type)
	}
}

func TestParse_BranchRouteBlock(t *testing.T) {
	sc := mustParse(t, `
		request_route { drop; }
		branch_route[MY_BRANCH] {
			drop;
		}
	`)
	if _, ok := sc.BranchRoutes["MY_BRANCH"]; !ok {
		t.Errorf("expected branch_route MY_BRANCH; got map=%v", sc.BranchRoutes)
	}
}

func TestParse_OnReplyRouteBlock(t *testing.T) {
	sc := mustParse(t, `
		request_route { drop; }
		onreply_route[MY_REPLY] {
			drop;
		}
	`)
	if _, ok := sc.OnReplyRoutes["MY_REPLY"]; !ok {
		t.Errorf("expected onreply_route MY_REPLY; got map=%v", sc.OnReplyRoutes)
	}
}

func TestParse_AllRouteBlockKindsCoexist(t *testing.T) {
	sc := mustParse(t, `
		request_route { route(SUB); }
		route[SUB] { drop; }
		failure_route[F1] { drop; }
		branch_route[B1] { drop; }
		onreply_route[R1] { drop; }
	`)
	if _, ok := sc.Routes["SUB"]; !ok {
		t.Error("expected named route SUB")
	}
	if _, ok := sc.FailureRoutes["F1"]; !ok {
		t.Error("expected failure_route F1")
	}
	if _, ok := sc.BranchRoutes["B1"]; !ok {
		t.Error("expected branch_route B1")
	}
	if _, ok := sc.OnReplyRoutes["R1"]; !ok {
		t.Error("expected onreply_route R1")
	}
}

func TestParse_RouteBlockMissingName(t *testing.T) {
	// A route-block keyword without [NAME] should be a parse error.
	if _, err := ParseScript(`failure_route { drop; }`); err == nil {
		t.Error("expected parse error for failure_route without [NAME]")
	}
}

func TestParse_RouteBlockMissingBrace(t *testing.T) {
	if _, err := ParseScript(`failure_route[X] drop;`); err == nil {
		t.Error("expected parse error for missing { after failure_route[X]")
	}
}

// --- Parsing: t_on_* statements ---

func TestParse_TOnReply(t *testing.T) {
	sc := mustParse(t, `
		request_route {
			t_on_reply("MY_REPLY");
		}
	`)
	if len(sc.Root) != 1 || sc.Root[0].Type != ActTOnReply {
		t.Fatalf("expected root to have one ActTOnReply, got %v", sc.Root)
	}
	if sc.Root[0].RouteName != "MY_REPLY" {
		t.Errorf("RouteName = %q, want MY_REPLY", sc.Root[0].RouteName)
	}
}

func TestParse_TOnFailure(t *testing.T) {
	sc := mustParse(t, `
		request_route {
			t_on_failure("MY_FAIL");
		}
	`)
	if len(sc.Root) != 1 || sc.Root[0].Type != ActTOnFailure {
		t.Fatalf("expected ActTOnFailure, got %v", sc.Root)
	}
	if sc.Root[0].RouteName != "MY_FAIL" {
		t.Errorf("RouteName = %q, want MY_FAIL", sc.Root[0].RouteName)
	}
}

func TestParse_TOnBranch(t *testing.T) {
	sc := mustParse(t, `
		request_route {
			t_on_branch("MY_BRANCH");
		}
	`)
	if len(sc.Root) != 1 || sc.Root[0].Type != ActTOnBranch {
		t.Fatalf("expected ActTOnBranch, got %v", sc.Root)
	}
	if sc.Root[0].RouteName != "MY_BRANCH" {
		t.Errorf("RouteName = %q, want MY_BRANCH", sc.Root[0].RouteName)
	}
}

func TestParse_TOnBareName(t *testing.T) {
	// Kamailio allows bare (unquoted) route names in t_on_*.
	sc := mustParse(t, `
		request_route {
			t_on_reply(MY_REPLY);
		}
	`)
	if sc.Root[0].RouteName != "MY_REPLY" {
		t.Errorf("RouteName = %q, want MY_REPLY", sc.Root[0].RouteName)
	}
}

func TestParse_TOnMissingName(t *testing.T) {
	if _, err := ParseScript(`request_route { t_on_reply(); }`); err == nil {
		t.Error("expected parse error for t_on_reply() with no name")
	}
}

// --- Execution: t_on_* records route names on ExecContext ---

func TestExecute_TOnReplySetsRoute(t *testing.T) {
	sc := mustParse(t, `
		request_route {
			t_on_reply("R1");
		}
	`)
	ctx := NewExecContext(nil, nil, "")
	if err := sc.Execute(ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ctx.TMRoutes.OnReply != "R1" {
		t.Errorf("TMRoutes.OnReply = %q, want R1", ctx.TMRoutes.OnReply)
	}
	if ctx.TMRoutes.OnFailure != "" {
		t.Errorf("TMRoutes.OnFailure = %q, want empty", ctx.TMRoutes.OnFailure)
	}
}

func TestExecute_TOnFailureSetsRoute(t *testing.T) {
	sc := mustParse(t, `
		request_route {
			t_on_failure("F1");
		}
	`)
	ctx := NewExecContext(nil, nil, "")
	_ = sc.Execute(ctx)
	if ctx.TMRoutes.OnFailure != "F1" {
		t.Errorf("TMRoutes.OnFailure = %q, want F1", ctx.TMRoutes.OnFailure)
	}
}

func TestExecute_TOnBranchSetsRoute(t *testing.T) {
	sc := mustParse(t, `
		request_route {
			t_on_branch("B1");
		}
	`)
	ctx := NewExecContext(nil, nil, "")
	_ = sc.Execute(ctx)
	if ctx.TMRoutes.OnBranch != "B1" {
		t.Errorf("TMRoutes.OnBranch = %q, want B1", ctx.TMRoutes.OnBranch)
	}
}

func TestExecute_AllTOnSetTogether(t *testing.T) {
	sc := mustParse(t, `
		request_route {
			t_on_reply("R1");
			t_on_failure("F1");
			t_on_branch("B1");
		}
	`)
	ctx := NewExecContext(nil, nil, "")
	_ = sc.Execute(ctx)
	if ctx.TMRoutes.OnReply != "R1" {
		t.Errorf("OnReply = %q, want R1", ctx.TMRoutes.OnReply)
	}
	if ctx.TMRoutes.OnFailure != "F1" {
		t.Errorf("OnFailure = %q, want F1", ctx.TMRoutes.OnFailure)
	}
	if ctx.TMRoutes.OnBranch != "B1" {
		t.Errorf("OnBranch = %q, want B1", ctx.TMRoutes.OnBranch)
	}
}

// --- Execution: ExecuteFailureRoute / ExecuteOnReplyRoute / ExecuteBranchRoute ---

func TestExecute_FailureRoute(t *testing.T) {
	sc := mustParse(t, `
		request_route { drop; }
		failure_route[MY_FAIL] {
			xlog("failure");
			drop;
		}
	`)
	ctx := NewExecContext(nil, nil, "")
	if err := sc.ExecuteFailureRoute("MY_FAIL", ctx); err != nil {
		t.Fatalf("ExecuteFailureRoute: %v", err)
	}
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "failure" {
		t.Errorf("Logs = %v, want [failure]", ctx.Logs)
	}
	if !ctx.Drop {
		t.Error("expected Drop to be set by failure_route")
	}
}

func TestExecute_OnReplyRoute(t *testing.T) {
	sc := mustParse(t, `
		request_route { drop; }
		onreply_route[MY_REPLY] {
			xlog("reply");
		}
	`)
	ctx := NewExecContext(nil, nil, "")
	if err := sc.ExecuteOnReplyRoute("MY_REPLY", ctx); err != nil {
		t.Fatalf("ExecuteOnReplyRoute: %v", err)
	}
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "reply" {
		t.Errorf("Logs = %v, want [reply]", ctx.Logs)
	}
}

func TestExecute_BranchRoute(t *testing.T) {
	sc := mustParse(t, `
		request_route { drop; }
		branch_route[MY_BRANCH] {
			xlog("branch");
		}
	`)
	ctx := NewExecContext(nil, nil, "")
	if err := sc.ExecuteBranchRoute("MY_BRANCH", ctx); err != nil {
		t.Fatalf("ExecuteBranchRoute: %v", err)
	}
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "branch" {
		t.Errorf("Logs = %v, want [branch]", ctx.Logs)
	}
}

func TestExecute_FailureRouteNotFound(t *testing.T) {
	sc := mustParse(t, `request_route { drop; }`)
	ctx := NewExecContext(nil, nil, "")
	err := sc.ExecuteFailureRoute("NOPE", ctx)
	if !errors.Is(err, ErrRouteNotFound) {
		t.Errorf("expected ErrRouteNotFound, got %v", err)
	}
}

func TestExecute_OnReplyRouteNotFound(t *testing.T) {
	sc := mustParse(t, `request_route { drop; }`)
	ctx := NewExecContext(nil, nil, "")
	if err := sc.ExecuteOnReplyRoute("NOPE", ctx); !errors.Is(err, ErrRouteNotFound) {
		t.Errorf("expected ErrRouteNotFound, got %v", err)
	}
}

func TestExecute_BranchRouteNotFound(t *testing.T) {
	sc := mustParse(t, `request_route { drop; }`)
	ctx := NewExecContext(nil, nil, "")
	if err := sc.ExecuteBranchRoute("NOPE", ctx); !errors.Is(err, ErrRouteNotFound) {
		t.Errorf("expected ErrRouteNotFound, got %v", err)
	}
}

func TestExecute_EmptyNameIsNoOp(t *testing.T) {
	sc := mustParse(t, `request_route { drop; }`)
	ctx := NewExecContext(nil, nil, "")
	// An empty route name should be a silent no-op, not an error.
	if err := sc.ExecuteFailureRoute("", ctx); err != nil {
		t.Errorf("ExecuteFailureRoute(''): %v", err)
	}
	if err := sc.ExecuteOnReplyRoute("", ctx); err != nil {
		t.Errorf("ExecuteOnReplyRoute(''): %v", err)
	}
	if err := sc.ExecuteBranchRoute("", ctx); err != nil {
		t.Errorf("ExecuteBranchRoute(''): %v", err)
	}
}

func TestExecute_NilScriptAndContext(t *testing.T) {
	var sc *Script
	// Nil script / context must not panic.
	if err := sc.ExecuteFailureRoute("X", nil); err != nil {
		t.Errorf("nil script ExecuteFailureRoute: %v", err)
	}
	ctx := NewExecContext(nil, nil, "")
	if err := sc.ExecuteFailureRoute("X", ctx); err != nil {
		t.Errorf("nil script ExecuteFailureRoute with ctx: %v", err)
	}
}

// --- End-to-end: t_on_* in request_route binds the route that the
// executor later dispatches via the Execute* entry points. ---

func TestEndToEnd_BindAndDispatch(t *testing.T) {
	// request_route sets t_on_failure("FAIL"), then the caller
	// dispatches the FAIL failure_route and observes the side effect.
	sc := mustParse(t, `
		request_route {
			t_on_failure("FAIL");
		}
		failure_route[FAIL] {
			xlog("failure-dispatched");
		}
	`)
	ctx := NewExecContext(nil, nil, "")
	if err := sc.Execute(ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The proxy would now read ctx.TMRoutes.OnFailure and register a
	// tm callback that calls ExecuteFailureRoute when the transaction
	// fails. Simulate that here.
	if ctx.TMRoutes.OnFailure != "FAIL" {
		t.Fatalf("OnFailure = %q, want FAIL", ctx.TMRoutes.OnFailure)
	}
	if err := sc.ExecuteFailureRoute(ctx.TMRoutes.OnFailure, ctx); err != nil {
		t.Fatalf("ExecuteFailureRoute: %v", err)
	}
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "failure-dispatched" {
		t.Errorf("Logs = %v, want [failure-dispatched]", ctx.Logs)
	}
}
