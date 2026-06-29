// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Right-value expression engine tests
 *
 * Exercises constant evaluation, pseudo-variable resolution, arithmetic,
 * string concatenation, comparison, logical, regex, nested expressions,
 * type conversion and concurrency safety.
 */

package script

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// rvBuildSIP constructs a minimal SIP request for parser.ParseMsg.
func rvBuildSIP(method, ruri, from, to string) []byte {
	return []byte(method + " " + ruri + " SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK1\r\n" +
		"From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Call-ID: test-call-1@example.com\r\n" +
		"CSeq: 1 " + method + "\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

// rvMustParseMsg parses a SIP message and fails the test on error.
func rvMustParseMsg(t *testing.T, method, ruri, from, to string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(rvBuildSIP(method, ruri, from, to))
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	if err := parser.ParseMsgURI(msg); err != nil {
		t.Fatalf("ParseMsgURI: %v", err)
	}
	return msg
}

// rvMustParseRVal parses text and fails the test on error.
func rvMustParseRVal(t *testing.T, text string) RVal {
	t.Helper()
	rv, err := ParseRVal(text)
	if err != nil {
		t.Fatalf("ParseRVal(%q) error: %v", text, err)
	}
	return rv
}

// rvMustEval evaluates rv and fails the test on error.
func rvMustEval(t *testing.T, rv RVal, msg *parser.SIPMsg, ctx *ExecContext) RVal {
	t.Helper()
	val, err := EvalRVal(rv, msg, ctx)
	if err != nil {
		t.Fatalf("EvalRVal error: %v", err)
	}
	return val
}

// rvMustInt evaluates rv to an int and fails the test on error.
func rvMustInt(t *testing.T, rv RVal, msg *parser.SIPMsg, ctx *ExecContext) int {
	t.Helper()
	n, err := EvalRValInt(rv, msg, ctx)
	if err != nil {
		t.Fatalf("EvalRValInt error: %v", err)
	}
	return n
}

// rvMustStr evaluates rv to a string and fails the test on error.
func rvMustStr(t *testing.T, rv RVal, msg *parser.SIPMsg, ctx *ExecContext) string {
	t.Helper()
	s, err := EvalRValStr(rv, msg, ctx)
	if err != nil {
		t.Fatalf("EvalRValStr error: %v", err)
	}
	return s
}

// rvMustBool evaluates rv to a bool and fails the test on error.
func rvMustBool(t *testing.T, rv RVal, msg *parser.SIPMsg, ctx *ExecContext) bool {
	t.Helper()
	b, err := EvalRValBool(rv, msg, ctx)
	if err != nil {
		t.Fatalf("EvalRValBool error: %v", err)
	}
	return b
}

// ---------------------------------------------------------------------------
// 1. Constant evaluation
// ---------------------------------------------------------------------------

func TestRvConst_Int(t *testing.T) {
	rv := rvMustParseRVal(t, "42")
	val := rvMustEval(t, rv, nil, nil)
	if val.Type() != RvInt {
		t.Fatalf("expected RvInt, got %v", val.Type())
	}
	n := rvMustInt(t, rv, nil, nil)
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}
	if val.String() != "42" {
		t.Errorf("expected string '42', got %q", val.String())
	}
	if !val.Bool() {
		t.Errorf("expected bool true for non-zero int")
	}
}

func TestRvConst_ZeroInt(t *testing.T) {
	rv := rvMustParseRVal(t, "0")
	val := rvMustEval(t, rv, nil, nil)
	if val.Bool() {
		t.Errorf("expected bool false for zero int")
	}
}

func TestRvConst_String(t *testing.T) {
	rv := rvMustParseRVal(t, `"hello world"`)
	val := rvMustEval(t, rv, nil, nil)
	if val.Type() != RvStr {
		t.Fatalf("expected RvStr, got %v", val.Type())
	}
	s := rvMustStr(t, rv, nil, nil)
	if s != "hello world" {
		t.Errorf("expected 'hello world', got %q", s)
	}
	n := rvMustInt(t, rv, nil, nil)
	if n != 0 {
		t.Errorf("expected int 0 for non-numeric string, got %d", n)
	}
	if !val.Bool() {
		t.Errorf("expected bool true for non-empty string")
	}
}

func TestRvConst_EmptyString(t *testing.T) {
	rv := rvMustParseRVal(t, `""`)
	val := rvMustEval(t, rv, nil, nil)
	if val.Bool() {
		t.Errorf("expected bool false for empty string")
	}
}

func TestRvConst_NumericString(t *testing.T) {
	rv := rvMustParseRVal(t, `"123"`)
	n := rvMustInt(t, rv, nil, nil)
	if n != 123 {
		t.Errorf("expected 123 from numeric string, got %d", n)
	}
}

func TestRvConst_HexString(t *testing.T) {
	rv := rvMustParseRVal(t, `"0xff"`)
	n := rvMustInt(t, rv, nil, nil)
	if n != 255 {
		t.Errorf("expected 255 from hex string, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// 2. Pseudo-variable reference evaluation
// ---------------------------------------------------------------------------

func TestRvPVar_Method(t *testing.T) {
	msg := rvMustParseMsg(t, "INVITE", "sip:bob@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "example.com")
	rv := rvMustParseRVal(t, "method")
	s := rvMustStr(t, rv, msg, ctx)
	if s != "INVITE" {
		t.Errorf("expected 'INVITE', got %q", s)
	}
}

func TestRvPVar_ReqUser(t *testing.T) {
	msg := rvMustParseMsg(t, "INVITE", "sip:bob@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "example.com")
	rv := rvMustParseRVal(t, "$rU")
	s := rvMustStr(t, rv, msg, ctx)
	if s != "bob" {
		t.Errorf("expected 'bob', got %q", s)
	}
}

func TestRvPVar_FromDomain(t *testing.T) {
	msg := rvMustParseMsg(t, "INVITE", "sip:bob@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "example.com")
	rv := rvMustParseRVal(t, "$fd")
	s := rvMustStr(t, rv, msg, ctx)
	if s != "example.com" {
		t.Errorf("expected 'example.com', got %q", s)
	}
}

func TestRvPVar_ScriptVar(t *testing.T) {
	ctx := NewExecContext(nil, nil, "example.com")
	ctx.mu.Lock()
	ctx.Vars["myvar"] = "testvalue"
	ctx.mu.Unlock()

	rv := rvMustParseRVal(t, "$var(myvar)")
	s := rvMustStr(t, rv, nil, ctx)
	if s != "testvalue" {
		t.Errorf("expected 'testvalue', got %q", s)
	}
}

func TestRvPVar_UndefinedScriptVar(t *testing.T) {
	ctx := NewExecContext(nil, nil, "example.com")
	rv := rvMustParseRVal(t, "$var(undefined)")
	s := rvMustStr(t, rv, nil, ctx)
	if s != "" {
		t.Errorf("expected empty string for undefined var, got %q", s)
	}
}

// ---------------------------------------------------------------------------
// 3. Arithmetic operations
// ---------------------------------------------------------------------------

func TestRvArith_Add(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["a"] = "41"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, "$var(a) + 1")
	n := rvMustInt(t, rv, nil, ctx)
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}
}

func TestRvArith_Sub(t *testing.T) {
	rv := rvMustParseRVal(t, "100 - 37")
	n := rvMustInt(t, rv, nil, nil)
	if n != 63 {
		t.Errorf("expected 63, got %d", n)
	}
}

func TestRvArith_Mul(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["b"] = "21"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, "$var(b) * 2")
	n := rvMustInt(t, rv, nil, ctx)
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}
}

func TestRvArith_Div(t *testing.T) {
	rv := rvMustParseRVal(t, "100 / 4")
	n := rvMustInt(t, rv, nil, nil)
	if n != 25 {
		t.Errorf("expected 25, got %d", n)
	}
}

func TestRvArith_Mod(t *testing.T) {
	rv := rvMustParseRVal(t, "17 % 5")
	n := rvMustInt(t, rv, nil, nil)
	if n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
}

func TestRvArith_DivByZero(t *testing.T) {
	rv := rvMustParseRVal(t, "10 / 0")
	_, err := EvalRValInt(rv, nil, nil)
	if err == nil {
		t.Fatalf("expected error for division by zero")
	}
}

func TestRvArith_ModByZero(t *testing.T) {
	rv := rvMustParseRVal(t, "10 % 0")
	_, err := EvalRValInt(rv, nil, nil)
	if err == nil {
		t.Fatalf("expected error for modulo by zero")
	}
}

func TestRvArith_Bitwise(t *testing.T) {
	tests := []struct {
		expr     string
		expected int
	}{
		{"12 & 10", 8},
		{"12 | 10", 14},
		{"12 ^ 10", 6},
		{"1 << 4", 16},
		{"256 >> 4", 16},
	}
	for _, tc := range tests {
		rv := rvMustParseRVal(t, tc.expr)
		n := rvMustInt(t, rv, nil, nil)
		if n != tc.expected {
			t.Errorf("%s = %d, expected %d", tc.expr, n, tc.expected)
		}
	}
}

func TestRvArith_UnaryMinus(t *testing.T) {
	rv := rvMustParseRVal(t, "-42")
	n := rvMustInt(t, rv, nil, nil)
	if n != -42 {
		t.Errorf("expected -42, got %d", n)
	}
}

func TestRvArith_BitwiseNot(t *testing.T) {
	rv := rvMustParseRVal(t, "~0")
	n := rvMustInt(t, rv, nil, nil)
	if n != -1 {
		t.Errorf("expected -1, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// 4. String concatenation
// ---------------------------------------------------------------------------

func TestRvConcat_Plus(t *testing.T) {
	msg := rvMustParseMsg(t, "INVITE", "sip:bob@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "example.com")
	rv := rvMustParseRVal(t, `$rU + "@" + $fd`)
	s := rvMustStr(t, rv, msg, ctx)
	if s != "bob@example.com" {
		t.Errorf("expected 'bob@example.com', got %q", s)
	}
}

func TestRvConcat_DotOperator(t *testing.T) {
	rv := rvMustParseRVal(t, `"foo" . "bar"`)
	s := rvMustStr(t, rv, nil, nil)
	if s != "foobar" {
		t.Errorf("expected 'foobar', got %q", s)
	}
}

func TestRvConcat_Mixed(t *testing.T) {
	rv := rvMustParseRVal(t, `"count=" . 42`)
	s := rvMustStr(t, rv, nil, nil)
	if s != "count=42" {
		t.Errorf("expected 'count=42', got %q", s)
	}
}

func TestRvConcat_StringAndInt(t *testing.T) {
	rv := rvMustParseRVal(t, `"answer=" + 42`)
	s := rvMustStr(t, rv, nil, nil)
	if s != "answer=42" {
		t.Errorf("expected 'answer=42', got %q", s)
	}
}

// ---------------------------------------------------------------------------
// 5. Comparison operations
// ---------------------------------------------------------------------------

func TestRvCmp_IntEq(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["x"] = "10"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, "$var(x) == 10")
	b := rvMustBool(t, rv, nil, ctx)
	if !b {
		t.Errorf("expected true for $var(x) == 10")
	}
}

func TestRvCmp_IntNe(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["x"] = "10"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, "$var(x) != 20")
	b := rvMustBool(t, rv, nil, ctx)
	if !b {
		t.Errorf("expected true for $var(x) != 20")
	}
}

func TestRvCmp_StrEq(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["s"] = "hello"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, `$var(s) == "hello"`)
	b := rvMustBool(t, rv, nil, ctx)
	if !b {
		t.Errorf("expected true for $var(s) == \"hello\"")
	}
}

func TestRvCmp_StrNe(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["s"] = "hello"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, `$var(s) != "world"`)
	b := rvMustBool(t, rv, nil, ctx)
	if !b {
		t.Errorf("expected true for $var(s) != \"world\"")
	}
}

func TestRvCmp_PVNotEqual(t *testing.T) {
	msg := rvMustParseMsg(t, "INVITE", "sip:bob@example.com",
		"<sip:alice@example.com>", "<sip:alice@example.com>")
	ctx := NewExecContext(msg, nil, "example.com")
	rv := rvMustParseRVal(t, "$rU != $tU")
	b := rvMustBool(t, rv, msg, ctx)
	if !b {
		t.Errorf("expected true for $rU != $tU (bob != alice)")
	}
}

func TestRvCmp_LtGt(t *testing.T) {
	tests := []struct {
		expr     string
		expected bool
	}{
		{"5 < 10", true},
		{"10 < 5", false},
		{"5 > 10", false},
		{"10 > 5", true},
		{"5 <= 5", true},
		{"5 >= 5", true},
		{"4 <= 5", true},
		{"6 <= 5", false},
		{"6 >= 5", true},
		{"4 >= 5", false},
	}
	for _, tc := range tests {
		rv := rvMustParseRVal(t, tc.expr)
		b := rvMustBool(t, rv, nil, nil)
		if b != tc.expected {
			t.Errorf("%s = %v, expected %v", tc.expr, b, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// 6. Logical operations
// ---------------------------------------------------------------------------

func TestRvLogic_And(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.Flags = 1 << 1 // set flag 1
	rv := rvMustParseRVal(t, "isflagset(1) && !isflagset(2)")
	b := rvMustBool(t, rv, nil, ctx)
	if !b {
		t.Errorf("expected true for isflagset(1) && !isflagset(2)")
	}
}

func TestRvLogic_Or(t *testing.T) {
	msg := rvMustParseMsg(t, "INVITE", "sip:bob@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "example.com")
	rv := rvMustParseRVal(t, `method == "INVITE" || method == "ACK"`)
	b := rvMustBool(t, rv, msg, ctx)
	if !b {
		t.Errorf("expected true for method == INVITE || method == ACK")
	}
}

func TestRvLogic_Not(t *testing.T) {
	rv := rvMustParseRVal(t, "!0")
	b := rvMustBool(t, rv, nil, nil)
	if !b {
		t.Errorf("expected true for !0")
	}
}

func TestRvLogic_AndShortCircuit(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	rv := rvMustParseRVal(t, "0 && 1")
	b := rvMustBool(t, rv, nil, ctx)
	if b {
		t.Errorf("expected false for 0 && 1")
	}
}

func TestRvLogic_OrShortCircuit(t *testing.T) {
	rv := rvMustParseRVal(t, "1 || 0")
	b := rvMustBool(t, rv, nil, nil)
	if !b {
		t.Errorf("expected true for 1 || 0")
	}
}

func TestRvLogic_FlagAlias(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.Flags = 1 << 3
	rv := rvMustParseRVal(t, "flag(3)")
	b := rvMustBool(t, rv, nil, ctx)
	if !b {
		t.Errorf("expected true for flag(3)")
	}
}

// ---------------------------------------------------------------------------
// 7. Regex matching
// ---------------------------------------------------------------------------

func TestRvRegex_Match(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["num"] = "13800138000"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, `$var(num) =~ "^1[0-9]{10}$"`)
	b := rvMustBool(t, rv, nil, ctx)
	if !b {
		t.Errorf("expected true for regex match")
	}
}

func TestRvRegex_NoMatch(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["num"] = "abc"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, `$var(num) =~ "^1[0-9]{10}$"`)
	b := rvMustBool(t, rv, nil, ctx)
	if b {
		t.Errorf("expected false for regex non-match")
	}
}

func TestRvRegex_CaseInsensitive(t *testing.T) {
	rv := rvMustParseRVal(t, `"Hello" =~ "^hello$"`)
	b := rvMustBool(t, rv, nil, nil)
	if !b {
		t.Errorf("expected true for case-insensitive regex match")
	}
}

func TestRvRegex_InvalidPattern(t *testing.T) {
	rv := rvMustParseRVal(t, `"test" =~ "["`)
	_, err := EvalRValBool(rv, nil, nil)
	if err == nil {
		t.Fatalf("expected error for invalid regex")
	}
}

// ---------------------------------------------------------------------------
// 8. Nested expressions
// ---------------------------------------------------------------------------

func TestRvNested_Paren(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["a"] = "10"
	ctx.Vars["b"] = "20"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, "($var(a) + $var(b)) * 2")
	n := rvMustInt(t, rv, nil, ctx)
	if n != 60 {
		t.Errorf("expected 60, got %d", n)
	}
}

func TestRvNested_Deep(t *testing.T) {
	rv := rvMustParseRVal(t, "((2 + 3) * 4 - 1) / 1")
	n := rvMustInt(t, rv, nil, nil)
	if n != 19 {
		t.Errorf("expected 19, got %d", n)
	}
}

func TestRvNested_StringConcatWithArith(t *testing.T) {
	rv := rvMustParseRVal(t, `"result=" . (3 + 4)`)
	s := rvMustStr(t, rv, nil, nil)
	if s != "result=7" {
		t.Errorf("expected 'result=7', got %q", s)
	}
}

func TestRvNested_Complex(t *testing.T) {
	msg := rvMustParseMsg(t, "INVITE", "sip:bob@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "example.com")
	rv := rvMustParseRVal(t, `($rU == "bob") && ($fd == "example.com")`)
	b := rvMustBool(t, rv, msg, ctx)
	if !b {
		t.Errorf("expected true for complex nested expression")
	}
}

// ---------------------------------------------------------------------------
// 9. Type conversion (int <-> str)
// ---------------------------------------------------------------------------

func TestRvConv_IntCast(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["x"] = "41"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, "int($var(x)) + 1")
	n := rvMustInt(t, rv, nil, ctx)
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}
}

func TestRvConv_StrCast(t *testing.T) {
	rv := rvMustParseRVal(t, `str(42)`)
	s := rvMustStr(t, rv, nil, nil)
	if s != "42" {
		t.Errorf("expected '42', got %q", s)
	}
}

func TestRvConv_IntToStringBackToInt(t *testing.T) {
	rv := rvMustParseRVal(t, `int(str(123))`)
	n := rvMustInt(t, rv, nil, nil)
	if n != 123 {
		t.Errorf("expected 123, got %d", n)
	}
}

func TestRvConv_Strlen(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["s"] = "hello"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, "strlen($var(s))")
	n := rvMustInt(t, rv, nil, ctx)
	if n != 5 {
		t.Errorf("expected 5, got %d", n)
	}
}

func TestRvConv_Len(t *testing.T) {
	rv := rvMustParseRVal(t, `len("world")`)
	n := rvMustInt(t, rv, nil, nil)
	if n != 5 {
		t.Errorf("expected 5, got %d", n)
	}
}

func TestRvConv_Defined(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["y"] = "exists"
	ctx.mu.Unlock()

	rvDefined := rvMustParseRVal(t, "defined($var(y))")
	b := rvMustBool(t, rvDefined, nil, ctx)
	if !b {
		t.Errorf("expected true for defined($var(y))")
	}

	rvUndefined := rvMustParseRVal(t, "defined($var(missing))")
	b2 := rvMustBool(t, rvUndefined, nil, ctx)
	if b2 {
		t.Errorf("expected false for defined($var(missing))")
	}
}

func TestRvConv_Streq(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["a"] = "hello"
	ctx.Vars["b"] = "hello"
	ctx.mu.Unlock()
	rv := rvMustParseRVal(t, "streq($var(a), $var(b))")
	b := rvMustBool(t, rv, nil, ctx)
	if !b {
		t.Errorf("expected true for streq($var(a), $var(b))")
	}
}

func TestRvConv_Strempty(t *testing.T) {
	rv := rvMustParseRVal(t, `strempty("")`)
	b := rvMustBool(t, rv, nil, nil)
	if !b {
		t.Errorf("expected true for strempty(\"\")")
	}

	rv2 := rvMustParseRVal(t, `strempty("x")`)
	b2 := rvMustBool(t, rv2, nil, nil)
	if b2 {
		t.Errorf("expected false for strempty(\"x\")")
	}
}

// ---------------------------------------------------------------------------
// 10. Concurrency safety
// ---------------------------------------------------------------------------

func TestRvConcurrent_Eval(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	ctx.mu.Lock()
	ctx.Vars["a"] = "10"
	ctx.Vars["b"] = "20"
	ctx.mu.Unlock()

	rv := rvMustParseRVal(t, "($var(a) + $var(b)) * 2")

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			n, err := EvalRValInt(rv, nil, ctx)
			if err != nil {
				errs <- err
				return
			}
			if n != 60 {
				errs <- fmt.Errorf("expected 60, got %d", n)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent eval error: %v", err)
	}
}

func TestRvConcurrent_Regex(t *testing.T) {
	rv := rvMustParseRVal(t, `"Hello" =~ "^hello$"`)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			b, err := EvalRValBool(rv, nil, nil)
			if err != nil {
				errs <- err
				return
			}
			if !b {
				errs <- fmt.Errorf("expected true")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent regex error: %v", err)
	}
}

func TestRvConcurrent_VarAccess(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	rv := rvMustParseRVal(t, "$var(counter)")

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Writers
	for i := 0; i < goroutines; i++ {
		go func(val int) {
			defer wg.Done()
			ctx.mu.Lock()
			ctx.Vars["counter"] = fmt.Sprintf("%d", val)
			ctx.mu.Unlock()
		}(i)
	}

	// Readers
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := EvalRValStr(rv, nil, ctx)
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent var access error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Additional edge-case tests
// ---------------------------------------------------------------------------

func TestRvParse_Empty(t *testing.T) {
	_, err := ParseRVal("")
	if err == nil {
		t.Fatalf("expected error for empty expression")
	}
}

func TestRvParse_TrailingToken(t *testing.T) {
	_, err := ParseRVal("1 + 2 extra")
	if err == nil {
		t.Fatalf("expected error for trailing token")
	}
}

func TestRvParse_UnbalancedParen(t *testing.T) {
	_, err := ParseRVal("(1 + 2")
	if err == nil {
		t.Fatalf("expected error for unbalanced parenthesis")
	}
}

func TestRvEval_NilRVal(t *testing.T) {
	val, err := EvalRVal(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error for nil rval: %v", err)
	}
	if val == nil {
		t.Fatalf("expected non-nil result for nil rval")
	}
	if val.String() != "" {
		t.Errorf("expected empty string for nil rval, got %q", val.String())
	}
}

func TestRvEval_NilMsgCtx(t *testing.T) {
	rv := rvMustParseRVal(t, "$rU")
	val, err := EvalRVal(rv, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val.String() != "" {
		t.Errorf("expected empty string for nil msg, got %q", val.String())
	}
}

func TestRvOp_Name(t *testing.T) {
	tests := []struct {
		op   RvOp
		name string
	}{
		{RvOpPlus, "+"},
		{RvOpMinus, "-"},
		{RvOpMul, "*"},
		{RvOpDiv, "/"},
		{RvOpMod, "%"},
		{RvOpConcat, "."},
		{RvOpIntEq, "=="},
		{RvOpIntNe, "!="},
		{RvOpMatch, "=~"},
		{RvOpLAnd, "&&"},
		{RvOpLOr, "||"},
		{RvOpNot, "!"},
	}
	for _, tc := range tests {
		if got := tc.op.OpName(); got != tc.name {
			t.Errorf("OpName for op %d = %q, expected %q", tc.op, got, tc.name)
		}
	}
}

func TestRvConst_Type(t *testing.T) {
	intConst := &RvConst{IntVal: 42, IsInt: true}
	if intConst.Type() != RvInt {
		t.Errorf("expected RvInt for int constant")
	}

	strConst := &RvConst{StrVal: "hello"}
	if strConst.Type() != RvStr {
		t.Errorf("expected RvStr for string constant")
	}

	var nilConst *RvConst
	if nilConst.Type() != RvNone {
		t.Errorf("expected RvNone for nil constant")
	}
}

func TestRvExpr_Type(t *testing.T) {
	e := &RvExpr{Op: RvOpPlus}
	if e.Type() != RvRveExpr {
		t.Errorf("expected RvRveExpr for expression")
	}
}

func TestRvPVarRef_Type(t *testing.T) {
	r := &RvPVarRef{Name: "rU"}
	if r.Type() != RvPVar {
		t.Errorf("expected RvPVar for PV reference")
	}
}
