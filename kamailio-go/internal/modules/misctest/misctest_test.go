// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Misctest module tests.
 */

package misctest

import (
	"sync"
	"testing"
)

func TestRegisterRunList(t *testing.T) {
	m := New()
	m.RegisterTest("pass", func() bool { return true })
	m.RegisterTest("fail", func() bool { return false })
	if got := m.Count(); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}
	names := m.ListTests()
	if len(names) != 2 || names[0] != "fail" || names[1] != "pass" {
		t.Fatalf("unexpected list: %v", names)
	}
	ok, err := m.RunTest("pass")
	if err != nil || !ok {
		t.Fatalf("RunTest(pass) = %v, %v, want true,nil", ok, err)
	}
	ok, err = m.RunTest("fail")
	if err != nil || ok {
		t.Fatalf("RunTest(fail) = %v, %v, want false,nil", ok, err)
	}
}

func TestRunTestUnknown(t *testing.T) {
	m := New()
	if _, err := m.RunTest("nope"); err == nil {
		t.Fatal("expected error for unknown test")
	}
}

func TestRegisterIgnoresInvalid(t *testing.T) {
	m := New()
	m.RegisterTest("", func() bool { return true })
	m.RegisterTest("nil", nil)
	if got := m.Count(); got != 0 {
		t.Fatalf("Count = %d, want 0 (invalid registrations ignored)", got)
	}
}

func TestRegisterReplaces(t *testing.T) {
	m := New()
	m.RegisterTest("t", func() bool { return false })
	m.RegisterTest("t", func() bool { return true })
	ok, _ := m.RunTest("t")
	if !ok {
		t.Fatal("expected replaced test to return true")
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	RegisterTest("global", func() bool { return true })
	ok, err := RunTest("global")
	if err != nil || !ok {
		t.Fatalf("global RunTest = %v, %v", ok, err)
	}
	if len(ListTests()) == 0 {
		t.Fatal("expected non-empty global test list")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.RegisterTest("t", func() bool { return true })
			_, _ = m.RunTest("t")
			_ = m.ListTests()
			_ = m.Count()
		}(i)
	}
	wg.Wait()
}
