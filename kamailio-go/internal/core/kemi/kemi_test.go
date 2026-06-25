// SPDX-License-Identifier: GPL-2.0-or-later

package kemi

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/avp"
	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newMaxFwdMsg builds a SIPMsg carrying a Max-Forwards header with the
// given value. It is used by the maxfwd builtin tests.
func newMaxFwdMsg(value string) *parser.SIPMsg {
	return &parser.SIPMsg{
		MaxForwards: &parser.HdrField{Body: str.Mk(value)},
	}
}

// newRouteMsg builds a SIPMsg that has a Route header set, used by the
// rr.loose_route builtin test.
func newRouteMsg() *parser.SIPMsg {
	return &parser.SIPMsg{
		Route: &parser.HdrField{Body: str.Mk("<sip:proxy@example.com;lr>")},
	}
}

// newBufMsg builds a SIPMsg whose Buf contains the given bytes, used by
// the textops search builtin tests.
func newBufMsg(buf string) *parser.SIPMsg {
	return &parser.SIPMsg{Buf: []byte(buf)}
}

// ---------------------------------------------------------------------------
// 1. 注册和查找模块/函数 (Register and find modules/functions)
// ---------------------------------------------------------------------------

// TestRegisterModule_FindFunc verifies that registering a module makes
// its functions discoverable by both bare and qualified names.
func TestRegisterModule_FindFunc(t *testing.T) {
	e := New()
	mod := &KemiModule{
		Name: "testmod",
		Funcs: []KemiExport{
			{
				Name:       "greet",
				MinParams:  0,
				MaxParams:  0,
				ReturnType: RetInt,
				Func: func(msg *parser.SIPMsg, params ...interface{}) int {
					return KemiTrue
				},
			},
		},
	}
	if err := e.RegisterModule(mod); err != nil {
		t.Fatalf("RegisterModule failed: %v", err)
	}
	if !e.HasModule("testmod") {
		t.Error("HasModule returned false after registration")
	}
	if e.HasModule("nope") {
		t.Error("HasModule returned true for unregistered module")
	}
	if e.FindFunc("greet") == nil {
		t.Error("FindFunc by bare name returned nil")
	}
	if e.FindFunc("testmod.greet") == nil {
		t.Error("FindFunc by qualified name returned nil")
	}
	if e.FindFunc("testmod.missing") != nil {
		t.Error("FindFunc returned non-nil for missing function")
	}
	if e.FindFuncByModule("testmod", "greet") == nil {
		t.Error("FindFuncByModule returned nil")
	}
	if e.FindFuncByModule("testmod", "missing") != nil {
		t.Error("FindFuncByModule returned non-nil for missing function")
	}
	if e.FindFuncByModule("missing", "greet") != nil {
		t.Error("FindFuncByModule returned non-nil for missing module")
	}
}

// TestRegisterModule_Duplicate verifies that registering the same
// module twice returns an error and does not corrupt the engine.
func TestRegisterModule_Duplicate(t *testing.T) {
	e := New()
	mod := &KemiModule{
		Name:  "dup",
		Funcs: []KemiExport{{Name: "f", MinParams: 0, MaxParams: 0, Func: func(*parser.SIPMsg, ...interface{}) int { return KemiTrue }}},
	}
	if err := e.RegisterModule(mod); err != nil {
		t.Fatalf("first RegisterModule failed: %v", err)
	}
	err := e.RegisterModule(mod)
	if err == nil {
		t.Fatal("expected error on duplicate registration, got nil")
	}
	if e.ModuleCount() != 1 {
		t.Errorf("expected module count 1 after duplicate, got %d", e.ModuleCount())
	}
}

// TestRegisterModule_Invalid verifies that nil modules and modules with
// empty names or empty function names are rejected.
func TestRegisterModule_Invalid(t *testing.T) {
	e := New()
	if err := e.RegisterModule(nil); err == nil {
		t.Error("expected error for nil module")
	}
	if err := e.RegisterModule(&KemiModule{Name: ""}); err == nil {
		t.Error("expected error for empty module name")
	}
	if err := e.RegisterModule(&KemiModule{
		Name:  "bad",
		Funcs: []KemiExport{{Name: "", Func: func(*parser.SIPMsg, ...interface{}) int { return KemiTrue }}},
	}); err == nil {
		t.Error("expected error for empty function name")
	}
}

// TestRegisterFunc_Incremental verifies that RegisterFunc creates the
// module on demand when it does not yet exist.
func TestRegisterFunc_Incremental(t *testing.T) {
	e := New()
	exp := &KemiExport{
		Name:       "ping",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
	}
	if err := e.RegisterFunc("inc", exp); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	if !e.HasModule("inc") {
		t.Error("expected module inc to be created")
	}
	if e.FindFunc("inc.ping") == nil {
		t.Error("expected inc.ping to be registered")
	}
	// Adding a second function to the same module should not error.
	exp2 := &KemiExport{
		Name:       "pong",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
	}
	if err := e.RegisterFunc("inc", exp2); err != nil {
		t.Fatalf("second RegisterFunc failed: %v", err)
	}
	if e.FuncCount() != 2 {
		t.Errorf("expected FuncCount 2, got %d", e.FuncCount())
	}
}

// TestRegisterFunc_Invalid verifies that RegisterFunc rejects empty
// module names and nil/empty exports.
func TestRegisterFunc_Invalid(t *testing.T) {
	e := New()
	if err := e.RegisterFunc("", &KemiExport{Name: "x"}); err == nil {
		t.Error("expected error for empty module name")
	}
	if err := e.RegisterFunc("m", nil); err == nil {
		t.Error("expected error for nil export")
	}
	if err := e.RegisterFunc("m", &KemiExport{Name: ""}); err == nil {
		t.Error("expected error for empty function name")
	}
}

// TestBareName_FirstWins verifies that when two modules register a
// function with the same bare name, the first registration wins for
// bare-name lookup while qualified lookup still resolves each.
func TestBareName_FirstWins(t *testing.T) {
	e := New()
	calls := int32(0)
	mkMod := func(name string) *KemiModule {
		return &KemiModule{
			Name: name,
			Funcs: []KemiExport{{
				Name:       "shared",
				MinParams:  0,
				MaxParams:  0,
				ReturnType: RetInt,
				Func: func(*parser.SIPMsg, ...interface{}) int {
					atomic.AddInt32(&calls, 1)
					return KemiTrue
				},
			}},
		}
	}
	if err := e.RegisterModule(mkMod("first")); err != nil {
		t.Fatalf("register first: %v", err)
	}
	if err := e.RegisterModule(mkMod("second")); err != nil {
		t.Fatalf("register second: %v", err)
	}
	// Bare name lookup must resolve to the first registration.
	exp := e.FindFunc("shared")
	if exp == nil {
		t.Fatal("FindFunc shared returned nil")
	}
	// Calling by bare name should invoke the first module's function.
	if _, err := e.Call("shared", nil); err != nil {
		t.Fatalf("Call shared failed: %v", err)
	}
	// Both qualified names must still resolve independently.
	if e.FindFunc("first.shared") == nil {
		t.Error("first.shared not found")
	}
	if e.FindFunc("second.shared") == nil {
		t.Error("second.shared not found")
	}
}

// ---------------------------------------------------------------------------
// 2. 调用函数 (正确参数) (Call functions with correct params)
// ---------------------------------------------------------------------------

// TestCall_CorrectParams verifies that Call invokes the function and
// returns its return code when parameters are valid.
func TestCall_CorrectParams(t *testing.T) {
	e := New()
	got := int32(0)
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "add",
		MinParams:  2,
		MaxParams:  2,
		ParamTypes: []ParamType{ParamInt, ParamInt},
		ReturnType: RetInt,
		Func: func(msg *parser.SIPMsg, params ...interface{}) int {
			atomic.AddInt32(&got, 1)
			return KemiTrue
		},
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	ret, err := e.Call("m.add", nil, 1, 2)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if ret != KemiTrue {
		t.Errorf("expected KemiTrue, got %d", ret)
	}
	if atomic.LoadInt32(&got) != 1 {
		t.Error("function was not invoked exactly once")
	}
}

// TestCallByModule verifies that CallByModule resolves the function
// within the named module and invokes it.
func TestCallByModule(t *testing.T) {
	e := New()
	if err := e.RegisterFunc("mod", &KemiExport{
		Name:       "fn",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	ret, err := e.CallByModule("mod", "fn", nil)
	if err != nil {
		t.Fatalf("CallByModule failed: %v", err)
	}
	if ret != KemiTrue {
		t.Errorf("expected KemiTrue, got %d", ret)
	}
}

// TestCall_PassthroughParams verifies that parameters are forwarded to
// the function implementation unchanged.
func TestCall_PassthroughParams(t *testing.T) {
	e := New()
	var seen []interface{}
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "echo",
		MinParams:  3,
		MaxParams:  3,
		ParamTypes: []ParamType{ParamString, ParamInt, ParamBool},
		ReturnType: RetInt,
		Func: func(msg *parser.SIPMsg, params ...interface{}) int {
			seen = params
			return KemiTrue
		},
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	if _, err := e.Call("m.echo", nil, "hi", 42, true); err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 params, got %d", len(seen))
	}
	if s, _ := convertToString(seen[0]); s != "hi" {
		t.Errorf("expected param 0 'hi', got %v", seen[0])
	}
}

// ---------------------------------------------------------------------------
// 3. 参数验证 (太少/太多参数) (Parameter validation - too few/too many)
// ---------------------------------------------------------------------------

// TestValidateParams_TooFew verifies that calling a function with fewer
// than MinParams parameters returns an error and does not invoke the
// function.
func TestValidateParams_TooFew(t *testing.T) {
	e := New()
	called := false
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "need2",
		MinParams:  2,
		MaxParams:  2,
		ParamTypes: []ParamType{ParamString, ParamString},
		ReturnType: RetInt,
		Func: func(*parser.SIPMsg, ...interface{}) int {
			called = true
			return KemiTrue
		},
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	_, err := e.Call("m.need2", nil, "only-one")
	if err == nil {
		t.Fatal("expected error for too few params, got nil")
	}
	if called {
		t.Error("function should not have been invoked")
	}
}

// TestValidateParams_TooMany verifies that calling a function with more
// than MaxParams parameters returns an error.
func TestValidateParams_TooMany(t *testing.T) {
	e := New()
	called := false
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "one",
		MinParams:  1,
		MaxParams:  1,
		ParamTypes: []ParamType{ParamString},
		ReturnType: RetInt,
		Func: func(*parser.SIPMsg, ...interface{}) int {
			called = true
			return KemiTrue
		},
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	_, err := e.Call("m.one", nil, "a", "b")
	if err == nil {
		t.Fatal("expected error for too many params, got nil")
	}
	if called {
		t.Error("function should not have been invoked")
	}
}

// TestValidateParams_VarParams verifies that a function declared with
// MaxParams=VarParams accepts any number of parameters at or above
// MinParams.
func TestValidateParams_VarParams(t *testing.T) {
	e := New()
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "varargs",
		MinParams:  1,
		MaxParams:  VarParams,
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	for _, n := range []int{1, 2, 5, 10} {
		params := make([]interface{}, n)
		for i := range params {
			params[i] = "x"
		}
		if _, err := e.Call("m.varargs", nil, params...); err != nil {
			t.Errorf("expected success with %d params, got %v", n, err)
		}
	}
	// Below MinParams should still fail.
	if _, err := e.Call("m.varargs", nil); err == nil {
		t.Error("expected error with 0 params below MinParams")
	}
}

// TestValidateParams_TypeMismatch verifies that a parameter whose value
// cannot be converted to the declared type is rejected.
func TestValidateParams_TypeMismatch(t *testing.T) {
	e := New()
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "intfn",
		MinParams:  1,
		MaxParams:  1,
		ParamTypes: []ParamType{ParamInt},
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	// A struct cannot be converted to int.
	if _, err := e.Call("m.intfn", nil, struct{}{}); err == nil {
		t.Error("expected error for non-int param on int-typed function")
	}
}

// ---------------------------------------------------------------------------
// 4. 参数类型转换 (Parameter type conversion)
// ---------------------------------------------------------------------------

// TestConvertParam_ToString verifies that ConvertParam converts common
// value types to strings.
func TestConvertParam_ToString(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want string
	}{
		{"string", "hello", "hello"},
		{"bytes", []byte("hi"), "hi"},
		{"int", 42, "42"},
		{"int64", int64(7), "7"},
		{"uint", uint(3), "3"},
		{"bool-true", true, "true"},
		{"bool-false", false, "false"},
		{"float", 3.5, "3.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ConvertParam(c.in, ParamString)
			if err != nil {
				t.Fatalf("ConvertParam failed: %v", err)
			}
			s, ok := got.(string)
			if !ok {
				t.Fatalf("expected string, got %T", got)
			}
			if s != c.want {
				t.Errorf("expected %q, got %q", c.want, s)
			}
		})
	}
}

// TestConvertParam_ToInt verifies that ConvertParam converts common
// value types to ints.
func TestConvertParam_ToInt(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want int
	}{
		{"int", 42, 42},
		{"int64", int64(7), 7},
		{"string-num", "17", 17},
		{"string-num-spaced", "  17  ", 17},
		{"bytes-num", []byte("9"), 9},
		{"bool-true", true, 1},
		{"bool-false", false, 0},
		{"float", 3.7, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ConvertParam(c.in, ParamInt)
			if err != nil {
				t.Fatalf("ConvertParam failed: %v", err)
			}
			n, ok := got.(int)
			if !ok {
				t.Fatalf("expected int, got %T", got)
			}
			if n != c.want {
				t.Errorf("expected %d, got %d", c.want, n)
			}
		})
	}
}

// TestConvertParam_ToBool verifies that ConvertParam converts common
// value types to bools, accepting the same string spellings as
// Kamailio's boolean parameters.
func TestConvertParam_ToBool(t *testing.T) {
	truthy := []interface{}{true, 1, "true", "yes", "on", "1", "TRUE", "Yes"}
	for _, v := range truthy {
		got, err := ConvertParam(v, ParamBool)
		if err != nil {
			t.Errorf("ConvertParam(%v) failed: %v", v, err)
			continue
		}
		if !got.(bool) {
			t.Errorf("expected %v to convert to true, got false", v)
		}
	}
	falsy := []interface{}{false, 0, "false", "no", "off", "0", "FALSE", "No"}
	for _, v := range falsy {
		got, err := ConvertParam(v, ParamBool)
		if err != nil {
			t.Errorf("ConvertParam(%v) failed: %v", v, err)
			continue
		}
		if got.(bool) {
			t.Errorf("expected %v to convert to false, got true", v)
		}
	}
}

// TestConvertParam_Any verifies that ParamAny returns the value
// unchanged.
func TestConvertParam_Any(t *testing.T) {
	v := struct{ A int }{A: 1}
	got, err := ConvertParam(v, ParamAny)
	if err != nil {
		t.Fatalf("ConvertParam failed: %v", err)
	}
	if got != v {
		t.Errorf("ParamAny should return value unchanged")
	}
}

// TestConvertParam_Errors verifies that ConvertParam returns errors for
// nil values and unparseable strings.
func TestConvertParam_Errors(t *testing.T) {
	if _, err := ConvertParam(nil, ParamString); err == nil {
		t.Error("expected error converting nil to string")
	}
	if _, err := ConvertParam(nil, ParamInt); err == nil {
		t.Error("expected error converting nil to int")
	}
	if _, err := ConvertParam(nil, ParamBool); err == nil {
		t.Error("expected error converting nil to bool")
	}
	if _, err := ConvertParam("not-a-number", ParamInt); err == nil {
		t.Error("expected error converting non-numeric string to int")
	}
	if _, err := ConvertParam("not-a-bool", ParamBool); err == nil {
		t.Error("expected error converting non-bool string to bool")
	}
}

// TestConvertParam_PVar verifies that ParamPVar accepts string-like
// values and rejects others.
func TestConvertParam_PVar(t *testing.T) {
	got, err := ConvertParam("$fu", ParamPVar)
	if err != nil {
		t.Fatalf("ConvertParam failed: %v", err)
	}
	if s, _ := got.(string); s != "$fu" {
		t.Errorf("expected $fu, got %v", got)
	}
}

// TestParamType_String verifies the String method of ParamType.
func TestParamType_String(t *testing.T) {
	cases := map[ParamType]string{
		ParamString: "string",
		ParamInt:    "int",
		ParamBool:   "bool",
		ParamPVar:   "pvar",
		ParamAny:    "any",
	}
	for pt, want := range cases {
		if got := pt.String(); got != want {
			t.Errorf("ParamType(%d).String() = %q, want %q", pt, got, want)
		}
	}
}

// TestReturnType_String verifies the String method of ReturnType.
func TestReturnType_String(t *testing.T) {
	cases := map[ReturnType]string{
		RetInt:  "int",
		RetStr:  "str",
		RetBool: "bool",
		RetNone: "none",
	}
	for rt, want := range cases {
		if got := rt.String(); got != want {
			t.Errorf("ReturnType(%d).String() = %q, want %q", rt, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// 5. 内置函数调用 (sl_send_reply, is_myself 等) (Builtin function calls)
// ---------------------------------------------------------------------------

// newBuiltinEngine builds a fresh Engine with all builtins registered.
func newBuiltinEngine(t *testing.T) *Engine {
	t.Helper()
	e := New()
	if err := RegisterBuiltins(e); err != nil {
		t.Fatalf("RegisterBuiltins failed: %v", err)
	}
	return e
}

// TestBuiltin_ModulesRegistered verifies that all expected builtin
// modules are present after RegisterBuiltins.
func TestBuiltin_ModulesRegistered(t *testing.T) {
	e := newBuiltinEngine(t)
	want := []string{"sl", "rr", "maxfwd", "textops", "corex", "xlog", "pv", "avpops"}
	for _, m := range want {
		if !e.HasModule(m) {
			t.Errorf("expected builtin module %q to be registered", m)
		}
	}
	if e.ModuleCount() != len(want) {
		t.Errorf("expected %d modules, got %d", len(want), e.ModuleCount())
	}
	if e.FuncCount() == 0 {
		t.Error("expected at least one builtin function")
	}
}

// TestBuiltin_SL_SendReply verifies sl_send_reply accepts a valid code
// and reason and rejects out-of-range codes.
func TestBuiltin_SL_SendReply(t *testing.T) {
	e := newBuiltinEngine(t)
	ret, err := e.Call("sl.sl_send_reply", nil, 200, "OK")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if ret != KemiTrue {
		t.Errorf("expected KemiTrue for valid code, got %d", ret)
	}
	// Out-of-range code should be rejected by the function body.
	ret, _ = e.Call("sl.sl_send_reply", nil, 999, "Bad")
	if ret != KemiFalse {
		t.Errorf("expected KemiFalse for code 999, got %d", ret)
	}
}

// TestBuiltin_SL_Others verifies the remaining sl functions execute.
func TestBuiltin_SL_Others(t *testing.T) {
	e := newBuiltinEngine(t)
	if ret, _ := e.Call("sl.sl_reply_error", nil); ret != KemiTrue {
		t.Errorf("sl_reply_error returned %d", ret)
	}
	if ret, _ := e.Call("sl.sl_forward_reply", nil, "SIP/2.0"); ret != KemiTrue {
		t.Errorf("sl_forward_reply returned %d", ret)
	}
}

// TestBuiltin_RR verifies the rr functions execute and that
// loose_route reflects the presence of Route headers.
func TestBuiltin_RR(t *testing.T) {
	e := newBuiltinEngine(t)
	if ret, _ := e.Call("rr.record_route", nil); ret != KemiTrue {
		t.Errorf("record_route returned %d", ret)
	}
	if ret, _ := e.Call("rr.record_route", nil, "1.2.3.4"); ret != KemiTrue {
		t.Errorf("record_route with param returned %d", ret)
	}
	if ret, _ := e.Call("rr.record_route_preset", nil, "1.2.3.4"); ret != KemiTrue {
		t.Errorf("record_route_preset returned %d", ret)
	}
	if ret, _ := e.Call("rr.add_rr_param", nil, "lr=yes"); ret != KemiTrue {
		t.Errorf("add_rr_param returned %d", ret)
	}
	// loose_route returns false without Route, true with Route.
	if ret, _ := e.Call("rr.loose_route", &parser.SIPMsg{}); ret != KemiFalse {
		t.Errorf("loose_route without Route returned %d, want KemiFalse", ret)
	}
	if ret, _ := e.Call("rr.loose_route", newRouteMsg()); ret != KemiTrue {
		t.Errorf("loose_route with Route returned %d, want KemiTrue", ret)
	}
}

// TestBuiltin_MaxFwd verifies is_maxfwd_lt compares the message's
// Max-Forwards value against the given limit.
func TestBuiltin_MaxFwd(t *testing.T) {
	e := newBuiltinEngine(t)
	msg := newMaxFwdMsg("5")
	// 5 < 10 -> true
	if ret, _ := e.Call("maxfwd.is_maxfwd_lt", msg, 10); ret != KemiTrue {
		t.Errorf("is_maxfwd_lt(5,10) = %d, want KemiTrue", ret)
	}
	// 5 < 5 -> false
	if ret, _ := e.Call("maxfwd.is_maxfwd_lt", msg, 5); ret != KemiFalse {
		t.Errorf("is_maxfwd_lt(5,5) = %d, want KemiFalse", ret)
	}
	// No Max-Forwards header -> false.
	if ret, _ := e.Call("maxfwd.is_maxfwd_lt", &parser.SIPMsg{}, 10); ret != KemiFalse {
		t.Errorf("is_maxfwd_lt(no-header,10) = %d, want KemiFalse", ret)
	}
	if ret, _ := e.Call("maxfwd.decrement_maxfwd", msg); ret != KemiTrue {
		t.Errorf("decrement_maxfwd returned %d", ret)
	}
}

// TestBuiltin_TextOps verifies the textops search functions match
// against the message buffer.
func TestBuiltin_TextOps(t *testing.T) {
	e := newBuiltinEngine(t)
	msg := newBufMsg("INVITE sip:bob@example.com SIP/2.0\r\n\r\nbody-content")
	if ret, _ := e.Call("textops.search", msg, "bob@example"); ret != KemiTrue {
		t.Errorf("search matched returned %d, want KemiTrue", ret)
	}
	if ret, _ := e.Call("textops.search", msg, "nobody@home"); ret != KemiFalse {
		t.Errorf("search unmatched returned %d, want KemiFalse", ret)
	}
	if ret, _ := e.Call("textops.search_body", msg, "body-content"); ret != KemiTrue {
		t.Errorf("search_body returned %d, want KemiTrue", ret)
	}
	if ret, _ := e.Call("textops.subst", msg, "/a/b/"); ret != KemiTrue {
		t.Errorf("subst returned %d", ret)
	}
	if ret, _ := e.Call("textops.subst_uri", msg, "/a/b/"); ret != KemiTrue {
		t.Errorf("subst_uri returned %d", ret)
	}
	if ret, _ := e.Call("textops.append_to_reply", msg, "X-Test: yes"); ret != KemiTrue {
		t.Errorf("append_to_reply returned %d", ret)
	}
}

// TestBuiltin_CoreX verifies corex.is_myself recognises local
// addresses.
func TestBuiltin_CoreX(t *testing.T) {
	e := newBuiltinEngine(t)
	msg := &parser.SIPMsg{}
	locals := []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"sip:user@127.0.0.1:5060",
		"sip:user@localhost;transport=tcp",
	}
	for _, uri := range locals {
		if ret, _ := e.Call("corex.is_myself", msg, uri); ret != KemiTrue {
			t.Errorf("is_myself(%q) = %d, want KemiTrue", uri, ret)
		}
	}
	remotes := []string{
		"sip:user@example.com",
		"example.com",
		"1.2.3.4",
	}
	for _, uri := range remotes {
		if ret, _ := e.Call("corex.is_myself", msg, uri); ret != KemiFalse {
			t.Errorf("is_myself(%q) = %d, want KemiFalse", uri, ret)
		}
	}
	if ret, _ := e.Call("corex.force_send_socket", msg, "udp:1.2.3.4:5060"); ret != KemiTrue {
		t.Errorf("force_send_socket returned %d", ret)
	}
	if ret, _ := e.Call("corex.set_forward_no_connect", msg); ret != KemiTrue {
		t.Errorf("set_forward_no_connect returned %d", ret)
	}
}

// TestBuiltin_XLog verifies the xlog functions execute.
func TestBuiltin_XLog(t *testing.T) {
	e := newBuiltinEngine(t)
	if ret, _ := e.Call("xlog.xlog", nil, "L_INFO", "hello"); ret != KemiTrue {
		t.Errorf("xlog returned %d", ret)
	}
	if ret, _ := e.Call("xlog.xdbg", nil, "debug-msg"); ret != KemiTrue {
		t.Errorf("xdbg returned %d", ret)
	}
	if ret, _ := e.Call("xlog.xerr", nil, "err-msg"); ret != KemiTrue {
		t.Errorf("xerr returned %d", ret)
	}
}

// TestBuiltin_PV verifies pv_get/pv_set/pv_unset operate on the
// per-engine pseudo-variable store.
func TestBuiltin_PV(t *testing.T) {
	e := newBuiltinEngine(t)
	// Before set, get returns false.
	if ret, _ := e.Call("pv.pv_get", nil, "$foo"); ret != KemiFalse {
		t.Errorf("pv_get before set returned %d, want KemiFalse", ret)
	}
	// Set then get returns true.
	if ret, _ := e.Call("pv.pv_set", nil, "$foo", "bar"); ret != KemiTrue {
		t.Errorf("pv_set returned %d", ret)
	}
	if ret, _ := e.Call("pv.pv_get", nil, "$foo"); ret != KemiTrue {
		t.Errorf("pv_get after set returned %d, want KemiTrue", ret)
	}
	// Unset then get returns false again.
	if ret, _ := e.Call("pv.pv_unset", nil, "$foo"); ret != KemiTrue {
		t.Errorf("pv_unset returned %d", ret)
	}
	if ret, _ := e.Call("pv.pv_get", nil, "$foo"); ret != KemiFalse {
		t.Errorf("pv_get after unset returned %d, want KemiFalse", ret)
	}
}

// TestBuiltin_AVPOps verifies avp_write/avp_check/avp_delete operate on
// the per-engine AVP store.
func TestBuiltin_AVPOps(t *testing.T) {
	e := newBuiltinEngine(t)
	// Write a value.
	if ret, _ := e.Call("avpops.avp_write", nil, "user", "alice"); ret != KemiTrue {
		t.Errorf("avp_write returned %d", ret)
	}
	// Check equality.
	if ret, _ := e.Call("avpops.avp_check", nil, "user", "eq", "alice"); ret != KemiTrue {
		t.Errorf("avp_check eq alice returned %d, want KemiTrue", ret)
	}
	// Check inequality.
	if ret, _ := e.Call("avpops.avp_check", nil, "user", "ne", "bob"); ret != KemiTrue {
		t.Errorf("avp_check ne bob returned %d, want KemiTrue", ret)
	}
	// Check equality against wrong value.
	if ret, _ := e.Call("avpops.avp_check", nil, "user", "eq", "bob"); ret != KemiFalse {
		t.Errorf("avp_check eq bob returned %d, want KemiFalse", ret)
	}
	// Check missing AVP.
	if ret, _ := e.Call("avpops.avp_check", nil, "missing", "eq", "x"); ret != KemiFalse {
		t.Errorf("avp_check on missing returned %d, want KemiFalse", ret)
	}
	// Delete then check.
	if ret, _ := e.Call("avpops.avp_delete", nil, "user"); ret != KemiTrue {
		t.Errorf("avp_delete returned %d", ret)
	}
	if ret, _ := e.Call("avpops.avp_check", nil, "user", "eq", "alice"); ret != KemiFalse {
		t.Errorf("avp_check after delete returned %d, want KemiFalse", ret)
	}
}

// TestBuiltin_EngineIsolation verifies that two engines built
// independently keep their pv/avpops state isolated.
func TestBuiltin_EngineIsolation(t *testing.T) {
	e1 := newBuiltinEngine(t)
	e2 := newBuiltinEngine(t)
	if _, err := e1.Call("pv.pv_set", nil, "$shared", "one"); err != nil {
		t.Fatalf("pv_set on e1 failed: %v", err)
	}
	// e2 must not see the value set on e1.
	if ret, _ := e2.Call("pv.pv_get", nil, "$shared"); ret != KemiFalse {
		t.Errorf("expected isolation, e2 saw value from e1: ret=%d", ret)
	}
}

// TestRegisterBuiltins_NilEngine verifies that calling RegisterBuiltins
// with a nil engine returns an error.
func TestRegisterBuiltins_NilEngine(t *testing.T) {
	err := RegisterBuiltins(nil)
	if err == nil {
		t.Fatal("expected error for nil engine, got nil")
	}
	if !errors.Is(err, errNilEngine) {
		t.Errorf("expected errNilEngine, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 6. 链式执行 (Chain execution)
// ---------------------------------------------------------------------------

// TestExecuteChain_AllTrue verifies that a chain of returning-KemiTrue
// calls runs to completion and returns KemiTrue.
func TestExecuteChain_AllTrue(t *testing.T) {
	e := newBuiltinEngine(t)
	ctx := NewExecContext(nil)
	calls := []FuncCall{
		{Module: "sl", Name: "sl_reply_error"},
		{Module: "xlog", Name: "xdbg", Params: []interface{}{"a"}},
		{Module: "rr", Name: "record_route"},
	}
	ret := e.ExecuteChain(ctx, calls)
	if ret != KemiTrue {
		t.Errorf("expected KemiTrue, got %d", ret)
	}
	if ctx.Error != nil {
		t.Errorf("expected no error, got %v", ctx.Error)
	}
	if ctx.Result != KemiTrue {
		t.Errorf("expected ctx.Result KemiTrue, got %d", ctx.Result)
	}
}

// TestExecuteChain_DropStops verifies that a KemiDrop return stops the
// chain and returns KemiDrop.
func TestExecuteChain_DropStops(t *testing.T) {
	e := New()
	dropped := int32(0)
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "dropfn",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func: func(*parser.SIPMsg, ...interface{}) int {
			atomic.AddInt32(&dropped, 1)
			return KemiDrop
		},
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "after",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func: func(*parser.SIPMsg, ...interface{}) int {
			t.Error("after should not be called after drop")
			return KemiTrue
		},
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	ctx := NewExecContext(nil)
	calls := []FuncCall{
		{Name: "dropfn"},
		{Name: "after"},
	}
	ret := e.ExecuteChain(ctx, calls)
	if ret != KemiDrop {
		t.Errorf("expected KemiDrop, got %d", ret)
	}
	if atomic.LoadInt32(&dropped) != 1 {
		t.Error("dropfn should have been called once")
	}
}

// TestExecuteChain_FalseStops verifies that a KemiFalse return stops the
// chain and returns KemiFalse.
func TestExecuteChain_FalseStops(t *testing.T) {
	e := New()
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "falsefn",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiFalse },
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "after",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func: func(*parser.SIPMsg, ...interface{}) int {
			t.Error("after should not be called after false")
			return KemiTrue
		},
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	ctx := NewExecContext(nil)
	calls := []FuncCall{
		{Name: "falsefn"},
		{Name: "after"},
	}
	ret := e.ExecuteChain(ctx, calls)
	if ret != KemiFalse {
		t.Errorf("expected KemiFalse, got %d", ret)
	}
	if ctx.Error != nil {
		t.Errorf("expected no ctx.Error for false return, got %v", ctx.Error)
	}
}

// TestExecuteChain_NotFound verifies that an unknown function in the
// chain sets ctx.Error and returns KemiFalse.
func TestExecuteChain_NotFound(t *testing.T) {
	e := New()
	ctx := NewExecContext(nil)
	calls := []FuncCall{{Name: "does-not-exist"}}
	ret := e.ExecuteChain(ctx, calls)
	if ret != KemiFalse {
		t.Errorf("expected KemiFalse, got %d", ret)
	}
	if ctx.Error == nil {
		t.Error("expected ctx.Error to be set")
	}
}

// TestExecuteChain_Empty verifies that an empty chain returns KemiTrue.
func TestExecuteChain_Empty(t *testing.T) {
	e := New()
	ctx := NewExecContext(nil)
	ret := e.ExecuteChain(ctx, nil)
	if ret != KemiTrue {
		t.Errorf("expected KemiTrue for empty chain, got %d", ret)
	}
}

// TestExecuteChain_NilContext verifies that a nil context is handled
// safely.
func TestExecuteChain_NilContext(t *testing.T) {
	e := New()
	ret := e.ExecuteChain(nil, []FuncCall{{Name: "x"}})
	if ret != KemiFalse {
		t.Errorf("expected KemiFalse for nil context, got %d", ret)
	}
}

// TestExecute_Single verifies that Execute invokes a single function and
// records the result in the context.
func TestExecute_Single(t *testing.T) {
	e := newBuiltinEngine(t)
	ctx := NewExecContext(nil)
	ret := e.Execute(ctx, "xlog.xdbg", "hello")
	if ret != KemiTrue {
		t.Errorf("expected KemiTrue, got %d", ret)
	}
	if ctx.Result != KemiTrue {
		t.Errorf("expected ctx.Result KemiTrue, got %d", ctx.Result)
	}
	if ctx.Error != nil {
		t.Errorf("expected no error, got %v", ctx.Error)
	}
}

// TestExecute_NotFound verifies that Execute on an unknown function
// sets ctx.Error and returns KemiFalse.
func TestExecute_NotFound(t *testing.T) {
	e := New()
	ctx := NewExecContext(nil)
	ret := e.Execute(ctx, "nope")
	if ret != KemiFalse {
		t.Errorf("expected KemiFalse, got %d", ret)
	}
	if ctx.Error == nil {
		t.Error("expected ctx.Error to be set")
	}
}

// TestExecute_NilContext verifies that Execute on a nil context returns
// KemiFalse without panicking.
func TestExecute_NilContext(t *testing.T) {
	e := New()
	if ret := e.Execute(nil, "anything"); ret != KemiFalse {
		t.Errorf("expected KemiFalse for nil context, got %d", ret)
	}
}

// TestNewExecContext verifies that NewExecContext initialises the Vars
// map and AVP store.
func TestNewExecContext(t *testing.T) {
	ctx := NewExecContext(nil)
	if ctx.Vars == nil {
		t.Error("expected Vars to be non-nil")
	}
	if ctx.AVPs == nil {
		t.Error("expected AVPs to be non-nil")
	}
	ctx.Vars["k"] = "v"
	if ctx.Vars["k"] != "v" {
		t.Error("Vars map not usable")
	}
	ctx.AVPs.AddString("a", "b")
	if v, ok := ctx.AVPs.First("a"); !ok || v.S != "b" {
		t.Errorf("AVP store not usable: %+v", v)
	}
}

// ---------------------------------------------------------------------------
// 7. 并发安全测试 (50 goroutine 并发调用) (Concurrency safety)
// ---------------------------------------------------------------------------

// TestConcurrent_Call verifies that concurrent Call invocations against
// a single engine are safe under the race detector.
func TestConcurrent_Call(t *testing.T) {
	e := New()
	var counter int64
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "inc",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func: func(*parser.SIPMsg, ...interface{}) int {
			atomic.AddInt64(&counter, 1)
			return KemiTrue
		},
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := e.Call("m.inc", nil); err != nil {
				t.Errorf("Call failed: %v", err)
			}
		}()
	}
	wg.Wait()
	if atomic.LoadInt64(&counter) != n {
		t.Errorf("expected %d invocations, got %d", n, counter)
	}
}

// TestConcurrent_RegisterAndCall verifies that concurrent registration
// and invocation against a single engine are safe under the race
// detector.
func TestConcurrent_RegisterAndCall(t *testing.T) {
	e := New()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n * 2)
	// Half the goroutines register distinct functions.
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("fn%d", i)
			_ = e.RegisterFunc("m", &KemiExport{
				Name:       name,
				MinParams:  0,
				MaxParams:  0,
				ReturnType: RetInt,
				Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
			})
		}(i)
	}
	// The other half repeatedly look up and call functions.
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("m.fn%d", i)
			_, _ = e.Call(name, nil)
		}(i)
	}
	wg.Wait()
	if e.FuncCount() != n {
		t.Errorf("expected %d functions, got %d", n, e.FuncCount())
	}
}

// TestConcurrent_PVSetGet verifies that concurrent pv_set/pv_get calls
// against the builtin engine are safe under the race detector.
func TestConcurrent_PVSetGet(t *testing.T) {
	e := newBuiltinEngine(t)
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, _ = e.Call("pv.pv_set", nil, fmt.Sprintf("$k%d", i%5), fmt.Sprintf("v%d", i))
		}(i)
		go func(i int) {
			defer wg.Done()
			_, _ = e.Call("pv.pv_get", nil, fmt.Sprintf("$k%d", i%5))
		}(i)
	}
	wg.Wait()
}

// TestConcurrent_ListFunctions verifies that concurrent ListFunctions
// reads are safe under the race detector while a writer registers
// modules.
func TestConcurrent_ListFunctions(t *testing.T) {
	e := New()
	// Pre-register a few modules so readers have something to see.
	for i := 0; i < 5; i++ {
		_ = e.RegisterFunc("m", &KemiExport{
			Name:       fmt.Sprintf("f%d", i),
			MinParams:  0,
			MaxParams:  0,
			ReturnType: RetInt,
			Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
		})
	}
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = e.RegisterFunc("m", &KemiExport{
				Name:       fmt.Sprintf("g%d", i),
				MinParams:  0,
				MaxParams:  0,
				ReturnType: RetInt,
				Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
			})
		}(i)
		go func() {
			defer wg.Done()
			_ = e.ListFunctions()
			_ = e.FuncCount()
			_ = e.ModuleCount()
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// 8. DefaultEngine 单例模式 (DefaultEngine singleton)
// ---------------------------------------------------------------------------

// TestDefaultEngine_Singleton verifies that DefaultEngine returns the
// same instance across calls until Init resets it.
func TestDefaultEngine_Singleton(t *testing.T) {
	Init()
	a := DefaultEngine()
	b := DefaultEngine()
	if a != b {
		t.Fatal("DefaultEngine returned different instances")
	}
	// Registering through the package-level helper must affect the
	// singleton.
	if err := RegisterFunc("singletonmod", &KemiExport{
		Name:       "fn",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	if !DefaultEngine().HasModule("singletonmod") {
		t.Error("DefaultEngine did not see package-level registration")
	}
	if FindFunc("singletonmod.fn") == nil {
		t.Error("FindFunc package-level helper returned nil")
	}
}

// TestDefaultEngine_InitResets verifies that Init replaces the singleton
// with a fresh engine.
func TestDefaultEngine_InitResets(t *testing.T) {
	Init()
	e := DefaultEngine()
	_ = e.RegisterFunc("temp", &KemiExport{
		Name:       "fn",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
	})
	if !DefaultEngine().HasModule("temp") {
		t.Fatal("expected temp module before reset")
	}
	Init()
	if DefaultEngine().HasModule("temp") {
		t.Error("expected temp module to be gone after Init")
	}
	if DefaultEngine() == e {
		t.Error("expected Init to create a new engine instance")
	}
}

// TestDefaultEngine_PackageHelpers verifies that the package-level Call
// helper delegates to the default engine.
func TestDefaultEngine_PackageHelpers(t *testing.T) {
	Init()
	if err := RegisterFunc("ph", &KemiExport{
		Name:       "fn",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	ret, err := Call("ph.fn", nil)
	if err != nil {
		t.Fatalf("package-level Call failed: %v", err)
	}
	if ret != KemiTrue {
		t.Errorf("expected KemiTrue, got %d", ret)
	}
	ret, err = CallByModule("ph", "fn", nil)
	if err != nil {
		t.Fatalf("package-level CallByModule failed: %v", err)
	}
	if ret != KemiTrue {
		t.Errorf("expected KemiTrue, got %d", ret)
	}
	if ListFunctions() == nil {
		t.Error("package-level ListFunctions returned nil")
	}
	if ListModuleFunctions("ph") == nil {
		t.Error("package-level ListModuleFunctions returned nil")
	}
	if ListModuleFunctions("nope") != nil {
		t.Error("expected nil for unknown module")
	}
}

// ---------------------------------------------------------------------------
// 9. 函数列表查询 (Function list queries)
// ---------------------------------------------------------------------------

// TestListFunctions verifies that ListFunctions returns all functions in
// registration order.
func TestListFunctions(t *testing.T) {
	e := New()
	mods := []string{"a", "b", "c"}
	for _, m := range mods {
		if err := e.RegisterModule(&KemiModule{
			Name:  m,
			Funcs: []KemiExport{{Name: m + "_fn", MinParams: 0, MaxParams: 0, Func: func(*parser.SIPMsg, ...interface{}) int { return KemiTrue }}},
		}); err != nil {
			t.Fatalf("RegisterModule %s failed: %v", m, err)
		}
	}
	fns := e.ListFunctions()
	if len(fns) != 3 {
		t.Fatalf("expected 3 functions, got %d", len(fns))
	}
	// Order must match registration order.
	want := []string{"a_fn", "b_fn", "c_fn"}
	for i, w := range want {
		if fns[i].Name != w {
			t.Errorf("ListFunctions[%d] = %q, want %q", i, fns[i].Name, w)
		}
	}
}

// TestListModuleFunctions verifies that ListModuleFunctions returns the
// functions of a specific module and nil for unknown modules.
func TestListModuleFunctions(t *testing.T) {
	e := New()
	if err := e.RegisterModule(&KemiModule{
		Name: "m",
		Funcs: []KemiExport{
			{Name: "f1", MinParams: 0, MaxParams: 0, Func: func(*parser.SIPMsg, ...interface{}) int { return KemiTrue }},
			{Name: "f2", MinParams: 0, MaxParams: 0, Func: func(*parser.SIPMsg, ...interface{}) int { return KemiTrue }},
		},
	}); err != nil {
		t.Fatalf("RegisterModule failed: %v", err)
	}
	fns := e.ListModuleFunctions("m")
	if len(fns) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(fns))
	}
	if fns[0].Name != "f1" || fns[1].Name != "f2" {
		t.Errorf("unexpected function names: %q, %q", fns[0].Name, fns[1].Name)
	}
	if e.ListModuleFunctions("nope") != nil {
		t.Error("expected nil for unknown module")
	}
}

// TestListModuleFunctions_Immutable verifies that mutating the slice
// returned by ListModuleFunctions does not affect the engine's
// internal state.
func TestListModuleFunctions_Immutable(t *testing.T) {
	e := New()
	if err := e.RegisterModule(&KemiModule{
		Name:  "m",
		Funcs: []KemiExport{{Name: "f1", MinParams: 0, MaxParams: 0, Func: func(*parser.SIPMsg, ...interface{}) int { return KemiTrue }}},
	}); err != nil {
		t.Fatalf("RegisterModule failed: %v", err)
	}
	fns := e.ListModuleFunctions("m")
	fns[0].Name = "mutated"
	// Re-fetch and ensure the engine's copy is unchanged.
	again := e.ListModuleFunctions("m")
	if again[0].Name != "f1" {
		t.Errorf("engine state was mutated: got %q, want %q", again[0].Name, "f1")
	}
}

// TestCounts verifies ModuleCount and FuncCount reflect registrations.
func TestCounts(t *testing.T) {
	e := New()
	if e.ModuleCount() != 0 || e.FuncCount() != 0 {
		t.Fatal("expected zero counts on fresh engine")
	}
	if err := e.RegisterModule(&KemiModule{
		Name: "m1",
		Funcs: []KemiExport{
			{Name: "a", MinParams: 0, MaxParams: 0, Func: func(*parser.SIPMsg, ...interface{}) int { return KemiTrue }},
			{Name: "b", MinParams: 0, MaxParams: 0, Func: func(*parser.SIPMsg, ...interface{}) int { return KemiTrue }},
		},
	}); err != nil {
		t.Fatalf("RegisterModule failed: %v", err)
	}
	if e.ModuleCount() != 1 {
		t.Errorf("expected ModuleCount 1, got %d", e.ModuleCount())
	}
	if e.FuncCount() != 2 {
		t.Errorf("expected FuncCount 2, got %d", e.FuncCount())
	}
}

// ---------------------------------------------------------------------------
// 10. 错误处理 (Error handling)
// ---------------------------------------------------------------------------

// TestCall_NotFound verifies that calling an unknown function returns
// KemiFalse and an error.
func TestCall_NotFound(t *testing.T) {
	e := New()
	ret, err := e.Call("does.not.exist", nil)
	if ret != KemiFalse {
		t.Errorf("expected KemiFalse, got %d", ret)
	}
	if err == nil {
		t.Error("expected error for unknown function")
	}
}

// TestCallByModule_NotFound verifies that CallByModule on an unknown
// function returns KemiFalse and an error.
func TestCallByModule_NotFound(t *testing.T) {
	e := New()
	ret, err := e.CallByModule("nope", "fn", nil)
	if ret != KemiFalse {
		t.Errorf("expected KemiFalse, got %d", ret)
	}
	if err == nil {
		t.Error("expected error for unknown module/function")
	}
}

// TestValidateParams_NilExport verifies that ValidateParams rejects a
// nil export.
func TestValidateParams_NilExport(t *testing.T) {
	e := New()
	if err := e.ValidateParams(nil, nil); err == nil {
		t.Error("expected error for nil export")
	}
}

// TestExecute_ParamError verifies that Execute records a parameter
// validation error in the context.
func TestExecute_ParamError(t *testing.T) {
	e := New()
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "need1",
		MinParams:  1,
		MaxParams:  1,
		ParamTypes: []ParamType{ParamString},
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	ctx := NewExecContext(nil)
	ret := e.Execute(ctx, "m.need1") // missing required param
	if ret != KemiFalse {
		t.Errorf("expected KemiFalse, got %d", ret)
	}
	if ctx.Error == nil {
		t.Error("expected ctx.Error to be set")
	}
	if ctx.Result != KemiFalse {
		t.Errorf("expected ctx.Result KemiFalse, got %d", ctx.Result)
	}
}

// TestExecuteChain_ParamError verifies that a parameter validation
// failure in the chain aborts execution and sets ctx.Error.
func TestExecuteChain_ParamError(t *testing.T) {
	e := New()
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "need1",
		MinParams:  1,
		MaxParams:  1,
		ParamTypes: []ParamType{ParamString},
		ReturnType: RetInt,
		Func:       func(*parser.SIPMsg, ...interface{}) int { return KemiTrue },
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	called := int32(0)
	if err := e.RegisterFunc("m", &KemiExport{
		Name:       "after",
		MinParams:  0,
		MaxParams:  0,
		ReturnType: RetInt,
		Func: func(*parser.SIPMsg, ...interface{}) int {
			atomic.AddInt32(&called, 1)
			return KemiTrue
		},
	}); err != nil {
		t.Fatalf("RegisterFunc failed: %v", err)
	}
	ctx := NewExecContext(nil)
	calls := []FuncCall{
		{Name: "m.need1"}, // missing param -> error
		{Name: "after"},   // should not run
	}
	ret := e.ExecuteChain(ctx, calls)
	if ret != KemiFalse {
		t.Errorf("expected KemiFalse, got %d", ret)
	}
	if ctx.Error == nil {
		t.Error("expected ctx.Error to be set")
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Error("after should not have been called after error")
	}
}

// TestRegisterBuiltins_Twice verifies that registering builtins twice
// on the same engine returns an error (modules already registered).
func TestRegisterBuiltins_Twice(t *testing.T) {
	e := New()
	if err := RegisterBuiltins(e); err != nil {
		t.Fatalf("first RegisterBuiltins failed: %v", err)
	}
	err := RegisterBuiltins(e)
	if err == nil {
		t.Fatal("expected error on second RegisterBuiltins, got nil")
	}
}

// ---------------------------------------------------------------------------
// Internal helper coverage
// ---------------------------------------------------------------------------

// TestIsMyself covers the isMyself helper for a range of inputs.
func TestIsMyself(t *testing.T) {
	if isMyself("localhost") != KemiTrue {
		t.Error("localhost should be myself")
	}
	if isMyself("127.0.0.1") != KemiTrue {
		t.Error("127.0.0.1 should be myself")
	}
	if isMyself("::1") != KemiTrue {
		t.Error("::1 should be myself")
	}
	if isMyself("") != KemiTrue {
		t.Error("empty should be myself")
	}
	if isMyself("example.com") != KemiFalse {
		t.Error("example.com should not be myself")
	}
}

// TestExtractHost covers the extractHost helper.
func TestExtractHost(t *testing.T) {
	cases := map[string]string{
		"sip:user@host.com:5060": "host.com",
		"sip:host.com;transport=tcp": "host.com",
		"<sip:host.com>":          "host.com",
		"host.com":                "host.com",
		"sip:user@127.0.0.1":      "127.0.0.1",
	}
	for in, want := range cases {
		if got := extractHost(in); got != want {
			t.Errorf("extractHost(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMaxForwardsValue covers the maxForwardsValue helper.
func TestMaxForwardsValue(t *testing.T) {
	if v := maxForwardsValue(newMaxFwdMsg("70")); v != 70 {
		t.Errorf("expected 70, got %d", v)
	}
	if v := maxForwardsValue(newMaxFwdMsg("  5 ")); v != 5 {
		t.Errorf("expected 5, got %d", v)
	}
	if v := maxForwardsValue(newMaxFwdMsg("not-a-number")); v != -1 {
		t.Errorf("expected -1 for unparseable, got %d", v)
	}
	if v := maxForwardsValue(&parser.SIPMsg{}); v != -1 {
		t.Errorf("expected -1 for nil MaxForwards, got %d", v)
	}
	if v := maxForwardsValue(nil); v != -1 {
		t.Errorf("expected -1 for nil msg, got %d", v)
	}
}

// TestAvpCheck covers the avpCheck helper for both string and int AVPs.
func TestAvpCheck(t *testing.T) {
	// String equality.
	if got := avpCheck(avp.Value{Kind: avp.KindString, S: "alice"}, "eq", "alice"); got != KemiTrue {
		t.Errorf("eq string match = %d, want KemiTrue", got)
	}
	if got := avpCheck(avp.Value{Kind: avp.KindString, S: "alice"}, "eq", "bob"); got != KemiFalse {
		t.Errorf("eq string mismatch = %d, want KemiFalse", got)
	}
	if got := avpCheck(avp.Value{Kind: avp.KindString, S: "alice"}, "==", "alice"); got != KemiTrue {
		t.Errorf("== string match = %d, want KemiTrue", got)
	}
	// String inequality.
	if got := avpCheck(avp.Value{Kind: avp.KindString, S: "alice"}, "ne", "bob"); got != KemiTrue {
		t.Errorf("ne string = %d, want KemiTrue", got)
	}
	if got := avpCheck(avp.Value{Kind: avp.KindString, S: "alice"}, "!=", "bob"); got != KemiTrue {
		t.Errorf("!= string = %d, want KemiTrue", got)
	}
	// Int equality.
	if got := avpCheck(avp.Value{Kind: avp.KindInt, I: 42}, "eq", "42"); got != KemiTrue {
		t.Errorf("eq int match = %d, want KemiTrue", got)
	}
	if got := avpCheck(avp.Value{Kind: avp.KindInt, I: 42}, "ne", "7"); got != KemiTrue {
		t.Errorf("ne int = %d, want KemiTrue", got)
	}
	// Unknown operator.
	if got := avpCheck(avp.Value{Kind: avp.KindString, S: "x"}, "xx", "x"); got != KemiFalse {
		t.Errorf("unknown op = %d, want KemiFalse", got)
	}
}

// TestQualifiedKey covers the qualifiedKey helper.
func TestQualifiedKey(t *testing.T) {
	if got := qualifiedKey("mod", "fn"); got != "mod.fn" {
		t.Errorf("qualifiedKey = %q, want %q", got, "mod.fn")
	}
}

// TestIsQualified covers the isQualified helper.
func TestIsQualified(t *testing.T) {
	if !isQualified("mod.fn") {
		t.Error("mod.fn should be qualified")
	}
	if isQualified("fn") {
		t.Error("fn should not be qualified")
	}
}
