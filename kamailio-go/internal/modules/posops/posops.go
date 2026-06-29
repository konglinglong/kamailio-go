// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * posops - position-aware message operations.
 *
 * Search, replace, insert and append text in a SIP message buffer with
 * explicit byte offsets. Mirrors the kamailio posops module.
 */

package posops

import (
	"regexp"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// PosOpsModule performs position-based buffer manipulation.
type PosOpsModule struct{}

// New returns a new PosOpsModule.
func New() *PosOpsModule { return &PosOpsModule{} }

// Search looks for pattern in msg.Buf starting at position. Returns the
// absolute byte index of the first match and true on success.
func (m *PosOpsModule) Search(msg *parser.SIPMsg, pattern string, position int) (int, bool) {
	if m == nil || msg == nil || msg.Buf == nil {
		return 0, false
	}
	if position < 0 {
		position = 0
	}
	if position > len(msg.Buf) {
		return 0, false
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, false
	}
	loc := re.FindIndex(msg.Buf[position:])
	if loc == nil {
		return 0, false
	}
	return position + loc[0], true
}

// Replace substitutes every match of pattern (at or after position) with
// replacement in msg.Buf and returns the number of substitutions.
func (m *PosOpsModule) Replace(msg *parser.SIPMsg, pattern, replacement string, position int) int {
	if m == nil || msg == nil || msg.Buf == nil {
		return 0
	}
	if position < 0 {
		position = 0
	}
	if position > len(msg.Buf) {
		return 0
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0
	}
	matches := re.FindAll(msg.Buf[position:], -1)
	count := len(matches)
	if count == 0 {
		return 0
	}
	head := msg.Buf[:position]
	tail := re.ReplaceAll(msg.Buf[position:], []byte(replacement))
	msg.Buf = append(head, tail...)
	msg.Len = len(msg.Buf)
	msg.BufSize = len(msg.Buf)
	return count
}

// Insert inserts text into msg.Buf at position and returns the new length.
func (m *PosOpsModule) Insert(msg *parser.SIPMsg, text string, position int) int {
	if m == nil || msg == nil {
		return 0
	}
	if position < 0 {
		position = 0
	}
	if msg.Buf == nil {
		msg.Buf = []byte(text)
		msg.Len = len(msg.Buf)
		msg.BufSize = msg.Len
		return msg.Len
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
	return msg.Len
}

// Append appends text to the end of msg.Buf and returns the new length.
func (m *PosOpsModule) Append(msg *parser.SIPMsg, text string) int {
	if m == nil || msg == nil {
		return 0
	}
	msg.Buf = append(msg.Buf, []byte(text)...)
	msg.Len = len(msg.Buf)
	msg.BufSize = msg.Len
	return msg.Len
}
