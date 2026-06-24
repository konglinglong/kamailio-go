// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - auth_db module tests.
 *
 * These tests use the in-memory database backend (db.MemoryConn) so no real
 * MySQL/PostgreSQL server is required. Digest responses are computed with
 * the auth package helpers to guarantee correctness.
 */

package auth_db

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/auth"
	"github.com/kamailio/kamailio-go/internal/core/db"
	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// subscriberKeys is the column shape used by the in-memory subscriber table.
var subscriberKeys = []db.DBKey{
	{Name: "username", Type: db.DBValString},
	{Name: "domain", Type: db.DBValString},
	{Name: "ha1", Type: db.DBValString},
}

// newTestModule creates an AuthDBModule backed by a fresh in-memory DB
// populated with the given subscribers.
func newTestModule(t *testing.T, subs ...[3]string) *AuthDBModule {
	t.Helper()
	conn, err := db.Open("memory", "")
	if err != nil {
		t.Fatalf("Open memory: %v", err)
	}
	for _, s := range subs {
		if err := conn.Insert("subscriber", subscriberKeys, []db.DBValue{
			db.NewStringValue(s[0]),
			db.NewStringValue(s[1]),
			db.NewStringValue(s[2]),
		}); err != nil {
			t.Fatalf("Insert subscriber: %v", err)
		}
	}
	return NewAuthDBModule(conn, DefaultAuthDBConfig())
}

// computeDigest builds a valid Digest response for the given parameters.
func computeDigest(username, realm, password, method, uri, nonce, nc, cnonce, qop string) string {
	ha1 := auth.CalcHA1(parser.AlgMD5, username, realm, password, "", "")
	ha2 := auth.CalcHA2(parser.AlgMD5, method, uri, "", parser.QopNone)
	return auth.CalcResponse(parser.AlgMD5, ha1, nonce, nc, cnonce, qop, ha2)
}

// buildAuthMsg constructs a SIP REGISTER message with a Digest Authorization
// header carrying the supplied digest parameters.
func buildAuthMsg(response string) []byte {
	return []byte(strings.Join([]string{
		"REGISTER sip:example.com SIP/2.0",
		"Via: SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bKtest",
		"From: <sip:alice@example.com>;tag=test",
		"To: <sip:alice@example.com>",
		"Call-ID: test-call-id@test",
		"CSeq: 1 REGISTER",
		fmt.Sprintf(`Authorization: Digest username="alice",realm="example.com",nonce="test-nonce-123",uri="sip:example.com",response="%s",qop=auth,nc=00000001,cnonce="test-cnonce",algorithm=MD5`, response),
		"Content-Length: 0",
		"",
		"",
	}, "\r\n"))
}

// mustParseMsg parses a SIP message, failing the test on error.
func mustParseMsg(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	return msg
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestAuthenticate verifies that a valid Digest response authenticates
// successfully and an invalid one fails.
func TestAuthenticate(t *testing.T) {
	const (
		username = "alice"
		realm    = "example.com"
		password = "secret"
	)
	ha1 := auth.CalcHA1(parser.AlgMD5, username, realm, password, "", "")

	mod := newTestModule(t, [3]string{username, realm, ha1})

	// Valid response.
	response := computeDigest(username, realm, password, "REGISTER", "sip:example.com", "test-nonce-123", "00000001", "test-cnonce", "auth")
	msg := mustParseMsg(t, buildAuthMsg(response))

	ok, err := mod.Authenticate(msg, username, realm)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !ok {
		t.Error("expected authentication to succeed")
	}

	// Invalid response (wrong password).
	badResponse := computeDigest(username, realm, "wrong", "REGISTER", "sip:example.com", "test-nonce-123", "00000001", "test-cnonce", "auth")
	badMsg := mustParseMsg(t, buildAuthMsg(badResponse))

	ok, err = mod.Authenticate(badMsg, username, realm)
	if err != nil {
		t.Fatalf("Authenticate (bad): %v", err)
	}
	if ok {
		t.Error("expected authentication to fail with wrong password")
	}
}

// TestAuthenticate_NoAuthHeader verifies that a message without an
// Authorization header returns an error.
func TestAuthenticate_NoAuthHeader(t *testing.T) {
	ha1 := auth.CalcHA1(parser.AlgMD5, "alice", "example.com", "secret", "", "")
	mod := newTestModule(t, [3]string{"alice", "example.com", ha1})

	msg := mustParseMsg(t, []byte(strings.Join([]string{
		"REGISTER sip:example.com SIP/2.0",
		"Via: SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bKtest",
		"From: <sip:alice@example.com>;tag=test",
		"To: <sip:alice@example.com>",
		"Call-ID: test@test",
		"CSeq: 1 REGISTER",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")))

	_, err := mod.Authenticate(msg, "alice", "example.com")
	if err == nil {
		t.Fatal("expected error for missing Authorization header")
	}
}

// TestAuthenticate_UserNotFound verifies that authenticating an unknown user
// returns an error.
func TestAuthenticate_UserNotFound(t *testing.T) {
	mod := newTestModule(t) // empty DB

	response := computeDigest("alice", "example.com", "secret", "REGISTER", "sip:example.com", "test-nonce-123", "00000001", "test-cnonce", "auth")
	msg := mustParseMsg(t, buildAuthMsg(response))

	ok, err := mod.Authenticate(msg, "alice", "example.com")
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
	if ok {
		t.Error("expected false for unknown user")
	}
}

// TestGetHA1 verifies HA1 retrieval for both stored-HA1 and
// calculate-HA1-from-password modes.
func TestGetHA1(t *testing.T) {
	// Stored HA1 mode (CalculateHA1 = false).
	ha1 := auth.CalcHA1(parser.AlgMD5, "alice", "example.com", "secret", "", "")
	mod := newTestModule(t, [3]string{"alice", "example.com", ha1})

	got, err := mod.GetHA1("alice", "example.com")
	if err != nil {
		t.Fatalf("GetHA1: %v", err)
	}
	if got != ha1 {
		t.Errorf("GetHA1 = %q, want %q", got, ha1)
	}

	// CalculateHA1 mode: PassColumn stores plaintext password.
	conn, _ := db.Open("memory", "")
	_ = conn.Insert("subscriber", subscriberKeys, []db.DBValue{
		db.NewStringValue("bob"),
		db.NewStringValue("example.com"),
		db.NewStringValue("bobpass"),
	})
	mod2 := NewAuthDBModule(conn, &AuthDBConfig{
		UserColumn:   "username",
		DomainColumn: "domain",
		PassColumn:   "ha1",
		CalculateHA1: true,
	})

	wantHA1 := auth.CalcHA1(parser.AlgMD5, "bob", "example.com", "bobpass", "", "")
	got, err = mod2.GetHA1("bob", "example.com")
	if err != nil {
		t.Fatalf("GetHA1 (calc): %v", err)
	}
	if got != wantHA1 {
		t.Errorf("GetHA1 (calc) = %q, want %q", got, wantHA1)
	}

	// Unknown user.
	_, err = mod.GetHA1("nobody", "example.com")
	if err == nil {
		t.Error("expected error for unknown user")
	}
}

// TestIsUserInDB verifies user existence checks.
func TestIsUserInDB(t *testing.T) {
	ha1 := auth.CalcHA1(parser.AlgMD5, "alice", "example.com", "secret", "", "")
	mod := newTestModule(t,
		[3]string{"alice", "example.com", ha1},
		[3]string{"bob", "example.com", "bobha1"},
	)

	if !mod.IsUserInDB("alice", "example.com") {
		t.Error("expected alice to be in DB")
	}
	if !mod.IsUserInDB("bob", "example.com") {
		t.Error("expected bob to be in DB")
	}
	if mod.IsUserInDB("nobody", "example.com") {
		t.Error("expected nobody to NOT be in DB")
	}
	if mod.IsUserInDB("alice", "other.com") {
		t.Error("expected alice@other.com to NOT be in DB")
	}
}

// TestStats verifies that authentication attempts update the stats counters.
func TestStats(t *testing.T) {
	const (
		username = "alice"
		realm    = "example.com"
		password = "secret"
	)
	ha1 := auth.CalcHA1(parser.AlgMD5, username, realm, password, "", "")
	mod := newTestModule(t, [3]string{username, realm, ha1})

	// Successful authentication.
	response := computeDigest(username, realm, password, "REGISTER", "sip:example.com", "test-nonce-123", "00000001", "test-cnonce", "auth")
	msg := mustParseMsg(t, buildAuthMsg(response))

	_, _ = mod.Authenticate(msg, username, realm)

	// Failed authentication (wrong response).
	badMsg := mustParseMsg(t, buildAuthMsg("deadbeef"))
	_, _ = mod.Authenticate(badMsg, username, realm)

	stats := mod.Stats()
	if got := stats.AuthAttempts.Load(); got != 2 {
		t.Errorf("AuthAttempts = %d, want 2", got)
	}
	if got := stats.AuthSuccess.Load(); got != 1 {
		t.Errorf("AuthSuccess = %d, want 1", got)
	}
	if got := stats.AuthFailure.Load(); got != 1 {
		t.Errorf("AuthFailure = %d, want 1", got)
	}
}

// TestConfig verifies config defaults, validation and SetConfig.
func TestConfig(t *testing.T) {
	cfg := DefaultAuthDBConfig()
	if cfg.UserColumn != "username" {
		t.Errorf("UserColumn = %q, want username", cfg.UserColumn)
	}
	if cfg.DomainColumn != "domain" {
		t.Errorf("DomainColumn = %q, want domain", cfg.DomainColumn)
	}
	if cfg.PassColumn != "ha1" {
		t.Errorf("PassColumn = %q, want ha1", cfg.PassColumn)
	}
	if cfg.CalculateHA1 {
		t.Error("CalculateHA1 = true, want false")
	}
	if cfg.DBDriver != "memory" {
		t.Errorf("DBDriver = %q, want memory", cfg.DBDriver)
	}

	// Validate should pass for defaults.
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config validate: %v", err)
	}

	// SetConfig updates the module.
	mod := NewAuthDBModule(nil, nil)
	custom := &AuthDBConfig{
		UserColumn:   "user",
		DomainColumn: "realm",
		PassColumn:   "pass",
		CalculateHA1: true,
	}
	mod.SetConfig(custom)

	mod.mu.RLock()
	got := mod.config
	mod.mu.RUnlock()
	if got.UserColumn != "user" {
		t.Errorf("after SetConfig: UserColumn = %q, want user", got.UserColumn)
	}
	if !got.CalculateHA1 {
		t.Error("after SetConfig: CalculateHA1 = false, want true")
	}

	// Validate rejects empty columns.
	if err := (&AuthDBConfig{}).Validate(); err == nil {
		t.Error("expected error for empty config")
	}
}

// TestCountUsers verifies user counting by domain.
func TestCountUsers(t *testing.T) {
	ha1 := auth.CalcHA1(parser.AlgMD5, "alice", "example.com", "secret", "", "")
	mod := newTestModule(t,
		[3]string{"alice", "example.com", ha1},
		[3]string{"bob", "example.com", "bobha1"},
		[3]string{"carol", "other.com", "carolha1"},
	)

	count, err := mod.CountUsers("example.com")
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if count != 2 {
		t.Errorf("CountUsers(example.com) = %d, want 2", count)
	}

	count, err = mod.CountUsers("other.com")
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if count != 1 {
		t.Errorf("CountUsers(other.com) = %d, want 1", count)
	}

	count, err = mod.CountUsers("empty.com")
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if count != 0 {
		t.Errorf("CountUsers(empty.com) = %d, want 0", count)
	}
}

// TestReload verifies that Reload clears the credential cache.
func TestReload(t *testing.T) {
	ha1 := auth.CalcHA1(parser.AlgMD5, "alice", "example.com", "secret", "", "")
	mod := newTestModule(t, [3]string{"alice", "example.com", ha1})

	// Populate the cache.
	_, _ = mod.GetHA1("alice", "example.com")

	mod.mu.RLock()
	cached := len(mod.cache)
	mod.mu.RUnlock()
	if cached != 1 {
		t.Errorf("cache size before reload = %d, want 1", cached)
	}

	mod.Reload()

	mod.mu.RLock()
	cached = len(mod.cache)
	mod.mu.RUnlock()
	if cached != 0 {
		t.Errorf("cache size after reload = %d, want 0", cached)
	}
}

// TestDefaultAuthDB verifies the singleton accessor.
func TestDefaultAuthDB(t *testing.T) {
	m1 := DefaultAuthDB()
	m2 := DefaultAuthDB()
	if m1 != m2 {
		t.Error("DefaultAuthDB should return the same instance")
	}
	if m1.Stats() == nil {
		t.Error("Stats should not be nil")
	}

	// Init should not panic.
	Init()
}
