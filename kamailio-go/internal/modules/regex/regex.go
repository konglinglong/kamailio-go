// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Regex module - regular-expression operations on SIP messages.
 * Port of the kamailio regex module (src/modules/regex).
 *
 * The regex module exposes PCRE-style matching against the whole SIP
 * message, its body or an individual header, plus substitution helpers.
 * Pattern matching uses the Go standard library regexp package (RE2
 * syntax), which is a safe, bounded alternative to PCRE.
 *
 * It is safe for concurrent use: the module holds no mutable state after
 * construction and the process-wide singleton is guarded by a mutex.
 */

package regex

import (
	"bytes"
	"errors"
	"regexp"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// RegexModule implements the regex module functionality.
// C: struct module regex
type RegexModule struct {
	mu sync.RWMutex
}

// New creates a RegexModule instance.
func New() *RegexModule {
	return &RegexModule{}
}

// Compile compiles pattern and returns the resulting regexp. Returns an
// error when the pattern is invalid or empty.
//
//	C: pcre_compile() wrapper
func (m *RegexModule) Compile(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, errors.New("regex: empty pattern")
	}
	return regexp.Compile(pattern)
}

// mustCompile is a helper that compiles pattern and returns nil on error.
func mustCompile(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re
}

// Match reports whether pattern matches anywhere in input. Returns false
// when the pattern is invalid or does not match.
//
//	C: regex_match() analogue
func (m *RegexModule) Match(pattern string, input string) bool {
	re := mustCompile(pattern)
	if re == nil {
		return false
	}
	return re.MatchString(input)
}

// MatchMsg reports whether pattern matches anywhere in the entire SIP
// message (first line, headers and body). The message is serialised with
// RebuildMessage so modifications are reflected.
//
//	C: regex_match_msg() analogue
func (m *RegexModule) MatchMsg(msg *parser.SIPMsg, pattern string) bool {
	if msg == nil {
		return false
	}
	re := mustCompile(pattern)
	if re == nil {
		return false
	}
	return re.MatchString(msg.RebuildMessage())
}

// MatchBody reports whether pattern matches anywhere in the message body.
//
//	C: regex_match_body() analogue
func (m *RegexModule) MatchBody(msg *parser.SIPMsg, pattern string) bool {
	if msg == nil {
		return false
	}
	re := mustCompile(pattern)
	if re == nil {
		return false
	}
	return re.Match(bodyBytes(msg))
}

// MatchHeader reports whether pattern matches the body of the first header
// named headerName (case-insensitive). Returns false when the header is
// absent or the pattern is invalid.
//
//	C: regex_match_header() analogue
func (m *RegexModule) MatchHeader(msg *parser.SIPMsg, headerName string, pattern string) bool {
	if msg == nil || headerName == "" {
		return false
	}
	re := mustCompile(pattern)
	if re == nil {
		return false
	}
	for _, h := range msg.Headers {
		if equalFold(h.Name.String(), headerName) {
			return re.MatchString(h.Body.String())
		}
	}
	return false
}

// Replace substitutes every match of pattern in input with replacement
// and returns the result. Returns an error when the pattern is invalid.
//
//	C: regex_replace() analogue
func (m *RegexModule) Replace(pattern string, input string, replacement string) (string, error) {
	if pattern == "" {
		return "", errors.New("regex: empty pattern")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(input, replacement), nil
}

// ReplaceMsg substitutes every match of pattern in the message buffer with
// replacement, updating msg.Buf in place. Returns the number of
// replacements performed, or -1 when msg is nil or the pattern is invalid.
//
//	C: regex_replace_msg() analogue
func (m *RegexModule) ReplaceMsg(msg *parser.SIPMsg, pattern string, replacement string) int {
	if msg == nil {
		return -1
	}
	re := mustCompile(pattern)
	if re == nil {
		return -1
	}
	text := msg.RebuildMessage()
	matches := re.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return 0
	}
	out := re.ReplaceAllString(text, replacement)
	msg.Buf = []byte(out)
	msg.Len = len(out)
	msg.BufSize = len(out)
	return len(matches)
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

// equalFold reports whether a and b are equal under ASCII case-folding.
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca := a[i]
		cb := b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu    sync.RWMutex
	defaultRegex *RegexModule
)

// DefaultRegex returns the process-wide RegexModule, creating it on first
// use.
func DefaultRegex() *RegexModule {
	defaultMu.RLock()
	m := defaultRegex
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRegex == nil {
		defaultRegex = New()
	}
	return defaultRegex
}

// Init (re)initialises the process-wide RegexModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRegex = New()
}
