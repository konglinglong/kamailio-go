// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for switch/case statement implementation.
 */

package script

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// switchSIP builds a SIP request with the given method and request-URI
// user, then parses it (including the URI) so that $rU and $rm are
// available to the script runtime.
func switchSIP(t *testing.T, method, ruriUser string) *parser.SIPMsg {
	t.Helper()
	raw := buildSIP(method,
		"sip:"+ruriUser+"@example.com",
		"<sip:alice@example.com>",
		"<sip:bob@example.com>")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	if err := parser.ParseMsgURI(msg); err != nil {
		t.Fatalf("ParseMsgURI: %v", err)
	}
	return msg
}

// runSwitchScript parses and executes src against a context seeded with
// the given SIP message.  It fails the test on parse or exec error.
func runSwitchScript(t *testing.T, src string, msg *parser.SIPMsg) *ExecContext {
	t.Helper()
	sc := mustParse(t, src)
	ctx := NewExecContext(msg, nil, "example.com")
	if err := sc.Execute(ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return ctx
}

// -----------------------------------------------------------------------
// 1. TestSwitchBasic — basic single-case match
// -----------------------------------------------------------------------

func TestSwitchBasic(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "100":
            xlog("matched 100");
            break;
    }
}
`
	msg := switchSIP(t, "INVITE", "100")
	ctx := runSwitchScript(t, src, msg)
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "matched 100" {
		t.Errorf("expected [matched 100], got %+v", ctx.Logs)
	}
}

// -----------------------------------------------------------------------
// 2. TestSwitchMultipleCases — multiple distinct cases
// -----------------------------------------------------------------------

func TestSwitchMultipleCases(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "100":
            xlog("hundred");
            break;
        case "200":
            xlog("two hundred");
            break;
        case "300":
            xlog("three hundred");
            break;
    }
}
`
	// Match "200"
	msg := switchSIP(t, "INVITE", "200")
	ctx := runSwitchScript(t, src, msg)
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "two hundred" {
		t.Errorf("for rU=200: expected [two hundred], got %+v", ctx.Logs)
	}

	// Match "300"
	msg2 := switchSIP(t, "INVITE", "300")
	ctx2 := runSwitchScript(t, src, msg2)
	if len(ctx2.Logs) != 1 || ctx2.Logs[0] != "three hundred" {
		t.Errorf("for rU=300: expected [three hundred], got %+v", ctx2.Logs)
	}
}

// -----------------------------------------------------------------------
// 3. TestSwitchDefault — default branch is taken when no case matches
// -----------------------------------------------------------------------

func TestSwitchDefault(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "100":
            xlog("hundred");
            break;
        case "200":
            xlog("two hundred");
            break;
        default:
            xlog("default");
    }
}
`
	msg := switchSIP(t, "INVITE", "999")
	ctx := runSwitchScript(t, src, msg)
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "default" {
		t.Errorf("expected [default], got %+v", ctx.Logs)
	}
}

// -----------------------------------------------------------------------
// 4. TestSwitchFallthrough — multiple case labels share one code block
// -----------------------------------------------------------------------

func TestSwitchFallthrough(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "200":
        case "201":
            xlog("two hundred family");
            break;
        default:
            xlog("other");
    }
}
`
	// "200" should match the shared block
	msg200 := switchSIP(t, "INVITE", "200")
	ctx200 := runSwitchScript(t, src, msg200)
	if len(ctx200.Logs) != 1 || ctx200.Logs[0] != "two hundred family" {
		t.Errorf("for rU=200: expected [two hundred family], got %+v", ctx200.Logs)
	}

	// "201" should also match the shared block
	msg201 := switchSIP(t, "INVITE", "201")
	ctx201 := runSwitchScript(t, src, msg201)
	if len(ctx201.Logs) != 1 || ctx201.Logs[0] != "two hundred family" {
		t.Errorf("for rU=201: expected [two hundred family], got %+v", ctx201.Logs)
	}
}

// -----------------------------------------------------------------------
// 5. TestSwitchBreak — break stops execution within a case
// -----------------------------------------------------------------------

func TestSwitchBreak(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "100":
            xlog("before break");
            break;
            xlog("after break");
    }
}
`
	msg := switchSIP(t, "INVITE", "100")
	ctx := runSwitchScript(t, src, msg)
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "before break" {
		t.Errorf("expected only [before break], got %+v", ctx.Logs)
	}
}

// -----------------------------------------------------------------------
// 6. TestSwitchNoMatch — no matching case and no default
// -----------------------------------------------------------------------

func TestSwitchNoMatch(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "100":
            xlog("hundred");
            break;
        case "200":
            xlog("two hundred");
            break;
    }
    xlog("after switch");
}
`
	msg := switchSIP(t, "INVITE", "999")
	ctx := runSwitchScript(t, src, msg)
	// No case matched, no default — switch is a no-op, execution
	// continues after the switch.
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "after switch" {
		t.Errorf("expected [after switch], got %+v", ctx.Logs)
	}
}

// -----------------------------------------------------------------------
// 7. TestSwitchWithPVar — switch on $var(name)
// -----------------------------------------------------------------------

func TestSwitchWithPVar(t *testing.T) {
	const src = `
request_route {
    $var(key) = "gamma";
    switch ($var(key)) {
        case "alpha":
            xlog("alpha");
            break;
        case "beta":
            xlog("beta");
            break;
        case "gamma":
            xlog("gamma matched");
            break;
        default:
            xlog("unknown");
    }
}
`
	ctx := runSwitchScript(t, src, nil)
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "gamma matched" {
		t.Errorf("expected [gamma matched], got %+v", ctx.Logs)
	}
}

// -----------------------------------------------------------------------
// 8. TestSwitchWithMethod — switch on $rm (request method)
// -----------------------------------------------------------------------

func TestSwitchWithMethod(t *testing.T) {
	const src = `
request_route {
    switch ($rm) {
        case "INVITE":
            xlog("invite");
            break;
        case "REGISTER":
            xlog("register");
            break;
        case "BYE":
            xlog("bye");
            break;
        default:
            xlog("other method");
    }
}
`
	// INVITE
	msgInvite := switchSIP(t, "INVITE", "100")
	ctxInvite := runSwitchScript(t, src, msgInvite)
	if len(ctxInvite.Logs) != 1 || ctxInvite.Logs[0] != "invite" {
		t.Errorf("for INVITE: expected [invite], got %+v", ctxInvite.Logs)
	}

	// REGISTER
	msgReg := switchSIP(t, "REGISTER", "100")
	ctxReg := runSwitchScript(t, src, msgReg)
	if len(ctxReg.Logs) != 1 || ctxReg.Logs[0] != "register" {
		t.Errorf("for REGISTER: expected [register], got %+v", ctxReg.Logs)
	}
}

// -----------------------------------------------------------------------
// 9. TestSwitchNested — switch inside switch
// -----------------------------------------------------------------------

func TestSwitchNested(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "100":
            switch ($rm) {
                case "INVITE":
                    xlog("100-invite");
                    break;
                case "REGISTER":
                    xlog("100-register");
                    break;
                default:
                    xlog("100-other");
            }
            break;
        case "200":
            xlog("200-block");
            break;
        default:
            xlog("outer-default");
    }
}
`
	// rU=100, method=INVITE -> "100-invite"
	msg := switchSIP(t, "INVITE", "100")
	ctx := runSwitchScript(t, src, msg)
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "100-invite" {
		t.Errorf("expected [100-invite], got %+v", ctx.Logs)
	}

	// rU=100, method=REGISTER -> "100-register"
	msg2 := switchSIP(t, "REGISTER", "100")
	ctx2 := runSwitchScript(t, src, msg2)
	if len(ctx2.Logs) != 1 || ctx2.Logs[0] != "100-register" {
		t.Errorf("expected [100-register], got %+v", ctx2.Logs)
	}

	// rU=100, method=OPTIONS -> "100-other" (inner default)
	msg3 := switchSIP(t, "OPTIONS", "100")
	ctx3 := runSwitchScript(t, src, msg3)
	if len(ctx3.Logs) != 1 || ctx3.Logs[0] != "100-other" {
		t.Errorf("expected [100-other], got %+v", ctx3.Logs)
	}

	// rU=200 -> "200-block"
	msg4 := switchSIP(t, "INVITE", "200")
	ctx4 := runSwitchScript(t, src, msg4)
	if len(ctx4.Logs) != 1 || ctx4.Logs[0] != "200-block" {
		t.Errorf("expected [200-block], got %+v", ctx4.Logs)
	}

	// rU=999 -> "outer-default"
	msg5 := switchSIP(t, "INVITE", "999")
	ctx5 := runSwitchScript(t, src, msg5)
	if len(ctx5.Logs) != 1 || ctx5.Logs[0] != "outer-default" {
		t.Errorf("expected [outer-default], got %+v", ctx5.Logs)
	}
}

// -----------------------------------------------------------------------
// 10. TestSwitchConcurrent — concurrent execution is safe
// -----------------------------------------------------------------------

func TestSwitchConcurrent(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "100":
            xlog("hundred");
            break;
        case "200":
            xlog("two hundred");
            break;
        default:
            xlog("default");
    }
}
`
	sc := mustParse(t, src)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			user := fmt.Sprintf("%d", 100+(i%3)) // 100, 101, 102
			msg := switchSIP(t, "INVITE", user)
			ctx := NewExecContext(msg, nil, "example.com")
			if err := sc.Execute(ctx); err != nil {
				t.Errorf("goroutine %d: Execute: %v", i, err)
				return
			}
			switch user {
			case "100":
				if len(ctx.Logs) != 1 || ctx.Logs[0] != "hundred" {
					t.Errorf("goroutine %d (rU=%s): expected [hundred], got %+v", i, user, ctx.Logs)
				}
			default:
				if len(ctx.Logs) != 1 || ctx.Logs[0] != "default" {
					t.Errorf("goroutine %d (rU=%s): expected [default], got %+v", i, user, ctx.Logs)
				}
			}
		}()
	}
	wg.Wait()
}

// -----------------------------------------------------------------------
// Extra: TestSwitchDoAction — verify EvalSwitch via DoAction path
// -----------------------------------------------------------------------

func TestSwitchDoAction(t *testing.T) {
	msg := switchSIP(t, "INVITE", "100")

	sw := &SwitchStmt{
		Expr: &Expr{LeftPV: PVReqUser},
		Cases: []*SwitchCase{
			{
				Values: []string{"100"},
				Actions: []*Action{
					{Type: ActLog, Arg: "doaction-100"},
					{Type: ActBreak},
				},
			},
			{
				Values: []string{"200"},
				Actions: []*Action{
					{Type: ActLog, Arg: "doaction-200"},
					{Type: ActBreak},
				},
			},
			{
				IsDefault: true,
				Actions: []*Action{
					{Type: ActLog, Arg: "doaction-default"},
				},
			},
		},
	}
	a := &Action{Type: ActSwitch, Switch: sw}

	ctx := &RunActCtx{}
	InitRunActCtx(ctx)
	execCtx := NewExecContext(msg, nil, "")
	ret := DoAction(ctx, a, msg, execCtx)
	if ret != 1 {
		t.Errorf("expected ret 1, got %d", ret)
	}
	if len(execCtx.Logs) != 1 || execCtx.Logs[0] != "doaction-100" {
		t.Errorf("expected [doaction-100], got %+v", execCtx.Logs)
	}
}

// -----------------------------------------------------------------------
// Extra: TestSwitchFallthroughExec — no-break fall-through to next case
// -----------------------------------------------------------------------

func TestSwitchFallthroughExec(t *testing.T) {
	// Without a break, execution falls through to the next case's
	// actions (matching C switch semantics).
	const src = `
request_route {
    switch ($rU) {
        case "100":
            xlog("hundred");
        case "200":
            xlog("two hundred");
            break;
        case "300":
            xlog("three hundred");
            break;
    }
}
`
	// rU=100: should execute "hundred" then fall through to "two hundred"
	msg := switchSIP(t, "INVITE", "100")
	ctx := runSwitchScript(t, src, msg)
	if len(ctx.Logs) != 2 || ctx.Logs[0] != "hundred" || ctx.Logs[1] != "two hundred" {
		t.Errorf("for rU=100: expected [hundred, two hundred], got %+v", ctx.Logs)
	}

	// rU=200: should execute only "two hundred"
	msg2 := switchSIP(t, "INVITE", "200")
	ctx2 := runSwitchScript(t, src, msg2)
	if len(ctx2.Logs) != 1 || ctx2.Logs[0] != "two hundred" {
		t.Errorf("for rU=200: expected [two hundred], got %+v", ctx2.Logs)
	}
}

// -----------------------------------------------------------------------
// Extra: TestSwitchDropInsideCase — drop inside switch propagates
// -----------------------------------------------------------------------

func TestSwitchDropInsideCase(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "100":
            xlog("dropping");
            drop;
            break;
        default:
            xlog("default");
    }
    xlog("after switch");
}
`
	msg := switchSIP(t, "INVITE", "100")
	ctx := runSwitchScript(t, src, msg)
	if !ctx.Drop {
		t.Error("expected Drop=true")
	}
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "dropping" {
		t.Errorf("expected [dropping], got %+v", ctx.Logs)
	}
}

// -----------------------------------------------------------------------
// Extra: TestSwitchReplyInsideCase — sl_send_reply inside switch
// -----------------------------------------------------------------------

func TestSwitchReplyInsideCase(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "100":
            xlog("ok");
            break;
        default:
            sl_send_reply(404, "Not Found");
    }
}
`
	msg := switchSIP(t, "INVITE", "999")
	ctx := runSwitchScript(t, src, msg)
	if ctx.Reply == nil {
		t.Fatal("expected Reply to be set")
	}
	if ctx.Reply.Status != 404 || ctx.Reply.Reason != "Not Found" {
		t.Errorf("expected 404/Not Found, got %d/%q", ctx.Reply.Status, ctx.Reply.Reason)
	}
}

// -----------------------------------------------------------------------
// Extra: TestSwitchNumberCase — numeric case labels (without quotes)
// -----------------------------------------------------------------------

func TestSwitchNumberCase(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case 100:
            xlog("num-100");
            break;
        case 200:
            xlog("num-200");
            break;
    }
}
`
	msg := switchSIP(t, "INVITE", "100")
	ctx := runSwitchScript(t, src, msg)
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "num-100" {
		t.Errorf("expected [num-100], got %+v", ctx.Logs)
	}
}

// -----------------------------------------------------------------------
// Extra: TestSwitchRouteInsideCase — route() call inside switch
// -----------------------------------------------------------------------

func TestSwitchRouteInsideCase(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
        case "100":
            route(SPECIAL);
            break;
        default:
            route(GENERIC);
    }
}
route[SPECIAL] {
    xlog("special-route");
}
route[GENERIC] {
    xlog("generic-route");
}
`
	msg := switchSIP(t, "INVITE", "100")
	ctx := runSwitchScript(t, src, msg)
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "special-route" {
		t.Errorf("expected [special-route], got %+v", ctx.Logs)
	}

	msg2 := switchSIP(t, "INVITE", "999")
	ctx2 := runSwitchScript(t, src, msg2)
	if len(ctx2.Logs) != 1 || ctx2.Logs[0] != "generic-route" {
		t.Errorf("expected [generic-route], got %+v", ctx2.Logs)
	}
}

// -----------------------------------------------------------------------
// Extra: TestSwitchEmptyBody — switch with no cases
// -----------------------------------------------------------------------

func TestSwitchEmptyBody(t *testing.T) {
	const src = `
request_route {
    switch ($rU) {
    }
    xlog("after");
}
`
	msg := switchSIP(t, "INVITE", "100")
	ctx := runSwitchScript(t, src, msg)
	if len(ctx.Logs) != 1 || ctx.Logs[0] != "after" {
		t.Errorf("expected [after], got %+v", ctx.Logs)
	}
}
