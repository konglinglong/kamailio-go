// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the app_ruby module - Ruby script bindings (mock).
 */
package app_ruby

import (
	"sync"
	"testing"
)

func TestInitAndLoad(t *testing.T) {
	m := New()
	if m.IsLoaded() {
		t.Error("fresh module should not be loaded")
	}
	if err := m.Init(&RubyConfig{ScriptPath: "route.rb"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Init with ScriptPath")
	}
	if err := m.LoadScript("other.rb"); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after LoadScript")
	}
}

func TestRegisterAndCall(t *testing.T) {
	m := New()
	_ = m.Init(&RubyConfig{ScriptPath: "route.rb"})
	m.RegisterFunction("upper", func(args ...interface{}) (interface{}, error) {
		if len(args) == 0 {
			return "", nil
		}
		s, _ := args[0].(string)
		out := make([]rune, 0, len(s))
		for _, r := range s {
			if r >= 'a' && r <= 'z' {
				r -= 32
			}
			out = append(out, r)
		}
		return string(out), nil
	})
	got, err := m.CallFunction("upper", "hello")
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if got != "HELLO" {
		t.Errorf("CallFunction = %v, want HELLO", got)
	}
}

func TestCallErrors(t *testing.T) {
	m := New()
	if _, err := m.CallFunction("x"); err == nil {
		t.Error("CallFunction before load should error")
	}
	_ = m.Init(&RubyConfig{ScriptPath: "s.rb"})
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
	_ = m.Init(&RubyConfig{ScriptPath: "s.rb"})
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Reload")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultRuby()
	d2 := DefaultRuby()
	if d1 != d2 {
		t.Error("DefaultRuby should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	_ = m.Init(&RubyConfig{ScriptPath: "s.rb"})
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
