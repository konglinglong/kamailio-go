// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * auth_radius - RADIUS-backed SIP digest authentication.
 *
 * Port of the kamailio auth_radius module (src/modules/auth_radius). It
 * performs PAP-style and SIP Digest authentication against a RADIUS server
 * and exposes the returned SIP-AVP attributes.
 *
 * Because no RADIUS client library is present in go.mod, the actual network
 * exchange is performed through the RadiusClient interface. Init wires up an
 * in-memory mock implementation so the full API is exercisable without a
 * running RADIUS server; production callers can inject a real client via
 * SetClient.
 *
 * C equivalent: auth_radius.so - auth_radius.c / authorize.c.
 */

package auth_radius

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// RadiusClient - abstracted RADIUS client (no hard dependency on a client lib)
// ---------------------------------------------------------------------------

// AuthResult is the outcome of a RADIUS Access-Request: whether the request
// was accepted, the attributes returned by the server (non-nil when the user
// is known, regardless of password match), and any transport-level error.
type AuthResult struct {
	Accepted   bool
	Attributes map[string]string
}

// RadiusClient is the minimal subset of RADIUS operations required by the
// module. It is an interface so tests can substitute an in-memory mock and
// production code can plug in a real RADIUS client (radiusclient-ng, etc.).
type RadiusClient interface {
	// AuthPAP performs a PAP-style Access-Request. Attributes is non-nil when
	// the user is known (even if the password is wrong); nil when the user is
	// unknown.
	AuthPAP(username, password string) (AuthResult, error)
	// AuthDigest performs a SIP Digest Access-Request.
	AuthDigest(username, realm, uri, nonce, response, method string) (AuthResult, error)
	// Ping verifies connectivity to the RADIUS server.
	Ping() error
	// Close releases any underlying resources.
	Close() error
}

// mockUser is a registered user in the in-memory RADIUS mock.
type mockUser struct {
	password string
	attrs    map[string]string
	digest   map[string]string // keyed by realm -> expected response
}

// mockRadiusClient is an in-memory, concurrency-safe RadiusClient used when no
// real RADIUS server is available.
type mockRadiusClient struct {
	mu       sync.Mutex
	users    map[string]*mockUser
	failNext error // one-shot error injected by tests
}

// newMockRadiusClient creates an empty in-memory RadiusClient.
func newMockRadiusClient() *mockRadiusClient {
	return &mockRadiusClient{users: make(map[string]*mockUser)}
}

// SetUser registers a user with the given password and attributes in the mock.
func (c *mockRadiusClient) SetUser(username, password string, attrs map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make(map[string]string, len(attrs))
	for k, v := range attrs {
		cp[k] = v
	}
	c.users[username] = &mockUser{password: password, attrs: cp, digest: make(map[string]string)}
}

// SetDigestUser registers a digest credential for a user/realm pair.
func (c *mockRadiusClient) SetDigestUser(username, realm, response string, attrs map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	u, ok := c.users[username]
	if !ok {
		u = &mockUser{attrs: make(map[string]string), digest: make(map[string]string)}
		c.users[username] = u
	}
	for k, v := range attrs {
		u.attrs[k] = v
	}
	u.digest[realm] = response
}

// consumeFail returns the one-shot failure (if any) and clears it.
func (c *mockRadiusClient) consumeFail() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNext != nil {
		err := c.failNext
		c.failNext = nil
		return err
	}
	return nil
}

func (c *mockRadiusClient) AuthPAP(username, password string) (AuthResult, error) {
	if err := c.consumeFail(); err != nil {
		return AuthResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	u, ok := c.users[username]
	if !ok {
		return AuthResult{Accepted: false, Attributes: nil}, nil
	}
	out := make(map[string]string, len(u.attrs))
	for k, v := range u.attrs {
		out[k] = v
	}
	return AuthResult{Accepted: password == u.password, Attributes: out}, nil
}

func (c *mockRadiusClient) AuthDigest(username, realm, uri, nonce, response, method string) (AuthResult, error) {
	if err := c.consumeFail(); err != nil {
		return AuthResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	u, ok := c.users[username]
	if !ok {
		return AuthResult{Accepted: false, Attributes: nil}, nil
	}
	out := make(map[string]string, len(u.attrs))
	for k, v := range u.attrs {
		out[k] = v
	}
	want, hasRealm := u.digest[realm]
	if !hasRealm {
		return AuthResult{Accepted: false, Attributes: out}, nil
	}
	return AuthResult{Accepted: response == want, Attributes: out}, nil
}

func (c *mockRadiusClient) Ping() error {
	if err := c.consumeFail(); err != nil {
		return err
	}
	return nil
}

func (c *mockRadiusClient) Close() error { return nil }

// ---------------------------------------------------------------------------
// AuthRadiusModule
// ---------------------------------------------------------------------------

// AuthRadiusModule is the RADIUS authentication backend.
//
// C equivalent: the auth_radius module state (radius_config, service_type,
// append_realm_to_username, etc.).
type AuthRadiusModule struct {
	mu                   sync.RWMutex
	radiusServer         string
	radiusSecret         string
	radiusTimeout        time.Duration
	radiusRetries        int
	connected            bool
	client               RadiusClient
	appendRealmToUser    bool
}

// New returns a new AuthRadiusModule that is not yet connected.
func New() *AuthRadiusModule {
	return &AuthRadiusModule{}
}

// Init configures the RADIUS server, shared secret, timeout and retry count
// and marks the module connected. An empty server or negative retries leaves
// the module disconnected and returns an error.
func (m *AuthRadiusModule) Init(server, secret string, timeout time.Duration, retries int) error {
	if m == nil {
		return errors.New("auth_radius: nil module")
	}
	if strings.TrimSpace(server) == "" {
		return errors.New("auth_radius: empty server")
	}
	if retries < 0 {
		return fmt.Errorf("auth_radius: invalid retries %d", retries)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.radiusServer = server
	m.radiusSecret = secret
	m.radiusTimeout = timeout
	m.radiusRetries = retries
	if m.client == nil {
		m.client = newMockRadiusClient()
	}
	m.connected = true
	return nil
}

// SetClient injects a RadiusClient implementation (real or mock). Must be
// called before Init to take effect, or after Init to swap the client.
func (m *AuthRadiusModule) SetClient(c RadiusClient) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.client = c
}

// SetAppendRealmToUsername toggles whether the SIP realm is appended to the
// username before sending the Access-Request (C: append_realm_to_username).
func (m *AuthRadiusModule) SetAppendRealmToUsername(v bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendRealmToUser = v
}

// IsConnected reports whether Init has been called successfully.
func (m *AuthRadiusModule) IsConnected() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Server returns the configured RADIUS server address.
func (m *AuthRadiusModule) Server() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.radiusServer
}

// Secret returns the configured shared secret.
func (m *AuthRadiusModule) Secret() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.radiusSecret
}

// Timeout returns the configured command timeout.
func (m *AuthRadiusModule) Timeout() time.Duration {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.radiusTimeout
}

// Retries returns the configured retry count.
func (m *AuthRadiusModule) Retries() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.radiusRetries
}

// effectiveUsername applies the append_realm_to_username transformation: when
// enabled and the username has no realm, the RADIUS server host is appended as
// "@<host>".
func (m *AuthRadiusModule) effectiveUsername(username string) string {
	if !m.appendRealmToUser {
		return username
	}
	if strings.Contains(username, "@") {
		return username
	}
	host := m.radiusServer
	if i := strings.LastIndex(host, ":"); i >= 0 {
		// Strip the port. Handles "host:port"; leaves bare hosts untouched.
		// Only strip when the part after ':' is numeric to avoid stripping
		// IPv6 addresses incorrectly.
		portPart := host[i+1:]
		if isNumeric(portPart) {
			host = host[:i]
		}
	}
	if host == "" {
		return username
	}
	return username + "@" + host
}

// isNumeric reports whether s consists entirely of decimal digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// RadiusAuth performs a PAP-style Access-Request. Returns true when the
// server accepts the credentials.
func (m *AuthRadiusModule) RadiusAuth(username, password string) (bool, error) {
	if m == nil {
		return false, errors.New("auth_radius: nil module")
	}
	m.mu.RLock()
	connected := m.connected
	client := m.client
	user := m.effectiveUsername(username)
	m.mu.RUnlock()
	if !connected {
		return false, errors.New("auth_radius: not connected")
	}
	if client == nil {
		return false, errors.New("auth_radius: no radius client")
	}
	res, err := client.AuthPAP(user, password)
	if err != nil {
		return false, fmt.Errorf("auth_radius: %w", err)
	}
	return res.Accepted, nil
}

// RadiusAuthDigest performs a SIP Digest Access-Request. Returns true when
// the server accepts the digest response.
func (m *AuthRadiusModule) RadiusAuthDigest(username, realm, uri, nonce, response, method string) (bool, error) {
	if m == nil {
		return false, errors.New("auth_radius: nil module")
	}
	m.mu.RLock()
	connected := m.connected
	client := m.client
	m.mu.RUnlock()
	if !connected {
		return false, errors.New("auth_radius: not connected")
	}
	if client == nil {
		return false, errors.New("auth_radius: no radius client")
	}
	res, err := client.AuthDigest(username, realm, uri, nonce, response, method)
	if err != nil {
		return false, fmt.Errorf("auth_radius: %w", err)
	}
	return res.Accepted, nil
}

// CheckUserExists reports whether the RADIUS server knows about the user. It
// issues a PAP Access-Request with an empty password and treats a non-nil
// attribute set as evidence the user exists.
func (m *AuthRadiusModule) CheckUserExists(username string) (bool, error) {
	if m == nil {
		return false, errors.New("auth_radius: nil module")
	}
	m.mu.RLock()
	connected := m.connected
	client := m.client
	user := m.effectiveUsername(username)
	m.mu.RUnlock()
	if !connected {
		return false, errors.New("auth_radius: not connected")
	}
	if client == nil {
		return false, errors.New("auth_radius: no radius client")
	}
	res, err := client.AuthPAP(user, "")
	if err != nil {
		return false, fmt.Errorf("auth_radius: %w", err)
	}
	return res.Attributes != nil, nil
}

// GetAttributes returns the SIP-AVP attributes the RADIUS server associates
// with the user. Returns an empty (non-nil) map for unknown users.
func (m *AuthRadiusModule) GetAttributes(username string) (map[string]string, error) {
	if m == nil {
		return nil, errors.New("auth_radius: nil module")
	}
	m.mu.RLock()
	connected := m.connected
	client := m.client
	user := m.effectiveUsername(username)
	m.mu.RUnlock()
	if !connected {
		return nil, errors.New("auth_radius: not connected")
	}
	if client == nil {
		return nil, errors.New("auth_radius: no radius client")
	}
	res, err := client.AuthPAP(user, "")
	if err != nil {
		return nil, fmt.Errorf("auth_radius: %w", err)
	}
	if res.Attributes == nil {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(res.Attributes))
	for k, v := range res.Attributes {
		out[k] = v
	}
	return out, nil
}

// TestConnection verifies connectivity to the RADIUS server.
func (m *AuthRadiusModule) TestConnection() error {
	if m == nil {
		return errors.New("auth_radius: nil module")
	}
	m.mu.RLock()
	connected := m.connected
	client := m.client
	m.mu.RUnlock()
	if !connected {
		return errors.New("auth_radius: not connected")
	}
	if client == nil {
		return errors.New("auth_radius: no radius client")
	}
	return client.Ping()
}

// ---------------------------------------------------------------------------
// Package-level singleton (project pattern: New / Default* / Init)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *AuthRadiusModule
)

// DefaultAuthRadius returns the process-wide module, creating it on first use.
func DefaultAuthRadius() *AuthRadiusModule {
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

// Init (re)configures the package-wide module with the supplied server,
// secret, timeout and retries.
func Init(server, secret string, timeout time.Duration, retries int) error {
	return DefaultAuthRadius().Init(server, secret, timeout, retries)
}

// IsConnected is the package-level wrapper.
func IsConnected() bool { return DefaultAuthRadius().IsConnected() }

// RadiusAuth is the package-level wrapper for PAP authentication.
func RadiusAuth(username, password string) (bool, error) {
	return DefaultAuthRadius().RadiusAuth(username, password)
}
