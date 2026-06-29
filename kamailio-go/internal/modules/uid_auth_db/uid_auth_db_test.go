// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the UIDAuthDB module.
 */

package uid_auth_db

import (
	"sync"
	"testing"
)

func TestAuthenticate(t *testing.T) {
	m := New()
	m.AddUser("alice", "example.com", "ha1-alice")
	m.AddUser("bob", "example.com", "ha1-bob")

	ok, err := m.Authenticate("alice", "example.com")
	if err != nil {
		t.Fatalf("Authenticate(alice) error = %v", err)
	}
	if !ok {
		t.Errorf("Authenticate(alice) = false, want true")
	}
	// Unknown user -> false, no error.
	ok, err = m.Authenticate("nobody", "example.com")
	if err != nil {
		t.Errorf("Authenticate(unknown) error = %v", err)
	}
	if ok {
		t.Errorf("Authenticate(unknown) = true, want false")
	}
	// Different domain -> false.
	ok, _ = m.Authenticate("alice", "other.com")
	if ok {
		t.Errorf("Authenticate(alice, other.com) = true, want false")
	}
	// Empty user -> error.
	if _, err := m.Authenticate("", "example.com"); err == nil {
		t.Errorf("Authenticate(empty user) should error")
	}
}

func TestGetCredentialsAndCount(t *testing.T) {
	m := New()
	if m.CountUsers() != 0 {
		t.Fatalf("CountUsers() = %d, want 0", m.CountUsers())
	}
	m.AddUser("alice", "example.com", "secret-1")
	m.AddUser("bob", "example.com", "secret-2")
	if m.CountUsers() != 2 {
		t.Errorf("CountUsers() = %d, want 2", m.CountUsers())
	}
	pw, err := m.GetCredentials("alice", "example.com")
	if err != nil {
		t.Fatalf("GetCredentials(alice) error = %v", err)
	}
	if pw != "secret-1" {
		t.Errorf("GetCredentials(alice) = %q, want secret-1", pw)
	}
	if _, err := m.GetCredentials("nobody", "example.com"); err == nil {
		t.Errorf("GetCredentials(unknown) should error")
	}
	// Overwrite existing user.
	m.AddUser("alice", "example.com", "secret-3")
	if pw, _ := m.GetCredentials("alice", "example.com"); pw != "secret-3" {
		t.Errorf("GetCredentials(alice) after overwrite = %q, want secret-3", pw)
	}
	if m.CountUsers() != 2 {
		t.Errorf("CountUsers() after overwrite = %d, want 2", m.CountUsers())
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
			m.AddUser(user, "example.com", "pw")
			_, _ = m.Authenticate(user, "example.com")
			_, _ = m.GetCredentials(user, "example.com")
			m.CountUsers()
		}(i)
	}
	wg.Wait()
	if m.CountUsers() != goroutines {
		t.Errorf("CountUsers() after concurrent = %d, want %d", m.CountUsers(), goroutines)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultUIDAuthDB()
	if d == nil {
		t.Fatal("DefaultUIDAuthDB() = nil")
	}
	if d != DefaultUIDAuthDB() {
		t.Fatal("DefaultUIDAuthDB() returned different instances")
	}
	d.AddUser("default", "example.com", "pw")
	if d.CountUsers() != 1 {
		t.Fatalf("CountUsers() = %d, want 1", d.CountUsers())
	}
	Init()
	if DefaultUIDAuthDB().CountUsers() != 0 {
		t.Errorf("CountUsers() after re-Init = %d, want 0", DefaultUIDAuthDB().CountUsers())
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
