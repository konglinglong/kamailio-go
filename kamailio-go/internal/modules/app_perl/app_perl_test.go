// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the app_perl module - Perl script bindings (mock).
 */
package app_perl

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

func TestInitAndLoad(t *testing.T) {
	m := New()
	if m.IsLoaded() {
		t.Error("fresh module should not be loaded")
	}
	if err := m.Init(&Config{ScriptPath: "route.pl"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Init with ScriptPath")
	}
	if m.ScriptCount() != 1 {
		t.Errorf("ScriptCount = %d, want 1", m.ScriptCount())
	}
	if err := m.LoadScript("other.pl"); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if m.ScriptCount() != 2 {
		t.Errorf("ScriptCount = %d, want 2", m.ScriptCount())
	}
}

func TestLoadScriptEmpty(t *testing.T) {
	m := New()
	if err := m.LoadScript(""); err == nil {
		t.Error("LoadScript(empty) should error")
	}
}

func TestInitNilConfig(t *testing.T) {
	m := New()
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init(nil): %v", err)
	}
	if m.IsLoaded() {
		t.Error("nil config should not load a script")
	}
}

func TestRegisterAndCall(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "route.pl"})
	if err := m.RegisterFunction("add", "route.pl"); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}
	mock := m.interp.(*mockPerlInterp)
	mock.RegisterFunc("add", func(args ...interface{}) interface{} {
		sum := 0
		for _, a := range args[1:] { // skip msg
			if n, ok := a.(string); ok {
				if v := parseInt(n); v >= 0 {
					sum += v
				}
			}
		}
		return sum
	})
	ret, err := m.CallFunction("add", &parser.SIPMsg{}, "1", "2", "3")
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if ret != 6 {
		t.Errorf("CallFunction = %d, want 6", ret)
	}
}

func TestCallBeforeLoad(t *testing.T) {
	m := New()
	if _, err := m.CallFunction("x", &parser.SIPMsg{}); err == nil {
		t.Error("CallFunction before load should error")
	}
}

func TestCallUnknownFunction(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.pl"})
	if _, err := m.CallFunction("missing", &parser.SIPMsg{}); err == nil {
		t.Error("CallFunction unknown should error")
	}
}

func TestRegisterFunctionErrors(t *testing.T) {
	m := New()
	if err := m.RegisterFunction("", "s.pl"); err == nil {
		t.Error("RegisterFunction(empty name) should error")
	}
	if err := m.RegisterFunction("f", ""); err == nil {
		t.Error("RegisterFunction(empty path) should error")
	}
}

func TestEval(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.pl"})
	got, err := m.Eval("5 + 6")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got != "5 + 6" {
		t.Errorf("Eval = %v, want %q", got, "5 + 6")
	}
}

func TestEvalBeforeLoad(t *testing.T) {
	m := New()
	if _, err := m.Eval("1"); err == nil {
		t.Error("Eval before load should error")
	}
}

func TestReload(t *testing.T) {
	m := New()
	if err := m.Reload(); err == nil {
		t.Error("Reload with no script should error")
	}
	_ = m.Init(&Config{ScriptPath: "s.pl"})
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Reload")
	}
}

func TestReloadAfterLoadScript(t *testing.T) {
	m := New()
	_ = m.LoadScript("only.pl")
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload after LoadScript: %v", err)
	}
}

func TestClose(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.pl"})
	m.Close()
	if m.IsLoaded() {
		t.Error("expected not loaded after Close")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultAppPerl()
	d2 := DefaultAppPerl()
	if d1 != d2 {
		t.Error("DefaultAppPerl should return same instance")
	}
	// Init resets to a new instance.
	Init()
	d3 := DefaultAppPerl()
	if d3 == d1 {
		t.Error("Init should reset default module to a new instance")
	}
}

func TestMockInterpreter(t *testing.T) {
	mock := newMockPerlInterp()
	if err := mock.LoadScript("a.pl"); err != nil {
		t.Fatalf("mock LoadScript: %v", err)
	}
	if !mock.HasScript("a.pl") {
		t.Error("mock should have script a.pl")
	}
	mock.RegisterFunc("greet", func(args ...interface{}) interface{} {
		return 42
	})
	res, err := mock.CallFunction("greet")
	if err != nil {
		t.Fatalf("mock CallFunction: %v", err)
	}
	if res.(int) != 42 {
		t.Errorf("mock result = %v, want 42", res)
	}
	if _, err := mock.CallFunction("nope"); err == nil {
		t.Error("mock unknown function should error")
	}
	mock.Close()
	if mock.HasScript("a.pl") {
		t.Error("mock should not have script after Close")
	}
}

func TestNewWithInterpreter(t *testing.T) {
	mock := newMockPerlInterp()
	mock.RegisterFunc("ping", func(args ...interface{}) interface{} { return 1 })
	m := NewWithInterpreter(mock)
	_ = m.Init(&Config{ScriptPath: "s.pl"})
	_ = m.RegisterFunction("ping", "s.pl")
	ret, err := m.CallFunction("ping", &parser.SIPMsg{})
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if ret != 1 {
		t.Errorf("ret = %d, want 1", ret)
	}
}

func TestNewWithNilInterpreter(t *testing.T) {
	m := NewWithInterpreter(nil)
	if m.interp == nil {
		t.Error("nil interpreter should fall back to mock")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.pl"})
	_ = m.RegisterFunction("n", "s.pl")
	mock := m.interp.(*mockPerlInterp)
	mock.RegisterFunc("n", func(args ...interface{}) interface{} { return len(args) })
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.CallFunction("n", &parser.SIPMsg{}, "a", "b")
			_ = m.IsLoaded()
			_ = m.ScriptCount()
		}()
	}
	wg.Wait()
}

func TestPackageLevelHelpers(t *testing.T) {
	Init()
	if err := LoadScript("pkg.pl"); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if err := RegisterFunction("f", "pkg.pl"); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}
	d := DefaultAppPerl()
	mock := d.interp.(*mockPerlInterp)
	mock.RegisterFunc("f", func(args ...interface{}) interface{} { return 0 })
	ret, err := CallFunction("f", &parser.SIPMsg{})
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if ret != 0 {
		t.Errorf("ret = %d, want 0", ret)
	}
	if _, err := Eval("x"); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
}

// parseInt parses a decimal string; returns -1 on failure.
func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}
