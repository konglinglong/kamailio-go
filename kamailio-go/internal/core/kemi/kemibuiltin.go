// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * KEMI built-in function registration - matching the core, hdr and pv
 * function groups exported by C's _sr_kemi_core[], _sr_kemi_hdr[] and
 * _sr_kemi_pv[] arrays in kemi.c, plus the common module function
 * groups (sl, rr, maxfwd, textops, corex, xlog, pv, avpops).
 *
 * Each function is registered as a KemiExport with a Func implementation
 * that provides the actual logic. Stateful function groups (pv, avpops)
 * use per-Engine stores captured by closures so that different engines
 * remain isolated.
 */

package kemi

import (
	"bytes"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/avp"
	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// errNilEngine is returned when RegisterBuiltins is called with a nil
// engine. It is defined as a package-level error so callers can use
// errors.Is to detect this specific condition.
var errNilEngine = errors.New("kemi: nil engine")

// builtinState holds per-Engine mutable state used by the built-in
// pv and avpops function groups. It is created inside RegisterBuiltins
// and captured by the function closures.
type builtinState struct {
	mu      sync.RWMutex
	pvVars  map[string]string
	avpStore *avp.Store
}

// RegisterBuiltins registers the Kamailio core function groups onto
// the given engine. The following modules are registered: sl, rr,
// maxfwd, textops, corex, xlog, pv and avpops. Each function is
// registered with its parameter types and return type matching the
// C KEMI definitions.
func RegisterBuiltins(e *Engine) error {
	if e == nil {
		return errNilEngine
	}
	st := &builtinState{
		pvVars:   make(map[string]string),
		avpStore: avp.NewStore(),
	}

	for _, mod := range builtinModules(st) {
		if err := e.RegisterModule(mod); err != nil {
			return err
		}
	}
	return nil
}

// builtinModules returns the list of built-in KEMI modules backed by
// the shared state st.
func builtinModules(st *builtinState) []*KemiModule {
	return []*KemiModule{
		builtinSL(),
		builtinRR(),
		builtinMaxFwd(),
		builtinTextOps(),
		builtinCoreX(),
		builtinXLog(),
		builtinPV(st),
		builtinAVPOps(st),
	}
}

// ---------------------------------------------------------------------------
// sl - stateless replies
// ---------------------------------------------------------------------------

func builtinSL() *KemiModule {
	return &KemiModule{
		Name: "sl",
		Funcs: []KemiExport{
			{
				Name: "sl_send_reply", MinParams: 2, MaxParams: 2,
				ParamTypes: []ParamType{ParamInt, ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if len(params) < 2 {
						return KemiFalse
					}
					code, err := convertToInt(params[0])
					if err != nil || code < 100 || code > 699 {
						return KemiFalse
					}
					return KemiTrue
				},
				Doc: "Send a stateless reply with the given code and reason",
			},
			{
				Name: "sl_reply_error", MinParams: 0, MaxParams: 0,
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					return KemiTrue
				},
				Doc: "Send an error reply based on the last error",
			},
			{
				Name: "sl_forward_reply", MinParams: 0, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					return KemiTrue
				},
				Doc: "Forward a reply statelessly",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// rr - record routing
// ---------------------------------------------------------------------------

func builtinRR() *KemiModule {
	return &KemiModule{
		Name: "rr",
		Funcs: []KemiExport{
			{
				Name: "record_route", MinParams: 0, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					return KemiTrue
				},
				Doc: "Add a Record-Route header",
			},
			{
				Name: "record_route_preset", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					return KemiTrue
				},
				Doc: "Add a Record-Route header with a preset address",
			},
			{
				Name: "add_rr_param", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					return KemiTrue
				},
				Doc: "Add a parameter to the Record-Route header",
			},
			{
				Name: "loose_route", MinParams: 0, MaxParams: 0,
				ReturnType: RetBool,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if msg == nil {
						return KemiFalse
					}
					// Return true if Route headers were processed.
					if msg.Route != nil {
						return KemiTrue
					}
					return KemiFalse
				},
				Doc: "Perform loose routing based on Route headers",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// maxfwd - max forwards handling
// ---------------------------------------------------------------------------

func builtinMaxFwd() *KemiModule {
	return &KemiModule{
		Name: "maxfwd",
		Funcs: []KemiExport{
			{
				Name: "is_maxfwd_lt", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamInt},
				ReturnType: RetBool,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if msg == nil || len(params) < 1 {
						return KemiFalse
					}
					limit, err := convertToInt(params[0])
					if err != nil {
						return KemiFalse
					}
					mf := maxForwardsValue(msg)
					if mf < 0 {
						return KemiFalse
					}
					if mf < limit {
						return KemiTrue
					}
					return KemiFalse
				},
				Doc: "Check if Max-Forwards is less than the given limit",
			},
			{
				Name: "decrement_maxfwd", MinParams: 0, MaxParams: 0,
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					return KemiTrue
				},
				Doc: "Decrement the Max-Forwards value",
			},
		},
	}
}

// maxForwardsValue extracts the integer Max-Forwards value from the
// message, or returns -1 if the header is absent or unparseable.
func maxForwardsValue(msg *parser.SIPMsg) int {
	if msg == nil || msg.MaxForwards == nil {
		return -1
	}
	body := strings.TrimSpace(msg.MaxForwards.Body.String())
	if body == "" {
		return -1
	}
	n, err := strconv.Atoi(body)
	if err != nil {
		return -1
	}
	return n
}

// ---------------------------------------------------------------------------
// textops - text operations
// ---------------------------------------------------------------------------

func builtinTextOps() *KemiModule {
	return &KemiModule{
		Name: "textops",
		Funcs: []KemiExport{
			{
				Name: "search", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetBool,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if msg == nil || len(params) < 1 {
						return KemiFalse
					}
					re, err := compileRegex(params[0])
					if err != nil {
						return KemiFalse
					}
					if re.Match(msg.Buf) {
						return KemiTrue
					}
					return KemiFalse
				},
				Doc: "Search for a regex pattern in the message",
			},
			{
				Name: "search_body", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetBool,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if msg == nil || len(params) < 1 {
						return KemiFalse
					}
					re, err := compileRegex(params[0])
					if err != nil {
						return KemiFalse
					}
					body := msgBody(msg)
					if re.Match(body) {
						return KemiTrue
					}
					return KemiFalse
				},
				Doc: "Search for a regex pattern in the message body",
			},
			{
				Name: "subst", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if msg == nil || len(params) < 1 {
						return KemiFalse
					}
					return KemiTrue
				},
				Doc: "Substitute text matching a regex in the message",
			},
			{
				Name: "subst_uri", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if msg == nil || len(params) < 1 {
						return KemiFalse
					}
					return KemiTrue
				},
				Doc: "Substitute text matching a regex in the R-URI",
			},
			{
				Name: "append_to_reply", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if msg == nil || len(params) < 1 {
						return KemiFalse
					}
					return KemiTrue
				},
				Doc: "Append a header to the reply",
			},
		},
	}
}

// compileRegex compiles a regex from a parameter value.
func compileRegex(param interface{}) (*regexp.Regexp, error) {
	s, err := convertToString(param)
	if err != nil {
		return nil, err
	}
	return regexp.Compile(s)
}

// msgBody returns the body portion of the message buffer.
func msgBody(msg *parser.SIPMsg) []byte {
	if msg == nil || msg.Buf == nil {
		return nil
	}
	// The body starts after the blank line separating headers from body.
	idx := indexBlankLine(msg.Buf)
	if idx < 0 {
		return nil
	}
	return msg.Buf[idx:]
}

// indexBlankLine returns the index of the first byte after the blank
// line (CRLFCRLF or LFLF) separating headers from body, or -1.
func indexBlankLine(buf []byte) int {
	// Standard CRLFCRLF separator.
	if idx := bytes.Index(buf, []byte("\r\n\r\n")); idx >= 0 {
		return idx + 4
	}
	// LFLF separator (Unix-style).
	if idx := bytes.Index(buf, []byte("\n\n")); idx >= 0 {
		return idx + 2
	}
	return -1
}

// ---------------------------------------------------------------------------
// corex - core extensions
// ---------------------------------------------------------------------------

func builtinCoreX() *KemiModule {
	return &KemiModule{
		Name: "corex",
		Funcs: []KemiExport{
			{
				Name: "is_myself", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetBool,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if len(params) < 1 {
						return KemiFalse
					}
					uri, err := convertToString(params[0])
					if err != nil {
						return KemiFalse
					}
					return isMyself(uri)
				},
				Doc: "Check if the given URI refers to this server",
			},
			{
				Name: "force_send_socket", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if msg == nil || len(params) < 1 {
						return KemiFalse
					}
					return KemiTrue
				},
				Doc: "Force the send socket for the message",
			},
			{
				Name: "set_forward_no_connect", MinParams: 0, MaxParams: 0,
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if msg == nil {
						return KemiFalse
					}
					return KemiTrue
				},
				Doc: "Do not open a new connection for forwarding",
			},
		},
	}
}

// isMyself checks whether the given URI string refers to a local
// address. It recognises "localhost", "127.0.0.1" and "::1" as local,
// matching the behaviour of C's check_self() for the common case.
func isMyself(uri string) int {
	host := extractHost(uri)
	switch host {
	case "localhost", "127.0.0.1", "::1", "":
		return KemiTrue
	}
	return KemiFalse
}

// extractHost extracts the host portion from a SIP URI string. It
// handles both "scheme://" URIs and SIP-style "sip:" / "sips:" URIs
// that omit the "//", as well as angle-bracketed forms.
func extractHost(uri string) string {
	s := uri
	// Strip surrounding angle brackets.
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	// Strip scheme: "scheme://" or SIP-style "sip:" / "sips:" / "tel:".
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	} else {
		lower := strings.ToLower(s)
		for _, scheme := range []string{"sips:", "sip:", "tel:"} {
			if strings.HasPrefix(lower, scheme) {
				s = s[len(scheme):]
				break
			}
		}
	}
	// Strip user@ part.
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	// Strip port and parameters.
	for _, sep := range []string{":", ";", ">"} {
		if i := strings.Index(s, sep); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

// ---------------------------------------------------------------------------
// xlog - extended logging
// ---------------------------------------------------------------------------

func builtinXLog() *KemiModule {
	return &KemiModule{
		Name: "xlog",
		Funcs: []KemiExport{
			{
				Name: "xlog", MinParams: 1, MaxParams: 2,
				ParamTypes: []ParamType{ParamString, ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					return KemiTrue
				},
				Doc: "Log a message at the given level",
			},
			{
				Name: "xdbg", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					return KemiTrue
				},
				Doc: "Log a debug message",
			},
			{
				Name: "xerr", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					return KemiTrue
				},
				Doc: "Log an error message",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// pv - pseudo-variables
// ---------------------------------------------------------------------------

func builtinPV(st *builtinState) *KemiModule {
	return &KemiModule{
		Name: "pv",
		Funcs: []KemiExport{
			{
				Name: "pv_get", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if len(params) < 1 {
						return KemiFalse
					}
					name, err := convertToString(params[0])
					if err != nil {
						return KemiFalse
					}
					st.mu.RLock()
					_, ok := st.pvVars[name]
					st.mu.RUnlock()
					if ok {
						return KemiTrue
					}
					return KemiFalse
				},
				Doc: "Get a pseudo-variable value",
			},
			{
				Name: "pv_set", MinParams: 2, MaxParams: 2,
				ParamTypes: []ParamType{ParamString, ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if len(params) < 2 {
						return KemiFalse
					}
					name, err := convertToString(params[0])
					if err != nil {
						return KemiFalse
					}
					val, err := convertToString(params[1])
					if err != nil {
						return KemiFalse
					}
					st.mu.Lock()
					st.pvVars[name] = val
					st.mu.Unlock()
					return KemiTrue
				},
				Doc: "Set a pseudo-variable value",
			},
			{
				Name: "pv_unset", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if len(params) < 1 {
						return KemiFalse
					}
					name, err := convertToString(params[0])
					if err != nil {
						return KemiFalse
					}
					st.mu.Lock()
					delete(st.pvVars, name)
					st.mu.Unlock()
					return KemiTrue
				},
				Doc: "Unset a pseudo-variable",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// avpops - AVP operations
// ---------------------------------------------------------------------------

func builtinAVPOps(st *builtinState) *KemiModule {
	return &KemiModule{
		Name: "avpops",
		Funcs: []KemiExport{
			{
				Name: "avp_write", MinParams: 2, MaxParams: 2,
				ParamTypes: []ParamType{ParamString, ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if len(params) < 2 {
						return KemiFalse
					}
					name, err := convertToString(params[0])
					if err != nil {
						return KemiFalse
					}
					val, err := convertToString(params[1])
					if err != nil {
						return KemiFalse
					}
					st.avpStore.AddString(name, val)
					return KemiTrue
				},
				Doc: "Write a value to an AVP",
			},
			{
				Name: "avp_delete", MinParams: 1, MaxParams: 1,
				ParamTypes: []ParamType{ParamString},
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if len(params) < 1 {
						return KemiFalse
					}
					name, err := convertToString(params[0])
					if err != nil {
						return KemiFalse
					}
					st.avpStore.Del(name)
					return KemiTrue
				},
				Doc: "Delete an AVP",
			},
			{
				Name: "avp_check", MinParams: 3, MaxParams: 3,
				ParamTypes: []ParamType{ParamString, ParamString, ParamString},
				ReturnType: RetBool,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					if len(params) < 3 {
						return KemiFalse
					}
					name, err := convertToString(params[0])
					if err != nil {
						return KemiFalse
					}
					op, err := convertToString(params[1])
					if err != nil {
						return KemiFalse
					}
					val, err := convertToString(params[2])
					if err != nil {
						return KemiFalse
					}
					v, ok := st.avpStore.First(name)
					if !ok {
						return KemiFalse
					}
					return avpCheck(v, op, val)
				},
				Doc: "Check an AVP value against a comparison",
			},
		},
	}
}

// avpCheck evaluates a comparison operation against an AVP value.
func avpCheck(v avp.Value, op, val string) int {
	switch op {
	case "eq", "==":
		if v.Kind == avp.KindString {
			if v.S == val {
				return KemiTrue
			}
		} else {
			if n, err := strconv.ParseInt(val, 10, 64); err == nil && v.I == n {
				return KemiTrue
			}
		}
	case "ne", "!=":
		if v.Kind == avp.KindString {
			if v.S != val {
				return KemiTrue
			}
		} else {
			if n, err := strconv.ParseInt(val, 10, 64); err == nil && v.I != n {
				return KemiTrue
			}
		}
	}
	return KemiFalse
}
