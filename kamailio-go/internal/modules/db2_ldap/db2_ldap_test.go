// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the db2_ldap module.
 */

package db2_ldap

import (
	"sync"
	"testing"
)

func TestInitAndIsConnected(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Errorf("IsConnected() = true before Init")
	}
	m.Init("ldap://example.com")
	if !m.IsConnected() {
		t.Errorf("IsConnected() = false after Init")
	}
}

func TestAddSearchDelete(t *testing.T) {
	m := New()
	m.Init("ldap://example.com")

	if err := m.Add("cn=alice,dc=example,dc=com", map[string]string{"uid": "alice", "phone": "1001"}); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if err := m.Add("cn=bob,dc=example,dc=com", map[string]string{"uid": "bob", "phone": "1002"}); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	// Duplicate DN.
	if err := m.Add("cn=alice,dc=example,dc=com", map[string]string{"uid": "alice"}); err == nil {
		t.Errorf("Add(duplicate) should error")
	}

	// Search all under base.
	res, err := m.Search("cn=", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("Search returned %d entries, want 2", len(res))
	}

	// Filter by uid.
	res, _ = m.Search("cn=", "uid=alice")
	if len(res) != 1 {
		t.Fatalf("Search(uid=alice) returned %d, want 1", len(res))
	}
	if res[0]["phone"] != "1001" {
		t.Errorf("Search(uid=alice)[phone] = %q, want %q", res[0]["phone"], "1001")
	}

	// Delete.
	if err := m.Delete("cn=alice,dc=example,dc=com"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if err := m.Delete("cn=alice,dc=example,dc=com"); err == nil {
		t.Errorf("Delete(missing) should error")
	}
	if got := m.Count(); got != 1 {
		t.Errorf("Count() after delete = %d, want 1", got)
	}
}

func TestAddEmptyDN(t *testing.T) {
	m := New()
	if err := m.Add("", map[string]string{"a": "b"}); err == nil {
		t.Errorf("Add(\"\") should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init("ldap://pkg")
	d := DefaultDB2LDAP()
	if d == nil {
		t.Fatalf("DefaultDB2LDAP() returned nil")
	}
	if !IsConnected() {
		t.Errorf("package IsConnected() = false")
	}
	if err := Add("cn=p,dc=x", map[string]string{"uid": "p"}); err != nil {
		t.Fatalf("package Add error: %v", err)
	}
	res, _ := Search("cn=", "uid=p")
	if len(res) != 1 {
		t.Errorf("package Search returned %d, want 1", len(res))
	}
}

func TestConcurrent(t *testing.T) {
	Init("ldap://c")
	shared := DefaultDB2LDAP()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			dn := "cn=" + itoa(i) + ",dc=x"
			shared.Add(dn, map[string]string{"uid": itoa(i)})
			shared.Search("cn=", "uid="+itoa(i))
			shared.Delete(dn)
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != 0 {
		t.Errorf("Count() after concurrent = %d, want 0", got)
	}
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
