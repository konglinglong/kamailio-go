// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - ldap module tests.
 *
 * These tests do NOT require a running LDAP server. They exercise the
 * connector against an in-memory mock LDAPConn so search / authenticate
 * flows are verified, including concurrent access.
 */

package ldap

import (
	"sync"
	"testing"
)

func sampleEntries() []*LDAPResult {
	return []*LDAPResult{
		{
			DN: "uid=alice,ou=users,dc=example,dc=org",
			Attributes: map[string][]string{
				"uid":          {"alice"},
				"cn":           {"Alice Example"},
				"mail":         {"alice@example.com"},
				"userPassword": {"secret"},
			},
		},
		{
			DN: "uid=bob,ou=users,dc=example,dc=org",
			Attributes: map[string][]string{
				"uid":          {"bob"},
				"cn":           {"Bob Example"},
				"mail":         {"bob@example.com"},
				"userPassword": {"hunter2"},
			},
		},
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server != "127.0.0.1" {
		t.Errorf("server = %q, want 127.0.0.1", cfg.Server)
	}
	if cfg.Port != 389 {
		t.Errorf("port = %d, want 389", cfg.Port)
	}
	if cfg.BaseDN != "dc=example,dc=org" {
		t.Errorf("base DN = %q", cfg.BaseDN)
	}
	if cfg.SearchFilter != "(uid=%s)" {
		t.Errorf("filter = %q", cfg.SearchFilter)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Errorf("default config: %v", err)
	}
	if err := (&Config{}).Validate(); err == nil {
		t.Error("empty config expected error")
	}
	if err := (&Config{Server: "h", Port: 0, BaseDN: "dc=x"}).Validate(); err == nil {
		t.Error("port 0 expected error")
	}
}

func TestSetters(t *testing.T) {
	m := New()
	m.SetServer("ldap.example.com", 636)
	m.SetCredentials("cn=admin,dc=example,dc=org", "pw")
	m.SetBaseDN("dc=test,dc=org")
	cfg := m.Config()
	if cfg.Server != "ldap.example.com" || cfg.Port != 636 {
		t.Errorf("server cfg = %v", cfg)
	}
	if cfg.BindDN != "cn=admin,dc=example,dc=org" || cfg.BindPassword != "pw" {
		t.Errorf("creds cfg = %v", cfg)
	}
	if cfg.BaseDN != "dc=test,dc=org" {
		t.Errorf("base DN = %q", cfg.BaseDN)
	}
}

func TestConnectAndBind(t *testing.T) {
	m := New()
	conn, err := m.Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if conn == nil {
		t.Fatal("nil conn")
	}
	// Default config has a BindDN so the connection should already be bound.
	results, err := conn.Search("dc=example,dc=org", "(uid=alice)", ScopeSubtree)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("empty mock returned %d results", len(results))
	}
}

func TestSearchUser(t *testing.T) {
	m := New()
	m.SetMockData(sampleEntries())
	res, err := m.SearchUser("alice")
	if err != nil {
		t.Fatalf("SearchUser: %v", err)
	}
	if res == nil {
		t.Fatal("SearchUser returned nil")
	}
	if res.DN != "uid=alice,ou=users,dc=example,dc=org" {
		t.Errorf("DN = %q", res.DN)
	}
	if res.GetAttr("cn") != "Alice Example" {
		t.Errorf("cn = %q", res.GetAttr("cn"))
	}
	if res.GetAttr("mail") != "alice@example.com" {
		t.Errorf("mail = %q", res.GetAttr("mail"))
	}
}

func TestSearchUserNotFound(t *testing.T) {
	m := New()
	m.SetMockData(sampleEntries())
	res, err := m.SearchUser("nobody")
	if err != nil {
		t.Fatalf("SearchUser: %v", err)
	}
	if res != nil {
		t.Errorf("expected nil, got %v", res)
	}
}

func TestSearchUserEmpty(t *testing.T) {
	m := New()
	if _, err := m.SearchUser(""); err == nil {
		t.Error("SearchUser('') expected error")
	}
}

func TestSearchFilter(t *testing.T) {
	m := New()
	m.SetMockData(sampleEntries())
	results, err := m.SearchFilter("(uid=bob)")
	if err != nil {
		t.Fatalf("SearchFilter: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].GetAttr("cn") != "Bob Example" {
		t.Errorf("cn = %q", results[0].GetAttr("cn"))
	}
}

func TestSearchFilterAll(t *testing.T) {
	m := New()
	m.SetMockData(sampleEntries())
	results, err := m.SearchFilter("")
	if err != nil {
		t.Fatalf("SearchFilter: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("results = %d, want 2", len(results))
	}
}

func TestAuthenticateSuccess(t *testing.T) {
	m := New()
	m.SetMockData(sampleEntries())
	ok, err := m.Authenticate("alice", "secret")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !ok {
		t.Error("Authenticate returned false for valid creds")
	}
}

func TestAuthenticateWrongPassword(t *testing.T) {
	m := New()
	m.SetMockData(sampleEntries())
	// The mock Bind accepts any non-empty username, so to test a wrong
	// password we install a factory that rejects a specific password.
	m.SetConnFactory(func(cfg Config) (LDAPConn, error) {
		return &strictMockConn{cfg: cfg, entries: sampleEntries(), reject: "wrong"}, nil
	})
	ok, err := m.Authenticate("alice", "wrong")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if ok {
		t.Error("Authenticate returned true for wrong password")
	}
}

func TestAuthenticateUnknownUser(t *testing.T) {
	m := New()
	m.SetMockData(sampleEntries())
	ok, err := m.Authenticate("ghost", "pw")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if ok {
		t.Error("Authenticate returned true for unknown user")
	}
}

func TestInitResetsPool(t *testing.T) {
	m := New()
	m.SetMockData(sampleEntries())
	if _, err := m.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	cfg := *DefaultConfig()
	cfg.Server = "127.0.0.1"
	if err := m.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.SearchCount() != 0 {
		t.Errorf("SearchCount = %d after Init, want 0", m.SearchCount())
	}
}

func TestDefaultAndInit(t *testing.T) {
	cfg := *DefaultConfig()
	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	d := DefaultLDAP()
	if d == nil {
		t.Fatal("DefaultLDAP nil")
	}
	d.SetMockData(sampleEntries())
	if _, err := SearchUser("alice"); err != nil {
		t.Fatalf("SearchUser: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.SetMockData(sampleEntries())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			user := "alice"
			if i%2 == 0 {
				user = "bob"
			}
			if _, err := m.SearchUser(user); err != nil {
				t.Errorf("SearchUser %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if got := m.SearchCount(); got != 50 {
		t.Errorf("SearchCount = %d, want 50", got)
	}
}

// strictMockConn rejects binds whose password matches the reject field.
type strictMockConn struct {
	cfg     Config
	entries []*LDAPResult
	reject  string
	bound   bool
	closed  bool
	mu      sync.Mutex
}

func (c *strictMockConn) Bind(username, password string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errClosed
	}
	if password == c.reject {
		return errBadCreds
	}
	c.bound = true
	return nil
}

func (c *strictMockConn) Search(baseDN, filter string, scope int) ([]*LDAPResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errClosed
	}
	if !c.bound {
		return nil, errNotBound
	}
	attr, val := parseSimpleFilter(filter)
	var out []*LDAPResult
	for _, e := range c.entries {
		if baseDN != "" && !hasSuffixDN(e.DN, baseDN) {
			continue
		}
		if attr != "" && e.GetAttr(attr) != val {
			continue
		}
		out = append(out, cloneResult(e))
	}
	return out, nil
}

func (c *strictMockConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

var (
	errClosed   = errString("ldap: connection closed")
	errNotBound = errString("ldap: not bound")
	errBadCreds = errString("ldap: invalid credentials")
)

type errString string

func (e errString) Error() string { return string(e) }

func hasSuffixDN(dn, suffix string) bool {
	return len(dn) >= len(suffix) && dn[len(dn)-len(suffix):] == suffix
}
