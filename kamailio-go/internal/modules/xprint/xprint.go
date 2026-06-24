// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * XPrint module - extended printing of SIP message parts.
 * Port of the kamailio xprint module (src/modules/xprint).
 *
 * The xprint module formats parts of a SIP message (first line, headers,
 * body) and expands pseudo-variable tokens against a format string.
 * This Go counterpart supports a small set of tokens covering the most
 * common message fields.
 *
 * It is safe for concurrent use.
 */

package xprint

import (
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// XPrintModule formats parts of a SIP message.
// C: struct module xprint
type XPrintModule struct {
	mu sync.RWMutex
}

// New creates an XPrintModule.
func New() *XPrintModule {
	return &XPrintModule{}
}

// Print expands the pseudo-variable tokens in format against msg and
// returns the result. Supported tokens:
//
//	%ci - Call-ID
//	%fu - From header body
//	%tu - To header body
//	%ru - Request-URI
//	%rm - request method
//	%rs - reply status code
//
//	C: xprint() / xdbg()
func (m *XPrintModule) Print(msg *parser.SIPMsg, format string) string {
	if m == nil || msg == nil {
		return format
	}
	out := format
	out = strings.ReplaceAll(out, "%ci", headerBody(msg, msg.CallID, parser.HdrCallID))
	out = strings.ReplaceAll(out, "%fu", headerBody(msg, msg.From, parser.HdrFrom))
	out = strings.ReplaceAll(out, "%tu", headerBody(msg, msg.To, parser.HdrTo))
	out = strings.ReplaceAll(out, "%ru", requestURI(msg))
	out = strings.ReplaceAll(out, "%rm", method(msg))
	out = strings.ReplaceAll(out, "%rs", statusCode(msg))
	return out
}

// PrintHeaders returns every header of msg formatted as "Name: Body", one
// per line.
//
//	C: xprint_headers()
func (m *XPrintModule) PrintHeaders(msg *parser.SIPMsg) string {
	if m == nil || msg == nil {
		return ""
	}
	var b strings.Builder
	for _, h := range msg.Headers {
		if h == nil {
			continue
		}
		b.WriteString(h.Name.String())
		b.WriteString(": ")
		b.WriteString(h.Body.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// PrintBody returns the message body as a string, or the empty string when
// the message has no body.
//
//	C: xprint_body()
func (m *XPrintModule) PrintBody(msg *parser.SIPMsg) string {
	if m == nil || msg == nil {
		return ""
	}
	if b, ok := msg.Body.([]byte); ok {
		return string(b)
	}
	if s, ok := msg.Body.(string); ok {
		return s
	}
	return ""
}

// PrintFirstLine returns the first line of msg: the request line for a
// request, or the status line for a reply.
//
//	C: xprint_first_line()
func (m *XPrintModule) PrintFirstLine(msg *parser.SIPMsg) string {
	if m == nil || msg == nil || msg.FirstLine == nil {
		return ""
	}
	fl := msg.FirstLine
	if fl.Req != nil {
		return strings.TrimSpace(fl.Req.Method.String() + " " +
			fl.Req.URI.String() + " " + fl.Req.Version.String())
	}
	if fl.Reply != nil {
		return strings.TrimSpace(fl.Reply.Version.String() + " " +
			fl.Reply.Status.String() + " " + fl.Reply.Reason.String())
	}
	return ""
}

// headerBody returns the body string of a header, looking it up by quick
// reference first, then by type.
func headerBody(msg *parser.SIPMsg, quick *parser.HdrField, ht parser.HdrType) string {
	if quick != nil {
		return quick.Body.String()
	}
	if msg != nil {
		if h := msg.GetHeaderByType(ht); h != nil {
			return h.Body.String()
		}
	}
	return ""
}

// requestURI returns the request URI string from msg's request line.
func requestURI(msg *parser.SIPMsg) string {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return ""
	}
	return msg.FirstLine.Req.URI.String()
}

// method returns the request method string from msg's request line.
func method(msg *parser.SIPMsg) string {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return ""
	}
	return msg.FirstLine.Req.Method.String()
}

// statusCode returns the reply status code string from msg's reply line.
func statusCode(msg *parser.SIPMsg) string {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Reply == nil {
		return ""
	}
	return msg.FirstLine.Reply.Status.String()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.RWMutex
	defaultXPrint  *XPrintModule
)

// DefaultXPrint returns the process-wide XPrintModule, creating it on first
// use.
func DefaultXPrint() *XPrintModule {
	defaultMu.RLock()
	m := defaultXPrint
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultXPrint == nil {
		defaultXPrint = New()
	}
	return defaultXPrint
}

// Init (re)initialises the process-wide XPrintModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultXPrint = New()
}
