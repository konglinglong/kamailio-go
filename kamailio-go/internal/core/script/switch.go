// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Switch/case statement implementation
 *
 * Implements switch/case/default routing script constructs,
 * corresponding to Kamailio's C switch.c.
 *
 * The C implementation (switch.c) "flattens" all case action lists
 * into a single linked list and uses jump bookmarks so that, when a
 * case matches, execution runs forward from that point.  A break
 * statement jumps to the end of the switch.  Without a break,
 * execution falls through into the next case's actions — this is
 * standard C switch semantics.
 *
 * This Go port models the same behaviour: EvalSwitch finds the
 * matching case (or default) and executes cases forward from that
 * index, honouring break, drop, exit and return flags.
 */

package script

import (
	"strings"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// SwitchCase represents one case branch in a switch statement.
type SwitchCase struct {
	Values    []string  // case value list (empty for default)
	Actions   []*Action // actions for this case
	IsDefault bool      // true if this is the default case
}

// SwitchStmt represents a complete switch statement.
type SwitchStmt struct {
	Expr  *Expr         // switch expression (evaluates to string)
	Cases []*SwitchCase // case branch list
}

// EvalSwitch executes a switch statement.
// Returns:
//   1 = normal completion (or break)
//   0 = drop
//  -1 = error
func EvalSwitch(ctx *RunActCtx, sw *SwitchStmt, msg *parser.SIPMsg, execCtx *ExecContext) int {
	if sw == nil {
		return 1
	}

	// Evaluate the switch expression to a string value.
	val := evalSwitchValue(sw.Expr, msg, execCtx)

	// Find the matching case index and the default index.
	matchIdx := -1
	defaultIdx := -1
	for i, c := range sw.Cases {
		if c.IsDefault {
			if defaultIdx < 0 {
				defaultIdx = i
			}
			continue
		}
		if matchIdx >= 0 {
			continue
		}
		for _, v := range c.Values {
			if v == val {
				matchIdx = i
				break
			}
		}
	}

	// If no case matched, fall back to default.
	if matchIdx < 0 {
		matchIdx = defaultIdx
	}
	if matchIdx < 0 {
		// No match and no default — switch completes normally.
		return 1
	}

	// Execute from the matching case onward, with fall-through
	// (matching C semantics where actions are flattened into one list).
	for i := matchIdx; i < len(sw.Cases); i++ {
		c := sw.Cases[i]
		ret := runActionBlock(ctx, c.Actions, msg, execCtx)
		if ret < 0 {
			return ret // error
		}
		// Check for exit/return/drop — propagate.
		if ctx.RunFlags&(ExitRF|ReturnRF|DropRF) != 0 {
			return ret
		}
		// Check for break — ends the switch.
		if ctx.RunFlags&BreakRF != 0 {
			ctx.RunFlags &^= BreakRF
			return 1
		}
		// No break — fall through to next case.
	}
	return 1
}

// evalSwitchValue resolves the switch expression to a string value.
// It supports pseudo-variables ($rU, $rm, ...), the "method" and "uri"
// keywords, and $var(name) references.
func evalSwitchValue(e *Expr, msg *parser.SIPMsg, execCtx *ExecContext) string {
	if e == nil {
		return ""
	}
	if execCtx == nil {
		return ""
	}
	var lhs string
	switch strings.ToLower(e.LeftStr) {
	case "method":
		if msg != nil && msg.FirstLine != nil && msg.FirstLine.Req != nil {
			lhs = msg.FirstLine.Req.Method.String()
		}
	case "uri":
		lhs = execCtx.RURI
	default:
		if e.LeftPV != PVNone {
			lhs, _ = resolvePV(e.LeftPV, msg, execCtx)
		} else if len(e.LeftStr) > 0 {
			lower := strings.ToLower(e.LeftStr)
			if strings.HasPrefix(lower, "$var(") && strings.HasSuffix(lower, ")") {
				name := e.LeftStr[len("$var(") : len(e.LeftStr)-1]
				execCtx.mu.RLock()
				lhs = execCtx.Vars[name]
				execCtx.mu.RUnlock()
			}
		}
	}
	return lhs
}
