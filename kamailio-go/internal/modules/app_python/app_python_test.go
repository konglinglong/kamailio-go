// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the app_python module - Python2 script bindings (mock).
 */
package app_python

import (
	"sync"
	"testing"
)

func TestInitAndLoad(t *testing.T) {
	m := New()
	if m.IsLoaded() {
		t.Error("fresh module should not be loaded")
	}
	if err := m.Init(&Config{ScriptPath: "handler.py"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Init with ScriptPath")
	}
	if m.ScriptCount() != 1 {
		t.Errorf("ScriptCount = %d, want 1", m.ScriptCount())
	}
	if err := m.LoadScript("other.py"); err != nil {
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
	_ = m.Init(&Config{ScriptPath: "handler.py"})
	if err := m.RegisterFunction("greet", "handler.py"); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}
	mock := m.interp.(*mockPythonInterp)
	mock.RegisterFunc("greet", func(args ...interface{}) interface{} {
		name := "world"
		if len(args) > 0 {
			if s, ok := args[0].(string); ok {
				name = s
			}
		}
		return "hello " + name
	})
	got, err := m.CallFunction("greet", "alice")
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if got != "hello alice" {
		t.Errorf("CallFunction = %v, want hello alice", got)
	}
}

func TestCallBeforeLoad(t *testing.T) {
	m := New()
	if _, err := m.CallFunction("x"); err == nil {
		t.Error("CallFunction before load should error")
	}
}

func TestCallUnknownFunction(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.py"})
	if _, err := m.CallFunction("missing"); err == nil {
		t.Error("CallFunction unknown should error")
	}
}

func TestRegisterFunctionErrors(t *testing.T) {
	m := New()
	if err := m.RegisterFunction("", "s.py"); err == nil {
		t.Error("RegisterFunction(empty name) should error")
	}
	if err := m.RegisterFunction("f", ""); err == nil {
		t.Error("RegisterFunction(empty path) should error")
	}
}

func TestEval(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.py"})
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
	_ = m.Init(&Config{ScriptPath: "s.py"})
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Reload")
	}
}

func TestReloadAfterLoadScript(t *testing.T) {
	m := New()
	_ = m.LoadScript("only.py")
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload after LoadScript: %v", err)
	}
}

func TestClose(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.py"})
	m.Close()
	if m.IsLoaded() {
		t.Error("expected not loaded after Close")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultAppPython()
	d2 := DefaultAppPython()
	if d1 != d2 {
		t.Error("DefaultAppPython should return same instance")
	}
	Init()
	d3 := DefaultAppPython()
	if d3 == d1 {
		t.Error("Init should reset default module to a new instance")
	}
}

func TestMockInterpreter(t *testing.T) {
	mock := newMockPythonInterp()
	if err := mock.LoadScript("a.py"); err != nil {
		t.Fatalf("mock LoadScript: %v", err)
	}
	if !mock.HasScript("a.py") {
		t.Error("mock should have script a.py")
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
	if mock.HasScript("a.py") {
		t.Error("mock should not have script after Close")
	}
}

func TestNewWithInterpreter(t *testing.T) {
	mock := newMockPythonInterp()
	mock.RegisterFunc("ping", func(args ...interface{}) interface{} { return "pong" })
	m := NewWithInterpreter(mock)
	_ = m.Init(&Config{ScriptPath: "s.py"})
	_ = m.RegisterFunction("ping", "s.py")
	got, err := m.CallFunction("ping")
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if got != "pong" {
		t.Errorf("got = %v, want pong", got)
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
	_ = m.Init(&Config{ScriptPath: "s.py"})
	_ = m.RegisterFunction("n", "s.py")
	mock := m.interp.(*mockPythonInterp)
	mock.RegisterFunc("n", func(args ...interface{}) interface{} { return len(args) })
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.CallFunction("n", 1, 2)
			_ = m.IsLoaded()
			_ = m.ScriptCount()
		}()
	}
	wg.Wait()
}

func TestPackageLevelHelpers(t *testing.T) {
	Init()
	if err := LoadScript("pkg.py"); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if err := RegisterFunction("f", "pkg.py"); err != nil {
		t.Fatalf("RegisterFunction: %v", err)
	}
	d := DefaultAppPython()
	mock := d.interp.(*mockPythonInterp)
	mock.RegisterFunc("f", func(args ...interface{}) interface{} { return 0 })
	got, err := CallFunction("f")
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if got != 0 {
		t.Errorf("got = %v, want 0", got)
	}
	if _, err := Eval("x"); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
}
