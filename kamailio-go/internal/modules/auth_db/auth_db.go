// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * auth_db module - Digest authentication against a database backend.
 * Port of the kamailio auth_db module (src/modules/auth_db/auth_db_mod.c).
 *
 * The C module loads subscriber credentials (HA1 hashes) from a database
 * and verifies Digest Authentication responses. This Go port mirrors that
 * behaviour on top of the generic db.DBConn interface and the auth package's
 * Digest computation helpers.
 *
 * C equivalent column defaults (auth_db_mod.c):
 *   USER_COL    = "username"
 *   DOMAIN_COL  = "domain"
 *   PASS_COL    = "ha1"
 *   calc_ha1    = 0  (store HA1, not plaintext)
 */

package auth_db

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/auth"
	"github.com/kamailio/kamailio-go/internal/core/db"
	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// DefaultTable is the default subscriber table name.
// C: the table is configured via db_url; "subscriber" is the Kamailio default.
const DefaultTable = "subscriber"

// ErrNoConnection is returned when no database connection is available.
var ErrNoConnection = errors.New("auth_db: no database connection")

// ErrUserNotFound is returned when a subscriber is not in the database.
var ErrUserNotFound = errors.New("auth_db: user not found")

// AuthDBConfig holds the configuration for the auth_db module.
//
// C equivalent: the module parameters db_url, user_column, domain_column,
// password_column, calculate_ha1.
type AuthDBConfig struct {
	DBDriver     string
	DBURL        string
	UserColumn   string
	DomainColumn string
	PassColumn   string
	CalculateHA1 bool
}

// DefaultAuthDBConfig returns a config with Kamailio-style defaults.
func DefaultAuthDBConfig() *AuthDBConfig {
	return &AuthDBConfig{
		DBDriver:     "memory",
		DBURL:        "",
		UserColumn:   "username",
		DomainColumn: "domain",
		PassColumn:   "ha1",
		CalculateHA1: false,
	}
}

// Validate checks required config fields.
func (c *AuthDBConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("auth_db config: nil")
	}
	if c.UserColumn == "" {
		return fmt.Errorf("auth_db config: user_column is required")
	}
	if c.DomainColumn == "" {
		return fmt.Errorf("auth_db config: domain_column is required")
	}
	if c.PassColumn == "" {
		return fmt.Errorf("auth_db config: pass_column is required")
	}
	return nil
}

// AuthDBStats holds atomic counters for authentication attempts.
type AuthDBStats struct {
	AuthAttempts atomic.Int64
	AuthSuccess  atomic.Int64
	AuthFailure  atomic.Int64
}

// AuthDBModule implements database-backed Digest authentication.
//
// C equivalent: the auth_db module state (auth_db_handle, auth_dbf, calc_ha1).
type AuthDBModule struct {
	mu     sync.RWMutex
	config *AuthDBConfig
	conn   db.DBConn
	stats  *AuthDBStats
	cache  map[string]string // "username:domain" -> HA1
}

// Compile-time check that AuthDBModule is usable.
var _ interface {
	SetConfig(*AuthDBConfig)
} = (*AuthDBModule)(nil)

// NewAuthDBModule creates a new AuthDBModule with the given connection and
// config. If cfg is nil, DefaultAuthDBConfig is used.
func NewAuthDBModule(conn db.DBConn, cfg *AuthDBConfig) *AuthDBModule {
	if cfg == nil {
		cfg = DefaultAuthDBConfig()
	}
	return &AuthDBModule{
		config: cfg,
		conn:   conn,
		stats:  &AuthDBStats{},
		cache:  make(map[string]string),
	}
}

// SetConfig updates the module configuration. Clears the credential cache
// since column names may have changed.
func (m *AuthDBModule) SetConfig(cfg *AuthDBConfig) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = DefaultAuthDBConfig()
	}
	m.config = cfg
	m.cache = make(map[string]string)
}

// SetConn sets the database connection used for credential lookups.
func (m *AuthDBModule) SetConn(conn db.DBConn) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conn = conn
	m.cache = make(map[string]string)
}

// Stats returns the module's statistics counters.
func (m *AuthDBModule) Stats() *AuthDBStats {
	if m == nil {
		return nil
	}
	return m.stats
}

// ensureConn returns the current connection, opening one from config if none
// has been set.
func (m *AuthDBModule) ensureConn() (db.DBConn, error) {
	m.mu.RLock()
	conn := m.conn
	cfg := m.config
	m.mu.RUnlock()
	if conn != nil {
		return conn, nil
	}
	if cfg == nil || cfg.DBDriver == "" {
		return nil, ErrNoConnection
	}
	c, err := db.Open(cfg.DBDriver, cfg.DBURL)
	if err != nil {
		return nil, fmt.Errorf("auth_db: open db: %w", err)
	}
	m.mu.Lock()
	if m.conn == nil {
		m.conn = c
	} else {
		_ = c.Close()
		c = m.conn
	}
	m.mu.Unlock()
	return c, nil
}

// cacheKey builds the cache key for a (username, domain) pair.
func cacheKey(username, domain string) string {
	return username + ":" + domain
}

// Authenticate verifies the Digest credentials in msg against the database.
// It looks up the HA1 for (username, domain), recomputes the expected
// response, and compares it with the response in the Authorization header.
//
// C equivalent: www_authenticate / proxy_authenticate in authorize.c.
func (m *AuthDBModule) Authenticate(msg *parser.SIPMsg, username, domain string) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("nil auth_db module")
	}
	if m.stats != nil {
		m.stats.AuthAttempts.Add(1)
	}
	if msg == nil {
		if m.stats != nil {
			m.stats.AuthFailure.Add(1)
		}
		return false, fmt.Errorf("nil sip message")
	}

	hdr := msg.Authorization
	if hdr == nil {
		if m.stats != nil {
			m.stats.AuthFailure.Add(1)
		}
		return false, fmt.Errorf("no Authorization header")
	}

	authBody, err := parser.ParseAuthorizationFromHeader(hdr)
	if err != nil {
		if m.stats != nil {
			m.stats.AuthFailure.Add(1)
		}
		return false, fmt.Errorf("parse authorization: %w", err)
	}

	ha1, err := m.GetHA1(username, domain)
	if err != nil {
		if m.stats != nil {
			m.stats.AuthFailure.Add(1)
		}
		return false, err
	}

	// Determine the SIP method string for HA2 computation.
	method := ""
	if msg.FirstLine != nil && msg.FirstLine.Req != nil {
		method = msg.FirstLine.Req.Method.String()
	}

	uri := authBody.URI.String()
	nonce := authBody.Nonce.String()
	nc := authBody.NC.String()
	cnonce := authBody.CNonce.String()
	qopStr := authBody.QopStr.String()

	ha2 := auth.CalcHA2(authBody.Algorithm, method, uri, "", parser.QopNone)
	expected := auth.CalcResponse(authBody.Algorithm, ha1, nonce, nc, cnonce, qopStr, ha2)

	if strings.EqualFold(authBody.Response.String(), expected) {
		if m.stats != nil {
			m.stats.AuthSuccess.Add(1)
		}
		return true, nil
	}

	if m.stats != nil {
		m.stats.AuthFailure.Add(1)
	}
	return false, nil
}

// GetHA1 retrieves the HA1 hash for (username, domain) from the database.
// When CalculateHA1 is true, the PassColumn stores a plaintext password and
// HA1 is computed on the fly.
//
// C equivalent: get_ha1 / calculate_ha1 in authorize.c.
func (m *AuthDBModule) GetHA1(username, domain string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("nil auth_db module")
	}

	// Check cache first.
	key := cacheKey(username, domain)
	m.mu.RLock()
	if ha1, ok := m.cache[key]; ok {
		cfg := m.config
		m.mu.RUnlock()
		_ = cfg
		return ha1, nil
	}
	m.mu.RUnlock()

	conn, err := m.ensureConn()
	if err != nil {
		return "", err
	}

	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	keys := []db.DBKey{
		{Name: cfg.UserColumn, Type: db.DBValString},
		{Name: cfg.DomainColumn, Type: db.DBValString},
		{Name: cfg.PassColumn, Type: db.DBValString},
	}
	where := []db.DBCondition{
		{Key: cfg.UserColumn, Op: "=", Value: db.NewStringValue(username)},
		{Key: cfg.DomainColumn, Op: "=", Value: db.NewStringValue(domain)},
	}

	res, err := conn.Query(DefaultTable, keys, where, "", 1, 0)
	if err != nil {
		return "", fmt.Errorf("auth_db: query: %w", err)
	}
	if res == nil || res.RowCount() == 0 {
		return "", ErrUserNotFound
	}

	passVal := res.Row(0).GetString(cfg.PassColumn)
	var ha1 string
	if cfg.CalculateHA1 {
		ha1 = auth.CalcHA1(parser.AlgMD5, username, domain, passVal, "", "")
	} else {
		ha1 = passVal
	}

	// Cache the result.
	m.mu.Lock()
	m.cache[key] = ha1
	m.mu.Unlock()

	return ha1, nil
}

// IsUserInDB reports whether a subscriber with (username, domain) exists.
func (m *AuthDBModule) IsUserInDB(username, domain string) bool {
	if m == nil {
		return false
	}
	_, err := m.GetHA1(username, domain)
	return err == nil
}

// CountUsers returns the number of subscribers in the given domain.
func (m *AuthDBModule) CountUsers(domain string) (int, error) {
	if m == nil {
		return 0, fmt.Errorf("nil auth_db module")
	}
	conn, err := m.ensureConn()
	if err != nil {
		return 0, err
	}

	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	where := []db.DBCondition{
		{Key: cfg.DomainColumn, Op: "=", Value: db.NewStringValue(domain)},
	}
	res, err := conn.Query(DefaultTable, nil, where, "", 0, 0)
	if err != nil {
		return 0, fmt.Errorf("auth_db: count: %w", err)
	}
	if res == nil {
		return 0, nil
	}
	return res.RowCount(), nil
}

// Reload clears the credential cache so subsequent lookups re-query the
// database.
//
// C equivalent: there is no explicit reload in the C module (it queries on
// every auth), but the cache invalidation mirrors the intent.
func (m *AuthDBModule) Reload() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.cache = make(map[string]string)
	m.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Default singleton
// ---------------------------------------------------------------------------

var (
	defaultOnce sync.Once
	defaultMod  *AuthDBModule
)

// DefaultAuthDB returns the singleton AuthDBModule instance, creating it on
// first call with DefaultAuthDBConfig.
func DefaultAuthDB() *AuthDBModule {
	defaultOnce.Do(func() {
		defaultMod = NewAuthDBModule(nil, DefaultAuthDBConfig())
	})
	return defaultMod
}

// Init initialises the default AuthDBModule singleton. It is safe to call
// multiple times.
func Init() {
	_ = DefaultAuthDB()
}
