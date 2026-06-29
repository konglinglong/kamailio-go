// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the FileOut module.
 */

package file_out

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriteReadDelete(t *testing.T) {
	dir := t.TempDir()
	m := New()
	m.Init(dir)
	data := []byte("hello file_out")
	if err := m.Write("a.txt", data); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	got, err := m.Read("a.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Read() = %q, want %q", got, data)
	}
	if !m.Delete("a.txt") {
		t.Errorf("Delete(a.txt) = false, want true")
	}
	if _, err := m.Read("a.txt"); err == nil {
		t.Errorf("Read() after delete should error")
	}
	if m.Delete("a.txt") {
		t.Errorf("Delete(a.txt) twice = true, want false")
	}
	// Empty name -> error.
	if err := m.Write("", data); err == nil {
		t.Errorf("Write(empty name) should error")
	}
}

func TestListAndNotInitialised(t *testing.T) {
	// Not initialised -> errors / nil.
	m := New()
	if err := m.Write("a.txt", []byte("x")); err == nil {
		t.Errorf("Write() before Init should error")
	}
	if _, err := m.Read("a.txt"); err == nil {
		t.Errorf("Read() before Init should error")
	}
	if m.List() != nil {
		t.Errorf("List() before Init should be nil")
	}

	dir := t.TempDir()
	m.Init(dir)
	m.Write("b.txt", []byte("b"))
	m.Write("a.txt", []byte("a"))
	m.Write("c.bin", []byte("c"))
	got := m.List()
	want := []string{"a.txt", "b.txt", "c.bin"}
	if len(got) != len(want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("List()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestConcurrent(t *testing.T) {
	dir := t.TempDir()
	m := New()
	m.Init(dir)
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			name := "f" + itoa(i) + ".txt"
			data := []byte("data-" + itoa(i))
			_ = m.Write(name, data)
			if got, err := m.Read(name); err != nil || !bytes.Equal(got, data) {
				t.Errorf("Read(%s) round-trip failed: %v", name, err)
			}
			m.List()
			m.Delete(name)
		}(i)
	}
	wg.Wait()
	if got := m.List(); len(got) != 0 {
		t.Errorf("List() after concurrent = %v, want empty", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultFileOut()
	if d == nil {
		t.Fatal("DefaultFileOut() = nil")
	}
	if d != DefaultFileOut() {
		t.Fatal("DefaultFileOut() returned different instances")
	}
	// Default starts unconfigured.
	if err := d.Write("x", []byte("y")); err == nil {
		t.Errorf("default Write before Init(addr) should error")
	}
	dir := t.TempDir()
	d.Init(dir)
	if err := d.Write("default.txt", []byte("z")); err != nil {
		t.Fatalf("default Write after Init error = %v", err)
	}
	if got, _ := d.Read("default.txt"); string(got) != "z" {
		t.Errorf("default Read = %q, want z", got)
	}
	// Package Init() resets to unconfigured.
	Init()
	if err := DefaultFileOut().Write("x", []byte("y")); err == nil {
		t.Errorf("Write after re-Init should error")
	}
	// Clean up the temp dir's leftover file.
	_ = os.Remove(filepath.Join(dir, "default.txt"))
}

// itoa is a tiny local int->string helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
