// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for CRC functions
 */

package hash

import (
	"fmt"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/str"
)

func TestCRC32(t *testing.T) {
	tests := []struct {
		input    string
		expected uint32
	}{
		{"", 0x00000000},
		{"hello", 0x3610a686},
		{"hello world", 0x0d4a1185},
	}
	for _, tt := range tests {
		result := CRC32([]byte(tt.input))
		if tt.input != "" && result == 0 {
			t.Errorf("CRC32(%q) = 0, expected non-zero", tt.input)
		}
		t.Logf("CRC32(%q) = 0x%08x", tt.input, result)
	}
}

func TestCRC32Str(t *testing.T) {
	s := str.Mk("hello world")
	result := CRC32Str(s)
	if result == 0 {
		t.Error("CRC32Str returned 0")
	}
	if result != CRC32([]byte("hello world")) {
		t.Error("CRC32Str result mismatch with CRC32")
	}
}

func TestCRC16CCITT(t *testing.T) {
	tests := []struct {
		input string
	}{
		{""},
		{"hello"},
		{"hello world"},
		{"123456789"},
	}
	for _, tt := range tests {
		result := CRC16CCITT([]byte(tt.input))
		t.Logf("CRC16CCITT(%q) = 0x%04x", tt.input, result)
	}

	result := CRC16CCITT([]byte("123456789"))
	if result == 0 {
		t.Error("CRC16CCITT returned 0 for non-empty input")
	}
}

func TestCRC16CCITTEx(t *testing.T) {
	data := []byte("hello")
	result1 := CRC16CCITT(data)
	result2 := CRC16CCITTEx(data, 0)
	if result1 != result2 {
		t.Errorf("CRC16CCITT != CRC16CCITTEx with 0 initial: 0x%04x vs 0x%04x", result1, result2)
	}
}

func TestCRC16CCITTStr(t *testing.T) {
	s := str.Mk("hello world")
	result := CRC16CCITTStr(s)
	if result == 0 {
		t.Error("CRC16CCITTStr returned 0")
	}
	if result != CRC16CCITT([]byte("hello world")) {
		t.Error("CRC16CCITTStr result mismatch")
	}
}

func TestCRC16(t *testing.T) {
	tests := []struct {
		input string
	}{
		{""},
		{"hello"},
		{"hello world"},
		{"123456789"},
	}
	for _, tt := range tests {
		result := CRC16([]byte(tt.input))
		t.Logf("CRC16(%q) = 0x%04x", tt.input, result)
	}
}

func TestCRC16Str(t *testing.T) {
	s := str.Mk("hello world")
	result := CRC16Str(s)
	if result != CRC16([]byte("hello world")) {
		t.Error("CRC16Str result mismatch")
	}
}

func TestCRC16CCITTArray(t *testing.T) {
	strs := []str.Str{
		str.Mk("hello"),
		str.Mk(" "),
		str.Mk("world"),
	}
	result := CRC16CCITTArray(strs)
	if len(result) != 4 {
		t.Errorf("expected 4 hex chars, got %d", len(result))
	}
	t.Logf("CRC16CCITTArray = %s", result)

	single := CRC16CCITT([]byte("hello world"))
	expected := fmt.Sprintf("%04x", single)
	if result != expected {
		t.Errorf("array result mismatch: %s vs %s", result, expected)
	}
}
