// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the app_python3 module - Python3 script bindings (mock).
 */
package app_python3

import (
	"sync"
	"testing"
)

func TestInitAndLoad(t *testing.T) {
	m := New()
	if m.IsLoaded() {
		t.Error("fresh module should not be loaded")
	}
	if err := m.Init(&PythonConfig{ScriptPath: "route.py"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Init with ScriptPath")
	}
	if err := m.LoadScript("other.py"); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after LoadScript")
	}
}

func TestRegisterAndCall(t *testing.T) {
	m := New()
	_ = m.Init(&PythonConfig{ScriptPath: "route.py"})
	m.RegisterFunction("greet", func(args ...interface{}) (interface{}, error) {
		name := "world"
		if len(args) > 0 {
			if s, ok := args[0].(string); ok {
				name = s
			}
		}
		return "hello " + name, nil
	})
	got, err := m.CallFunction("greet", "alice")
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if got != "hello alice" {
		t.Errorf("CallFunction = %v, want hello alice", got)
	}
}

func TestCallErrors(t *testing.T) {
	m := New()
	if _, err := m.CallFunction("x"); err == nil {
		t.Error("CallFunction before load should error")
	}
	_ = m.Init(&PythonConfig{ScriptPath: "s.py"})
	if _, err := m.CallFunction("missing"); err == nil {
		t.Error("CallFunction missing should error")
	}
}

func TestLoadScriptEmpty(t *testing.T) {
	m := New()
	if err := m.LoadScript(""); err == nil {
		t.Error("LoadScript(empty) should error")
	}
}

func TestReload(t *testing.T) {
	m := New()
	if err := m.Reload(); err == nil {
		t.Error("Reload with no script should error")
	}
	_ = m.Init(&PythonConfig{ScriptPath: "s.py"})
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Reload")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultPython3()
	d2 := DefaultPython3()
	if d1 != d2 {
		t.Error("DefaultPython3 should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	_ = m.Init(&PythonConfig{ScriptPath: "s.py"})
	m.RegisterFunction("n", func(args ...interface{}) (interface{}, error) {
		return len(args), nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.CallFunction("n", 1, 2)
			_ = m.IsLoaded()
		}()
	}
	wg.Wait()
}
