// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for strutils functions
 */

package str

import (
	"strings"
	"testing"
)

func TestEscapeCommon(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello 'world'", "hello \\'world\\'"},
		{"test \"quote\"", "test \\\"quote\\\""},
		{"back\\slash", "back\\\\slash"},
		{"normal text", "normal text"},
		{"", ""},
	}
	for _, tt := range tests {
		result := EscapeCommon([]byte(tt.input))
		if string(result) != tt.expected {
			t.Errorf("EscapeCommon(%q) = %q, want %q", tt.input, string(result), tt.expected)
		}
	}
}

func TestUnescapeCommon(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello \\'world\\'", "hello 'world'"},
		{"test \\\"quote\\\"", "test \"quote\""},
		{"back\\\\slash", "back\\slash"},
		{"normal text", "normal text"},
		{"", ""},
	}
	for _, tt := range tests {
		result := UnescapeCommon([]byte(tt.input))
		if string(result) != tt.expected {
			t.Errorf("UnescapeCommon(%q) = %q, want %q", tt.input, string(result), tt.expected)
		}
	}
}

func TestEscapeCRLF(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"line1\nline2", "line1\\nline2"},
		{"line1\r\nline2", "line1\\r\\nline2"},
		{"normal text", "normal text"},
		{"", ""},
	}
	for _, tt := range tests {
		result := EscapeCRLF(Mk(tt.input))
		if result.String() != tt.expected {
			t.Errorf("EscapeCRLF(%q) = %q, want %q", tt.input, result.String(), tt.expected)
		}
	}
}

func TestUnescapeCRLF(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"line1\\nline2", "line1\nline2"},
		{"line1\\r\\nline2", "line1\r\nline2"},
		{"normal text", "normal text"},
		{"", ""},
	}
	for _, tt := range tests {
		result := UnescapeCRLF(Mk(tt.input))
		if result.String() != tt.expected {
			t.Errorf("UnescapeCRLF(%q) = %q, want %q", tt.input, result.String(), tt.expected)
		}
	}
}

func TestEscapeUser(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"user@host", "user%40host"},
		{"user name", "user%20name"},
		{"alice", "alice"},
		{"test!~*'()", "test!~*'()"},
		{"test&=+$,;?/", "test&=+$,;?/"},
	}
	for _, tt := range tests {
		result, err := EscapeUser(Mk(tt.input))
		if err != nil {
			t.Errorf("EscapeUser(%q) error: %v", tt.input, err)
			continue
		}
		if result.String() != tt.expected {
			t.Errorf("EscapeUser(%q) = %q, want %q", tt.input, result.String(), tt.expected)
		}
	}
}

func TestUnescapeUser(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"user%40host", "user@host"},
		{"user%20name", "user name"},
		{"alice", "alice"},
		{"test%21%7E%2A%27%28%29", "test!~*'()"},
	}
	for _, tt := range tests {
		result, err := UnescapeUser(Mk(tt.input))
		if err != nil {
			t.Errorf("UnescapeUser(%q) error: %v", tt.input, err)
			continue
		}
		if result.String() != tt.expected {
			t.Errorf("UnescapeUser(%q) = %q, want %q", tt.input, result.String(), tt.expected)
		}
	}
}

func TestEscapeCSV(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "\"hello\""},
		{"hello \"world\"", "\"hello \"\"world\"\"\""},
		{"", "\"\""},
	}
	for _, tt := range tests {
		result := EscapeCSV(Mk(tt.input))
		if result.String() != tt.expected {
			t.Errorf("EscapeCSV(%q) = %q, want %q", tt.input, result.String(), tt.expected)
		}
	}
}

func TestCmpStr(t *testing.T) {
	tests := []struct {
		s1       string
		s2       string
		expected int
	}{
		{"abc", "abc", 0},
		{"abc", "abd", -1},
		{"abd", "abc", 1},
		{"abc", "abcd", -1},
		{"abcd", "abc", 1},
		{"", "", 0},
		{"", "a", -1},
		{"a", "", 1},
	}
	for _, tt := range tests {
		result := CmpStr(Mk(tt.s1), Mk(tt.s2))
		if result != tt.expected {
			t.Errorf("CmpStr(%q, %q) = %d, want %d", tt.s1, tt.s2, result, tt.expected)
		}
	}
}

func TestCmpiStr(t *testing.T) {
	tests := []struct {
		s1       string
		s2       string
		expected int
	}{
		{"ABC", "abc", 0},
		{"abc", "ABC", 0},
		{"AbC", "aBc", 0},
		{"abc", "abd", -1},
		{"ABD", "abc", 1},
		{"abc", "ABCD", -1},
		{"ABCD", "abc", 1},
	}
	for _, tt := range tests {
		result := CmpiStr(Mk(tt.s1), Mk(tt.s2))
		if result != tt.expected {
			t.Errorf("CmpiStr(%q, %q) = %d, want %d", tt.s1, tt.s2, result, tt.expected)
		}
	}
}

func TestTrim(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello  ", "hello"},
		{"\thello\t", "hello"},
		{"hello", "hello"},
		{"   ", ""},
		{"", ""},
	}
	for _, tt := range tests {
		result := Trim(Mk(tt.input))
		if result.String() != tt.expected {
			t.Errorf("Trim(%q) = %q, want %q", tt.input, result.String(), tt.expected)
		}
	}
}

func TestToLower(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"HELLO", "hello"},
		{"Hello World", "hello world"},
		{"hello", "hello"},
		{"", ""},
	}
	for _, tt := range tests {
		result := ToLower(Mk(tt.input))
		if result.String() != tt.expected {
			t.Errorf("ToLower(%q) = %q, want %q", tt.input, result.String(), tt.expected)
		}
	}
}

func TestEqualFold(t *testing.T) {
	if !EqualFold(Mk("HELLO"), Mk("hello")) {
		t.Error("expected EqualFold(HELLO, hello) = true")
	}
	if EqualFold(Mk("hello"), Mk("world")) {
		t.Error("expected EqualFold(hello, world) = false")
	}
}

func TestJSONEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello \"world\"", "hello \\\"world\\\""},
		{"line1\nline2", "line1\\nline2"},
		{"tab\there", "tab\\there"},
	}
	for _, tt := range tests {
		result := JSONEscape(Mk(tt.input))
		if result.String() != tt.expected {
			t.Errorf("JSONEscape(%q) = %q, want %q", tt.input, result.String(), tt.expected)
		}
	}
}

func TestReplace(t *testing.T) {
	result := Replace(Mk("hello world"), Mk("world"), Mk("go"), 1)
	if result.String() != "hello go" {
		t.Errorf("Replace = %q, want %q", result.String(), "hello go")
	}

	result = Replace(Mk("aaa"), Mk("a"), Mk("b"), -1)
	if result.String() != "bbb" {
		t.Errorf("Replace all = %q, want %q", result.String(), "bbb")
	}
}

func TestFields(t *testing.T) {
	result := Fields(Mk("hello world test"))
	if len(result) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(result))
	}
	if result[0].String() != "hello" || result[1].String() != "world" || result[2].String() != "test" {
		t.Errorf("unexpected fields result")
	}
}

func TestEscapeParam(t *testing.T) {
	result, err := EscapeParam(Mk("param=value;other"))
	if err != nil {
		t.Fatalf("EscapeParam error: %v", err)
	}
	expected := "param%3dvalue%3bother"
	if result.String() != expected {
		t.Errorf("EscapeParam = %q, want %q", result.String(), expected)
	}

	result2, err := EscapeParam(Mk("[::test&test]"))
	if err != nil {
		t.Fatalf("EscapeParam error: %v", err)
	}
	if !strings.Contains(result2.String(), "test&test") {
		t.Errorf("EscapeParam with & should not escape &")
	}
}
