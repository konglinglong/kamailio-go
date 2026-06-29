// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * TextOpsX module - extended text operations.
 * Port of the kamailio textopsx module (src/modules/textopsx).
 *
 * Extends textops with regular-expression search/substitute over the whole
 * message buffer and content-type / SDP media inspection helpers
 * (is_audio / is_video / is_application).
 *
 * It is safe for concurrent use.
 */
package textopsx

import (
	"bytes"
	"regexp"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// TextOpsXModule implements the textopsx module functionality.
// C: struct module textopsx
type TextOpsXModule struct {
	mu sync.RWMutex
}

// NewTextOpsXModule creates a TextOpsXModule.
func NewTextOpsXModule() *TextOpsXModule {
	return &TextOpsXModule{}
}

// compilePattern compiles a regex pattern, returning nil on error.
func compilePattern(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
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
	if msg.Buf != nil && msg.Len > 0 {
		idx := bytes.Index(msg.Buf[:msg.Len], []byte("\r\n\r\n"))
		if idx != -1 && idx+4 <= msg.Len {
			return msg.Buf[idx+4 : msg.Len]
		}
	}
	return nil
}

// contentTypeOf returns the lower-cased, trimmed Content-Type value of
// msg, or "" when absent.
func contentTypeOf(msg *parser.SIPMsg) string {
	if msg == nil {
		return ""
	}
	if msg.ContentType != nil {
		return strings.ToLower(strings.TrimSpace(msg.ContentType.Body.String()))
	}
	for _, h := range msg.Headers {
		if h != nil && strings.EqualFold(h.Name.String(), "Content-Type") {
			return strings.ToLower(strings.TrimSpace(h.Body.String()))
		}
	}
	return ""
}

// bodyHasLine reports whether the message body contains a line beginning with
// prefix (after trimming whitespace).
func bodyHasLine(msg *parser.SIPMsg, prefix string) bool {
	body := bodyBytes(msg)
	if len(body) == 0 {
		return false
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}

// SearchRe searches for pattern anywhere in the message buffer (first line,
// headers and body). Returns true if the pattern matches.
// C: search_re()
func (t *TextOpsXModule) SearchRe(msg *parser.SIPMsg, pattern string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if msg == nil {
		return false
	}
	re := compilePattern(pattern)
	if re == nil {
		return false
	}
	return re.Match(msg.Buf)
}

// SubstRe substitutes every match of pattern with replacement in the whole
// message buffer. Returns the number of substitutions performed.
// C: subst_re()
func (t *TextOpsXModule) SubstRe(msg *parser.SIPMsg, pattern, replacement string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
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

// IsAudio reports whether the message carries audio media. For an SDP body
// this inspects the m= lines; otherwise it checks the Content-Type prefix.
// C: is_audio()
func (t *TextOpsXModule) IsAudio(msg *parser.SIPMsg) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ct := contentTypeOf(msg)
	if strings.HasPrefix(ct, "audio/") {
		return true
	}
	if ct == "application/sdp" {
		return bodyHasLine(msg, "m=audio")
	}
	return false
}

// IsVideo reports whether the message carries video media.
// C: is_video()
func (t *TextOpsXModule) IsVideo(msg *parser.SIPMsg) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ct := contentTypeOf(msg)
	if strings.HasPrefix(ct, "video/") {
		return true
	}
	if ct == "application/sdp" {
		return bodyHasLine(msg, "m=video")
	}
	return false
}

// IsApplication reports whether the message Content-Type is an application/*
// type (including application/sdp).
// C: is_application()
func (t *TextOpsXModule) IsApplication(msg *parser.SIPMsg) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return strings.HasPrefix(contentTypeOf(msg), "application/")
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultTextOpsX *TextOpsXModule
)

// DefaultTextOpsX returns the process-wide TextOpsXModule, creating one on
// first use.
func DefaultTextOpsX() *TextOpsXModule {
	defaultMu.RLock()
	m := defaultTextOpsX
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTextOpsX == nil {
		defaultTextOpsX = NewTextOpsXModule()
	}
	return defaultTextOpsX
}

// Init (re)initialises the process-wide TextOpsXModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultTextOpsX = NewTextOpsXModule()
}
