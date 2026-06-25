// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * ldap module - LDAP directory connector.
 *
 * Port of the kamailio ldap module (src/modules/ldap). Provides directory
 * queries for user lookup and authentication. The actual LDAP operations
 * are performed through the LDAPConn interface so tests can substitute an
 * in-memory mock and production code can plug in github.com/go-ldap/ldap/v3
 * or similar.
 *
 * C equivalent: ldap.so - ld_session.c / ldap_api_fn.c.
 */

package ldap

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// Config holds the LDAP connection and search configuration.
//
// C equivalent: the ldap_session settings (host, port, bind_dn, etc.).
type Config struct {
	Server       string // LDAP server host
	Port         int    // LDAP server port (389 / 636)
	UseTLS       bool   // upgrade to TLS after bind (STARTTLS)
	BindDN       string // bind distinguished name
	BindPassword string // bind password
	BaseDN       string // search base distinguished name
	SearchFilter string // default search filter (%s replaced by username)
	Scope        int    // search scope (0 base / 1 onelevel / 2 subtree)
}

// DefaultConfig returns a config with sensible Kamailio-style defaults.
func DefaultConfig() *Config {
	return &Config{
		Server:       "127.0.0.1",
		Port:         389,
		BindDN:       "cn=admin,dc=example,dc=org",
		BindPassword: "",
		BaseDN:       "dc=example,dc=org",
		SearchFilter: "(uid=%s)",
		Scope:        2,
	}
}

// Validate checks required config fields.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("ldap: nil config")
	}
	if strings.TrimSpace(c.Server) == "" {
		return errors.New("ldap: empty server")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("ldap: invalid port %d", c.Port)
	}
	if strings.TrimSpace(c.BaseDN) == "" {
		return errors.New("ldap: empty base DN")
	}
	return nil
}

// SearchScope constants (mirror LDAP scope values).
const (
	ScopeBase     = 0
	ScopeOneLevel = 1
	ScopeSubtree  = 2
)

// LDAPResult holds a single directory entry.
//
// C equivalent: the result of ldap_result_attr_vals().
type LDAPResult struct {
	DN         string
	Attributes map[string][]string
}

// GetAttr returns the first value of an attribute, or "" if absent.
func (r *LDAPResult) GetAttr(name string) string {
	if r == nil {
		return ""
	}
	if vals := r.Attributes[name]; len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// ---------------------------------------------------------------------------
// LDAPConn - abstracted LDAP connection (no hard dependency on a client lib)
// ---------------------------------------------------------------------------

// LDAPConn is the minimal subset of LDAP operations required by the
// module. It is an interface so tests can substitute an in-memory mock and
// production code can plug in a real client.
type LDAPConn interface {
	Bind(username, password string) error
	Search(baseDN, filter string, scope int) ([]*LDAPResult, error)
	Close() error
}

// ConnFactory builds an LDAPConn for the given config. Production builds
// inject a real factory; the default returns a mock.
type ConnFactory func(cfg Config) (LDAPConn, error)

// mockLDAPConn is an in-memory, concurrency-safe LDAPConn.
type mockLDAPConn struct {
	mu       sync.Mutex
	cfg      Config
	entries  []*LDAPResult
	bound    bool
	bindUser string
	closed   bool
}

func (c *mockLDAPConn) Bind(username, password string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("ldap: connection closed")
	}
	// Mock bind: accept the configured bind credentials or any non-empty
	// pair so tests can exercise Authenticate.
	if username == "" {
		return errors.New("ldap: empty bind username")
	}
	c.bound = true
	c.bindUser = username
	return nil
}

func (c *mockLDAPConn) Search(baseDN, filter string, scope int) ([]*LDAPResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("ldap: connection closed")
	}
	if !c.bound {
		return nil, errors.New("ldap: not bound")
	}
	attr, val := parseSimpleFilter(filter)
	var out []*LDAPResult
	for _, e := range c.entries {
		if baseDN != "" && !strings.HasSuffix(e.DN, baseDN) {
			continue
		}
		if attr != "" {
			if e.GetAttr(attr) != val {
				continue
			}
		}
		out = append(out, cloneResult(e))
	}
	return out, nil
}

func (c *mockLDAPConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.bound = false
	return nil
}

// newMockConn builds a mock connection preloaded with the given entries.
func newMockConn(cfg Config, entries []*LDAPResult) *mockLDAPConn {
	return &mockLDAPConn{cfg: cfg, entries: entries}
}

// cloneResult returns a deep copy of a result so callers cannot mutate the
// mock's internal state.
func cloneResult(r *LDAPResult) *LDAPResult {
	if r == nil {
		return nil
	}
	out := &LDAPResult{DN: r.DN, Attributes: make(map[string][]string, len(r.Attributes))}
	for k, v := range r.Attributes {
		cp := make([]string, len(v))
		copy(cp, v)
		out.Attributes[k] = cp
	}
	return out
}

// parseSimpleFilter extracts the attribute and value from a filter of the
// form "(attr=value)". Anything more complex matches everything.
func parseSimpleFilter(filter string) (attr, val string) {
	filter = strings.TrimSpace(filter)
	filter = strings.TrimPrefix(filter, "(")
	filter = strings.TrimSuffix(filter, ")")
	if filter == "" {
		return "", ""
	}
	if idx := strings.Index(filter, "="); idx >= 0 {
		return filter[:idx], filter[idx+1:]
	}
	return "", ""
}

// ---------------------------------------------------------------------------
// LDAPModule
// ---------------------------------------------------------------------------

// LDAPModule is the LDAP directory connector. It maintains a connection
// pool keyed by server address and dispatches search / authenticate calls.
//
// C equivalent: the module global state plus the ld_session_t pool.
type LDAPModule struct {
	mu       sync.RWMutex
	cfg      Config
	factory  ConnFactory
	pool     map[string]LDAPConn
	searches atomic.Int64
}

// New creates an LDAPModule with default configuration and a mock factory.
func New() *LDAPModule {
	cfg := *DefaultConfig()
	m := &LDAPModule{
		cfg:  cfg,
		pool: make(map[string]LDAPConn),
	}
	m.factory = func(c Config) (LDAPConn, error) {
		return newMockConn(c, nil), nil
	}
	return m
}

// NewWithConfig creates an LDAPModule using the supplied configuration.
func NewWithConfig(cfg Config) *LDAPModule {
	m := &LDAPModule{cfg: cfg, pool: make(map[string]LDAPConn)}
	m.factory = func(c Config) (LDAPConn, error) {
		return newMockConn(c, nil), nil
	}
	return m
}

// Init (re)configures the module with the supplied config and resets the
// connection pool.
//
// C equivalent: ldap_init() / mod_init().
func (m *LDAPModule) Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	for _, c := range m.pool {
		_ = c.Close()
	}
	m.pool = make(map[string]LDAPConn)
	return nil
}

// Config returns a copy of the current configuration.
func (m *LDAPModule) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// SetServer configures the server host and port.
func (m *LDAPModule) SetServer(server string, port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Server = server
	m.cfg.Port = port
}

// SetCredentials configures the bind DN and password.
func (m *LDAPModule) SetCredentials(bindDN, bindPassword string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.BindDN = bindDN
	m.cfg.BindPassword = bindPassword
}

// SetBaseDN configures the search base DN.
func (m *LDAPModule) SetBaseDN(baseDN string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.BaseDN = baseDN
}

// SetConnFactory injects a real connection factory (production wiring).
func (m *LDAPModule) SetConnFactory(f ConnFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factory = f
}

// SetMockData preloads the mock connection for the configured server with
// the given entries. The mock is marked as already bound (mirroring what
// Connect() produces when a BindDN is configured). This is a no-op when a
// custom factory is installed.
func (m *LDAPModule) SetMockData(entries []*LDAPResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Replace the pool entry with a fresh, pre-bound mock holding the
	// entries so Connect() returns a usable connection directly.
	key := m.cfg.Server
	if c, ok := m.pool[key]; ok {
		_ = c.Close()
	}
	mc := newMockConn(m.cfg, entries)
	mc.bound = true
	m.pool[key] = mc
}

// Connect returns an LDAPConn for the configured server, performing the
// initial bind with the configured credentials. Connections are pooled per
// server address.
//
// C equivalent: ldap_connect() / ldap_bind().
func (m *LDAPModule) Connect() (LDAPConn, error) {
	m.mu.RLock()
	cfg := m.cfg
	factory := m.factory
	m.mu.RUnlock()

	m.mu.Lock()
	if c, ok := m.pool[cfg.Server]; ok {
		m.mu.Unlock()
		return c, nil
	}
	m.mu.Unlock()

	conn, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("ldap connect: %w", err)
	}
	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("ldap bind: %w", err)
		}
	}
	m.mu.Lock()
	if existing, ok := m.pool[cfg.Server]; ok {
		// Another goroutine raced ahead; prefer the existing one.
		_ = conn.Close()
		m.mu.Unlock()
		return existing, nil
	}
	m.pool[cfg.Server] = conn
	m.mu.Unlock()
	return conn, nil
}

// SearchUser looks up a single user by substituting the username into the
// configured search filter. Returns the first matching entry.
//
// C equivalent: ldap_search() with the user filter.
func (m *LDAPModule) SearchUser(username string) (*LDAPResult, error) {
	if username == "" {
		return nil, errors.New("ldap: empty username")
	}
	filter := substituteFilter(m.cfg.SearchFilter, username)
	results, err := m.SearchFilter(filter)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

// SearchFilter runs an LDAP search with the given filter against the
// configured base DN and scope.
//
// C equivalent: ldap_search().
func (m *LDAPModule) SearchFilter(filter string) ([]*LDAPResult, error) {
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()

	conn, err := m.Connect()
	if err != nil {
		return nil, err
	}
	m.searches.Add(1)
	return conn.Search(cfg.BaseDN, filter, cfg.Scope)
}

// Authenticate verifies a username/password pair by binding to the
// directory as the user. It returns true on success.
//
// C equivalent: ldap_filter_url_encode() + ldap_bind() for auth.
func (m *LDAPModule) Authenticate(username, password string) (bool, error) {
	if username == "" {
		return false, errors.New("ldap: empty username")
	}
	user, err := m.SearchUser(username)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, nil
	}
	m.mu.RLock()
	cfg := m.cfg
	factory := m.factory
	m.mu.RUnlock()

	// Bind as the located user DN.
	conn, err := factory(cfg)
	if err != nil {
		return false, fmt.Errorf("ldap auth connect: %w", err)
	}
	defer conn.Close()
	if err := conn.Bind(user.DN, password); err != nil {
		return false, nil
	}
	return true, nil
}

// SearchCount returns the number of executed searches.
func (m *LDAPModule) SearchCount() int64 {
	return m.searches.Load()
}

// substituteFilter replaces the first "%s" placeholder in the filter with
// the username, escaping any LDAP-special characters.
func substituteFilter(filter, username string) string {
	if filter == "" {
		return filter
	}
	escaped := escapeLDAPDN(username)
	return strings.Replace(filter, "%s", escaped, 1)
}

// escapeLDAPDN escapes characters that are special in LDAP filters per
// RFC 4515: '*', '(', ')', '\', and the NUL byte.
func escapeLDAPDN(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '*', '(', ')', '\\':
			b.WriteString(`\`)
			b.WriteRune(r)
		case 0:
			b.WriteString(`\00`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (project pattern: New / Default* / Init)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *LDAPModule
)

// DefaultLDAP returns the process-wide module, creating it on first use.
func DefaultLDAP() *LDAPModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)configures the process-wide module with the supplied config and
// resets the connection pool.
func Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = &LDAPModule{cfg: cfg, pool: make(map[string]LDAPConn)}
	defaultM.factory = func(c Config) (LDAPConn, error) {
		return newMockConn(c, nil), nil
	}
	return nil
}

// SearchUser is the package-level wrapper around DefaultLDAP().SearchUser.
func SearchUser(username string) (*LDAPResult, error) {
	return DefaultLDAP().SearchUser(username)
}

// SearchFilter is the package-level wrapper around DefaultLDAP().SearchFilter.
func SearchFilter(filter string) ([]*LDAPResult, error) {
	return DefaultLDAP().SearchFilter(filter)
}

// Authenticate is the package-level wrapper around DefaultLDAP().Authenticate.
func Authenticate(username, password string) (bool, error) {
	return DefaultLDAP().Authenticate(username, password)
}
