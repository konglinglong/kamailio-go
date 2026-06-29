// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * String utilities - matching C strutils.c
 * Includes escape/unescape functions, URL encoding/decoding,
 * comparison functions, and hex conversion utilities.
 */

package str

import (
	"bytes"
	"errors"
	"strings"
)

var (
	ErrInvalidChar     = errors.New("invalid character")
	ErrIncompleteEscape = errors.New("incomplete escape sequence")
	ErrInvalidHex      = errors.New("invalid hex digit")
)

func EscapeCommon(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, 0, len(src)*2)
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\'', '"', '\\':
			dst = append(dst, '\\', src[i])
		case '\000':
			dst = append(dst, '\\', '0')
		default:
			dst = append(dst, src[i])
		}
	}
	return dst
}

func UnescapeCommon(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, 0, len(src))
	i := 0
	for i < len(src) {
		if src[i] == '\\' && i+1 < len(src) {
			switch src[i+1] {
			case '\'':
				dst = append(dst, '\'')
				i++
			case '"':
				dst = append(dst, '"')
				i++
			case '\\':
				dst = append(dst, '\\')
				i++
			case '0':
				dst = append(dst, '\000')
				i++
			default:
				dst = append(dst, src[i])
			}
		} else {
			dst = append(dst, src[i])
		}
		i++
	}
	return dst
}

func EscapeCRLF(sin Str) Str {
	if sin.IsEmpty() {
		return Str{}
	}
	dst := make([]byte, 0, sin.Len*2)
	for i := 0; i < sin.Len; i++ {
		switch sin.S[i] {
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		default:
			dst = append(dst, sin.S[i])
		}
	}
	return MkBytes(dst)
}

func UnescapeCRLF(sin Str) Str {
	if sin.IsEmpty() {
		return Str{}
	}
	dst := make([]byte, 0, sin.Len)
	i := 0
	for i < sin.Len {
		if sin.S[i] == '\\' && i+1 < sin.Len {
			switch sin.S[i+1] {
			case 'n':
				dst = append(dst, '\n')
				i++
			case 'r':
				dst = append(dst, '\r')
				i++
			default:
				dst = append(dst, sin.S[i])
			}
		} else {
			dst = append(dst, sin.S[i])
		}
		i++
	}
	return MkBytes(dst)
}

func hexToChar(h byte) (byte, bool) {
	switch {
	case h >= '0' && h <= '9':
		return h - '0', true
	case h >= 'a' && h <= 'f':
		return h - 'a' + 10, true
	case h >= 'A' && h <= 'F':
		return h - 'A' + 10, true
	default:
		return 0, false
	}
}

func charToHex(c byte) byte {
	if c < 10 {
		return c + '0'
	}
	return c - 10 + 'a'
}

func isUserUnreserved(c byte) bool {
	switch c {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')',
		'&', '=', '+', '$', ',', ';', '?', '/':
		return true
	default:
		return false
	}
}

func isParamUnreserved(c byte) bool {
	switch c {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')',
		'[', ']', '/', ':', '&', '+', '$':
		return true
	default:
		return false
	}
}

func EscapeUser(sin Str) (Str, error) {
	if sin.IsEmpty() {
		return Str{}, nil
	}
	dst := make([]byte, 0, sin.Len*3)
	for i := 0; i < sin.Len; i++ {
		c := sin.S[i]
		if c < 32 || c > 126 {
			return Str{}, ErrInvalidChar
		}
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			dst = append(dst, c)
		} else if isUserUnreserved(c) {
			dst = append(dst, c)
		} else {
			dst = append(dst, '%', charToHex(c>>4), charToHex(c&0x0f))
		}
	}
	return MkBytes(dst), nil
}

func UnescapeUser(sin Str) (Str, error) {
	if sin.IsEmpty() {
		return Str{}, nil
	}
	dst := make([]byte, 0, sin.Len)
	for i := 0; i < sin.Len; i++ {
		if sin.S[i] == '%' {
			if i+2 >= sin.Len {
				return Str{}, ErrIncompleteEscape
			}
			h1, ok := hexToChar(sin.S[i+1])
			if !ok {
				return Str{}, ErrInvalidHex
			}
			h2, ok := hexToChar(sin.S[i+2])
			if !ok {
				return Str{}, ErrInvalidHex
			}
			c := (h1 << 4) + h2
			if c < 32 || c > 126 {
				return Str{}, ErrInvalidChar
			}
			dst = append(dst, c)
			i += 2
		} else {
			dst = append(dst, sin.S[i])
		}
	}
	return MkBytes(dst), nil
}

func EscapeParam(sin Str) (Str, error) {
	if sin.IsEmpty() {
		return Str{}, nil
	}
	dst := make([]byte, 0, sin.Len*3)
	for i := 0; i < sin.Len; i++ {
		c := sin.S[i]
		if c < 32 || c > 126 {
			return Str{}, ErrInvalidChar
		}
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			dst = append(dst, c)
		} else if isParamUnreserved(c) {
			dst = append(dst, c)
		} else {
			dst = append(dst, '%', charToHex(c>>4), charToHex(c&0x0f))
		}
	}
	return MkBytes(dst), nil
}

func UnescapeParam(sin Str) (Str, error) {
	return UnescapeUser(sin)
}

func EscapeCSV(sin Str) Str {
	if sin.IsEmpty() {
		return Mk("\"\"")
	}
	dst := make([]byte, 0, sin.Len*2+2)
	dst = append(dst, '"')
	for i := 0; i < sin.Len; i++ {
		if sin.S[i] == '"' {
			dst = append(dst, '"')
		}
		dst = append(dst, sin.S[i])
	}
	dst = append(dst, '"')
	return MkBytes(dst)
}

func CmpStr(s1, s2 Str) int {
	if s1.Len == 0 && s2.Len == 0 {
		return 0
	}
	if s1.Len == 0 {
		return -1
	}
	if s2.Len == 0 {
		return 1
	}
	minLen := s1.Len
	if s2.Len < minLen {
		minLen = s2.Len
	}
	ret := bytes.Compare(s1.S[:minLen], s2.S[:minLen])
	if ret == 0 {
		if s1.Len == s2.Len {
			return 0
		}
		if s1.Len < s2.Len {
			return -1
		}
		return 1
	}
	return ret
}

func CmpiStr(s1, s2 Str) int {
	if s1.Len == 0 && s2.Len == 0 {
		return 0
	}
	if s1.Len == 0 {
		return -1
	}
	if s2.Len == 0 {
		return 1
	}
	minLen := s1.Len
	if s2.Len < minLen {
		minLen = s2.Len
	}
	for i := 0; i < minLen; i++ {
		c1 := lowerCase(s1.S[i])
		c2 := lowerCase(s2.S[i])
		if c1 < c2 {
			return -1
		}
		if c1 > c2 {
			return 1
		}
	}
	if s1.Len == s2.Len {
		return 0
	}
	if s1.Len < s2.Len {
		return -1
	}
	return 1
}

func lowerCase(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func UrlEncode(sin Str) (Str, error) {
	return EscapeUser(sin)
}

func UrlDecode(sin Str) (Str, error) {
	return UnescapeUser(sin)
}

func JSONEscape(sin Str) Str {
	if sin.IsEmpty() {
		return Str{}
	}
	dst := make([]byte, 0, sin.Len*2)
	for i := 0; i < sin.Len; i++ {
		switch sin.S[i] {
		case '"':
			dst = append(dst, '\\', '"')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\f':
			dst = append(dst, '\\', 'f')
		default:
			if sin.S[i] < 32 {
				dst = append(dst, '\\', 'u', '0', '0',
					charToHex(sin.S[i]>>4), charToHex(sin.S[i]&0x0f))
			} else {
				dst = append(dst, sin.S[i])
			}
		}
	}
	return MkBytes(dst)
}

func TrimTrailing(sin Str) Str {
	if sin.IsEmpty() {
		return sin
	}
	end := sin.Len
	for end > 0 && (sin.S[end-1] == ' ' || sin.S[end-1] == '\t') {
		end--
	}
	if end == sin.Len {
		return sin
	}
	return Str{S: sin.S, Len: end}
}

func TrimLeading(sin Str) Str {
	if sin.IsEmpty() {
		return sin
	}
	start := 0
	for start < sin.Len && (sin.S[start] == ' ' || sin.S[start] == '\t') {
		start++
	}
	if start == 0 {
		return sin
	}
	return Str{S: sin.S[start:], Len: sin.Len - start}
}

func Trim(sin Str) Str {
	return TrimTrailing(TrimLeading(sin))
}

func ToLower(sin Str) Str {
	if sin.IsEmpty() {
		return sin
	}
	dst := make([]byte, sin.Len)
	for i := 0; i < sin.Len; i++ {
		dst[i] = lowerCase(sin.S[i])
	}
	return MkBytes(dst)
}

func Contains(s, substr Str) bool {
	return s.Index(substr) >= 0
}

func ContainsString(s Str, substr string) bool {
	return strings.Contains(s.String(), substr)
}

func EqualFold(s1, s2 Str) bool {
	return CmpiStr(s1, s2) == 0
}

func Repeat(s Str, count int) Str {
	if count <= 0 || s.IsEmpty() {
		return Str{}
	}
	dst := make([]byte, 0, s.Len*count)
	for i := 0; i < count; i++ {
		dst = append(dst, s.S[:s.Len]...)
	}
	return MkBytes(dst)
}

func Replace(s, old, new Str, n int) Str {
	if old.IsEmpty() || s.IsEmpty() || n == 0 {
		return s.Clone()
	}
	result := make([]byte, 0, s.Len)
	start := 0
	count := 0
	for {
		idx := s.Skip(start).Index(old)
		if idx < 0 {
			break
		}
		result = append(result, s.S[start:start+idx]...)
		result = append(result, new.S[:new.Len]...)
		start += idx + old.Len
		count++
		if n > 0 && count >= n {
			break
		}
	}
	result = append(result, s.S[start:]...)
	return MkBytes(result)
}

func Fields(s Str) []Str {
	if s.IsEmpty() {
		return nil
	}
	var fields []Str
	start := -1
	for i := 0; i < s.Len; i++ {
		if s.S[i] == ' ' || s.S[i] == '\t' {
			if start >= 0 {
				fields = append(fields, Str{S: s.S[start:], Len: i - start})
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		fields = append(fields, Str{S: s.S[start:], Len: s.Len - start})
	}
	return fields
}
