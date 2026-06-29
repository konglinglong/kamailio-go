// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Utils module.
 */

package utils

import (
	"sync"
	"testing"
)

func TestBase64(t *testing.T) {
	m := New()
	cases := []string{"", "hello", "Kamailio-Go", "a b c"}
	for _, in := range cases {
		enc := m.EncodeBase64(in)
		dec, err := m.DecodeBase64(enc)
		if err != nil {
			t.Errorf("DecodeBase64(%q) error = %v", enc, err)
			continue
		}
		if dec != in {
			t.Errorf("round-trip base64 %q -> %q -> %q", in, enc, dec)
		}
	}
	// Invalid base64 -> error.
	if _, err := m.DecodeBase64("!!!not-base64!!!"); err == nil {
		t.Errorf("DecodeBase64(invalid) should error")
	}
}

func TestHexAndURL(t *testing.T) {
	m := New()
	// Hex round-trip.
	enc := m.EncodeHex("hello")
	if enc != "68656c6c6f" {
		t.Errorf("EncodeHex(hello) = %q, want 68656c6c6f", enc)
	}
	dec, err := m.DecodeHex(enc)
	if err != nil {
		t.Fatalf("DecodeHex() error = %v", err)
	}
	if dec != "hello" {
		t.Errorf("DecodeHex() = %q, want hello", dec)
	}
	if _, err := m.DecodeHex("xyz"); err == nil {
		t.Errorf("DecodeHex(invalid) should error")
	}
	// URL round-trip.
	enc = m.URLEncode("a b&c=d")
	dec, err = m.URLDecode(enc)
	if err != nil {
		t.Fatalf("URLDecode() error = %v", err)
	}
	if dec != "a b&c=d" {
		t.Errorf("URLDecode() = %q, want 'a b&c=d'", dec)
	}
	if _, err := m.URLDecode("%zz"); err == nil {
		t.Errorf("URLDecode(invalid) should error")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			enc := m.EncodeBase64("concurrent")
			_, _ = m.DecodeBase64(enc)
			_ = m.EncodeHex("concurrent")
			_, _ = m.DecodeHex(m.EncodeHex("concurrent"))
			_ = m.URLEncode("a b")
			_, _ = m.URLDecode(m.URLEncode("a b"))
		}()
	}
	wg.Wait()
}

func TestDefault(t *testing.T) {
	if New() == nil {
		t.Fatal("New() = nil")
	}
	if DefaultUtils() == nil {
		t.Fatal("DefaultUtils() = nil")
	}
	if Init() == nil {
		t.Fatal("Init() = nil")
	}
}
