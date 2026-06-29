// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go — Lvalue assignment engine.
 *
 * Implements the left-value assignment system matching the C lvalue.c /
 * lvalue.h design.  Supports assignment to script variables ($var),
 * pseudo-variable targets ($rU, $fd, …), AVPs ($avp), XAVPs ($xavp),
 * Request-URI, Destination-URI, From and To header parts.
 *
 * The public surface consists of:
 *   - LvType / LVal interface  — type-tagged assignment targets
 *   - RVal interface           — right-hand value providers
 *   - Assign / AssignStr / AssignInt — top-level assignment entry points
 *   - ParseLVal                — text → LVal parser
 *   - XAVPStore                — extended AVP storage used by ExecContext
 */

package script

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kamailio/kamailio-go/internal/core/avp"
	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// ---------------------------------------------------------------------------
// LvType — left-value category tag
// ---------------------------------------------------------------------------

// LvType identifies the kind of left-value being assigned to.
type LvType int

const (
	LvNone   LvType = iota
	LvPVar          // pseudo-variable / script variable ($var(x), $rU, …)
	LvAvp           // AVP ($avp(s:tag) or $avp(i:idx))
	LvXAvp          // XAVP ($xavp(root[key]))
	LvRURI          // Request-URI ($ru, $ruri)
	LvDstURI        // Destination URI ($du)
	LvFrom          // From header ($fu, $from)
	LvTo            // To header ($tu, $to)
	LvBranch        // Branch ($branch(uri))
)

// ---------------------------------------------------------------------------
// LVal — left-value interface
// ---------------------------------------------------------------------------

// LVal is the contract every assignment target must satisfy.
type LVal interface {
	Type() LvType
	Assign(value string, msg *parser.SIPMsg, ctx *ExecContext) error
	AssignInt(value int, msg *parser.SIPMsg, ctx *ExecContext) error
	Get(msg *parser.SIPMsg, ctx *ExecContext) (string, error)
	GetInt(msg *parser.SIPMsg, ctx *ExecContext) (int, error)
}

// ---------------------------------------------------------------------------
// AssignRVal — right-value interface for assignments
// ---------------------------------------------------------------------------

// AssignRVal provides the value(s) for the right-hand side of an assignment.
type AssignRVal interface {
	Str(msg *parser.SIPMsg, ctx *ExecContext) (string, error)
	Int(msg *parser.SIPMsg, ctx *ExecContext) (int, error)
	IsInt() bool
}

// --- AssignRVal implementations ---

// strRVal wraps a plain string.
type strRVal struct{ v string }

func (r *strRVal) Str(_ *parser.SIPMsg, _ *ExecContext) (string, error) { return r.v, nil }
func (r *strRVal) Int(_ *parser.SIPMsg, _ *ExecContext) (int, error) {
	return strconv.Atoi(r.v)
}
func (r *strRVal) IsInt() bool { return false }

// intRVal wraps a plain integer.
type intRVal struct{ v int }

func (r *intRVal) Str(_ *parser.SIPMsg, _ *ExecContext) (string, error) {
	return strconv.Itoa(r.v), nil
}
func (r *intRVal) Int(_ *parser.SIPMsg, _ *ExecContext) (int, error) { return r.v, nil }
func (r *intRVal) IsInt() bool { return true }

// varRVal reads a $var(name) from the execution context.
type varRVal struct{ name string }

func (r *varRVal) Str(_ *parser.SIPMsg, ctx *ExecContext) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("nil context")
	}
	ctx.mu.RLock()
	v := ctx.Vars[r.name]
	ctx.mu.RUnlock()
	return v, nil
}
func (r *varRVal) Int(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	s, err := r.Str(msg, ctx)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}
func (r *varRVal) IsInt() bool { return false }

// pvRVal reads a pseudo-variable ($rU, $fd, …) from the message/context.
type pvRVal struct{ name string }

func (r *pvRVal) Str(msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	pv := ParsePV("$" + r.name)
	if pv == PVNone {
		return "", fmt.Errorf("unknown PV: $%s", r.name)
	}
	v, ok := resolvePV(pv, msg, ctx)
	if !ok {
		return "", fmt.Errorf("PV $%s not resolved", r.name)
	}
	return v, nil
}
func (r *pvRVal) Int(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	s, err := r.Str(msg, ctx)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}
func (r *pvRVal) IsInt() bool { return false }

// concatRVal concatenates multiple AssignRVal parts into a single string.
type concatRVal struct{ parts []AssignRVal }

func (r *concatRVal) Str(msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	var sb strings.Builder
	for _, p := range r.parts {
		s, err := p.Str(msg, ctx)
		if err != nil {
			return "", err
		}
		sb.WriteString(s)
	}
	return sb.String(), nil
}
func (r *concatRVal) Int(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	s, err := r.Str(msg, ctx)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}
func (r *concatRVal) IsInt() bool { return false }

// --- AssignRVal constructors ---

// NewStrRVal creates a string right-value.
func NewStrRVal(s string) AssignRVal { return &strRVal{v: s} }

// NewIntRVal creates an integer right-value.
func NewIntRVal(n int) AssignRVal { return &intRVal{v: n} }

// NewVarRVal creates a right-value that reads $var(name) at evaluation time.
func NewVarRVal(name string) AssignRVal { return &varRVal{name: name} }

// NewPVRVal creates a right-value that reads the named pseudo-variable.
func NewPVRVal(name string) AssignRVal { return &pvRVal{name: name} }

// NewConcatRVal creates a right-value that concatenates the given parts.
func NewConcatRVal(parts ...AssignRVal) AssignRVal { return &concatRVal{parts: parts} }

// ---------------------------------------------------------------------------
// LvPVarRef — script variable / generic pseudo-variable
// ---------------------------------------------------------------------------

// LvPVarRef handles $var(name) assignment and, as a convenience, can also
// resolve short-form PV names (rU, fd, …) by delegating to dedicated types.
type LvPVarRef struct {
	Name string // e.g. "var(x)", "rU", "fd"
}

func (r *LvPVarRef) Type() LvType { return LvPVar }

func (r *LvPVarRef) Assign(value string, msg *parser.SIPMsg, ctx *ExecContext) error {
	name := r.Name
	if strings.HasPrefix(name, "var(") && strings.HasSuffix(name, ")") {
		varName := name[4 : len(name)-1]
		if ctx == nil {
			return fmt.Errorf("nil context")
		}
		ctx.mu.Lock()
		ctx.Vars[varName] = value
		ctx.mu.Unlock()
		return nil
	}
	// Delegate to a dedicated LVal for known PV short names.
	lv, err := pvNameToLVal(name)
	if err != nil {
		return err
	}
	return lv.Assign(value, msg, ctx)
}

func (r *LvPVarRef) AssignInt(value int, msg *parser.SIPMsg, ctx *ExecContext) error {
	name := r.Name
	if strings.HasPrefix(name, "var(") && strings.HasSuffix(name, ")") {
		varName := name[4 : len(name)-1]
		if ctx == nil {
			return fmt.Errorf("nil context")
		}
		ctx.mu.Lock()
		ctx.Vars[varName] = strconv.Itoa(value)
		ctx.mu.Unlock()
		return nil
	}
	lv, err := pvNameToLVal(name)
	if err != nil {
		return err
	}
	return lv.AssignInt(value, msg, ctx)
}

func (r *LvPVarRef) Get(msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	name := r.Name
	if strings.HasPrefix(name, "var(") && strings.HasSuffix(name, ")") {
		varName := name[4 : len(name)-1]
		if ctx == nil {
			return "", fmt.Errorf("nil context")
		}
		ctx.mu.RLock()
		v := ctx.Vars[varName]
		ctx.mu.RUnlock()
		return v, nil
	}
	lv, err := pvNameToLVal(name)
	if err != nil {
		return "", err
	}
	return lv.Get(msg, ctx)
}

func (r *LvPVarRef) GetInt(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	s, err := r.Get(msg, ctx)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// ---------------------------------------------------------------------------
// LvAvpRef — AVP left-value
// ---------------------------------------------------------------------------

// LvAvpRef handles $avp(s:name) and $avp(i:idx) assignment.
type LvAvpRef struct {
	Scope string // "s" (string name) or "i" (integer name)
	Name  string // AVP name or index-as-string
}

func (r *LvAvpRef) Type() LvType { return LvAvp }

func (r *LvAvpRef) Assign(value string, msg *parser.SIPMsg, ctx *ExecContext) error {
	if ctx == nil || ctx.AVPs == nil {
		return fmt.Errorf("nil context or AVP store")
	}
	ctx.AVPs.Del(r.Name)
	ctx.AVPs.AddString(r.Name, value)
	return nil
}

func (r *LvAvpRef) AssignInt(value int, msg *parser.SIPMsg, ctx *ExecContext) error {
	if ctx == nil || ctx.AVPs == nil {
		return fmt.Errorf("nil context or AVP store")
	}
	ctx.AVPs.Del(r.Name)
	ctx.AVPs.AddInt(r.Name, int64(value))
	return nil
}

func (r *LvAvpRef) Get(msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	if ctx == nil || ctx.AVPs == nil {
		return "", fmt.Errorf("nil context or AVP store")
	}
	v, ok := ctx.AVPs.First(r.Name)
	if !ok {
		return "", nil
	}
	switch v.Kind {
	case avp.KindString:
		return v.S, nil
	case avp.KindInt:
		return strconv.FormatInt(v.I, 10), nil
	}
	return "", nil
}

func (r *LvAvpRef) GetInt(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	if ctx == nil || ctx.AVPs == nil {
		return 0, fmt.Errorf("nil context or AVP store")
	}
	v, ok := ctx.AVPs.First(r.Name)
	if !ok {
		return 0, nil
	}
	switch v.Kind {
	case avp.KindInt:
		return int(v.I), nil
	case avp.KindString:
		return strconv.Atoi(v.S)
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// LvXAvpRef — XAVP left-value
// ---------------------------------------------------------------------------

// LvXAvpRef handles $xavp(root[key]) assignment.
type LvXAvpRef struct {
	Root string
	Key  string
	Idx  int
}

func (r *LvXAvpRef) Type() LvType { return LvXAvp }

func (r *LvXAvpRef) Assign(value string, msg *parser.SIPMsg, ctx *ExecContext) error {
	if ctx == nil || ctx.XAVPs == nil {
		return fmt.Errorf("nil context or XAVP store")
	}
	ctx.XAVPs.SetStr(r.Root, r.Key, value)
	return nil
}

func (r *LvXAvpRef) AssignInt(value int, msg *parser.SIPMsg, ctx *ExecContext) error {
	if ctx == nil || ctx.XAVPs == nil {
		return fmt.Errorf("nil context or XAVP store")
	}
	ctx.XAVPs.SetInt(r.Root, r.Key, value)
	return nil
}

func (r *LvXAvpRef) Get(msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	if ctx == nil || ctx.XAVPs == nil {
		return "", fmt.Errorf("nil context or XAVP store")
	}
	v, ok := ctx.XAVPs.GetStr(r.Root, r.Key)
	if !ok {
		// Fall back to int value.
		iv, iok := ctx.XAVPs.GetInt(r.Root, r.Key)
		if iok {
			return strconv.Itoa(iv), nil
		}
		return "", nil
	}
	return v, nil
}

func (r *LvXAvpRef) GetInt(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	if ctx == nil || ctx.XAVPs == nil {
		return 0, fmt.Errorf("nil context or XAVP store")
	}
	v, ok := ctx.XAVPs.GetInt(r.Root, r.Key)
	if !ok {
		// Fall back to string value and parse.
		sv, sok := ctx.XAVPs.GetStr(r.Root, r.Key)
		if sok {
			return strconv.Atoi(sv)
		}
		return 0, nil
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// LvRURIRef — Request-URI left-value
// ---------------------------------------------------------------------------

// LvRURIRef handles $ru (full), $rU (user), $rd (domain) and the long
// forms $ruri / $ruri(user) / $ruri(domain).
type LvRURIRef struct {
	Part string // "full", "user", "domain", "port"
}

func (r *LvRURIRef) Type() LvType { return LvRURI }

func (r *LvRURIRef) Assign(value string, msg *parser.SIPMsg, ctx *ExecContext) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	uri := ruriStr(ctx, msg)
	switch r.Part {
	case "full", "":
		ctx.mu.Lock()
		ctx.RURI = value
		ctx.mu.Unlock()
	case "user":
		ctx.mu.Lock()
		ctx.RURI = replaceUser(uri, value)
		ctx.mu.Unlock()
	case "domain":
		ctx.mu.Lock()
		ctx.RURI = replaceHost(uri, value)
		ctx.mu.Unlock()
	case "port":
		ctx.mu.Lock()
		ctx.RURI = replacePort(uri, value)
		ctx.mu.Unlock()
	default:
		return fmt.Errorf("unknown RURI part: %q", r.Part)
	}
	return nil
}

func (r *LvRURIRef) AssignInt(value int, msg *parser.SIPMsg, ctx *ExecContext) error {
	return r.Assign(strconv.Itoa(value), msg, ctx)
}

func (r *LvRURIRef) Get(msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	uri := ruriStr(ctx, msg)
	if uri == "" {
		return "", nil
	}
	switch r.Part {
	case "full", "":
		return uri, nil
	case "user":
		if u, err := parser.ParseURI(uri); err == nil && u != nil {
			return u.User.String(), nil
		}
		return extractUser(uri), nil
	case "domain":
		if u, err := parser.ParseURI(uri); err == nil && u != nil {
			return u.Host.String(), nil
		}
		return extractHost(uri), nil
	case "port":
		if u, err := parser.ParseURI(uri); err == nil && u != nil {
			return u.Port.String(), nil
		}
		return "", nil
	}
	return "", fmt.Errorf("unknown RURI part: %q", r.Part)
}

func (r *LvRURIRef) GetInt(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	s, err := r.Get(msg, ctx)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// ---------------------------------------------------------------------------
// LvDstURIRef — Destination-URI left-value
// ---------------------------------------------------------------------------

// LvDstURIRef handles $du / $dsturi assignment.
type LvDstURIRef struct{}

func (r *LvDstURIRef) Type() LvType { return LvDstURI }

func (r *LvDstURIRef) Assign(value string, msg *parser.SIPMsg, ctx *ExecContext) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	ctx.mu.Lock()
	ctx.DstURI = value
	ctx.mu.Unlock()
	return nil
}

func (r *LvDstURIRef) AssignInt(value int, msg *parser.SIPMsg, ctx *ExecContext) error {
	return r.Assign(strconv.Itoa(value), msg, ctx)
}

func (r *LvDstURIRef) Get(msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("nil context")
	}
	ctx.mu.RLock()
	v := ctx.DstURI
	ctx.mu.RUnlock()
	return v, nil
}

func (r *LvDstURIRef) GetInt(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	s, err := r.Get(msg, ctx)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// ---------------------------------------------------------------------------
// LvFromRef — From header left-value
// ---------------------------------------------------------------------------

// LvFromRef handles $fu (URI), $fd (domain), $fn (display name) and the
// long forms $from(u), $from(d), $from(n).
type LvFromRef struct {
	Part string // "u" (URI), "d" (domain), "n" (display name), "h" (host), "p" (port)
}

func (r *LvFromRef) Type() LvType { return LvFrom }

func (r *LvFromRef) Assign(value string, msg *parser.SIPMsg, ctx *ExecContext) error {
	tb, err := ensureFromBody(msg)
	if err != nil {
		return err
	}
	switch r.Part {
	case "u":
		uri, perr := parser.ParseURI(value)
		if perr != nil {
			return fmt.Errorf("parse From URI: %w", perr)
		}
		tb.URI = uri
		tb.ParsedURI = uri
	case "d", "h":
		if tb.URI == nil {
			return fmt.Errorf("From URI not parsed")
		}
		tb.URI.Host = str.Mk(value)
	case "n":
		tb.DisplayName = str.Mk(value)
	case "p":
		if tb.URI == nil {
			return fmt.Errorf("From URI not parsed")
		}
		tb.URI.Port = str.Mk(value)
	default:
		return fmt.Errorf("unknown From part: %q", r.Part)
	}
	return nil
}

func (r *LvFromRef) AssignInt(value int, msg *parser.SIPMsg, ctx *ExecContext) error {
	return r.Assign(strconv.Itoa(value), msg, ctx)
}

func (r *LvFromRef) Get(msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	tb, err := ensureFromBody(msg)
	if err != nil {
		return "", err
	}
	switch r.Part {
	case "u":
		if tb.URI != nil {
			return tb.URI.String(), nil
		}
		return "", nil
	case "d", "h":
		if tb.URI != nil {
			return tb.URI.Host.String(), nil
		}
		return "", nil
	case "n":
		return tb.DisplayName.String(), nil
	case "p":
		if tb.URI != nil {
			return tb.URI.Port.String(), nil
		}
		return "", nil
	}
	return "", fmt.Errorf("unknown From part: %q", r.Part)
}

func (r *LvFromRef) GetInt(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	s, err := r.Get(msg, ctx)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// ---------------------------------------------------------------------------
// LvToRef — To header left-value
// ---------------------------------------------------------------------------

// LvToRef handles $tu (URI), $td (domain) and the long forms $to(u), $to(d).
type LvToRef struct {
	Part string // "u" (URI), "d" (domain), "n" (display name), "h" (host), "p" (port)
}

func (r *LvToRef) Type() LvType { return LvTo }

func (r *LvToRef) Assign(value string, msg *parser.SIPMsg, ctx *ExecContext) error {
	tb, err := ensureToBody(msg)
	if err != nil {
		return err
	}
	switch r.Part {
	case "u":
		uri, perr := parser.ParseURI(value)
		if perr != nil {
			return fmt.Errorf("parse To URI: %w", perr)
		}
		tb.URI = uri
		tb.ParsedURI = uri
	case "d", "h":
		if tb.URI == nil {
			return fmt.Errorf("To URI not parsed")
		}
		tb.URI.Host = str.Mk(value)
	case "n":
		tb.DisplayName = str.Mk(value)
	case "p":
		if tb.URI == nil {
			return fmt.Errorf("To URI not parsed")
		}
		tb.URI.Port = str.Mk(value)
	default:
		return fmt.Errorf("unknown To part: %q", r.Part)
	}
	return nil
}

func (r *LvToRef) AssignInt(value int, msg *parser.SIPMsg, ctx *ExecContext) error {
	return r.Assign(strconv.Itoa(value), msg, ctx)
}

func (r *LvToRef) Get(msg *parser.SIPMsg, ctx *ExecContext) (string, error) {
	tb, err := ensureToBody(msg)
	if err != nil {
		return "", err
	}
	switch r.Part {
	case "u":
		if tb.URI != nil {
			return tb.URI.String(), nil
		}
		return "", nil
	case "d", "h":
		if tb.URI != nil {
			return tb.URI.Host.String(), nil
		}
		return "", nil
	case "n":
		return tb.DisplayName.String(), nil
	case "p":
		if tb.URI != nil {
			return tb.URI.Port.String(), nil
		}
		return "", nil
	}
	return "", fmt.Errorf("unknown To part: %q", r.Part)
}

func (r *LvToRef) GetInt(msg *parser.SIPMsg, ctx *ExecContext) (int, error) {
	s, err := r.Get(msg, ctx)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// ---------------------------------------------------------------------------
// Top-level assignment functions
// ---------------------------------------------------------------------------

// Assign evaluates rv and stores the result into lv.
// If rv is inherently an integer (IsInt() == true) the value is assigned
// via AssignInt; otherwise the string representation is used.
func Assign(lv LVal, rv AssignRVal, msg *parser.SIPMsg, ctx *ExecContext) error {
	if lv == nil {
		return fmt.Errorf("nil lvalue")
	}
	if rv == nil {
		return fmt.Errorf("nil rvalue")
	}
	if rv.IsInt() {
		n, err := rv.Int(msg, ctx)
		if err != nil {
			return err
		}
		return lv.AssignInt(n, msg, ctx)
	}
	s, err := rv.Str(msg, ctx)
	if err != nil {
		return err
	}
	return lv.Assign(s, msg, ctx)
}

// AssignStr stores a literal string into lv.
func AssignStr(lv LVal, val string, msg *parser.SIPMsg, ctx *ExecContext) error {
	if lv == nil {
		return fmt.Errorf("nil lvalue")
	}
	return lv.Assign(val, msg, ctx)
}

// AssignInt stores a literal integer into lv.
func AssignInt(lv LVal, val int, msg *parser.SIPMsg, ctx *ExecContext) error {
	if lv == nil {
		return fmt.Errorf("nil lvalue")
	}
	return lv.AssignInt(val, msg, ctx)
}

// ---------------------------------------------------------------------------
// ParseLVal — text → LVal parser
// ---------------------------------------------------------------------------

// ParseLVal parses a left-value specification such as "$var(x)", "$rU",
// "$avp(s:tag)", "$xavp(root[key])", "$ru", "$from(d)".
func ParseLVal(text string) (LVal, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "$") {
		return nil, fmt.Errorf("lvalue must start with '$': %q", text)
	}
	body := text[1:]
	lower := strings.ToLower(body)

	// $var(name)
	if strings.HasPrefix(lower, "var(") && strings.HasSuffix(body, ")") {
		name := body[4 : len(body)-1]
		return &LvPVarRef{Name: "var(" + name + ")"}, nil
	}

	// $avp(scope:name) or $avp(name)
	if strings.HasPrefix(lower, "avp(") && strings.HasSuffix(body, ")") {
		inner := body[4 : len(body)-1]
		return parseAvpLVal(inner)
	}

	// $xavp(root[key])
	if strings.HasPrefix(lower, "xavp(") && strings.HasSuffix(body, ")") {
		inner := body[5 : len(body)-1]
		return parseXAvpLVal(inner)
	}

	// $ruri(part) or $ruri
	if strings.HasPrefix(lower, "ruri(") && strings.HasSuffix(body, ")") {
		part := strings.ToLower(body[5 : len(body)-1])
		return &LvRURIRef{Part: part}, nil
	}
	if lower == "ruri" {
		return &LvRURIRef{Part: "full"}, nil
	}

	// $from(part)
	if strings.HasPrefix(lower, "from(") && strings.HasSuffix(body, ")") {
		part := strings.ToLower(body[5 : len(body)-1])
		return &LvFromRef{Part: part}, nil
	}

	// $to(part)
	if strings.HasPrefix(lower, "to(") && strings.HasSuffix(body, ")") {
		part := strings.ToLower(body[3 : len(body)-1])
		return &LvToRef{Part: part}, nil
	}

	// $dsturi
	if lower == "dsturi" {
		return &LvDstURIRef{}, nil
	}

	// Short-form PVs (case follows the same convention as ParsePV).
	switch body {
	case "ru", "RU":
		return &LvRURIRef{Part: "full"}, nil
	case "rU", "Ru":
		return &LvRURIRef{Part: "user"}, nil
	case "rd", "RD":
		return &LvRURIRef{Part: "domain"}, nil
	case "du", "DU":
		return &LvDstURIRef{}, nil
	case "fu", "FU":
		return &LvFromRef{Part: "u"}, nil
	case "fd", "FD":
		return &LvFromRef{Part: "d"}, nil
	case "fn", "FN":
		return &LvFromRef{Part: "n"}, nil
	case "tu", "TU":
		return &LvToRef{Part: "u"}, nil
	case "td", "TD":
		return &LvToRef{Part: "d"}, nil
	}

	return nil, fmt.Errorf("unsupported lvalue: %q", text)
}

// parseAvpLVal parses the inner part of $avp(...).
func parseAvpLVal(inner string) (LVal, error) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil, fmt.Errorf("empty AVP name")
	}
	if idx := strings.Index(inner, ":"); idx >= 0 {
		scope := strings.TrimSpace(inner[:idx])
		name := strings.TrimSpace(inner[idx+1:])
		return &LvAvpRef{Scope: scope, Name: name}, nil
	}
	// No scope prefix — default to string scope.
	return &LvAvpRef{Scope: "s", Name: inner}, nil
}

// parseXAvpLVal parses the inner part of $xavp(root[key]).
func parseXAvpLVal(inner string) (LVal, error) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil, fmt.Errorf("empty XAVP root")
	}
	root := inner
	key := ""
	if lb := strings.Index(inner, "["); lb >= 0 {
		root = strings.TrimSpace(inner[:lb])
		rest := inner[lb+1:]
		if rb := strings.Index(rest, "]"); rb >= 0 {
			key = strings.TrimSpace(rest[:rb])
		}
	}
	return &LvXAvpRef{Root: root, Key: key}, nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// pvNameToLVal converts a short PV name (e.g. "rU", "fd") to the
// corresponding dedicated LVal.  Returns an error for unknown names.
func pvNameToLVal(name string) (LVal, error) {
	switch name {
	case "ru", "RU":
		return &LvRURIRef{Part: "full"}, nil
	case "rU", "Ru":
		return &LvRURIRef{Part: "user"}, nil
	case "rd", "RD":
		return &LvRURIRef{Part: "domain"}, nil
	case "du", "DU":
		return &LvDstURIRef{}, nil
	case "fu", "FU":
		return &LvFromRef{Part: "u"}, nil
	case "fd", "FD":
		return &LvFromRef{Part: "d"}, nil
	case "fn", "FN":
		return &LvFromRef{Part: "n"}, nil
	case "tu", "TU":
		return &LvToRef{Part: "u"}, nil
	case "td", "TD":
		return &LvToRef{Part: "d"}, nil
	}
	return nil, fmt.Errorf("unsupported PV name for assignment: %q", name)
}

// ruriStr returns the current Request-URI string, preferring ctx.RURI
// and falling back to the message first-line URI.
func ruriStr(ctx *ExecContext, msg *parser.SIPMsg) string {
	if ctx != nil {
		ctx.mu.RLock()
		r := ctx.RURI
		ctx.mu.RUnlock()
		if r != "" {
			return r
		}
	}
	if msg != nil && msg.FirstLine != nil && msg.FirstLine.Req != nil {
		return msg.FirstLine.Req.URI.String()
	}
	return ""
}

// ensureFromBody returns the parsed From ToBody, parsing it if necessary.
func ensureFromBody(msg *parser.SIPMsg) (*parser.ToBody, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message")
	}
	if msg.From == nil {
		return nil, fmt.Errorf("no From header")
	}
	tb, err := msg.GetParsedFrom()
	if err != nil {
		return nil, fmt.Errorf("parse From header: %w", err)
	}
	if tb == nil {
		return nil, fmt.Errorf("failed to parse From header")
	}
	return tb, nil
}

// ensureToBody returns the parsed To ToBody, parsing it if necessary.
func ensureToBody(msg *parser.SIPMsg) (*parser.ToBody, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message")
	}
	if msg.To == nil {
		return nil, fmt.Errorf("no To header")
	}
	tb, err := msg.GetParsedTo()
	if err != nil {
		return nil, fmt.Errorf("parse To header: %w", err)
	}
	if tb == nil {
		return nil, fmt.Errorf("failed to parse To header")
	}
	return tb, nil
}

// extractUser extracts the user part from a SIP URI string.
func extractUser(uri string) string {
	colon := strings.Index(uri, ":")
	if colon < 0 {
		return ""
	}
	rest := uri[colon+1:]
	at := strings.Index(rest, "@")
	if at < 0 {
		return ""
	}
	return rest[:at]
}

// extractHost extracts the host part from a SIP URI string.
func extractHost(uri string) string {
	at := strings.Index(uri, "@")
	if at < 0 {
		return ""
	}
	rest := uri[at+1:]
	end := strings.IndexAny(rest, ":;>?")
	if end >= 0 {
		return rest[:end]
	}
	return rest
}
