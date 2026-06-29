// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Right-value expression engine
 *
 * Implements the rvalue expression evaluation engine matching C rvalue.c.
 * Supports arithmetic, string concatenation, comparison, logical,
 * regex matching, and function calls (strlen, defined, streq, isflagset).
 *
 * The design mirrors the C rvalue.h / rvalue.c type system:
 *   - RvType  corresponds to enum rval_type (RV_NONE, RV_LONG, RV_STR, ...)
 *   - RvOp    corresponds to enum rval_expr_op (RVE_PLUS_OP, RVE_MINUS_OP, ...)
 *   - RvConst corresponds to struct rvalue with a concrete value
 *   - RvExpr  corresponds to struct rval_expr (compound expression)
 */

package script

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// ---------------------------------------------------------------------------
// Type definitions
// ---------------------------------------------------------------------------

// RvType identifies the kind of a right-value, matching C enum rval_type.
type RvType int

const (
	RvNone        RvType = iota // RV_NONE  — uninitialised / undefined
	RvInt                       // RV_LONG  — integer
	RvStr                       // RV_STR   — string
	RvPVar                      // RV_PVAR  — pseudo-variable reference
	RvAvp                       // RV_AVP   — AVP reference
	RvSelect                    // RV_SEL   — select reference (compat)
	RvRveExpr                   // RV_BEXPR — nested expression
	RvActionResult              // RV_ACTION_ST — module function return value
)

// RvOp enumerates expression operators, matching C enum rval_expr_op.
type RvOp int

const (
	RvOpNone RvOp = iota
	RvOpPlus      // +  generic (int add or string concat) — RVE_PLUS_OP
	RvOpMinus     // -  — RVE_MINUS_OP
	RvOpMul       // *  — RVE_MUL_OP
	RvOpDiv       // /  — RVE_DIV_OP
	RvOpMod       // %  — RVE_MOD_OP
	RvOpBAnd      // &  — RVE_BAND_OP
	RvOpBOr       // |  — RVE_BOR_OP
	RvOpBNot      // ~  unary — RVE_BNOT_OP
	RvOpBxor      // ^  — RVE_BXOR_OP
	RvOpShl       // << — RVE_BLSHIFT_OP
	RvOpShr       // >> — RVE_BRSHIFT_OP
	RvOpConcat    // .  string concat — RVE_CONCAT_OP
	RvOpStrEq     // str eq — RVE_STREQ_OP
	RvOpStrNe     // str ne — RVE_STRDIFF_OP
	RvOpIntEq     // int eq (==, runtime type check) — RVE_EQ_OP
	RvOpIntNe     // int ne (!=, runtime type check) — RVE_DIFF_OP
	RvOpIntLt     // <  — RVE_LT_OP
	RvOpIntGt     // >  — RVE_GT_OP
	RvOpIntLe     // <= — RVE_LTE_OP
	RvOpGe        // >= — RVE_GTE_OP
	RvOpMatch     // =~ regex match — RVE_MATCH_OP
	RvOpNot       // !  unary logical not — RVE_LNOT_OP
	RvOpLAnd      // && — RVE_LAND_OP
	RvOpLOr       // || — RVE_LOR_OP
	RvOpLen       // len()     — RVE_STRLEN_OP
	RvOpDefined   // defined() — RVE_DEFINED_OP
	RvOpStreq     // streq()   — RVE_STREQ_OP (function form)
	RvOpStrlen    // strlen()  — RVE_STRLEN_OP (function form)
	// Additional operators not in the original spec list but required
	// for full expression support.
	RvOpUMinus    // unary minus  — RVE_UMINUS_OP
	RvOpBool      // cast to bool — RVE_BOOL_OP
	RvOpStrEmpty  // strempty()   — RVE_STREMPTY_OP
	RvOpIsFlagSet // isflagset()  — checks ctx.Flags
	RvOpIntCast   // int() cast   — RVE_LONG_OP
	RvOpStrCast   // str() cast   — RVE_STR_OP
)

// RVal is the interface implemented by every right-value type.
// For concrete values (RvConst) the methods return the actual value.
// For unevaluated references (RvPVarRef) and compound expressions
// (RvExpr) the methods return zero values — use EvalRVal to obtain
// a concrete result first.
type RVal interface {
	Type() RvType
	String() string
	Int() (int, error)
	Bool() bool
}

// RvConst is a constant right-value — either an integer or a string.
// Corresponds to C struct rvalue with type RV_LONG or RV_STR.
type RvConst struct {
	IntVal int
	StrVal string
	IsInt  bool
}

// RvPVarRef is a pseudo-variable reference such as $rU, $fd or $var(name).
// The Name field stores the PV name without the leading '$'.
type RvPVarRef struct {
	Name string // e.g. "rU", "fd", "var(x)"
}

// RvExpr is a compound expression: Left Op Right (binary) or Op Left (unary).
// Corresponds to C struct rval_expr.
type RvExpr struct {
	Op    RvOp
	Left  RVal
	Right RVal // nil for unary operators
}

// ---------------------------------------------------------------------------
// RvConst — RVal implementation
// ---------------------------------------------------------------------------

// Type returns RvInt or RvStr.
func (c *RvConst) Type() RvType {
	if c == nil {
		return RvNone
	}
	if c.IsInt {
		return RvInt
	}
	return RvStr
}

// String returns the string representation. For integers this is the
// decimal textual form (matching C sint2str).
func (c *RvConst) String() string {
	if c == nil {
		return ""
	}
	if c.IsInt {
		return strconv.Itoa(c.IntVal)
	}
	return c.StrVal
}

// Int returns the integer value. If the constant holds a string it is
// parsed as a decimal (or hex with 0x prefix) integer. A non-numeric
// string yields 0 with no error, mirroring C RVAL_GET_INT_ERR_IGN.
func (c *RvConst) Int() (int, error) {
	if c == nil {
		return 0, nil
	}
	if c.IsInt {
		return c.IntVal, nil
	}
	if c.StrVal == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(c.StrVal); err == nil {
		return n, nil
	}
	// Try hex (0x…) and other bases via ParseInt base 0.
	if n, err := strconv.ParseInt(c.StrVal, 0, 64); err == nil {
		return int(n), nil
	}
	return 0, nil
}

// Bool returns true for non-zero integers and non-empty strings.
func (c *RvConst) Bool() bool {
	if c == nil {
		return false
	}
	if c.IsInt {
		return c.IntVal != 0
	}
	return c.StrVal != ""
}

// ---------------------------------------------------------------------------
// RvPVarRef — RVal implementation
// ---------------------------------------------------------------------------

func (r *RvPVarRef) Type() RvType  { return RvPVar }
func (r *RvPVarRef) String() string { return "" }
func (r *RvPVarRef) Int() (int, error) {
	return 0, fmt.Errorf("cannot evaluate pseudo-variable %q without context", r.Name)
}
func (r *RvPVarRef) Bool() bool { return false }

// ---------------------------------------------------------------------------
// RvExpr — RVal implementation
// ---------------------------------------------------------------------------

func (e *RvExpr) Type() RvType  { return RvRveExpr }
func (e *RvExpr) String() string { return "" }
func (e *RvExpr) Int() (int, error) {
	return 0, fmt.Errorf("cannot evaluate expression without context")
}
func (e *RvExpr) Bool() bool { return false }

// OpName returns a human-readable name for the operator (debugging aid).
func (op RvOp) OpName() string {
	switch op {
	case RvOpPlus:
		return "+"
	case RvOpMinus:
		return "-"
	case RvOpMul:
		return "*"
	case RvOpDiv:
		return "/"
	case RvOpMod:
		return "%"
	case RvOpBAnd:
		return "&"
	case RvOpBOr:
		return "|"
	case RvOpBNot:
		return "~"
	case RvOpBxor:
		return "^"
	case RvOpShl:
		return "<<"
	case RvOpShr:
		return ">>"
	case RvOpConcat:
		return "."
	case RvOpStrEq:
		return "eq"
	case RvOpStrNe:
		return "ne"
	case RvOpIntEq:
		return "=="
	case RvOpIntNe:
		return "!="
	case RvOpIntLt:
		return "<"
	case RvOpIntGt:
		return ">"
	case RvOpIntLe:
		return "<="
	case RvOpGe:
		return ">="
	case RvOpMatch:
		return "=~"
	case RvOpNot:
		return "!"
	case RvOpLAnd:
		return "&&"
	case RvOpLOr:
		return "||"
	case RvOpLen, RvOpStrlen:
		return "strlen"
	case RvOpDefined:
		return "defined"
	case RvOpStreq:
		return "streq"
	case RvOpUMinus:
		return "unary-"
	case RvOpBool:
		return "bool"
	case RvOpStrEmpty:
		return "strempty"
	case RvOpIsFlagSet:
		return "isflagset"
	case RvOpIntCast:
		return "int"
	case RvOpStrCast:
		return "str"
	}
	return fmt.Sprintf("op(%d)", op)
}

// ---------------------------------------------------------------------------
// Evaluation engine
// ---------------------------------------------------------------------------

// EvalRVal evaluates a right-value expression to a concrete RVal (always
// an *RvConst). For constants the value is returned directly; for
// pseudo-variables the value is resolved from msg/ctx; for compound
// expressions the operator is applied recursively.
func EvalRVal(rv RVal, msg *parser.SIPMsg, ctx *ExecContext) (RVal, error) {
	if rv == nil {
		return &RvConst{}, nil
	}
	switch v := rv.(type) {
	case *RvConst:
		return v, nil
	case *RvPVarRef:
		return evalPVarRef(v, msg, ctx)
	case *RvExpr:
		return evalRvExpr(v, msg, ctx)
	default:
		return nil, fmt.Errorf("unknown rval type %T", rv)
	}
}

// EvalRValStr evaluates rv and returns its string representation.
func EvalRValStr(rv RVal, msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	val, err := EvalRVal(rv, msg, ctx)
	if err != nil {
		return "", err
	}
	return val.String(), nil
}

// EvalRValInt evaluates rv and returns its integer value.
func EvalRValInt(rv RVal, msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	val, err := EvalRVal(rv, msg, ctx)
	if err != nil {
		return 0, err
	}
	return val.Int()
}

// EvalRValBool evaluates rv and returns its boolean value.
func EvalRValBool(rv RVal, msg *parser.SIPMsg, ctx *ExecContext) (bool, error) {
	val, err := EvalRVal(rv, msg, ctx)
	if err != nil {
		return false, err
	}
	return val.Bool(), nil
}

// evalPVarRef resolves a pseudo-variable reference to a concrete value.
// Script variables ($var(name)) are read from ctx.Vars; standard PVs
// ($rU, $fd, …) are resolved via the existing resolvePV helper.
func evalPVarRef(ref *RvPVarRef, msg *parser.SIPMsg, ctx *ExecContext) (RVal, error) {
	name := ref.Name

	// $var(name) — script variable stored in ExecContext.Vars
	if strings.HasPrefix(name, "var(") && strings.HasSuffix(name, ")") {
		varName := name[4 : len(name)-1]
		if ctx != nil {
			ctx.mu.RLock()
			val, ok := ctx.Vars[varName]
			ctx.mu.RUnlock()
			if ok {
				return &RvConst{StrVal: val}, nil
			}
		}
		// undefined → empty string (matches C undef behaviour)
		return &RvConst{}, nil
	}

	// $avp(name) — not fully implemented; treat as undefined
	if strings.HasPrefix(name, "avp(") && strings.HasSuffix(name, ")") {
		return &RvConst{}, nil
	}

	// Standard PVs: $rU, $fd, $rm, …
	pv := ParsePV("$" + name)
	if pv != PVNone {
		val, ok := resolvePV(pv, msg, ctx)
		if ok {
			return &RvConst{StrVal: val}, nil
		}
		return &RvConst{}, nil
	}

	// Unknown PV name → empty
	return &RvConst{}, nil
}

// isPVarDefined reports whether the referenced PV currently has a value.
func isPVarDefined(ref *RvPVarRef, msg *parser.SIPMsg, ctx *ExecContext) bool {
	name := ref.Name

	if strings.HasPrefix(name, "var(") && strings.HasSuffix(name, ")") {
		varName := name[4 : len(name)-1]
		if ctx != nil {
			ctx.mu.RLock()
			_, ok := ctx.Vars[varName]
			ctx.mu.RUnlock()
			return ok
		}
		return false
	}

	if strings.HasPrefix(name, "avp(") && strings.HasSuffix(name, ")") {
		return false
	}

	pv := ParsePV("$" + name)
	if pv != PVNone {
		_, ok := resolvePV(pv, msg, ctx)
		return ok
	}
	return false
}

// evalRvExpr evaluates a compound expression by dispatching on the operator.
func evalRvExpr(e *RvExpr, msg *parser.SIPMsg, ctx *ExecContext) (RVal, error) {
	if e == nil {
		return &RvConst{}, nil
	}

	switch e.Op {
	// ---- Binary arithmetic (always integer) ----
	case RvOpMinus, RvOpMul, RvOpDiv, RvOpMod,
		RvOpBAnd, RvOpBOr, RvOpBxor, RvOpShl, RvOpShr:
		return evalIntBinary(e, msg, ctx)

	// ---- Generic plus (int add or string concat) ----
	case RvOpPlus:
		return evalPlus(e, msg, ctx)

	// ---- String concatenation (always string) ----
	case RvOpConcat:
		ls, err := EvalRValStr(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		rs, err := EvalRValStr(e.Right, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{StrVal: ls + rs}, nil

	// ---- Comparison (runtime type check) ----
	case RvOpIntEq, RvOpIntNe:
		return evalCmp(e, msg, ctx)

	// ---- Typed string comparison ----
	case RvOpStrEq, RvOpStrNe, RvOpStreq:
		ls, err := EvalRValStr(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		rs, err := EvalRValStr(e.Right, msg, ctx)
		if err != nil {
			return nil, err
		}
		var result bool
		switch e.Op {
		case RvOpStrEq, RvOpStreq:
			result = ls == rs
		case RvOpStrNe:
			result = ls != rs
		}
		return &RvConst{IntVal: b2i(result), IsInt: true}, nil

	// ---- Integer comparison ----
	case RvOpIntLt, RvOpIntGt, RvOpIntLe, RvOpGe:
		li, err := EvalRValInt(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		ri, err := EvalRValInt(e.Right, msg, ctx)
		if err != nil {
			return nil, err
		}
		var result bool
		switch e.Op {
		case RvOpIntLt:
			result = li < ri
		case RvOpIntGt:
			result = li > ri
		case RvOpIntLe:
			result = li <= ri
		case RvOpGe:
			result = li >= ri
		}
		return &RvConst{IntVal: b2i(result), IsInt: true}, nil

	// ---- Regex match ----
	case RvOpMatch:
		ls, err := EvalRValStr(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		rs, err := EvalRValStr(e.Right, msg, ctx)
		if err != nil {
			return nil, err
		}
		matched, err := regexMatch(rs, ls)
		if err != nil {
			return nil, err
		}
		return &RvConst{IntVal: b2i(matched), IsInt: true}, nil

	// ---- Logical AND (short-circuit) ----
	case RvOpLAnd:
		lv, err := EvalRVal(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		if !lv.Bool() {
			return &RvConst{IntVal: 0, IsInt: true}, nil
		}
		rv, err := EvalRVal(e.Right, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{IntVal: b2i(rv.Bool()), IsInt: true}, nil

	// ---- Logical OR (short-circuit) ----
	case RvOpLOr:
		lv, err := EvalRVal(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		if lv.Bool() {
			return &RvConst{IntVal: 1, IsInt: true}, nil
		}
		rv, err := EvalRVal(e.Right, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{IntVal: b2i(rv.Bool()), IsInt: true}, nil

	// ---- Unary operators ----
	case RvOpNot:
		lv, err := EvalRVal(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{IntVal: b2i(!lv.Bool()), IsInt: true}, nil

	case RvOpUMinus:
		li, err := EvalRValInt(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{IntVal: -li, IsInt: true}, nil

	case RvOpBNot:
		li, err := EvalRValInt(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{IntVal: ^li, IsInt: true}, nil

	case RvOpBool:
		lv, err := EvalRVal(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{IntVal: b2i(lv.Bool()), IsInt: true}, nil

	case RvOpIntCast:
		li, err := EvalRValInt(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{IntVal: li, IsInt: true}, nil

	case RvOpStrCast:
		ls, err := EvalRValStr(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{StrVal: ls}, nil

	// ---- String functions ----
	case RvOpLen, RvOpStrlen:
		ls, err := EvalRValStr(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{IntVal: len(ls), IsInt: true}, nil

	case RvOpStrEmpty:
		ls, err := EvalRValStr(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		return &RvConst{IntVal: b2i(ls == ""), IsInt: true}, nil

	case RvOpDefined:
		switch ref := e.Left.(type) {
		case *RvPVarRef:
			return &RvConst{IntVal: b2i(isPVarDefined(ref, msg, ctx)), IsInt: true}, nil
		default:
			// Non-PV operands are always defined.
			return &RvConst{IntVal: 1, IsInt: true}, nil
		}

	case RvOpIsFlagSet:
		flagN, err := EvalRValInt(e.Left, msg, ctx)
		if err != nil {
			return nil, err
		}
		if ctx != nil {
			set := (ctx.Flags & (1 << uint(flagN))) != 0
			return &RvConst{IntVal: b2i(set), IsInt: true}, nil
		}
		return &RvConst{IntVal: 0, IsInt: true}, nil

	case RvOpNone:
		return &RvConst{}, nil
	}

	return nil, fmt.Errorf("unsupported operator %s", e.Op.OpName())
}

// evalIntBinary evaluates a binary integer arithmetic operation.
func evalIntBinary(e *RvExpr, msg *parser.SIPMsg, ctx *ExecContext) (RVal, error) {
	li, err := EvalRValInt(e.Left, msg, ctx)
	if err != nil {
		return nil, err
	}
	ri, err := EvalRValInt(e.Right, msg, ctx)
	if err != nil {
		return nil, err
	}
	var result int
	switch e.Op {
	case RvOpMinus:
		result = li - ri
	case RvOpMul:
		result = li * ri
	case RvOpDiv:
		if ri == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		result = li / ri
	case RvOpMod:
		if ri == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		result = li % ri
	case RvOpBAnd:
		result = li & ri
	case RvOpBOr:
		result = li | ri
	case RvOpBxor:
		result = li ^ ri
	case RvOpShl:
		result = li << uint(ri)
	case RvOpShr:
		result = li >> uint(ri)
	default:
		return nil, fmt.Errorf("not a binary int operator: %s", e.Op.OpName())
	}
	return &RvConst{IntVal: result, IsInt: true}, nil
}

// evalPlus implements the generic '+' operator. When both operands are
// integers (or numeric strings that can be parsed as integers) integer
// addition is performed; otherwise string concatenation. This mirrors
// C RVE_PLUS_OP in rval_expr_eval, with the added heuristic that script
// variables stored as strings are treated as integers when they contain
// numeric values.
func evalPlus(e *RvExpr, msg *parser.SIPMsg, ctx *ExecContext) (RVal, error) {
	lv, err := EvalRVal(e.Left, msg, ctx)
	if err != nil {
		return nil, err
	}
	rv, err := EvalRVal(e.Right, msg, ctx)
	if err != nil {
		return nil, err
	}
	// If both operands are ints, do integer addition.
	if lv.Type() == RvInt && rv.Type() == RvInt {
		li, _ := lv.Int()
		ri, _ := rv.Int()
		return &RvConst{IntVal: li + ri, IsInt: true}, nil
	}
	// Try to parse both as ints (handles numeric string vars).
	li, lOk := rvTryInt(lv)
	ri, rOk := rvTryInt(rv)
	if lOk && rOk {
		return &RvConst{IntVal: li + ri, IsInt: true}, nil
	}
	// Fall back to string concatenation.
	return &RvConst{StrVal: lv.String() + rv.String()}, nil
}

// evalCmp implements the generic == and != operators. When both operands
// are integers (or numeric strings) integer comparison is performed;
// otherwise string comparison. This mirrors C RVE_EQ_OP / RVE_DIFF_OP.
func evalCmp(e *RvExpr, msg *parser.SIPMsg, ctx *ExecContext) (RVal, error) {
	lv, err := EvalRVal(e.Left, msg, ctx)
	if err != nil {
		return nil, err
	}
	rv, err := EvalRVal(e.Right, msg, ctx)
	if err != nil {
		return nil, err
	}
	// If both operands are ints, do integer comparison.
	if lv.Type() == RvInt && rv.Type() == RvInt {
		li, _ := lv.Int()
		ri, _ := rv.Int()
		return cmpResult(e.Op, li == ri, li != ri), nil
	}
	// Try to parse both as ints (handles numeric string vars).
	li, lOk := rvTryInt(lv)
	ri, rOk := rvTryInt(rv)
	if lOk && rOk {
		return cmpResult(e.Op, li == ri, li != ri), nil
	}
	// Fall back to string comparison.
	ls := lv.String()
	rs := rv.String()
	return cmpResult(e.Op, ls == rs, ls != rs), nil
}

// cmpResult builds a boolean RvConst from the eq/ne results.
func cmpResult(op RvOp, eq, ne bool) RVal {
	switch op {
	case RvOpIntEq:
		return &RvConst{IntVal: b2i(eq), IsInt: true}
	case RvOpIntNe:
		return &RvConst{IntVal: b2i(ne), IsInt: true}
	}
	return &RvConst{IntVal: 0, IsInt: true}
}

// rvTryInt attempts to interpret an evaluated RVal as an integer.
// Returns (value, true) on success, (0, false) otherwise.
func rvTryInt(rv RVal) (int, bool) {
	if rv == nil {
		return 0, false
	}
	c, ok := rv.(*RvConst)
	if !ok {
		return 0, false
	}
	if c.IsInt {
		return c.IntVal, true
	}
	return tryStrToInt(c.StrVal)
}

// tryStrToInt attempts to parse s as a decimal integer.
func tryStrToInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// regexMatch compiles pattern (case-insensitive, matching C REG_ICASE) and
// reports whether it matches s. Compiled regexes are cached for reuse.
func regexMatch(pattern, s string) (bool, error) {
	if pattern == "" {
		return false, nil
	}
	re, err := compileRegex(pattern)
	if err != nil {
		return false, fmt.Errorf("invalid regular expression %q: %v", pattern, err)
	}
	return re.MatchString(s), nil
}

// b2i converts a bool to 1 or 0.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Parser — tokenizer
// ---------------------------------------------------------------------------

type rvTokKind int

const (
	rvTokEOF rvTokKind = iota
	rvTokNumber
	rvTokString
	rvTokPV    // $rU, $var(x), …
	rvTokIdent // method, strlen, …
	rvTokOp    // + - * / % & | ^ ~ . < > ! << >> == != <= >= =~ && ||
	rvTokLParen
	rvTokRParen
	rvTokComma
)

type rvTok struct {
	kind rvTokKind
	text string
	pos  int
}

// rvTokenize scans text into a slice of tokens for the rvalue parser.
func rvTokenize(text string) ([]rvTok, error) {
	var tokens []rvTok
	i := 0
	n := len(text)
	for i < n {
		c := text[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '"':
			// string literal
			i++
			start := i
			var sb strings.Builder
			for i < n && text[i] != '"' {
				if text[i] == '\\' && i+1 < n {
					sb.WriteByte(text[i+1])
					i += 2
					continue
				}
				sb.WriteByte(text[i])
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("unterminated string at position %d", start)
			}
			tokens = append(tokens, rvTok{kind: rvTokString, text: sb.String(), pos: start})
			i++ // skip closing "
		case unicode.IsDigit(rune(c)):
			start := i
			for i < n && unicode.IsDigit(rune(text[i])) {
				i++
			}
			tokens = append(tokens, rvTok{kind: rvTokNumber, text: text[start:i], pos: start})
		case c == '$':
			// pseudo-variable: $name or $name(key)
			i++
			start := i
			for i < n && (unicode.IsLetter(rune(text[i])) || unicode.IsDigit(rune(text[i])) || text[i] == '_') {
				i++
			}
			name := text[start:i]
			if i < n && text[i] == '(' {
				// scan until matching ')'
				name += "("
				i++
				depth := 1
				for i < n && depth > 0 {
					if text[i] == '(' {
						depth++
					} else if text[i] == ')' {
						depth--
						if depth == 0 {
							name += ")"
							i++
							break
						}
					}
					name += string(text[i])
					i++
				}
			}
			tokens = append(tokens, rvTok{kind: rvTokPV, text: name, pos: start - 1})
		case unicode.IsLetter(rune(c)) || c == '_':
			start := i
			for i < n && (unicode.IsLetter(rune(text[i])) || unicode.IsDigit(rune(text[i])) || text[i] == '_') {
				i++
			}
			tokens = append(tokens, rvTok{kind: rvTokIdent, text: text[start:i], pos: start})
		case c == '(':
			tokens = append(tokens, rvTok{kind: rvTokLParen, text: "(", pos: i})
			i++
		case c == ')':
			tokens = append(tokens, rvTok{kind: rvTokRParen, text: ")", pos: i})
			i++
		case c == ',':
			tokens = append(tokens, rvTok{kind: rvTokComma, text: ",", pos: i})
			i++
		default:
			// operators — try two-char first, then single-char
			if i+1 < n {
				two := text[i : i+2]
				switch two {
				case "==", "!=", "<=", ">=", "=~", "&&", "||", "<<", ">>":
					tokens = append(tokens, rvTok{kind: rvTokOp, text: two, pos: i})
					i += 2
					continue
				}
			}
			switch c {
			case '+', '-', '*', '/', '%', '&', '|', '^', '~', '.', '<', '>', '!':
				tokens = append(tokens, rvTok{kind: rvTokOp, text: string(c), pos: i})
				i++
			default:
				return nil, fmt.Errorf("unexpected character %q at position %d", string(c), i)
			}
		}
	}
	tokens = append(tokens, rvTok{kind: rvTokEOF, text: "", pos: n})
	return tokens, nil
}

// ---------------------------------------------------------------------------
// Parser — recursive descent
// ---------------------------------------------------------------------------

type rvParser struct {
	tokens []rvTok
	pos    int
}

func (p *rvParser) cur() rvTok {
	if p.pos >= len(p.tokens) {
		return rvTok{kind: rvTokEOF}
	}
	return p.tokens[p.pos]
}

func (p *rvParser) advance() rvTok {
	t := p.cur()
	p.pos++
	return t
}

func (p *rvParser) expect(kind rvTokKind) (rvTok, error) {
	t := p.cur()
	if t.kind != kind {
		return t, fmt.Errorf("expected token kind %d, got kind %d (%q)", kind, t.kind, t.text)
	}
	p.pos++
	return t, nil
}

// ParseRVal parses a textual right-value expression into an RVal tree.
//
// Supported syntax:
//   - Integer and string literals: 42, "hello"
//   - Pseudo-variables: $rU, $fd, $var(name), $avp(name)
//   - Keywords: method (= $rm), uri (= $ru)
//   - Arithmetic: + - * / % << >> & | ^
//   - String concat: + (when left is string), . (explicit)
//   - Comparison: == != < > <= >=
//   - Regex match: =~
//   - Logical: && || !
//   - Unary: - ~ !
//   - Functions: strlen(), len(), defined(), streq(), isflagset(),
//     flag(), int(), str(), strempty()
//   - Parenthesised sub-expressions: (expr)
func ParseRVal(text string) (RVal, error) {
	tokens, err := rvTokenize(text)
	if err != nil {
		return nil, err
	}
	p := &rvParser{tokens: tokens, pos: 0}
	if p.cur().kind == rvTokEOF {
		return nil, fmt.Errorf("empty expression")
	}
	expr, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != rvTokEOF {
		return nil, fmt.Errorf("unexpected token %q after expression", p.cur().text)
	}
	return expr, nil
}

// parseOr: expr ('||' expr)*
func (p *rvParser) parseOr() (RVal, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == rvTokOp && p.cur().text == "||" {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &RvExpr{Op: RvOpLOr, Left: left, Right: right}
	}
	return left, nil
}

// parseAnd: expr ('&&' expr)*
func (p *rvParser) parseAnd() (RVal, error) {
	left, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == rvTokOp && p.cur().text == "&&" {
		p.advance()
		right, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		left = &RvExpr{Op: RvOpLAnd, Left: left, Right: right}
	}
	return left, nil
}

// parseCmp: expr (('==' | '!=' | '=~' | '<' | '>' | '<=' | '>=') expr)?
func (p *rvParser) parseCmp() (RVal, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != rvTokOp {
		return left, nil
	}
	opText := p.cur().text
	var op RvOp
	switch opText {
	case "==":
		op = RvOpIntEq
	case "!=":
		op = RvOpIntNe
	case "=~":
		op = RvOpMatch
	case "<":
		op = RvOpIntLt
	case ">":
		op = RvOpIntGt
	case "<=":
		op = RvOpIntLe
	case ">=":
		op = RvOpGe
	default:
		return left, nil
	}
	p.advance()
	right, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	return &RvExpr{Op: op, Left: left, Right: right}, nil
}

// parseAdd: expr (('+' | '-' | '.') expr)*
func (p *rvParser) parseAdd() (RVal, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == rvTokOp {
		var op RvOp
		switch p.cur().text {
		case "+":
			op = RvOpPlus
		case "-":
			op = RvOpMinus
		case ".":
			op = RvOpConcat
		default:
			break
		}
		if op == RvOpNone {
			break
		}
		p.advance()
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &RvExpr{Op: op, Left: left, Right: right}
	}
	return left, nil
}

// parseMul: expr (('*' | '/' | '%' | '&' | '|' | '^' | '<<' | '>>') expr)*
func (p *rvParser) parseMul() (RVal, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == rvTokOp {
		var op RvOp
		switch p.cur().text {
		case "*":
			op = RvOpMul
		case "/":
			op = RvOpDiv
		case "%":
			op = RvOpMod
		case "&":
			op = RvOpBAnd
		case "|":
			op = RvOpBOr
		case "^":
			op = RvOpBxor
		case "<<":
			op = RvOpShl
		case ">>":
			op = RvOpShr
		}
		if op == RvOpNone {
			break
		}
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &RvExpr{Op: op, Left: left, Right: right}
	}
	return left, nil
}

// parseUnary: ('!' | '~' | '-') parseUnary | parsePrimary
func (p *rvParser) parseUnary() (RVal, error) {
	if p.cur().kind == rvTokOp {
		switch p.cur().text {
		case "!":
			p.advance()
			operand, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &RvExpr{Op: RvOpNot, Left: operand}, nil
		case "~":
			p.advance()
			operand, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &RvExpr{Op: RvOpBNot, Left: operand}, nil
		case "-":
			p.advance()
			operand, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &RvExpr{Op: RvOpUMinus, Left: operand}, nil
		}
	}
	return p.parsePrimary()
}

// parsePrimary handles literals, PVs, keywords, function calls and
// parenthesised sub-expressions.
func (p *rvParser) parsePrimary() (RVal, error) {
	t := p.cur()
	switch t.kind {
	case rvTokNumber:
		p.advance()
		n, err := strconv.Atoi(t.text)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q: %v", t.text, err)
		}
		return &RvConst{IntVal: n, IsInt: true}, nil

	case rvTokString:
		p.advance()
		return &RvConst{StrVal: t.text}, nil

	case rvTokPV:
		p.advance()
		return &RvPVarRef{Name: t.text}, nil

	case rvTokLParen:
		p.advance()
		expr, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(rvTokRParen); err != nil {
			return nil, fmt.Errorf("expected ')' after sub-expression: %v", err)
		}
		return expr, nil

	case rvTokIdent:
		return p.parseIdentOrFunc()

	case rvTokEOF:
		return nil, fmt.Errorf("unexpected end of expression")
	}
	return nil, fmt.Errorf("unexpected token %q", t.text)
}

// parseIdentOrFunc handles identifiers: either keywords (method, uri) or
// function calls (strlen(…), defined(…), streq(…), isflagset(…), …).
func (p *rvParser) parseIdentOrFunc() (RVal, error) {
	t := p.advance()
	name := strings.ToLower(t.text)

	// Keywords that map to PVs
	switch name {
	case "method":
		return &RvPVarRef{Name: "rm"}, nil
	case "uri":
		return &RvPVarRef{Name: "ru"}, nil
	}

	// Function call?
	if p.cur().kind == rvTokLParen {
		return p.parseFuncCall(name)
	}

	// Bare identifier — treat as a string constant
	return &RvConst{StrVal: t.text}, nil
}

// parseFuncCall parses a function call: name '(' [expr (',' expr)*] ')'
func (p *rvParser) parseFuncCall(name string) (RVal, error) {
	if _, err := p.expect(rvTokLParen); err != nil {
		return nil, err
	}

	var args []RVal
	if p.cur().kind != rvTokRParen {
		for {
			arg, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if p.cur().kind == rvTokComma {
				p.advance()
				continue
			}
			break
		}
	}
	if _, err := p.expect(rvTokRParen); err != nil {
		return nil, fmt.Errorf("expected ')' after function arguments: %v", err)
	}

	switch name {
	case "strlen", "len":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() expects 1 argument, got %d", name, len(args))
		}
		return &RvExpr{Op: RvOpStrlen, Left: args[0]}, nil

	case "defined":
		if len(args) != 1 {
			return nil, fmt.Errorf("defined() expects 1 argument, got %d", len(args))
		}
		return &RvExpr{Op: RvOpDefined, Left: args[0]}, nil

	case "streq":
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("streq() expects 2 or 3 arguments, got %d", len(args))
		}
		return &RvExpr{Op: RvOpStreq, Left: args[0], Right: args[1]}, nil

	case "strempty":
		if len(args) != 1 {
			return nil, fmt.Errorf("strempty() expects 1 argument, got %d", len(args))
		}
		return &RvExpr{Op: RvOpStrEmpty, Left: args[0]}, nil

	case "isflagset", "flag":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() expects 1 argument, got %d", name, len(args))
		}
		return &RvExpr{Op: RvOpIsFlagSet, Left: args[0]}, nil

	case "int":
		if len(args) != 1 {
			return nil, fmt.Errorf("int() expects 1 argument, got %d", len(args))
		}
		return &RvExpr{Op: RvOpIntCast, Left: args[0]}, nil

	case "str":
		if len(args) != 1 {
			return nil, fmt.Errorf("str() expects 1 argument, got %d", len(args))
		}
		return &RvExpr{Op: RvOpStrCast, Left: args[0]}, nil

	case "bool":
		if len(args) != 1 {
			return nil, fmt.Errorf("bool() expects 1 argument, got %d", len(args))
		}
		return &RvExpr{Op: RvOpBool, Left: args[0]}, nil
	}

	return nil, fmt.Errorf("unknown function %q", name)
}

// ---------------------------------------------------------------------------
// Compiled-regex cache (thread-safe, avoids recompiling on every eval)
// ---------------------------------------------------------------------------

var regexCache sync.Map // map[string]*regexp.Regexp

func compileRegex(pattern string) (*regexp.Regexp, error) {
	if v, ok := regexCache.Load(pattern); ok {
		if re, ok := v.(*regexp.Regexp); ok {
			return re, nil
		}
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, err
	}
	regexCache.Store(pattern, re)
	return re, nil
}
