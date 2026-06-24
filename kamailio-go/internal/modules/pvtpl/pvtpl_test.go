// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - PVTpl module tests.
 */

package pvtpl

import (
	"sync"
	"testing"
)

func TestRegisterApplyList(t *testing.T) {
	m := New()
	m.Register("greeting", "Hello ${name} from ${place}!")
	out := m.Apply("greeting", map[string]string{"name": "Alice", "place": "Wonderland"})
	if out != "Hello Alice from Wonderland!" {
		t.Fatalf("Apply = %q", out)
	}
	list := m.List()
	if len(list) != 1 || list["greeting"] != "Hello ${name} from ${place}!" {
		t.Fatalf("List = %+v", list)
	}
}

func TestApplyUnknownAndUnknownVar(t *testing.T) {
	m := New()
	if got := m.Apply("missing", nil); got != "" {
		t.Fatalf("Apply(missing) = %q, want empty", got)
	}
	m.Register("t", "${known} and ${unknown}")
	out := m.Apply("t", map[string]string{"known": "yes"})
	if out != "yes and ${unknown}" {
		t.Fatalf("Apply with unknown var = %q, want 'yes and ${unknown}'", out)
	}
}

func TestRegisterReplaces(t *testing.T) {
	m := New()
	m.Register("t", "v1")
	m.Register("t", "v2")
	if m.List()["t"] != "v2" {
		t.Fatal("expected template to be replaced")
	}
}

func TestRegisterIgnoresEmptyName(t *testing.T) {
	m := New()
	m.Register("", "template")
	if len(m.List()) != 0 {
		t.Fatal("expected empty name to be ignored")
	}
}

func TestListCopyIsolation(t *testing.T) {
	m := New()
	m.Register("t", "v")
	list := m.List()
	list["t"] = "mutated"
	if m.List()["t"] != "v" {
		t.Fatal("expected List copy isolation")
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	Register("global", "${x}")
	if Apply("global", map[string]string{"x": "y"}) != "y" {
		t.Fatal("expected global Apply to substitute")
	}
	if len(List()) == 0 {
		t.Fatal("expected non-empty global list")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.Register("t", "${x}")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Register("t", "${x}")
			_ = m.Apply("t", map[string]string{"x": "v"})
			_ = m.List()
		}()
	}
	wg.Wait()
}
