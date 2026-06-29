// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - gzcompress module tests.
 */

package gzcompress

import (
	"strings"
	"sync"
	"testing"
)

func TestCompressDecompressBytes(t *testing.T) {
	m := New()
	plain := []byte("hello hello hello hello hello hello")
	ct, err := m.Compress(plain)
	if err != nil {
		t.Fatalf("Compress error: %v", err)
	}
	if string(ct) == string(plain) {
		t.Error("compressed should differ from input")
	}
	pt, err := m.Decompress(ct)
	if err != nil {
		t.Fatalf("Decompress error: %v", err)
	}
	if string(pt) != string(plain) {
		t.Errorf("round-trip mismatch: got %q want %q", pt, plain)
	}
}

func TestCompressDecompressString(t *testing.T) {
	m := New()
	plain := strings.Repeat("kamailio-go ", 50)
	enc, err := m.CompressString(plain)
	if err != nil {
		t.Fatalf("CompressString error: %v", err)
	}
	if enc == "" || strings.Contains(enc, " ") {
		t.Errorf("CompressString should return non-empty base64 without spaces")
	}
	out, err := m.DecompressString(enc)
	if err != nil {
		t.Fatalf("DecompressString error: %v", err)
	}
	if out != plain {
		t.Errorf("string round-trip mismatch: got %q want %q", out, plain)
	}
}

func TestErrors(t *testing.T) {
	m := New()
	if _, err := m.Compress(nil); err == nil {
		t.Errorf("Compress with empty data should error")
	}
	if _, err := m.Decompress(nil); err == nil {
		t.Errorf("Decompress with empty data should error")
	}
	if _, err := m.CompressString(""); err == nil {
		t.Errorf("CompressString with empty string should error")
	}
	if _, err := m.DecompressString(""); err == nil {
		t.Errorf("DecompressString with empty string should error")
	}
	if _, err := m.DecompressString("!!!not-base64!!!"); err == nil {
		t.Errorf("DecompressString with invalid base64 should error")
	}
	if _, err := m.Decompress([]byte("not gzip")); err == nil {
		t.Errorf("Decompress with invalid gzip should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultGZCompress()
	b := DefaultGZCompress()
	if a != b {
		t.Fatal("DefaultGZCompress should return the same instance")
	}
	ct, _ := a.Compress([]byte("x"))
	if pt, err := a.Decompress(ct); err != nil || string(pt) != "x" {
		t.Errorf("default round-trip failed: %v %q", err, pt)
	}
	Init()
	c := DefaultGZCompress()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ct, _ := m.Compress([]byte("data data data"))
			_, _ = m.Decompress(ct)
			enc, _ := m.CompressString("str str str")
			_, _ = m.DecompressString(enc)
		}()
	}
	wg.Wait()
}
