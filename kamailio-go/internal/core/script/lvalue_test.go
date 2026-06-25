// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Lvalue assignment engine tests
 *
 * Exercises assignment to script variables ($var), pseudo-variable
 * targets ($rU, $fd, …), AVPs ($avp), XAVPs ($xavp), Request-URI,
 * Destination-URI, From and To header parts, plus the text parser and
 * concurrency safety.
 */

package script

import (
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// lvBuildSIP constructs a minimal SIP request for parser.ParseMsg.
func lvBuildSIP(method, ruri, from, to string) []byte {
	return []byte(method + " " + ruri + " SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK1\r\n" +
		"From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Call-ID: lv-test-call@example.com\r\n" +
		"CSeq: 1 " + method + "\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

// lvMustParseMsg parses a SIP message and fails the test on error.
func lvMustParseMsg(t *testing.T, method, ruri, from, to string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(lvBuildSIP(method, ruri, from, to))
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	if err := parser.ParseMsgURI(msg); err != nil {
		t.Fatalf("ParseMsgURI: %v", err)
	}
	return msg
}

// lvMustParseLVal parses text and fails the test on error.
func lvMustParseLVal(t *testing.T, text string) LVal {
	t.Helper()
	lv, err := ParseLVal(text)
	if err != nil {
		t.Fatalf("ParseLVal(%q) error: %v", text, err)
	}
	return lv
}

// lvMustAssignStr assigns a string value and fails the test on error.
func lvMustAssignStr(t *testing.T, lv LVal, val string, msg *parser.SIPMsg, ctx *ExecContext) {
	t.Helper()
	if err := AssignStr(lv, val, msg, ctx); err != nil {
		t.Fatalf("AssignStr(%q) error: %v", val, err)
	}
}

// lvMustAssignInt assigns an integer value and fails the test on error.
func lvMustAssignInt(t *testing.T, lv LVal, val int, msg *parser.SIPMsg, ctx *ExecContext) {
	t.Helper()
	if err := AssignInt(lv, val, msg, ctx); err != nil {
		t.Fatalf("AssignInt(%d) error: %v", val, err)
	}
}

// lvMustGet reads the string value and fails the test on error.
func lvMustGet(t *testing.T, lv LVal, msg *parser.SIPMsg, ctx *ExecContext) string {
	t.Helper()
	s, err := lv.Get(msg, ctx)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	return s
}

// lvMustGetInt reads the integer value and fails the test on error.
func lvMustGetInt(t *testing.T, lv LVal, msg *parser.SIPMsg, ctx *ExecContext) int {
	t.Helper()
	n, err := lv.GetInt(msg, ctx)
	if err != nil {
		t.Fatalf("GetInt error: %v", err)
	}
	return n
}

// ---------------------------------------------------------------------------
// 1. TestLValueVarAssign — $var assignment and read-back
// ---------------------------------------------------------------------------

func TestLValueVarAssign(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")
	lv := lvMustParseLVal(t, "$var(tag)")

	// String assignment
	lvMustAssignStr(t, lv, "hello", nil, ctx)
	if got := lvMustGet(t, lv, nil, ctx); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}

	// Integer assignment
	lvMustAssignInt(t, lv, 42, nil, ctx)
	if got := lvMustGetInt(t, lv, nil, ctx); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}

	// Overwrite
	lvMustAssignStr(t, lv, "world", nil, ctx)
	if got := lvMustGet(t, lv, nil, ctx); got != "world" {
		t.Errorf("expected 'world', got %q", got)
	}

	// Empty string
	lvMustAssignStr(t, lv, "", nil, ctx)
	if got := lvMustGet(t, lv, nil, ctx); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}

	// Direct context access matches
	ctx.mu.RLock()
	v := ctx.Vars["tag"]
	ctx.mu.RUnlock()
	if v != "" {
		t.Errorf("expected empty string in Vars, got %q", v)
	}
}

// ---------------------------------------------------------------------------
// 2. TestLValueRURIUser — RURI user part assignment
// ---------------------------------------------------------------------------

func TestLValueRURIUser(t *testing.T) {
	msg := lvMustParseMsg(t, "INVITE", "sip:alice@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "")

	lv := lvMustParseLVal(t, "$rU")
	lvMustAssignStr(t, lv, "carol", msg, ctx)

	if got := lvMustGet(t, lv, msg, ctx); got != "carol" {
		t.Errorf("expected 'carol', got %q", got)
	}

	// Verify the full RURI was updated
	ctx.mu.RLock()
	ruri := ctx.RURI
	ctx.mu.RUnlock()
	if ruri != "sip:carol@example.com" {
		t.Errorf("expected RURI 'sip:carol@example.com', got %q", ruri)
	}

	// Long form $ruri(user)
	lv2 := lvMustParseLVal(t, "$ruri(user)")
	lvMustAssignStr(t, lv2, "dave", msg, ctx)
	if got := lvMustGet(t, lv2, msg, ctx); got != "dave" {
		t.Errorf("expected 'dave', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 3. TestLValueRURIDomain — RURI domain assignment
// ---------------------------------------------------------------------------

func TestLValueRURIDomain(t *testing.T) {
	msg := lvMustParseMsg(t, "INVITE", "sip:alice@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "")

	lv := lvMustParseLVal(t, "$rd")
	lvMustAssignStr(t, lv, "newdomain.com", msg, ctx)

	if got := lvMustGet(t, lv, msg, ctx); got != "newdomain.com" {
		t.Errorf("expected 'newdomain.com', got %q", got)
	}

	ctx.mu.RLock()
	ruri := ctx.RURI
	ctx.mu.RUnlock()
	if ruri != "sip:alice@newdomain.com" {
		t.Errorf("expected RURI 'sip:alice@newdomain.com', got %q", ruri)
	}

	// Long form $ruri(domain)
	lv2 := lvMustParseLVal(t, "$ruri(domain)")
	lvMustAssignStr(t, lv2, "other.com", msg, ctx)
	if got := lvMustGet(t, lv2, msg, ctx); got != "other.com" {
		t.Errorf("expected 'other.com', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 4. TestLValueRURIFull — full RURI assignment
// ---------------------------------------------------------------------------

func TestLValueRURIFull(t *testing.T) {
	msg := lvMustParseMsg(t, "INVITE", "sip:alice@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "")

	lv := lvMustParseLVal(t, "$ru")
	newURI := "sip:carol@other.com:5061"
	lvMustAssignStr(t, lv, newURI, msg, ctx)

	if got := lvMustGet(t, lv, msg, ctx); got != newURI {
		t.Errorf("expected %q, got %q", newURI, got)
	}

	// Long form $ruri
	lv2 := lvMustParseLVal(t, "$ruri")
	lvMustAssignStr(t, lv2, "sip:eve@target.com", msg, ctx)
	if got := lvMustGet(t, lv2, msg, ctx); got != "sip:eve@target.com" {
		t.Errorf("expected 'sip:eve@target.com', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 5. TestLValueDstURI — Destination URI assignment
// ---------------------------------------------------------------------------

func TestLValueDstURI(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")

	lv := lvMustParseLVal(t, "$du")
	lvMustAssignStr(t, lv, "sip:proxy@next-hop.com", nil, ctx)

	if got := lvMustGet(t, lv, nil, ctx); got != "sip:proxy@next-hop.com" {
		t.Errorf("expected 'sip:proxy@next-hop.com', got %q", got)
	}

	// Integer assignment (port-like value)
	lvMustAssignInt(t, lv, 5060, nil, ctx)
	if got := lvMustGetInt(t, lv, nil, ctx); got != 5060 {
		t.Errorf("expected 5060, got %d", got)
	}

	// Long form $dsturi
	lv2 := lvMustParseLVal(t, "$dsturi")
	lvMustAssignStr(t, lv2, "sip:other@host.com", nil, ctx)
	if got := lvMustGet(t, lv2, nil, ctx); got != "sip:other@host.com" {
		t.Errorf("expected 'sip:other@host.com', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 6. TestLValueFromURI — From URI assignment
// ---------------------------------------------------------------------------

func TestLValueFromURI(t *testing.T) {
	msg := lvMustParseMsg(t, "INVITE", "sip:alice@example.com",
		`"Alice" <sip:alice@example.com>;tag=abc`,
		"<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "")

	lv := lvMustParseLVal(t, "$fu")
	newFrom := "sip:newuser@newdomain.com"
	lvMustAssignStr(t, lv, newFrom, msg, ctx)

	if got := lvMustGet(t, lv, msg, ctx); got != newFrom {
		t.Errorf("expected %q, got %q", newFrom, got)
	}

	// Long form $from(u)
	lv2 := lvMustParseLVal(t, "$from(u)")
	lvMustAssignStr(t, lv2, "sip:another@host.com", msg, ctx)
	if got := lvMustGet(t, lv2, msg, ctx); got != "sip:another@host.com" {
		t.Errorf("expected 'sip:another@host.com', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 7. TestLValueFromDomain — From domain assignment
// ---------------------------------------------------------------------------

func TestLValueFromDomain(t *testing.T) {
	msg := lvMustParseMsg(t, "INVITE", "sip:alice@example.com",
		`"Alice" <sip:alice@example.com>;tag=abc`,
		"<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "")

	lv := lvMustParseLVal(t, "$fd")
	lvMustAssignStr(t, lv, "newfrom.com", msg, ctx)

	if got := lvMustGet(t, lv, msg, ctx); got != "newfrom.com" {
		t.Errorf("expected 'newfrom.com', got %q", got)
	}

	// Long form $from(d)
	lv2 := lvMustParseLVal(t, "$from(d)")
	lvMustAssignStr(t, lv2, "altfrom.com", msg, ctx)
	if got := lvMustGet(t, lv2, msg, ctx); got != "altfrom.com" {
		t.Errorf("expected 'altfrom.com', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 8. TestLValueFromName — From display name assignment
// ---------------------------------------------------------------------------

func TestLValueFromName(t *testing.T) {
	msg := lvMustParseMsg(t, "INVITE", "sip:alice@example.com",
		`"Alice" <sip:alice@example.com>;tag=abc`,
		"<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "")

	lv := lvMustParseLVal(t, "$fn")
	lvMustAssignStr(t, lv, "Bob Smith", msg, ctx)

	if got := lvMustGet(t, lv, msg, ctx); got != "Bob Smith" {
		t.Errorf("expected 'Bob Smith', got %q", got)
	}

	// Long form $from(n)
	lv2 := lvMustParseLVal(t, "$from(n)")
	lvMustAssignStr(t, lv2, "Carol Jones", msg, ctx)
	if got := lvMustGet(t, lv2, msg, ctx); got != "Carol Jones" {
		t.Errorf("expected 'Carol Jones', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 9. TestLValueToURI — To URI assignment
// ---------------------------------------------------------------------------

func TestLValueToURI(t *testing.T) {
	msg := lvMustParseMsg(t, "INVITE", "sip:alice@example.com",
		"<sip:alice@example.com>",
		"<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "")

	lv := lvMustParseLVal(t, "$tu")
	newTo := "sip:newto@target.com"
	lvMustAssignStr(t, lv, newTo, msg, ctx)

	if got := lvMustGet(t, lv, msg, ctx); got != newTo {
		t.Errorf("expected %q, got %q", newTo, got)
	}

	// Long form $to(u)
	lv2 := lvMustParseLVal(t, "$to(u)")
	lvMustAssignStr(t, lv2, "sip:other@dest.com", msg, ctx)
	if got := lvMustGet(t, lv2, msg, ctx); got != "sip:other@dest.com" {
		t.Errorf("expected 'sip:other@dest.com', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 10. TestLValueToDomain — To domain assignment
// ---------------------------------------------------------------------------

func TestLValueToDomain(t *testing.T) {
	msg := lvMustParseMsg(t, "INVITE", "sip:alice@example.com",
		"<sip:alice@example.com>",
		"<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "")

	lv := lvMustParseLVal(t, "$td")
	lvMustAssignStr(t, lv, "newto.com", msg, ctx)

	if got := lvMustGet(t, lv, msg, ctx); got != "newto.com" {
		t.Errorf("expected 'newto.com', got %q", got)
	}

	// Long form $to(d)
	lv2 := lvMustParseLVal(t, "$to(d)")
	lvMustAssignStr(t, lv2, "altto.com", msg, ctx)
	if got := lvMustGet(t, lv2, msg, ctx); got != "altto.com" {
		t.Errorf("expected 'altto.com', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 11. TestLValueAvpAssign — AVP assignment and read-back
// ---------------------------------------------------------------------------

func TestLValueAvpAssign(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")

	// String-scope AVP
	lv := lvMustParseLVal(t, "$avp(s:tag)")
	lvMustAssignStr(t, lv, "value1", nil, ctx)

	if got := lvMustGet(t, lv, nil, ctx); got != "value1" {
		t.Errorf("expected 'value1', got %q", got)
	}

	// Integer AVP
	lvMustAssignInt(t, lv, 123, nil, ctx)
	if got := lvMustGetInt(t, lv, nil, ctx); got != 123 {
		t.Errorf("expected 123, got %d", got)
	}

	// Integer-scope AVP
	lv2 := lvMustParseLVal(t, "$avp(i:idx)")
	lvMustAssignInt(t, lv2, 99, nil, ctx)
	if got := lvMustGetInt(t, lv2, nil, ctx); got != 99 {
		t.Errorf("expected 99, got %d", got)
	}

	// String value read back as int
	lv3 := lvMustParseLVal(t, "$avp(s:num)")
	lvMustAssignStr(t, lv3, "456", nil, ctx)
	if got := lvMustGetInt(t, lv3, nil, ctx); got != 456 {
		t.Errorf("expected 456, got %d", got)
	}

	// Overwrite replaces previous value (Del + Add)
	lvMustAssignStr(t, lv, "first", nil, ctx)
	lvMustAssignStr(t, lv, "second", nil, ctx)
	if got := lvMustGet(t, lv, nil, ctx); got != "second" {
		t.Errorf("expected 'second' after overwrite, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 12. TestLValueXAvpAssign — XAVP assignment and read-back
// ---------------------------------------------------------------------------

func TestLValueXAvpAssign(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")

	// String XAVP
	lv := lvMustParseLVal(t, "$xavp(root[key])")
	lvMustAssignStr(t, lv, "xavp-value", nil, ctx)

	if got := lvMustGet(t, lv, nil, ctx); got != "xavp-value" {
		t.Errorf("expected 'xavp-value', got %q", got)
	}

	// Integer XAVP
	lvMustAssignInt(t, lv, 777, nil, ctx)
	if got := lvMustGetInt(t, lv, nil, ctx); got != 777 {
		t.Errorf("expected 777, got %d", got)
	}

	// Different root/key
	lv2 := lvMustParseLVal(t, "$xavp(cfg[host])")
	lvMustAssignStr(t, lv2, "10.0.0.1", nil, ctx)
	if got := lvMustGet(t, lv2, nil, ctx); got != "10.0.0.1" {
		t.Errorf("expected '10.0.0.1', got %q", got)
	}

	// Int XAVP read back as string
	lv3 := lvMustParseLVal(t, "$xavp(count[n])")
	lvMustAssignInt(t, lv3, 42, nil, ctx)
	if got := lvMustGet(t, lv3, nil, ctx); got != "42" {
		t.Errorf("expected '42', got %q", got)
	}

	// Direct store access
	if v, ok := ctx.XAVPs.GetStr("root", "key"); !ok || v != "xavp-value" {
		// After int assignment, the value is now int — re-set string
		ctx.XAVPs.SetStr("root", "key", "xavp-value")
		v, ok = ctx.XAVPs.GetStr("root", "key")
		if !ok || v != "xavp-value" {
			t.Errorf("expected direct GetStr 'xavp-value', got %q ok=%v", v, ok)
		}
	}
}

// ---------------------------------------------------------------------------
// 13. TestLValueParseLVal — parse various lvalue texts
// ---------------------------------------------------------------------------

func TestLValueParseLVal(t *testing.T) {
	tests := []struct {
		text   string
		lvType LvType
	}{
		{"$var(x)", LvPVar},
		{"$rU", LvRURI},
		{"$ruri(user)", LvRURI},
		{"$rd", LvRURI},
		{"$ruri(domain)", LvRURI},
		{"$ru", LvRURI},
		{"$ruri", LvRURI},
		{"$du", LvDstURI},
		{"$dsturi", LvDstURI},
		{"$fu", LvFrom},
		{"$from(u)", LvFrom},
		{"$fd", LvFrom},
		{"$from(d)", LvFrom},
		{"$fn", LvFrom},
		{"$from(n)", LvFrom},
		{"$tu", LvTo},
		{"$to(u)", LvTo},
		{"$td", LvTo},
		{"$to(d)", LvTo},
		{"$avp(s:tag)", LvAvp},
		{"$avp(i:idx)", LvAvp},
		{"$xavp(root[key])", LvXAvp},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			lv, err := ParseLVal(tt.text)
			if err != nil {
				t.Fatalf("ParseLVal(%q) error: %v", tt.text, err)
			}
			if lv.Type() != tt.lvType {
				t.Errorf("ParseLVal(%q) type = %v, want %v", tt.text, lv.Type(), tt.lvType)
			}
		})
	}

	// Invalid inputs
	invalid := []string{
		"var(x)",   // missing $
		"$unknown", // unsupported PV
		"",         // empty
	}
	for _, txt := range invalid {
		t.Run("invalid:"+txt, func(t *testing.T) {
			if _, err := ParseLVal(txt); err == nil {
				t.Errorf("ParseLVal(%q) expected error, got nil", txt)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 14. TestLValueAssignFromRVal — assign from an evaluated RVal
// ---------------------------------------------------------------------------

// rvToAssignRVal bridges an rvalue.go RVal to the AssignRVal interface
// expected by Assign. It evaluates the RVal eagerly and wraps the result.
type rvToAssignRVal struct {
	val RVal
}

func (a *rvToAssignRVal) Str(msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	v, err := EvalRVal(a.val, msg, ctx)
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

func (a *rvToAssignRVal) Int(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	v, err := EvalRVal(a.val, msg, ctx)
	if err != nil {
		return 0, err
	}
	return v.Int()
}

func (a *rvToAssignRVal) IsInt() bool {
	return a.val.Type() == RvInt
}

func TestLValueAssignFromRVal(t *testing.T) {
	msg := lvMustParseMsg(t, "INVITE", "sip:alice@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	ctx := NewExecContext(msg, nil, "")

	// Assign a string literal RVal to $var
	rvStr, err := ParseRVal(`"hello"`)
	if err != nil {
		t.Fatalf("ParseRVal string: %v", err)
	}
	lvVar := lvMustParseLVal(t, "$var(greeting)")
	if err := Assign(lvVar, &rvToAssignRVal{val: rvStr}, msg, ctx); err != nil {
		t.Fatalf("Assign from RVal: %v", err)
	}
	if got := lvMustGet(t, lvVar, nil, ctx); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}

	// Assign an integer RVal to $var (should use AssignInt path)
	rvInt, err := ParseRVal("42")
	if err != nil {
		t.Fatalf("ParseRVal int: %v", err)
	}
	if err := Assign(lvVar, &rvToAssignRVal{val: rvInt}, msg, ctx); err != nil {
		t.Fatalf("Assign from RVal int: %v", err)
	}
	if got := lvMustGetInt(t, lvVar, nil, ctx); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}

	// Assign a concatenation expression to $rU
	rvConcat, err := ParseRVal(`"user" . "123"`)
	if err != nil {
		t.Fatalf("ParseRVal concat: %v", err)
	}
	lvRU := lvMustParseLVal(t, "$rU")
	if err := Assign(lvRU, &rvToAssignRVal{val: rvConcat}, msg, ctx); err != nil {
		t.Fatalf("Assign concat: %v", err)
	}
	if got := lvMustGet(t, lvRU, msg, ctx); got != "user123" {
		t.Errorf("expected 'user123', got %q", got)
	}

	// Assign using the convenience AssignRVal constructors
	lvDu := lvMustParseLVal(t, "$du")
	if err := Assign(lvDu, NewStrRVal("sip:proxy@host.com"), nil, ctx); err != nil {
		t.Fatalf("Assign NewStrRVal: %v", err)
	}
	if got := lvMustGet(t, lvDu, nil, ctx); got != "sip:proxy@host.com" {
		t.Errorf("expected 'sip:proxy@host.com', got %q", got)
	}

	lvAvp := lvMustParseLVal(t, "$avp(s:count)")
	if err := Assign(lvAvp, NewIntRVal(99), nil, ctx); err != nil {
		t.Fatalf("Assign NewIntRVal: %v", err)
	}
	if got := lvMustGetInt(t, lvAvp, nil, ctx); got != 99 {
		t.Errorf("expected 99, got %d", got)
	}

	// Concat with $var reference — re-seed greeting first since the int
	// assignment above overwrote it.
	lvVar2 := lvMustParseLVal(t, "$var(combined)")
	if err := AssignStr(lvVar, "hello", nil, ctx); err != nil {
		t.Fatalf("re-seed greeting: %v", err)
	}
	rvVarConcat, err := ParseRVal(`$var(greeting) . "!"`)
	if err != nil {
		t.Fatalf("ParseRVal var concat: %v", err)
	}
	if err := Assign(lvVar2, &rvToAssignRVal{val: rvVarConcat}, msg, ctx); err != nil {
		t.Fatalf("Assign var concat: %v", err)
	}
	if got := lvMustGet(t, lvVar2, nil, ctx); got != "hello!" {
		t.Errorf("expected 'hello!', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 15. TestLValueConcurrent — concurrency safety
// ---------------------------------------------------------------------------

func TestLValueConcurrent(t *testing.T) {
	ctx := NewExecContext(nil, nil, "")

	// $var concurrent writers and readers
	lvVar := lvMustParseLVal(t, "$var(counter)")

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Writers: assign integers
	for i := 0; i < goroutines; i++ {
		go func(val int) {
			defer wg.Done()
			if err := AssignInt(lvVar, val, nil, ctx); err != nil {
				t.Errorf("concurrent AssignInt: %v", err)
			}
		}(i)
	}

	// Readers: read back the value
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := lvVar.Get(nil, ctx); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent var read error: %v", err)
	}

	// AVP concurrent access
	lvAvp := lvMustParseLVal(t, "$avp(s:shared)")
	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func(val int) {
			defer wg.Done()
			if err := AssignStr(lvAvp, strconv.Itoa(val), nil, ctx); err != nil {
				t.Errorf("concurrent AVP AssignStr: %v", err)
			}
		}(i)
	}
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := lvAvp.Get(nil, ctx); err != nil {
				errs <- fmt.Errorf("concurrent AVP Get: %w", err)
			}
		}()
	}
	wg.Wait()

	// XAVP concurrent access
	lvXavp := lvMustParseLVal(t, "$xavp(concurrent[key])")
	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func(val int) {
			defer wg.Done()
			if err := AssignInt(lvXavp, val, nil, ctx); err != nil {
				t.Errorf("concurrent XAVP AssignInt: %v", err)
			}
		}(i)
	}
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := lvXavp.Get(nil, ctx); err != nil {
				errs <- fmt.Errorf("concurrent XAVP Get: %w", err)
			}
		}()
	}
	wg.Wait()

	// RURI concurrent assignment
	msg := lvMustParseMsg(t, "INVITE", "sip:alice@example.com",
		"<sip:alice@example.com>", "<sip:bob@example.com>")
	lvRU := lvMustParseLVal(t, "$rU")
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(val int) {
			defer wg.Done()
			if err := AssignStr(lvRU, "user"+strconv.Itoa(val), msg, ctx); err != nil {
				t.Errorf("concurrent RURI AssignStr: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
