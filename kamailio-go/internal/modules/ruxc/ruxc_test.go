// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - RuxC module tests.
 */

package ruxc

import (
	"sync"
	"testing"
)

func TestCompileEval(t *testing.T) {
	m := New()
	if m.IsCompiled() {
		t.Fatal("expected not compiled initially")
	}
	if err := m.Compile(`\d+`); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !m.IsCompiled() {
		t.Fatal("expected compiled after Compile")
	}
	got, err := m.Eval("abc123def456")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got != "123" {
		t.Fatalf("Eval = %q, want 123", got)
	}
	if m.Pattern() != `\d+` {
		t.Fatalf("Pattern = %q", m.Pattern())
	}
}

func TestCompileErrors(t *testing.T) {
	m := New()
	if err := m.Compile(""); err == nil {
		t.Fatal("expected error for empty pattern")
	}
	if err := m.Compile(`[`); err == nil {
		t.Fatal("expected error for invalid pattern")
	}
}

func TestEvalErrors(t *testing.T) {
	m := New()
	if _, err := m.Eval("abc"); err == nil {
		t.Fatal("expected error when no pattern compiled")
	}
	if err := m.Compile(`\d+`); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := m.Eval(""); err == nil {
		t.Fatal("expected error for empty expression")
	}
	got, _ := m.Eval("no digits here")
	if got != "" {
		t.Fatalf("Eval = %q, want empty (no match)", got)
	}
}

func TestRecompile(t *testing.T) {
	m := New()
	m.Compile(`\d+`)
	m.Compile(`[a-z]+`)
	got, _ := m.Eval("abc123")
	if got != "abc" {
		t.Fatalf("Eval after recompile = %q, want abc", got)
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	if IsCompiled() {
		t.Fatal("expected not compiled after Init")
	}
	if err := Compile(`\d+`); err != nil {
		t.Fatalf("global Compile: %v", err)
	}
	got, err := Eval("x42y")
	if err != nil || got != "42" {
		t.Fatalf("global Eval = %q, %v", got, err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.Compile(`\d+`)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Eval("abc123")
			_ = m.IsCompiled()
		}()
	}
	wg.Wait()
}
