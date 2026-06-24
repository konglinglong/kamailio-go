// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * UIDAuthDB module - user authentication against a credentials store.
 * Port of the kamailio uid_auth_db module (src/modules/uid_auth_db).
 *
 * The uid_auth_db module authenticates SIP users by looking up their
 * credentials (ha1 password digest) in a database. This Go counterpart
 * keeps an in-memory store of (user, domain) -> password records so the
 * module is fully testable without a live database.
 *
 * It is safe for concurrent use.
 */

package uid_auth_db

import (
	"errors"
	"fmt"
	"sync"
)

// userCred is one stored credential record.
type userCred struct {
	user     string
	domain   string
	password string
}

// UIDAuthDBModule authenticates users against an in-memory credential store.
// C: struct module uid_auth_db
type UIDAuthDBModule struct {
	mu    sync.RWMutex
	users map[string]*userCred
}

// New creates an empty UIDAuthDBModule.
func New() *UIDAuthDBModule {
	return &UIDAuthDBModule{users: make(map[string]*userCred)}
}

// userKey produces a stable key from a user and domain.
func userKey(user, domain string) string {
	return user + "@" + domain
}

// AddUser registers a credential record for (user, domain). It is the
// in-memory equivalent of populating the auth_db table.
func (m *UIDAuthDBModule) AddUser(user, domain, password string) {
	if m == nil || user == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.users == nil {
		m.users = make(map[string]*userCred)
	}
	m.users[userKey(user, domain)] = &userCred{
		user: user, domain: domain, password: password,
	}
}

// Authenticate reports whether a credential record exists for (user, domain).
//
//	C: auth_db() / check_credentials()
func (m *UIDAuthDBModule) Authenticate(user, domain string) (bool, error) {
	if m == nil {
		return false, errors.New("uid_auth_db: nil module")
	}
	if user == "" {
		return false, errors.New("uid_auth_db: empty user")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.users[userKey(user, domain)]
	return ok, nil
}

// GetCredentials returns the stored password for (user, domain). Returns an
// error when no record exists.
//
//	C: get_ha1() analogue
func (m *UIDAuthDBModule) GetCredentials(user, domain string) (string, error) {
	if m == nil {
		return "", errors.New("uid_auth_db: nil module")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	cred, ok := m.users[userKey(user, domain)]
	if !ok {
		return "", fmt.Errorf("uid_auth_db: no credentials for %s@%s", user, domain)
	}
	return cred.password, nil
}

// CountUsers returns the number of stored credential records.
func (m *UIDAuthDBModule) CountUsers() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.users)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultUIDAuthDB *UIDAuthDBModule
)

// DefaultUIDAuthDB returns the process-wide UIDAuthDBModule, creating it on
// first use.
func DefaultUIDAuthDB() *UIDAuthDBModule {
	defaultMu.RLock()
	m := defaultUIDAuthDB
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultUIDAuthDB == nil {
		defaultUIDAuthDB = New()
	}
	return defaultUIDAuthDB
}

// Init (re)initialises the process-wide UIDAuthDBModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultUIDAuthDB = New()
}
