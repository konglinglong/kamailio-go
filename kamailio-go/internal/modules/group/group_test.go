// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the group module.
 */

package group

import (
	"sort"
	"sync"
	"testing"
)

func TestAddRemoveIsUserInGroup(t *testing.T) {
	m := New()

	m.AddUser("admins", "alice")
	m.AddUser("admins", "bob")
	m.AddUser("users", "carol")

	if !m.IsUserInGroup("admins", "alice") {
		t.Errorf("IsUserInGroup(admins, alice) = false, want true")
	}
	if m.IsUserInGroup("admins", "carol") {
		t.Errorf("IsUserInGroup(admins, carol) = true, want false")
	}
	if m.IsUserInGroup("users", "alice") {
		t.Errorf("IsUserInGroup(users, alice) = true, want false")
	}

	if !m.RemoveUser("admins", "alice") {
		t.Errorf("RemoveUser(admins, alice) returned false")
	}
	if m.IsUserInGroup("admins", "alice") {
		t.Errorf("IsUserInGroup after remove = true, want false")
	}
	if m.RemoveUser("admins", "alice") {
		t.Errorf("RemoveUser twice should return false")
	}
	if m.RemoveUser("nonexistent", "x") {
		t.Errorf("RemoveUser(nonexistent) should return false")
	}
}

func TestGetGroups(t *testing.T) {
	m := New()
	m.AddUser("g1", "alice")
	m.AddUser("g2", "alice")
	m.AddUser("g3", "bob")

	groups := m.GetGroups("alice")
	sort.Strings(groups)
	if len(groups) != 2 || groups[0] != "g1" || groups[1] != "g2" {
		t.Errorf("GetGroups(alice) = %v, want [g1 g2]", groups)
	}
	if got := m.GetGroups("nobody"); len(got) != 0 {
		t.Errorf("GetGroups(nobody) = %v, want empty", got)
	}
}

func TestList(t *testing.T) {
	m := New()
	m.AddUser("g", "a")
	m.AddUser("g", "b")

	list := m.List()
	if len(list) != 1 {
		t.Fatalf("List() = %d groups, want 1", len(list))
	}
	if len(list["g"]) != 2 {
		t.Errorf("List()[g] = %v, want 2 members", list["g"])
	}
}

func TestAddEmpty(t *testing.T) {
	m := New()
	m.AddUser("", "u")
	m.AddUser("g", "")
	if got := m.GroupCount(); got != 0 {
		t.Errorf("GroupCount() = %d, want 0", got)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultGroup()
	if d == nil {
		t.Fatalf("DefaultGroup() returned nil")
	}
	AddUser("pkg", "u")
	if !IsUserInGroup("pkg", "u") {
		t.Errorf("package IsUserInGroup(pkg, u) = false")
	}
	if got := len(GetGroups("u")); got != 1 {
		t.Errorf("package GetGroups(u) = %d groups, want 1", got)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultGroup()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			g := "g" + itoa(i%5)
			u := "u" + itoa(i)
			shared.AddUser(g, u)
			shared.IsUserInGroup(g, u)
			shared.GetGroups(u)
			shared.List()
			shared.RemoveUser(g, u)
		}(i)
	}
	wg.Wait()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
