// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the app_ruby_proc module - Ruby procedural script bindings (mock).
 */
package app_ruby_proc

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
	if err := m.Init(&Config{ScriptPath: "route.rb"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Init with ScriptPath")
	}
	if m.ScriptCount() != 1 {
		t.Errorf("ScriptCount = %d, want 1", m.ScriptCount())
	}
	if err := m.LoadScript("other.rb"); err != nil {
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
	_ = m.Init(&Config{ScriptPath: "route.rb"})
	if err := m.RegisterFunction("upper", "route.rb"); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}
	mock := m.interp.(*mockRubyInterp)
	mock.RegisterFunc("upper", func(args ...interface{}) interface{} {
		// args[0] is msg, args[1:] are string args
		if len(args) < 2 {
			return -1
		}
		s, _ := args[1].(string)
		out := make([]rune, 0, len(s))
		for _, r := range s {
			if r >= 'a' && r <= 'z' {
				r -= 32
			}
			out = append(out, r)
		}
		_ = out
		return 1
	})
	ret, err := m.CallFunction("upper", &parser.SIPMsg{}, "hello")
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if ret != 1 {
		t.Errorf("ret = %d, want 1", ret)
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
	_ = m.Init(&Config{ScriptPath: "s.rb"})
	if _, err := m.CallFunction("missing", &parser.SIPMsg{}); err == nil {
		t.Error("CallFunction unknown should error")
	}
}

func TestRegisterFunctionErrors(t *testing.T) {
	m := New()
	if err := m.RegisterFunction("", "s.rb"); err == nil {
		t.Error("RegisterFunction(empty name) should error")
	}
	if err := m.RegisterFunction("f", ""); err == nil {
		t.Error("RegisterFunction(empty path) should error")
	}
}

func TestEval(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.rb"})
	got, err := m.Eval("1 + 2")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got != "1 + 2" {
		t.Errorf("Eval = %v, want %q", got, "1 + 2")
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
	_ = m.Init(&Config{ScriptPath: "s.rb"})
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Reload")
	}
}

func TestReloadAfterLoadScript(t *testing.T) {
	m := New()
	_ = m.LoadScript("only.rb")
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload after LoadScript: %v", err)
	}
}

func TestClose(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.rb"})
	m.Close()
	if m.IsLoaded() {
		t.Error("expected not loaded after Close")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultAppRubyProc()
	d2 := DefaultAppRubyProc()
	if d1 != d2 {
		t.Error("DefaultAppRubyProc should return same instance")
	}
	Init()
	d3 := DefaultAppRubyProc()
	if d3 == d1 {
		t.Error("Init should reset default module to a new instance")
	}
}

func TestMockInterpreter(t *testing.T) {
	mock := newMockRubyInterp()
	if err := mock.LoadScript("a.rb"); err != nil {
		t.Fatalf("mock LoadScript: %v", err)
	}
	if !mock.HasScript("a.rb") {
		t.Error("mock should have script a.rb")
	}
	mock.RegisterFunc("answer", func(args ...interface{}) interface{} { return 42 })
	res, err := mock.CallFunction("answer")
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
	if mock.HasScript("a.rb") {
		t.Error("mock should not have script after Close")
	}
}

func TestNewWithInterpreter(t *testing.T) {
	mock := newMockRubyInterp()
	mock.RegisterFunc("ping", func(args ...interface{}) interface{} { return 1 })
	m := NewWithInterpreter(mock)
	_ = m.Init(&Config{ScriptPath: "s.rb"})
	_ = m.RegisterFunction("ping", "s.rb")
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
	_ = m.Init(&Config{ScriptPath: "s.rb"})
	_ = m.RegisterFunction("n", "s.rb")
	mock := m.interp.(*mockRubyInterp)
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
	if err := LoadScript("pkg.rb"); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if err := RegisterFunction("f", "pkg.rb"); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}
	d := DefaultAppRubyProc()
	mock := d.interp.(*mockRubyInterp)
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
