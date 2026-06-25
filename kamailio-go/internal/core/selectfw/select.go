// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Select framework - matching C select.h / select_core.h / select_buf.h.
 *
 * The select framework provides a uniform dotted-path notation for
 * extracting fields from a parsed SIP message. The script syntax is:
 *
 *	@via.1.host          # first Via header, host part
 *	@ruri.user           # R-URI user part
 *	@from.tag            # From header tag parameter
 *	@msg.header."X-Foo"  # arbitrary header by name
 *
 * This Go port implements:
 *   - ParseSelect: tokenises a "@a.b.c" path into a SelectPath.
 *   - SelectRegistry: maps path components to handler functions.
 *   - Evaluate: resolves a path against a SIPMsg and returns the value.
 *
 * The default registry registers handlers for the most common selects
 * documented in select_core.h.
 */

package selectfw

import (
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// SelectValue is the result of evaluating a select. It carries a string
// value and an optional integer form.
type SelectValue struct {
	Str string
	Int int64
	OK  bool
}

// SelectPath is a parsed select expression. Each segment is either a
// named component (e.g. "via", "host") or an index (e.g. "1").
type SelectPath struct {
	Segments []string
}

// String renders the path in dotted notation, e.g. "via.1.host".
func (p SelectPath) String() string {
	return strings.Join(p.Segments, ".")
}

// SelectHandler resolves a select path against a message. The handler
// receives the message, the remaining path segments after its own
// component, and the index (if any) that followed the component.
//
// Handlers may chain: a handler for "via" may delegate to a sub-handler
// for "host" by inspecting the remaining segments.
type SelectHandler func(msg *parser.SIPMsg, remaining []string) (SelectValue, error)

// SelectRegistry maps the leading path component to its handler.
type SelectRegistry struct {
	mu       sync.RWMutex
	handlers map[string]SelectHandler
}

// NewSelectRegistry creates an empty registry.
func NewSelectRegistry() *SelectRegistry {
	return &SelectRegistry{handlers: make(map[string]SelectHandler)}
}

// Register associates a handler with the given leading component name.
// Names are case-insensitive (matching C's select_core.c behaviour).
func (r *SelectRegistry) Register(name string, h SelectHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[strings.ToLower(name)] = h
}

// Evaluate resolves a SelectPath against msg. The leading segment
// selects the handler; remaining segments are passed to the handler.
func (r *SelectRegistry) Evaluate(msg *parser.SIPMsg, path SelectPath) (SelectValue, error) {
	if len(path.Segments) == 0 {
		return SelectValue{}, errors.New("select: empty path")
	}
	r.mu.RLock()
	h, ok := r.handlers[strings.ToLower(path.Segments[0])]
	r.mu.RUnlock()
	if !ok {
		return SelectValue{}, errors.New("select: unknown component: " + path.Segments[0])
	}
	return h(msg, path.Segments[1:])
}

// ParseSelect parses a select expression string (without the leading
// "@" character) into a SelectPath. Quoted segments (e.g. header names
// containing dots) are supported via double quotes:
//
//	@msg.header."P-Asserted-Identity"  ->  ["msg", "header", "P-Asserted-Identity"]
func ParseSelect(s string) (SelectPath, error) {
	var segs []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == '.' && !inQuote:
			segs = append(segs, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	segs = append(segs, cur.String())
	if inQuote {
		return SelectPath{}, errors.New("select: unterminated quote")
	}
	// Filter out empty segments caused by leading dots or double dots.
	var filtered []string
	for _, s := range segs {
		if s != "" {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) == 0 {
		return SelectPath{}, errors.New("select: empty path")
	}
	return SelectPath{Segments: filtered}, nil
}

// ParseSelectFromExpr parses a full select expression including the
// leading "@" character. Returns an error if the "@" prefix is missing.
func ParseSelectFromExpr(expr string) (SelectPath, error) {
	if !strings.HasPrefix(expr, "@") {
		return SelectPath{}, errors.New("select: expression must start with @")
	}
	return ParseSelect(expr[1:])
}

// ErrNoValue is returned by handlers when the requested field is not
// present in the message.
var ErrNoValue = errors.New("select: no value")

// ErrInvalidPath is returned when a path cannot be resolved.
var ErrInvalidPath = errors.New("select: invalid path")

// ---------------------------------------------------------------------------
// Default registry with built-in handlers
// ---------------------------------------------------------------------------

var (
	defaultOnce     sync.Once
	defaultRegistry *SelectRegistry
)

// DefaultRegistry returns the process-wide select registry with the
// built-in handlers registered. Callers may add custom handlers on top.
func DefaultRegistry() *SelectRegistry {
	defaultOnce.Do(func() {
		defaultRegistry = NewSelectRegistry()
		registerBuiltins(defaultRegistry)
	})
	return defaultRegistry
}

// Evaluate is a convenience function that resolves a select expression
// (including the leading "@") against msg using the default registry.
func Evaluate(expr string, msg *parser.SIPMsg) (SelectValue, error) {
	path, err := ParseSelectFromExpr(expr)
	if err != nil {
		return SelectValue{}, err
	}
	return DefaultRegistry().Evaluate(msg, path)
}

// registerBuiltins installs the default select handlers.
func registerBuiltins(r *SelectRegistry) {
	r.Register("ruri", selectRURI)
	r.Register("furi", selectFromURI) // alias used in some configs
	r.Register("from", selectFrom)
	r.Register("turi", selectToURI)
	r.Register("to", selectTo)
	r.Register("via", selectVia)
	r.Register("callid", selectCallID)
	r.Register("cseq", selectCSeq)
	r.Register("contact", selectContact)
	r.Register("method", selectMethod)
	r.Register("msg", selectMsg)
	r.Register("user", selectUser)
	r.Register("domain", selectDomain)
	r.Register("v", selectVersion)
	r.Register("received", selectReceived)
}

// takeIndex extracts the first segment as a numeric index if present,
// returning the index (1-based, matching C convention) and remaining
// segments. If the first segment is not numeric, returns 1 and the
// unchanged remaining slice.
func takeIndex(segs []string) (int, []string) {
	if len(segs) == 0 {
		return 1, segs
	}
	if n, err := strconv.Atoi(segs[0]); err == nil {
		return n, segs[1:]
	}
	return 1, segs
}

func strVal(s string) SelectValue {
	return SelectValue{Str: s, OK: true}
}

func emptyVal() SelectValue {
	return SelectValue{OK: false}
}

func selectRURI(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return emptyVal(), ErrNoValue
	}
	uri := msg.FirstLine.Req.URI.String()
	if len(segs) == 0 {
		return strVal(uri), nil
	}
	switch segs[0] {
	case "user":
		return extractURIUser(uri)
	case "host", "domain":
		return extractURIHost(uri)
	case "port":
		return extractURIPort(uri)
	case "transport":
		return extractURIParam(uri, "transport")
	case "params":
		if len(segs) > 1 {
			return extractURIParam(uri, segs[1])
		}
		return strVal(""), nil
	default:
		return emptyVal(), ErrInvalidPath
	}
}

func selectFrom(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.From == nil {
		return emptyVal(), ErrNoValue
	}
	if len(segs) == 0 || segs[0] == "uri" {
		uri := extractAddrSpec(msg.From.Body.String())
		if len(segs) > 0 && segs[0] != "uri" {
			// fallthrough to sub-selects below
		} else if len(segs) == 0 {
			return strVal(uri), nil
		} else {
			// segs[0] == "uri", check for further sub-segments
			if len(segs) > 1 {
				return uriSubSelect(uri, segs[1])
			}
			return strVal(uri), nil
		}
	}
	switch segs[0] {
	case "tag":
		return extractHeaderParam(msg.From.Body.String(), "tag")
	case "user":
		return extractURIUser(extractAddrSpec(msg.From.Body.String()))
	case "host", "domain":
		return extractURIHost(extractAddrSpec(msg.From.Body.String()))
	case "port":
		return extractURIPort(extractAddrSpec(msg.From.Body.String()))
	default:
		return emptyVal(), ErrInvalidPath
	}
}

func selectFromURI(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.From == nil {
		return emptyVal(), ErrNoValue
	}
	uri := extractAddrSpec(msg.From.Body.String())
	if len(segs) == 0 {
		return strVal(uri), nil
	}
	return uriSubSelect(uri, segs[0])
}

func selectTo(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.To == nil {
		return emptyVal(), ErrNoValue
	}
	if len(segs) == 0 || segs[0] == "uri" {
		uri := extractAddrSpec(msg.To.Body.String())
		if len(segs) == 0 {
			return strVal(uri), nil
		}
		if segs[0] == "uri" && len(segs) > 1 {
			return uriSubSelect(uri, segs[1])
		}
		return strVal(uri), nil
	}
	switch segs[0] {
	case "tag":
		return extractHeaderParam(msg.To.Body.String(), "tag")
	case "user":
		return extractURIUser(extractAddrSpec(msg.To.Body.String()))
	case "host", "domain":
		return extractURIHost(extractAddrSpec(msg.To.Body.String()))
	default:
		return emptyVal(), ErrInvalidPath
	}
}

func selectToURI(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.To == nil {
		return emptyVal(), ErrNoValue
	}
	uri := extractAddrSpec(msg.To.Body.String())
	if len(segs) == 0 {
		return strVal(uri), nil
	}
	return uriSubSelect(uri, segs[0])
}

func selectVia(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil {
		return emptyVal(), ErrNoValue
	}
	idx, rest := takeIndex(segs)
	// idx is 1-based; we map it to msg.Via1 (idx=1) or msg.Via2 (idx=2).
	var via *parser.ViaBody
	if idx == 1 {
		via = msg.Via1
	} else if idx == 2 {
		via = msg.Via2
	}
	if via == nil {
		// Fall back to parsed Via headers.
		count := 0
		for _, h := range msg.Headers {
			if h.Type != parser.HdrVia {
				continue
			}
			count++
			if count == idx {
				if vb, ok := h.Parsed.(*parser.ViaBody); ok && vb != nil {
					via = vb
				}
				break
			}
		}
	}
	if via == nil {
		return emptyVal(), ErrNoValue
	}
	if len(rest) == 0 {
		// Return the full Via as a string approximation.
		return strVal(via.Host.String()), nil
	}
	switch rest[0] {
	case "host":
		return strVal(via.Host.String()), nil
	case "port":
		if via.Port > 0 {
			return strVal(strconv.FormatUint(uint64(via.Port), 10)), nil
		}
		if via.PortStr.Len > 0 {
			return strVal(via.PortStr.String()), nil
		}
		return emptyVal(), ErrNoValue
	case "protocol", "transport":
		return strVal(via.Transport.String()), nil
	case "branch":
		if via.Branch != nil {
			return strVal(via.Branch.Value.String()), nil
		}
		return emptyVal(), ErrNoValue
	case "received":
		if via.Received != nil {
			return strVal(via.Received.Value.String()), nil
		}
		return emptyVal(), ErrNoValue
	case "rport":
		if via.RPort != nil {
			return strVal(via.RPort.Value.String()), nil
		}
		return emptyVal(), ErrNoValue
	default:
		return emptyVal(), ErrInvalidPath
	}
}

func selectCallID(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.CallID == nil {
		return emptyVal(), ErrNoValue
	}
	return strVal(msg.CallID.Body.String()), nil
}

func selectCSeq(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.CSeq == nil {
		return emptyVal(), ErrNoValue
	}
	body := msg.CSeq.Body.String()
	if len(segs) == 0 {
		return strVal(body), nil
	}
	switch segs[0] {
	case "num", "number":
		// CSeq body is "<num> <method>"
		if i := strings.IndexByte(body, ' '); i > 0 {
			return strVal(body[:i]), nil
		}
		return strVal(body), nil
	case "method":
		if i := strings.IndexByte(body, ' '); i >= 0 && i+1 < len(body) {
			return strVal(body[i+1:]), nil
		}
		return emptyVal(), ErrNoValue
	default:
		return emptyVal(), ErrInvalidPath
	}
}

func selectContact(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.Contact == nil {
		return emptyVal(), ErrNoValue
	}
	uri := extractAddrSpec(msg.Contact.Body.String())
	if len(segs) == 0 {
		return strVal(uri), nil
	}
	switch segs[0] {
	case "uri":
		if len(segs) > 1 {
			return uriSubSelect(uri, segs[1])
		}
		return strVal(uri), nil
	case "user":
		return extractURIUser(uri)
	case "host", "domain":
		return extractURIHost(uri)
	case "q":
		return extractHeaderParam(msg.Contact.Body.String(), "q")
	default:
		return emptyVal(), ErrInvalidPath
	}
}

func selectMethod(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return emptyVal(), ErrNoValue
	}
	return strVal(msg.FirstLine.Req.Method.String()), nil
}

func selectMsg(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil {
		return emptyVal(), ErrNoValue
	}
	if len(segs) == 0 {
		return emptyVal(), ErrInvalidPath
	}
	switch segs[0] {
	case "header":
		if len(segs) < 2 {
			return emptyVal(), ErrInvalidPath
		}
		name := strings.Trim(segs[1], "\"")
		for _, h := range msg.Headers {
			if strings.EqualFold(h.Name.String(), name) {
				return strVal(h.Body.String()), nil
			}
		}
		return emptyVal(), ErrNoValue
	case "body":
		if msg.Buf != nil && msg.Len > 0 {
			// Find body offset.
			bodyStart := 0
			for _, h := range msg.Headers {
				bodyStart = h.Offset + h.Len
			}
			if bodyStart < msg.Len {
				body := msg.Buf[bodyStart:msg.Len]
				// Skip leading CRLF if present.
				body = bytesTrimCRLF(body)
				return strVal(string(body)), nil
			}
		}
		return emptyVal(), ErrNoValue
	case "first_line":
		if msg.FirstLine != nil {
			return strVal(firstLineString(msg.FirstLine)), nil
		}
		return emptyVal(), ErrNoValue
	case "request":
		if msg.FirstLine != nil && msg.FirstLine.Req != nil {
			return strVal(msg.FirstLine.Req.URI.String()), nil
		}
		return emptyVal(), ErrNoValue
	default:
		return emptyVal(), ErrInvalidPath
	}
}

func selectUser(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return emptyVal(), ErrNoValue
	}
	uri := msg.FirstLine.Req.URI.String()
	return extractURIUser(uri)
}

func selectDomain(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return emptyVal(), ErrNoValue
	}
	uri := msg.FirstLine.Req.URI.String()
	return extractURIHost(uri)
}

func selectVersion(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil || msg.FirstLine == nil {
		return emptyVal(), ErrNoValue
	}
	if msg.FirstLine.Req != nil {
		return strVal(msg.FirstLine.Req.Version.String()), nil
	}
	if msg.FirstLine.Reply != nil {
		return strVal(msg.FirstLine.Reply.Version.String()), nil
	}
	return emptyVal(), ErrNoValue
}

func selectReceived(msg *parser.SIPMsg, segs []string) (SelectValue, error) {
	if msg == nil {
		return emptyVal(), ErrNoValue
	}
	// In this in-process port, "received" returns the Via1 host as a
	// proxy for the source address. The C select uses msg->rcv.src_ip.
	if msg.Via1 != nil {
		return strVal(msg.Via1.Host.String()), nil
	}
	return emptyVal(), ErrNoValue
}

// ---------------------------------------------------------------------------
// URI / header parsing helpers
// ---------------------------------------------------------------------------

// extractAddrSpec extracts the addr-spec (sip:user@host) from a header
// body that may include display name and angle brackets.
func extractAddrSpec(body string) string {
	// If angle brackets are present, extract the content between them.
	if lt := strings.IndexByte(body, '<'); lt >= 0 {
		if gt := strings.IndexByte(body[lt:], '>'); gt > 0 {
			return body[lt+1 : lt+gt]
		}
	}
	// Otherwise, trim whitespace and parameters.
	body = strings.TrimSpace(body)
	if semi := strings.IndexByte(body, ';'); semi >= 0 {
		body = body[:semi]
	}
	return body
}

func extractURIUser(uri string) (SelectValue, error) {
	// Strip scheme.
	rest := uri
	if i := strings.Index(rest, ":"); i >= 0 {
		rest = rest[i+1:]
	}
	// Strip parameters.
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		rest = rest[:semi]
	}
	// Extract user part before @.
	if at := strings.IndexByte(rest, '@'); at >= 0 {
		return strVal(rest[:at]), nil
	}
	return emptyVal(), ErrNoValue
}

func extractURIHost(uri string) (SelectValue, error) {
	rest := uri
	if i := strings.Index(rest, ":"); i >= 0 {
		rest = rest[i+1:]
	}
	// Strip parameters.
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		rest = rest[:semi]
	}
	// Extract host part after @.
	if at := strings.IndexByte(rest, '@'); at >= 0 {
		rest = rest[at+1:]
	}
	// Handle IPv6 in brackets.
	if strings.HasPrefix(rest, "[") {
		if end := strings.IndexByte(rest, ']'); end >= 0 {
			return strVal(rest[1:end]), nil
		}
	}
	// Strip port.
	if colon := strings.IndexByte(rest, ':'); colon >= 0 {
		return strVal(rest[:colon]), nil
	}
	return strVal(rest), nil
}

func extractURIPort(uri string) (SelectValue, error) {
	rest := uri
	if i := strings.Index(rest, ":"); i >= 0 {
		rest = rest[i+1:]
	}
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		rest = rest[:semi]
	}
	if at := strings.IndexByte(rest, '@'); at >= 0 {
		rest = rest[at+1:]
	}
	// Handle IPv6: skip bracketed address.
	if strings.HasPrefix(rest, "[") {
		if end := strings.IndexByte(rest, ']'); end >= 0 {
			rest = rest[end+1:]
		}
	}
	if colon := strings.IndexByte(rest, ':'); colon >= 0 {
		end := colon + 1
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		return strVal(rest[colon+1 : end]), nil
	}
	return emptyVal(), ErrNoValue
}

func extractURIParam(uri, name string) (SelectValue, error) {
	semi := strings.IndexByte(uri, ';')
	if semi < 0 {
		return emptyVal(), ErrNoValue
	}
	params := uri[semi+1:]
	for _, p := range strings.Split(params, ";") {
		eq := strings.IndexByte(p, '=')
		if eq < 0 {
			continue
		}
		if strings.EqualFold(p[:eq], name) {
			return strVal(p[eq+1:]), nil
		}
	}
	return emptyVal(), ErrNoValue
}

func extractHeaderParam(body, name string) (SelectValue, error) {
	semi := strings.IndexByte(body, ';')
	if semi < 0 {
		return emptyVal(), ErrNoValue
	}
	params := body[semi+1:]
	for _, p := range strings.Split(params, ";") {
		eq := strings.IndexByte(p, '=')
		if eq < 0 {
			if strings.EqualFold(strings.TrimSpace(p), name) {
				// Flag-style parameter with no value.
				return strVal(""), nil
			}
			continue
		}
		if strings.EqualFold(strings.TrimSpace(p[:eq]), name) {
			return strVal(strings.TrimSpace(p[eq+1:])), nil
		}
	}
	return emptyVal(), ErrNoValue
}

func uriSubSelect(uri, sub string) (SelectValue, error) {
	switch sub {
	case "user":
		return extractURIUser(uri)
	case "host", "domain":
		return extractURIHost(uri)
	case "port":
		return extractURIPort(uri)
	case "transport":
		return extractURIParam(uri, "transport")
	default:
		return emptyVal(), ErrInvalidPath
	}
}

func firstLineString(fl *parser.MsgStart) string {
	if fl == nil {
		return ""
	}
	if fl.Req != nil {
		return fl.Req.Method.String() + " " + fl.Req.URI.String() + " " + fl.Req.Version.String()
	}
	if fl.Reply != nil {
		return fl.Reply.Version.String() + " " + fl.Reply.Status.String() + " " + fl.Reply.Reason.String()
	}
	return ""
}

func bytesTrimCRLF(b []byte) []byte {
	for len(b) >= 2 && b[0] == '\r' && b[1] == '\n' {
		b = b[2:]
	}
	return b
}

// Str converts a SelectValue to a str.Str. Empty/unset values produce
// an empty str.Str.
func (v SelectValue) Str2() str.Str {
	if !v.OK {
		return str.Str{}
	}
	return str.Mk(v.Str)
}
