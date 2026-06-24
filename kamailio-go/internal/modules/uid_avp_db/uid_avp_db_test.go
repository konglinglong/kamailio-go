// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the UIDAVPDB module.
 */

package uid_avp_db

import (
	"sync"
	"testing"
)

func TestStoreLoadDelete(t *testing.T) {
	m := New()
	m.StoreAVP("alice", "color", "blue")
	m.StoreAVP("alice", "lang", "en")
	avps := m.LoadAVPs("alice")
	if len(avps) != 2 {
		t.Fatalf("LoadAVPs(alice) = %v, want 2 entries", avps)
	}
	if avps["color"] != "blue" {
		t.Errorf("LoadAVPs(alice)[color] = %q, want blue", avps["color"])
	}
	if avps["lang"] != "en" {
		t.Errorf("LoadAVPs(alice)[lang] = %q, want en", avps["lang"])
	}
	// Mutating the returned map must not affect the store.
	avps["color"] = "red"
	if m.LoadAVPs("alice")["color"] != "blue" {
		t.Errorf("store was mutated by caller")
	}
	// Delete one AVP.
	if !m.DeleteAVP("alice", "color") {
		t.Errorf("DeleteAVP(alice, color) = false, want true")
	}
	rest := m.LoadAVPs("alice")
	if len(rest) != 1 {
		t.Fatalf("LoadAVPs(alice) after delete = %v, want 1 entry", rest)
	}
	if _, ok := rest["color"]; ok {
		t.Errorf("color should have been deleted")
	}
	// Delete again -> false.
	if m.DeleteAVP("alice", "color") {
		t.Errorf("DeleteAVP(alice, color) twice = true, want false")
	}
}

func TestLoadEmptyAndOverwrite(t *testing.T) {
	m := New()
	// Unknown user -> nil.
	if got := m.LoadAVPs("nobody"); got != nil {
		t.Errorf("LoadAVPs(unknown) = %v, want nil", got)
	}
	// Empty user -> nil.
	if got := m.LoadAVPs(""); got != nil {
		t.Errorf("LoadAVPs(empty) = %v, want nil", got)
	}
	// Overwrite existing value.
	m.StoreAVP("bob", "k", "v1")
	m.StoreAVP("bob", "k", "v2")
	got := m.LoadAVPs("bob")
	if len(got) != 1 {
		t.Fatalf("LoadAVPs(bob) = %v, want 1 entry", got)
	}
	if got["k"] != "v2" {
		t.Errorf("LoadAVPs(bob)[k] = %q, want v2 (overwrite)", got["k"])
	}
	// Deleting the last AVP removes the user entry.
	m.DeleteAVP("bob", "k")
	if got := m.LoadAVPs("bob"); got != nil {
		t.Errorf("LoadAVPs(bob) after last delete = %v, want nil", got)
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			user := "u" + itoa(i)
			m.StoreAVP(user, "n", "v")
			m.LoadAVPs(user)
			m.DeleteAVP(user, "n")
		}(i)
	}
	wg.Wait()
	// Every user's last AVP was deleted, so all entries are gone.
	for i := 0; i < goroutines; i++ {
		if got := m.LoadAVPs("u" + itoa(i)); got != nil {
			t.Errorf("LoadAVPs(u%d) = %v, want nil after delete", i, got)
		}
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultUIDAVPDB()
	if d == nil {
		t.Fatal("DefaultUIDAVPDB() = nil")
	}
	if d != DefaultUIDAVPDB() {
		t.Fatal("DefaultUIDAVPDB() returned different instances")
	}
	d.StoreAVP("default", "n", "v")
	if d.LoadAVPs("default")["n"] != "v" {
		t.Fatal("default store/load failed")
	}
	Init()
	if DefaultUIDAVPDB().LoadAVPs("default") != nil {
		t.Errorf("LoadAVPs after re-Init should be nil")
	}
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
