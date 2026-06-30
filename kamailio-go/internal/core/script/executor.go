// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Script routing engine
 *
 * Executor walks an AST produced by ParseScript, updating a
 * per-request ExecContext with side effects (replies, forwards, flag
 * bits, variables, branches). Proxy.ProcessRequest then reads the
 * ExecContext to decide how to finalize the SIP request.
 */

package script

import (
	"net"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/avp"
	"github.com/kamailio/kamailio-go/internal/core/log"
	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// ExecContext carries mutable per-request state for the script runtime.
// It bridges the script with the proxy core — fields like Reply /
// DstURI / Drop tell ProcessRequest what to do after the script runs.
type ExecContext struct {
	Msg     *parser.SIPMsg
	SrcAddr net.Addr
	Realm   string

	Flags    uint32
	RURI     string
	DstURI   string
	Branches []string

	Reply  *ReplyAction
	Drop   bool
	Return bool

	// TMRoutes holds the route-block names bound to the current
	// transaction by t_on_reply / t_on_failure / t_on_branch. The
	// proxy reads these after running request_route and registers the
	// corresponding tm callbacks; the callbacks in turn dispatch the
	// named route block via ExecuteOnReplyRoute / ExecuteFailureRoute /
	// ExecuteBranchRoute.
	TMRoutes TMRoutes

	// breakFlag is set by the break statement inside switch/while.
	// It is checked by runBlock and cleared by the enclosing
	// switch/while handler so it does not leak to outer blocks.
	breakFlag bool

	// $var(name) store — protected by mu.
	mu   *sync.RWMutex
	Vars map[string]string
	Logs []string

	// AVP and XAVP stores (used by lvalue.go assignments).
	AVPs   *avp.Store
	XAVPs  *XAVPStore
}

// TMRoutes records which named route blocks the script has bound to the
// current transaction via t_on_reply / t_on_failure / t_on_branch. A
// blank name means no route is set for that event.
type TMRoutes struct {
	OnReply   string
	OnFailure string
	OnBranch  string
}

// ReplyAction records a script's decision to reply to a request with a
// given status / reason / extra headers.
type ReplyAction struct {
	Status  int
	Reason  string
	Headers []string
}

// NewExecContext creates a fresh execution context for a request.
// msg may be nil (tests); otherwise RURI is pre-seeded from the message.
func NewExecContext(msg *parser.SIPMsg, src net.Addr, realm string) *ExecContext {
	ctx := &ExecContext{
		Msg:     msg,
		SrcAddr: src,
		Realm:   realm,
		mu:      &sync.RWMutex{},
		Vars:    make(map[string]string),
		AVPs:    avp.NewStore(),
		XAVPs:   NewXAVPStore(),
	}
	if msg != nil && msg.FirstLine != nil && msg.FirstLine.Req != nil {
		ctx.RURI = msg.FirstLine.Req.URI.String()
	}
	return ctx
}

// Execute runs the Script against ctx.
func (s *Script) Execute(ctx *ExecContext) error {
	if s == nil || ctx == nil {
		return nil
	}
	return s.runBlock(s.Root, ctx)
}

// ExecuteFailureRoute runs the named failure_route block against ctx.
// It is invoked by the tm engine's TMCBOnFailure callback. Returns
// ErrRouteNotFound when no failure_route with the given name exists;
// callers should treat that as a no-op rather than an error.
func (s *Script) ExecuteFailureRoute(name string, ctx *ExecContext) error {
	if s == nil || ctx == nil || name == "" {
		return nil
	}
	block, ok := s.FailureRoutes[name]
	if !ok {
		return ErrRouteNotFound
	}
	return s.runBlock(block, ctx)
}

// ExecuteOnReplyRoute runs the named onreply_route block against ctx.
// It is invoked by the tm engine's TMCBOnReply callback.
func (s *Script) ExecuteOnReplyRoute(name string, ctx *ExecContext) error {
	if s == nil || ctx == nil || name == "" {
		return nil
	}
	block, ok := s.OnReplyRoutes[name]
	if !ok {
		return ErrRouteNotFound
	}
	return s.runBlock(block, ctx)
}

// ExecuteBranchRoute runs the named branch_route block against ctx.
// It is invoked by the tm engine's TMCBOnBranch callback.
func (s *Script) ExecuteBranchRoute(name string, ctx *ExecContext) error {
	if s == nil || ctx == nil || name == "" {
		return nil
	}
	block, ok := s.BranchRoutes[name]
	if !ok {
		return ErrRouteNotFound
	}
	return s.runBlock(block, ctx)
}

// runBlock executes a slice of actions sequentially. It stops early if
// the script requests a reply, a drop, a return, or a break.
func (s *Script) runBlock(actions []*Action, ctx *ExecContext) error {
	for _, a := range actions {
		if ctx.Reply != nil || ctx.Drop || ctx.Return || ctx.breakFlag {
			return nil
		}
		if err := s.runOne(a, ctx); err != nil {
			return err
		}
	}
	return nil
}

// runOne dispatches a single action to its handler.
func (s *Script) runOne(a *Action, ctx *ExecContext) error {
	if a == nil {
		return nil
	}
	switch a.Type {
	case ActForward:
		if a.Arg != "" {
			ctx.DstURI = a.Arg
		}
	case ActSendReply:
		status := a.ArgNum
		if status == 0 {
			status = 200
		}
		reason := a.Arg2
		if reason == "" {
			reason = "OK"
		}
		ctx.Reply = &ReplyAction{Status: status, Reason: reason}
	case ActDrop:
		ctx.Drop = true
	case ActLog:
		ctx.Logs = append(ctx.Logs, a.Arg)
		log.Info("script: " + a.Arg)
	case ActSetFlag:
		ctx.Flags |= 1 << uint(a.ArgNum)
	case ActResetFlag:
		ctx.Flags &= ^(1 << uint(a.ArgNum))
	case ActIf:
		ok, err := s.evalExpr(a.Expr, ctx)
		if err != nil {
			return err
		}
		if ok {
			return s.runBlock(a.IfTrue, ctx)
		}
		return s.runBlock(a.IfFalse, ctx)
	case ActRecordRoute:
		ctx.mu.Lock()
		ctx.Vars["__record_route"] = "1"
		ctx.mu.Unlock()
	case ActAppendBranch:
		ctx.Branches = append(ctx.Branches, a.Arg)
	case ActSetRURI:
		ctx.RURI = a.Arg
	case ActSetDstURI:
		ctx.DstURI = a.Arg
	case ActSetVar:
		ctx.mu.Lock()
		ctx.Vars[a.Arg] = a.Arg2
		ctx.mu.Unlock()
	case ActRoute:
		block, ok := s.Routes[a.RouteName]
		if !ok {
			return nil
		}
		return s.runBlock(block, ctx)
	case ActReturn:
		ctx.Return = true
	case ActTOnReply:
		ctx.TMRoutes.OnReply = a.RouteName
	case ActTOnFailure:
		ctx.TMRoutes.OnFailure = a.RouteName
	case ActTOnBranch:
		ctx.TMRoutes.OnBranch = a.RouteName
	case ActBreak:
		ctx.breakFlag = true
	case ActSwitch:
		if a.Switch != nil {
			return s.evalSwitch(a.Switch, ctx)
		}
	}
	return nil
}

// evalSwitch executes a switch statement in the Script.Execute path.
// Unlike EvalSwitch (which uses DoAction and cannot dispatch routes),
// this method uses runBlock so that route() calls inside cases work.
// Break is handled via ctx.breakFlag, which is cleared here so it
// does not leak to the enclosing block.
func (s *Script) evalSwitch(sw *SwitchStmt, ctx *ExecContext) error {
	if sw == nil {
		return nil
	}

	val := evalSwitchValue(sw.Expr, ctx.Msg, ctx)

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

	if matchIdx < 0 {
		matchIdx = defaultIdx
	}
	if matchIdx < 0 {
		return nil
	}

	for i := matchIdx; i < len(sw.Cases); i++ {
		c := sw.Cases[i]
		if err := s.runBlock(c.Actions, ctx); err != nil {
			return err
		}
		if ctx.Reply != nil || ctx.Drop || ctx.Return {
			return nil
		}
		if ctx.breakFlag {
			ctx.breakFlag = false
			return nil
		}
	}
	return nil
}

// evalExpr evaluates one expression against the context.
func (s *Script) evalExpr(e *Expr, ctx *ExecContext) (bool, error) {
	if e == nil {
		return false, nil
	}
	if e.IsFlag {
		set := (ctx.Flags & (1 << uint(e.FlagN))) != 0
		if e.Negate {
			return !set, nil
		}
		return set, nil
	}
	var lhs string
	switch strings.ToLower(e.LeftStr) {
	case "method":
		if ctx.Msg != nil && ctx.Msg.FirstLine != nil && ctx.Msg.FirstLine.Req != nil {
			lhs = ctx.Msg.FirstLine.Req.Method.String()
		}
	case "uri":
		lhs = ctx.RURI
	default:
		if e.LeftPV != PVNone {
			lhs, _ = resolvePV(e.LeftPV, ctx.Msg, ctx)
		} else if len(e.LeftStr) > 0 {
			lower := strings.ToLower(e.LeftStr)
			if strings.HasPrefix(lower, "$var(") && strings.HasSuffix(lower, ")") {
				name := e.LeftStr[len("$var(") : len(e.LeftStr)-1]
				ctx.mu.RLock()
				lhs = ctx.Vars[name]
				ctx.mu.RUnlock()
			}
		}
	}
	switch e.Op {
	case "==":
		return lhs == e.Right, nil
	case "!=":
		return lhs != e.Right, nil
	}
	return false, nil
}
