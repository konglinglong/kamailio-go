// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Extended action executor
 *
 * Implements do_action dispatch for all extended action types
 * defined in action_ext.go, matching C action.c do_action().
 */

package script

import (
	"fmt"
	"net"
	"strings"

	"github.com/kamailio/kamailio-go/internal/core/log"
	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// DoAction dispatches a single action, matching C do_action().
// Returns: 1 = continue, 0 = drop/exit, -1 = error.
func DoAction(ctx *RunActCtx, a *Action, msg *parser.SIPMsg, execCtx *ExecContext) int {
	if a == nil {
		return 1
	}

	switch a.Type {
	// --- Base actions (handled by runOne in executor.go) ---
	case ActForward:
		if execCtx != nil {
			if a.Arg != "" {
				execCtx.DstURI = a.Arg
			}
		}
		return 1

	case ActSendReply:
		if execCtx != nil {
			status := a.ArgNum
			if status == 0 {
				status = 200
			}
			reason := a.Arg2
			if reason == "" {
				reason = "OK"
			}
			execCtx.Reply = &ReplyAction{Status: status, Reason: reason}
		}
		return 0

	case ActDrop:
		if execCtx != nil {
			execCtx.Drop = true
		}
		ctx.RunFlags |= DropRF
		return 0

	case ActExit:
		ctx.RunFlags |= ExitRF
		if execCtx != nil {
			execCtx.Return = true
		}
		return 0

	case ActBreak:
		ctx.RunFlags |= BreakRF
		return 1

	case ActLog:
		if execCtx != nil {
			execCtx.Logs = append(execCtx.Logs, a.Arg)
		}
		log.Info("script: " + a.Arg)
		return 1

	case ActSetFlag:
		if execCtx != nil {
			execCtx.Flags |= 1 << uint(a.ArgNum)
		}
		return 1

	case ActResetFlag:
		if execCtx != nil {
			execCtx.Flags &= ^(1 << uint(a.ArgNum))
		}
		return 1

	case ActIsFlagSet:
		if execCtx != nil {
			if (execCtx.Flags & (1 << uint(a.ArgNum))) != 0 {
				ctx.LastRetCode = 1
			} else {
				ctx.LastRetCode = -1
			}
		}
		return 1

	case ActIf:
		ok, err := evalActionExpr(a.Expr, msg, execCtx)
		if err != nil {
			return -1
		}
		if ok {
			return runActionBlock(ctx, a.IfTrue, msg, execCtx)
		}
		return runActionBlock(ctx, a.IfFalse, msg, execCtx)

	case ActRecordRoute:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__record_route"] = "1"
			execCtx.mu.Unlock()
		}
		return 1

	case ActAppendBranch:
		if execCtx != nil {
			execCtx.Branches = append(execCtx.Branches, a.Arg)
		}
		return 1

	case ActRemoveBranch:
		if execCtx != nil && len(execCtx.Branches) > 0 {
			idx := a.ArgNum
			if idx >= 0 && idx < len(execCtx.Branches) {
				execCtx.Branches = append(execCtx.Branches[:idx], execCtx.Branches[idx+1:]...)
			}
		}
		return 1

	case ActClearBranches:
		if execCtx != nil {
			execCtx.Branches = execCtx.Branches[:0]
		}
		return 1

	case ActSetRURI:
		if execCtx != nil {
			execCtx.RURI = a.Arg
		}
		return 1

	case ActSetDstURI:
		if execCtx != nil {
			execCtx.DstURI = a.Arg
		}
		return 1

	case ActSetVar:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars[a.Arg] = a.Arg2
			execCtx.mu.Unlock()
		}
		return 1

	case ActRoute:
		// Route dispatch is handled by Script.runBlock
		return 1

	case ActReturn:
		if execCtx != nil {
			execCtx.Return = true
		}
		ctx.RunFlags |= ReturnRF
		return 0

	// --- URI manipulation actions ---
	case ActSetHost:
		if execCtx != nil && execCtx.RURI != "" {
			execCtx.RURI = replaceHost(execCtx.RURI, a.Arg)
		}
		return 1

	case ActSetHostPort:
		if execCtx != nil && execCtx.RURI != "" {
			execCtx.RURI = replaceHostPort(execCtx.RURI, a.Arg)
		}
		return 1

	case ActSetUser:
		if execCtx != nil && execCtx.RURI != "" {
			execCtx.RURI = replaceUser(execCtx.RURI, a.Arg)
		}
		return 1

	case ActSetUserPass:
		if execCtx != nil && execCtx.RURI != "" {
			execCtx.RURI = replaceUserPass(execCtx.RURI, a.Arg)
		}
		return 1

	case ActSetPort:
		if execCtx != nil && execCtx.RURI != "" {
			execCtx.RURI = replacePort(execCtx.RURI, a.Arg)
		}
		return 1

	case ActRevertURI:
		if execCtx != nil && execCtx.Msg != nil {
			if execCtx.Msg.FirstLine != nil && execCtx.Msg.FirstLine.Req != nil {
				execCtx.RURI = execCtx.Msg.FirstLine.Req.URI.String()
			}
		}
		return 1

	// --- Prefix/stripping ---
	case ActPrefix:
		if execCtx != nil && execCtx.RURI != "" {
			execCtx.RURI = insertPrefix(execCtx.RURI, a.Arg)
		}
		return 1

	case ActStrip:
		if execCtx != nil && execCtx.RURI != "" {
			execCtx.RURI = stripPrefix(execCtx.RURI, a.ArgNum)
		}
		return 1

	case ActStripTail:
		if execCtx != nil && execCtx.RURI != "" {
			execCtx.RURI = stripTail(execCtx.RURI, a.ArgNum)
		}
		return 1

	// --- Transport-specific forward ---
	case ActForwardTCP:
		if execCtx != nil {
			execCtx.DstURI = a.Arg
			execCtx.mu.Lock()
			execCtx.Vars["__forward_transport"] = "TCP"
			execCtx.mu.Unlock()
		}
		return 1

	case ActForwardUDP:
		if execCtx != nil {
			execCtx.DstURI = a.Arg
			execCtx.mu.Lock()
			execCtx.Vars["__forward_transport"] = "UDP"
			execCtx.mu.Unlock()
		}
		return 1

	case ActForwardTLS:
		if execCtx != nil {
			execCtx.DstURI = a.Arg
			execCtx.mu.Lock()
			execCtx.Vars["__forward_transport"] = "TLS"
			execCtx.mu.Unlock()
		}
		return 1

	case ActForwardSCTP:
		if execCtx != nil {
			execCtx.DstURI = a.Arg
			execCtx.mu.Lock()
			execCtx.Vars["__forward_transport"] = "SCTP"
			execCtx.mu.Unlock()
		}
		return 1

	// --- Network manipulation ---
	case ActForceRPort:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__force_rport"] = "1"
			execCtx.mu.Unlock()
		}
		return 1

	case ActAddLocalRPort:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__add_local_rport"] = "1"
			execCtx.mu.Unlock()
		}
		return 1

	case ActSetAdvAddr:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__adv_addr"] = a.Arg
			execCtx.mu.Unlock()
		}
		return 1

	case ActSetAdvPort:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__adv_port"] = a.Arg
			execCtx.mu.Unlock()
		}
		return 1

	case ActForceSendSocket:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__force_socket"] = a.Arg
			execCtx.mu.Unlock()
		}
		return 1

	case ActForceTCPAlias:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__force_tcp_alias"] = a.Arg
			execCtx.mu.Unlock()
		}
		return 1

	// --- Connection control ---
	case ActSetFwdNoConnect:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__fwd_no_connect"] = "1"
			execCtx.mu.Unlock()
		}
		return 1

	case ActSetRplNoConnect:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__rpl_no_connect"] = "1"
			execCtx.mu.Unlock()
		}
		return 1

	case ActSetFwdClose:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__fwd_close"] = "1"
			execCtx.mu.Unlock()
		}
		return 1

	case ActSetRplClose:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__rpl_close"] = "1"
			execCtx.mu.Unlock()
		}
		return 1

	case ActUDPMTUTryProto:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars["__udp_mtu_try_proto"] = a.Arg
			execCtx.mu.Unlock()
		}
		return 1

	// --- Module function calls ---
	case ActModule0, ActModule1, ActModule2, ActModule3, ActModuleX:
		// Module function dispatch - returns retcode
		ctx.LastRetCode = 1
		return 1

	// --- AVP operations ---
	case ActAVPToURI:
		return 1

	case ActLoadAVP:
		return 1

	// --- Config operations ---
	case ActCfgSelect:
		return 1

	case ActCfgReset:
		return 1

	// --- Error ---
	case ActError:
		return -1

	// --- Eval/Assign/Add ---
	case ActEval:
		return 1

	case ActAssign:
		if execCtx != nil {
			execCtx.mu.Lock()
			execCtx.Vars[a.Arg] = a.Arg2
			execCtx.mu.Unlock()
		}
		return 1

	case ActAdd:
		if execCtx != nil {
			execCtx.mu.Lock()
			if v, ok := execCtx.Vars[a.Arg]; ok {
				execCtx.Vars[a.Arg] = v + a.Arg2
			} else {
				execCtx.Vars[a.Arg] = a.Arg2
			}
			execCtx.mu.Unlock()
		}
		return 1

	// --- Len comparison ---
	case ActLenGT:
		ctx.LastRetCode = 1
		return 1

	// --- While loop ---
	case ActWhile:
		maxIter := 10000
		for i := 0; i < maxIter; i++ {
			ok, err := evalActionExpr(a.Expr, msg, execCtx)
			if err != nil {
				return -1
			}
			if !ok {
				break
			}
			ret := runActionBlock(ctx, a.IfTrue, msg, execCtx)
			if ctx.RunFlags&(ExitRF|ReturnRF|DropRF) != 0 {
				return ret
			}
			if ctx.RunFlags&BreakRF != 0 {
				ctx.RunFlags &^= BreakRF
				break
			}
		}
		return 1

	// --- Switch/case ---
	case ActSwitch:
		if a.Switch != nil {
			return EvalSwitch(ctx, a.Switch, msg, execCtx)
		}
		return 1

	default:
		log.Warn(fmt.Sprintf("do_action: unknown action type %d", a.Type))
		return 1
	}
}

// runActionBlock executes a block of actions using DoAction.
func runActionBlock(ctx *RunActCtx, actions []*Action, msg *parser.SIPMsg, execCtx *ExecContext) int {
	for _, a := range actions {
		if execCtx != nil && (execCtx.Reply != nil || execCtx.Drop || execCtx.Return) {
			return 0
		}
		if ctx.RunFlags&(ExitRF|DropRF|BreakRF) != 0 {
			return 0
		}
		ret := DoAction(ctx, a, msg, execCtx)
		if ret <= 0 {
			return ret
		}
	}
	return 1
}

// evalActionExpr evaluates an expression for action execution.
func evalActionExpr(e *Expr, msg *parser.SIPMsg, execCtx *ExecContext) (bool, error) {
	if e == nil {
		return false, nil
	}
	if e.IsFlag {
		if execCtx == nil {
			return false, nil
		}
		set := (execCtx.Flags & (1 << uint(e.FlagN))) != 0
		if e.Negate {
			return !set, nil
		}
		return set, nil
	}

	var lhs string
	if execCtx != nil {
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
				} else {
					// Treat as literal value
					lhs = e.LeftStr
				}
			}
		}
	} else {
		// No exec context, treat LeftStr as literal
		lhs = e.LeftStr
	}

	switch e.Op {
	case "==":
		return lhs == e.Right, nil
	case "!=":
		return lhs != e.Right, nil
	}
	return false, nil
}

// RunTopRoute executes the top-level route block, matching C run_top_route().
func RunTopRoute(script *Script, msg *parser.SIPMsg, src net.Addr, realm string) (*ExecContext, *RunActCtx, error) {
	ctx := NewExecContext(msg, src, realm)
	runCtx := &RunActCtx{}
	InitRunActCtx(runCtx)

	if script == nil {
		return ctx, runCtx, nil
	}

	ret := runActionBlock(runCtx, script.Root, msg, ctx)
	if ret < 0 {
		return ctx, runCtx, fmt.Errorf("action execution error")
	}
	return ctx, runCtx, nil
}

// --- URI manipulation helpers ---

func replaceHost(uri, newHost string) string {
	at := strings.Index(uri, "@")
	if at < 0 {
		return uri
	}
	scheme := uri[:at+1]
	rest := uri[at+1:]
	colon := strings.IndexAny(rest, ":;>?")
	if colon >= 0 {
		return scheme + newHost + rest[colon:]
	}
	return scheme + newHost
}

func replaceHostPort(uri, newHostPort string) string {
	return replaceHost(uri, newHostPort)
}

func replaceUser(uri, newUser string) string {
	colon := strings.Index(uri, ":")
	if colon < 0 {
		return uri
	}
	scheme := uri[:colon+1]
	rest := uri[colon+1:]
	at := strings.Index(rest, "@")
	if at < 0 {
		return uri
	}
	return scheme + newUser + rest[at:]
}

func replaceUserPass(uri, newUserPass string) string {
	return replaceUser(uri, newUserPass)
}

func replacePort(uri, newPort string) string {
	at := strings.Index(uri, "@")
	if at < 0 {
		return uri
	}
	rest := uri[at+1:]
	colon := strings.IndexByte(rest, ':')
	if colon < 0 {
		// No port, try to add before ; or >
		semi := strings.IndexAny(rest, ";>?")
		if semi >= 0 {
			return uri[:at+1] + rest[:semi] + ":" + newPort + rest[semi:]
		}
		return uri + ":" + newPort
	}
	semi := strings.IndexAny(rest[colon+1:], ";>?")
	if semi >= 0 {
		return uri[:at+1+colon+1] + newPort + rest[colon+1+semi:]
	}
	return uri[:at+1+colon+1] + newPort
}

func insertPrefix(uri, prefix string) string {
	colon := strings.Index(uri, ":")
	if colon < 0 {
		return prefix + uri
	}
	scheme := uri[:colon+1]
	rest := uri[colon+1:]
	at := strings.Index(rest, "@")
	if at >= 0 {
		return scheme + prefix + rest
	}
	return scheme + prefix + rest
}

func stripPrefix(uri string, n int) string {
	colon := strings.Index(uri, ":")
	if colon < 0 {
		if len(uri) > n {
			return uri[n:]
		}
		return uri
	}
	scheme := uri[:colon+1]
	rest := uri[colon+1:]
	at := strings.Index(rest, "@")
	if at >= 0 {
		user := rest[:at]
		if len(user) > n {
			return scheme + user[n:] + rest[at:]
		}
		return scheme + rest[at:]
	}
	if len(rest) > n {
		return scheme + rest[n:]
	}
	return scheme
}

func stripTail(uri string, n int) string {
	colon := strings.Index(uri, ":")
	if colon < 0 {
		if len(uri) > n {
			return uri[:len(uri)-n]
		}
		return uri
	}
	scheme := uri[:colon+1]
	rest := uri[colon+1:]
	at := strings.Index(rest, "@")
	if at >= 0 {
		user := rest[:at]
		if len(user) > n {
			return scheme + user[:len(user)-n] + rest[at:]
		}
		return scheme + rest[at:]
	}
	if len(rest) > n {
		return scheme + rest[:len(rest)-n]
	}
	return scheme
}
