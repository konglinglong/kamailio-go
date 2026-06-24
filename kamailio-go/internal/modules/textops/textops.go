// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Text operations module - matching Kamailio modules/textops.
 *
 * Provides regular-expression based search/substitute operations on SIP
 * messages, header manipulation (append/insert/remove/count) and R-URI
 * rewriting. Pattern matching uses the Go standard library regexp package.
 */

package textops

import (
	"bytes"
	"regexp"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// TextOpsModule implements the textops module functionality.
// C: struct module textops
type TextOpsModule struct {
	// compiled caches are intentionally omitted: Kamailio recompiles the
	// pattern on every call (fixup happens at script parse time in C, but the
	// runtime search/subst always runs the regex against the live buffer).
	// We mirror that behaviour here for correctness over micro-optimisation.
}

// NewTextOpsModule creates a new TextOpsModule instance.
func NewTextOpsModule() *TextOpsModule {
	return &TextOpsModule{}
}

// compilePattern compiles a regex pattern, returning nil on error.
func compilePattern(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	// Search is case-sensitive by default in Kamailio's search(); callers
	// pass an explicit (?i) prefix when they need case-insensitivity.
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re
}

// bodyBytes returns the message body as a byte slice.
func bodyBytes(msg *parser.SIPMsg) []byte {
	if msg == nil {
		return nil
	}
	if b, ok := msg.Body.([]byte); ok {
		return b
	}
	// Fall back to extracting the body from the raw buffer.
	if msg.Buf != nil && msg.Len > 0 {
		idx := bytes.Index(msg.Buf[:msg.Len], []byte("\r\n\r\n"))
		if idx != -1 && idx+4 <= msg.Len {
			return msg.Buf[idx+4 : msg.Len]
		}
	}
	return nil
}

// headerTypeByName resolves a header name to its HdrType using the parser's
// header-name lookup. Unknown headers resolve to HdrOther.
func headerTypeByName(hdrName string) parser.HdrType {
	if hdrName == "" {
		return parser.HdrError
	}
	ht, _, _ := parser.ParseHeaderName([]byte(hdrName + ":"))
	return ht
}

// Search searches for pattern anywhere in the message buffer (first line,
// headers and body). Returns true if the pattern matches.
// C: search()
func (t *TextOpsModule) Search(msg *parser.SIPMsg, pattern string) bool {
	if msg == nil {
		return false
	}
	re := compilePattern(pattern)
	if re == nil {
		return false
	}
	return re.Match(msg.Buf)
}

// SearchBody searches for pattern in the message body only.
// C: search_body()
func (t *TextOpsModule) SearchBody(msg *parser.SIPMsg, pattern string) bool {
	if msg == nil {
		return false
	}
	re := compilePattern(pattern)
	if re == nil {
		return false
	}
	return re.Match(bodyBytes(msg))
}

// SearchHdr searches for pattern within the body of every header named
// hdrName (case-insensitive name match). Returns true on the first match.
// C: search_hf()
func (t *TextOpsModule) SearchHdr(msg *parser.SIPMsg, hdrName, pattern string) bool {
	if msg == nil || hdrName == "" {
		return false
	}
	re := compilePattern(pattern)
	if re == nil {
		return false
	}
	for _, h := range msg.Headers {
		if h == nil {
			continue
		}
		if strings.EqualFold(h.Name.String(), hdrName) {
			if re.MatchString(h.Body.String()) {
				return true
			}
		}
	}
	return false
}

// Subst substitutes every match of pattern with replacement in the whole
// message buffer. Returns the number of substitutions performed.
// C: subst()
func (t *TextOpsModule) Subst(msg *parser.SIPMsg, pattern, replacement string) int {
	if msg == nil {
		return 0
	}
	re := compilePattern(pattern)
	if re == nil {
		return 0
	}
	src := msg.Buf
	if src == nil {
		return 0
	}
	matches := re.FindAll(src, -1)
	count := len(matches)
	if count == 0 {
		return 0
	}
	msg.Buf = re.ReplaceAll(src, []byte(replacement))
	msg.Len = len(msg.Buf)
	msg.BufSize = len(msg.Buf)
	return count
}

// SubstBody substitutes every match of pattern with replacement in the
// message body. Returns the number of substitutions performed.
// C: subst_body()
func (t *TextOpsModule) SubstBody(msg *parser.SIPMsg, pattern, replacement string) int {
	if msg == nil {
		return 0
	}
	re := compilePattern(pattern)
	if re == nil {
		return 0
	}
	body := bodyBytes(msg)
	if body == nil {
		return 0
	}
	matches := re.FindAll(body, -1)
	count := len(matches)
	if count == 0 {
		return 0
	}
	msg.Body = re.ReplaceAll(body, []byte(replacement))
	return count
}

// ---------------------------------------------------------------------------
// Reply header accumulation (append_to_reply)
// ---------------------------------------------------------------------------

// SIPMsg has no reply-lump field exposed by the parser, so reply headers
// appended via AppendToReply are tracked in a package-level registry keyed by
// the message pointer. This mirrors Kamailio's msg->reply_lumps list.
var (
	replyMu      sync.Mutex
	replyHeaders = make(map[*parser.SIPMsg][]string)
)

// AppendToReply appends a header to the reply that will be generated for msg.
// C: append_to_reply()
func (t *TextOpsModule) AppendToReply(msg *parser.SIPMsg, header string) {
	if msg == nil {
		return
	}
	replyMu.Lock()
	defer replyMu.Unlock()
	replyHeaders[msg] = append(replyHeaders[msg], header)
}

// GetReplyHeaders returns the headers accumulated for msg via AppendToReply.
// The returned slice is a copy; callers may inspect it freely.
func GetReplyHeaders(msg *parser.SIPMsg) []string {
	replyMu.Lock()
	defer replyMu.Unlock()
	hdrs := replyHeaders[msg]
	if len(hdrs) == 0 {
		return nil
	}
	out := make([]string, len(hdrs))
	copy(out, hdrs)
	return out
}

// ClearReplyHeaders removes any reply headers accumulated for msg.
func ClearReplyHeaders(msg *parser.SIPMsg) {
	replyMu.Lock()
	defer replyMu.Unlock()
	delete(replyHeaders, msg)
}

// AppendToMsg appends text to the end of the message buffer.
// C: append_to_msg() / add_lump_rpl after body
func (t *TextOpsModule) AppendToMsg(msg *parser.SIPMsg, text string) {
	if msg == nil {
		return
	}
	msg.Buf = append(msg.Buf, []byte(text)...)
	msg.Len = len(msg.Buf)
	msg.BufSize = len(msg.Buf)
}

// InsertToMsg inserts text into the message buffer at the given byte position.
// C: insert_to_msg()
func (t *TextOpsModule) InsertToMsg(msg *parser.SIPMsg, text string, position int) {
	if msg == nil {
		return
	}
	if position < 0 {
		position = 0
	}
	if msg.Buf == nil {
		msg.Buf = []byte(text)
		msg.Len = len(msg.Buf)
		msg.BufSize = msg.Len
		return
	}
	if position > len(msg.Buf) {
		position = len(msg.Buf)
	}
	textBytes := []byte(text)
	result := make([]byte, 0, len(msg.Buf)+len(textBytes))
	result = append(result, msg.Buf[:position]...)
	result = append(result, textBytes...)
	result = append(result, msg.Buf[position:]...)
	msg.Buf = result
	msg.Len = len(msg.Buf)
	msg.BufSize = msg.Len
}

// RemoveHeader removes every header named hdrName (case-insensitive) from the
// message and returns the number of headers removed.
// C: remove_hf()
func (t *TextOpsModule) RemoveHeader(msg *parser.SIPMsg, hdrName string) int {
	if msg == nil || hdrName == "" {
		return 0
	}
	ht := headerTypeByName(hdrName)
	if ht == parser.HdrError {
		return 0
	}
	if ht != parser.HdrOther {
		count := msg.CountHeadersByType(ht)
		if count > 0 {
			msg.RemoveHeadersByType(ht)
		}
		return count
	}
	// Unknown header: match by name.
	count := 0
	kept := msg.Headers[:0]
	for _, h := range msg.Headers {
		if h != nil && strings.EqualFold(h.Name.String(), hdrName) {
			count++
			continue
		}
		kept = append(kept, h)
	}
	msg.Headers = kept
	return count
}

// IsPresentHeader returns true if at least one header named hdrName is
// present in the message.
// C: is_present_hf()
func (t *TextOpsModule) IsPresentHeader(msg *parser.SIPMsg, hdrName string) bool {
	return t.CountHeader(msg, hdrName) > 0
}

// CountHeader returns the number of headers named hdrName in the message.
// C: count_hf() / is_present_hf() variant
func (t *TextOpsModule) CountHeader(msg *parser.SIPMsg, hdrName string) int {
	if msg == nil || hdrName == "" {
		return 0
	}
	ht := headerTypeByName(hdrName)
	if ht == parser.HdrError {
		return 0
	}
	if ht != parser.HdrOther {
		return msg.CountHeadersByType(ht)
	}
	count := 0
	for _, h := range msg.Headers {
		if h != nil && strings.EqualFold(h.Name.String(), hdrName) {
			count++
		}
	}
	return count
}

// ChangeURI rewrites the Request-URI (R-URI) of the message and re-parses it.
// C: change_uri() / seturi()
func (t *TextOpsModule) ChangeURI(msg *parser.SIPMsg, newURI string) {
	if msg == nil {
		return
	}
	msg.SetRURI(newURI)
	if uri, err := parser.ParseURI(newURI); err == nil {
		msg.ParsedURI = uri
	}
}

// SetURI sets the Request-URI of the message. Equivalent to ChangeURI.
// C: set_uri() / seturi()
func (t *TextOpsModule) SetURI(msg *parser.SIPMsg, uri string) {
	t.ChangeURI(msg, uri)
}

// ---------------------------------------------------------------------------
// Package-level default instance and global functions
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.Mutex
	defaultTextOps = NewTextOpsModule()
)

// DefaultTextOps returns the package-level default TextOpsModule instance.
func DefaultTextOps() *TextOpsModule {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTextOps == nil {
		defaultTextOps = NewTextOpsModule()
	}
	return defaultTextOps
}

// Init initialises the textops module (resets the default instance).
// C: mod_init()
func Init() error {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultTextOps = NewTextOpsModule()
	return nil
}

// Search is the package-level wrapper around the default instance.
func Search(msg *parser.SIPMsg, pattern string) bool {
	return DefaultTextOps().Search(msg, pattern)
}

// Subst is the package-level wrapper around the default instance.
func Subst(msg *parser.SIPMsg, pattern, replacement string) int {
	return DefaultTextOps().Subst(msg, pattern, replacement)
}

// RemoveHeader is the package-level wrapper around the default instance.
func RemoveHeader(msg *parser.SIPMsg, hdrName string) int {
	return DefaultTextOps().RemoveHeader(msg, hdrName)
}

// IsPresentHeader is the package-level wrapper around the default instance.
func IsPresentHeader(msg *parser.SIPMsg, hdrName string) bool {
	return DefaultTextOps().IsPresentHeader(msg, hdrName)
}
