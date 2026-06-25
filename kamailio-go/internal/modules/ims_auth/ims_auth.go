// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS authentication module - AKA challenge/verify and auth-vector
 * retrieval from the HSS (Cx Multimedia-Auth-Request/Answer).
 * Port of the kamailio ims_auth module (src/modules/ims_auth).
 *
 * ims_auth provides the shared authentication service used by the IMS
 * registrars:
 *
 *  - retrieval of an Authentication Vector from the HSS over the Cx
 *    reference point (MAR/MAA);
 *  - short-term caching of authentication vectors keyed by IMPI so that
 *    re-registrations do not hammer the HSS;
 *  - building the WWW-Authenticate AKA challenge;
 *  - verifying the UE's Authorization response (RES == XRES).
 *
 * The actual AKA primitives (Milenage / TS 33.203) live in
 * internal/ims/auth; this module wraps them with caching, the HSS
 * client abstraction and pending-state tracking.
 *
 * It is safe for concurrent use.
 */

package ims_auth

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
	"github.com/kamailio/kamailio-go/internal/ims/auth"
)

// AuthResult is the outcome of Authenticate: the SIP status code, the
// reason phrase, and any response headers (e.g. WWW-Authenticate).
type AuthResult struct {
	StatusCode   uint16
	StatusReason string
	Headers      map[string]str.Str
	// Authenticated is true when the caller passed a valid Authorization
	// response; callers can branch on this without re-parsing the status.
	Authenticated bool
}

// AuthStatus values returned by Verify.
type AuthStatus int

const (
	AuthOK         AuthStatus = iota // response matches XRES
	AuthFailed                       // response does not match
	AuthNoPending                    // no challenge in flight
	AuthMalformed                    // Authorization header unparseable
)

// Config holds the ims_auth configuration.
type Config struct {
	Realm           string
	ServerName      string        // S-CSCF name reported to the HSS
	CacheTTL        time.Duration // auth-vector cache lifetime (0 = no expiry)
	MaxAttempts     int           // challenges before AuthFailed locks out
	ChallengeExpiry time.Duration // how long a pending challenge stays valid
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		Realm:           "ims.example.com",
		ServerName:      "scscf.ims.example.com",
		CacheTTL:        5 * time.Minute,
		MaxAttempts:     3,
		ChallengeExpiry: 5 * time.Minute,
	}
}

// AuthClient retrieves authentication vectors from the HSS (Cx MAR/MAA).
// Implementations translate to Diameter when a real CDP transport is
// available; the default in-memory client is used for tests and stand-alone
// deployments without an HSS.
//
//	C: cxdx_send_request(Cx Multimedia-Auth-Request)
type AuthClient interface {
	MAR(impi, realm string) (*auth.AuthVector, error)
}

// InMemoryAuthClient is an AuthClient backed by an in-memory map. Seed it
// with auth vectors keyed by IMPI before use.
type InMemoryAuthClient struct {
	mu      sync.RWMutex
	vectors map[string]*auth.AuthVector
}

// NewInMemoryAuthClient creates an empty InMemoryAuthClient.
func NewInMemoryAuthClient() *InMemoryAuthClient {
	return &InMemoryAuthClient{vectors: make(map[string]*auth.AuthVector)}
}

// SetAuthVector seeds an authentication vector for the given IMPI.
func (c *InMemoryAuthClient) SetAuthVector(impi string, av *auth.AuthVector) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vectors[impi] = av
}

// MAR returns a clone of the seeded vector, or ErrUnknownSubscriber.
func (c *InMemoryAuthClient) MAR(impi, realm string) (*auth.AuthVector, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	av, ok := c.vectors[impi]
	if !ok {
		return nil, ErrUnknownSubscriber
	}
	return cloneVector(av), nil
}

// cacheEntry holds a cached auth vector and its expiry.
type cacheEntry struct {
	vector  *auth.AuthVector
	expires time.Time // zero = no expiry
}

// pendingAuth tracks an in-flight 401 challenge for one IMPI.
type pendingAuth struct {
	vector    *auth.AuthVector
	challenge *auth.AKAChallenge
	opaque    string
	nonce     string
	attempts  int
	createdAt time.Time
}

// AuthModule provides the IMS authentication service: vector caching,
// HSS MAR retrieval, AKA challenge building and response verification.
type AuthModule struct {
	mu      sync.RWMutex
	config  Config
	client  AuthClient
	cache   map[string]*cacheEntry // IMPI -> vector
	pending map[string]*pendingAuth // IMPI -> pending challenge
}

// NewAuthModule creates a module with the default in-memory client.
func NewAuthModule() *AuthModule {
	cfg := DefaultConfig()
	return &AuthModule{
		config:  cfg,
		client:  NewInMemoryAuthClient(),
		cache:   make(map[string]*cacheEntry),
		pending: make(map[string]*pendingAuth),
	}
}

// NewAuthModuleWithConfig creates a module with the supplied configuration.
func NewAuthModuleWithConfig(cfg Config) *AuthModule {
	m := NewAuthModule()
	m.config = cfg
	return m
}

// SetAuthClient replaces the HSS client. Intended for dependency injection.
func (m *AuthModule) SetAuthClient(c AuthClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c == nil {
		c = NewInMemoryAuthClient()
	}
	m.client = c
}

// AuthClient returns the active HSS client.
func (m *AuthModule) AuthClient() AuthClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// InMemoryClient returns the client as an *InMemoryAuthClient, or nil when a
// custom client is configured.
func (m *AuthModule) InMemoryClient() *InMemoryAuthClient {
	if c, ok := m.AuthClient().(*InMemoryAuthClient); ok {
		return c
	}
	return nil
}

// SetRealm overrides the configured realm.
func (m *AuthModule) SetRealm(realm string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Realm = realm
}

// Realm returns the currently configured realm.
func (m *AuthModule) Realm() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Realm
}

// GetAuthVector fetches an authentication vector for the given IMPI,
// returning the cached entry when it is fresh and falling back to the HSS
// client otherwise. A fetched vector is cached for CacheTTL.
//
//	C: get_auth_vector() / MAR
func (m *AuthModule) GetAuthVector(impi, realm string) (*auth.AuthVector, error) {
	if impi == "" {
		return nil, errors.New("ims_auth: empty IMPI")
	}
	if av := m.cacheLookup(impi); av != nil {
		return cloneVector(av), nil
	}
	av, err := m.fetchFromHSS(impi, realm)
	if err != nil {
		return nil, err
	}
	m.cacheStore(impi, av)
	return cloneVector(av), nil
}

// BuildChallenge retrieves an auth vector (via GetAuthVector) and builds a
// WWW-Authenticate AKA challenge, storing the pending state for later
// verification. Returns the challenge header value and the opaque token.
//
//	C: build_auth_challenge()
func (m *AuthModule) BuildChallenge(impi, realm string) (str.Str, string, error) {
	av, err := m.GetAuthVector(impi, realm)
	if err != nil {
		return str.Str{}, "", err
	}
	opaque := auth.GenerateOpaque(impi, "")
	challenge := auth.BuildChallenge(av, realm, opaque)

	m.mu.Lock()
	m.pending[impi] = &pendingAuth{
		vector:    av,
		challenge: challenge,
		opaque:    opaque,
		nonce:     challenge.Nonce,
		createdAt: time.Now(),
	}
	m.mu.Unlock()

	return auth.BuildWWWAuthenticate(challenge), opaque, nil
}

// Verify checks the Authorization header in msg against the pending
// challenge stored for the IMPI extracted from the header. Returns the
// outcome and clears the pending state on success or lockout.
//
//	C: verify_auth_response()
func (m *AuthModule) Verify(msg *parser.SIPMsg) (AuthStatus, error) {
	if msg == nil {
		return AuthMalformed, errors.New("ims_auth: nil message")
	}
	if msg.Authorization == nil {
		return AuthMalformed, errors.New("ims_auth: no Authorization header")
	}
	akaResp, err := auth.ParseAuthorization(msg.Authorization.Body.String())
	if err != nil {
		return AuthMalformed, err
	}
	impi := akaResp.Username
	if impi == "" {
		return AuthMalformed, errors.New("ims_auth: no username in Authorization")
	}
	pending := m.popPending(impi, false)
	if pending == nil {
		return AuthNoPending, nil
	}
	if akaResp.Opaque != "" && akaResp.Opaque != pending.opaque {
		// Restore pending so the caller can re-send the right response.
		m.putPending(impi, pending)
		return AuthFailed, errors.New("ims_auth: opaque mismatch")
	}
	if !auth.VerifyResponse(pending.vector, akaResp) {
		m.mu.Lock()
		pending.attempts++
		attempts := pending.attempts
		m.mu.Unlock()
		maxAttempts := m.maxAttempts()
		if attempts >= maxAttempts {
			m.popPending(impi, true)
			return AuthFailed, nil
		}
		// Restore pending for the next attempt.
		m.putPending(impi, pending)
		return AuthFailed, nil
	}
	m.popPending(impi, true)
	return AuthOK, nil
}

// Authenticate drives the full authentication flow for a REGISTER-style
// message. With no Authorization it issues a 401 challenge; with a valid
// Authorization it returns 200 (Authenticated=true); on failure it returns
// 403 or re-challenges (401) up to MaxAttempts.
//
//	C: ims_authenticate()
func (m *AuthModule) Authenticate(msg *parser.SIPMsg) (*AuthResult, error) {
	if msg == nil {
		return nil, errors.New("ims_auth: nil message")
	}
	realm := m.Realm()
	if msg.Authorization == nil {
		impi := m.impiFromMessage(msg)
		if impi == "" {
			return &AuthResult{StatusCode: 403, StatusReason: "Forbidden"}, errors.New("ims_auth: no IMPI derivable")
		}
		wwwAuth, _, err := m.BuildChallenge(impi, realm)
		if err != nil {
			return &AuthResult{StatusCode: 403, StatusReason: "Forbidden"}, err
		}
		return &AuthResult{
			StatusCode:   401,
			StatusReason: "Unauthorized",
			Headers:      map[string]str.Str{"WWW-Authenticate": wwwAuth},
		}, nil
	}
	status, err := m.Verify(msg)
	switch status {
	case AuthOK:
		return &AuthResult{StatusCode: 200, StatusReason: "OK", Authenticated: true}, nil
	case AuthNoPending:
		// Re-challenge.
		impi := m.impiFromAuthorization(msg)
		if impi == "" {
			return &AuthResult{StatusCode: 403, StatusReason: "Forbidden"}, err
		}
		wwwAuth, _, cerr := m.BuildChallenge(impi, realm)
		if cerr != nil {
			return &AuthResult{StatusCode: 403, StatusReason: "Forbidden"}, cerr
		}
		return &AuthResult{
			StatusCode:   401,
			StatusReason: "Unauthorized",
			Headers:      map[string]str.Str{"WWW-Authenticate": wwwAuth},
		}, nil
	case AuthFailed:
		return &AuthResult{StatusCode: 403, StatusReason: "Forbidden"}, err
	default: // AuthMalformed
		return &AuthResult{StatusCode: 400, StatusReason: "Bad Request"}, err
	}
}

// IsAuthorised reports whether the message carries a Digest Authorization
// header. It does not verify the credentials.
//
//	C: is_authorised()
func (m *AuthModule) IsAuthorised(msg *parser.SIPMsg) bool {
	if msg == nil || msg.Authorization == nil {
		return false
	}
	body := strings.TrimSpace(msg.Authorization.Body.String())
	if body == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(body), "digest")
}

// HasPendingChallenge reports whether a challenge is in flight for the IMPI.
func (m *AuthModule) HasPendingChallenge(impi string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.pending[impi]
	return ok
}

// PendingCount returns the number of in-flight challenges.
func (m *AuthModule) PendingCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pending)
}

// CacheVector stores an auth vector in the cache, overriding any TTL with
// the configured CacheTTL.
func (m *AuthModule) CacheVector(impi string, av *auth.AuthVector) {
	m.cacheStore(impi, av)
}

// ClearCache empties the auth-vector cache.
func (m *AuthModule) ClearCache() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = make(map[string]*cacheEntry)
}

// CacheSize returns the number of cached auth vectors.
func (m *AuthModule) CacheSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.cache)
}

// CleanupExpired removes expired cache entries and stale pending
// challenges. Returns the total number of removed entries.
func (m *AuthModule) CleanupExpired() int {
	now := time.Now()
	removed := 0
	m.mu.Lock()
	defer m.mu.Unlock()
	for impi, e := range m.cache {
		if !e.expires.IsZero() && now.After(e.expires) {
			delete(m.cache, impi)
			removed++
		}
	}
	challengeExpiry := m.config.ChallengeExpiry
	if challengeExpiry <= 0 {
		challengeExpiry = 5 * time.Minute
	}
	for impi, p := range m.pending {
		if now.Sub(p.createdAt) > challengeExpiry {
			delete(m.pending, impi)
			removed++
		}
	}
	return removed
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func (m *AuthModule) maxAttempts() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.config.MaxAttempts <= 0 {
		return 3
	}
	return m.config.MaxAttempts
}

func (m *AuthModule) cacheLookup(impi string) *auth.AuthVector {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.cache[impi]
	if !ok {
		return nil
	}
	if !e.expires.IsZero() && time.Now().After(e.expires) {
		return nil
	}
	return e.vector
}

func (m *AuthModule) cacheStore(impi string, av *auth.AuthVector) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ttl := m.config.CacheTTL
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	m.cache[impi] = &cacheEntry{vector: av, expires: exp}
}

func (m *AuthModule) fetchFromHSS(impi, realm string) (*auth.AuthVector, error) {
	c := m.AuthClient()
	if c == nil {
		// No HSS: synthesize a vector so the flow is exercised in tests.
		return auth.GenerateAuthVector()
	}
	av, err := c.MAR(impi, realm)
	if err != nil {
		return nil, err
	}
	return av, nil
}

func (m *AuthModule) popPending(impi string, remove bool) *pendingAuth {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pending[impi]
	if !ok {
		return nil
	}
	if remove {
		delete(m.pending, impi)
	}
	return p
}

func (m *AuthModule) putPending(impi string, p *pendingAuth) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending[impi] = p
}

// impiFromMessage derives an IMPI from a REGISTER: prefer the Authorization
// username, then the To-header URI (user@realm).
func (m *AuthModule) impiFromMessage(msg *parser.SIPMsg) string {
	if impi := m.impiFromAuthorization(msg); impi != "" {
		return impi
	}
	if msg.To != nil {
		return deriveIMPI(extractAOR(msg.To.Body.String()))
	}
	return ""
}

func (m *AuthModule) impiFromAuthorization(msg *parser.SIPMsg) string {
	if msg == nil || msg.Authorization == nil {
		return ""
	}
	return extractAuthUsername(msg.Authorization.Body.String())
}

// ---------------------------------------------------------------------------
// package-level helpers
// ---------------------------------------------------------------------------

func cloneVector(av *auth.AuthVector) *auth.AuthVector {
	if av == nil {
		return nil
	}
	return &auth.AuthVector{
		RAND: append([]byte(nil), av.RAND...),
		XRES: append([]byte(nil), av.XRES...),
		CK:   append([]byte(nil), av.CK...),
		IK:   append([]byte(nil), av.IK...),
		AUTN: append([]byte(nil), av.AUTN...),
	}
}

// extractAOR returns the SIP URI from a To/From header body.
func extractAOR(body string) string {
	if body == "" {
		return ""
	}
	inner := body
	if idx := strings.Index(inner, "<"); idx >= 0 {
		end := strings.Index(inner[idx:], ">")
		if end >= 0 {
			inner = inner[idx+1 : idx+end]
		} else {
			inner = inner[idx+1:]
		}
	}
	return strings.TrimSpace(inner)
}

// extractAuthUsername returns the username from a Digest Authorization
// header body, or the empty string when absent.
func extractAuthUsername(body string) string {
	if body == "" {
		return ""
	}
	lower := strings.ToLower(body)
	idx := strings.Index(lower, "username=")
	if idx < 0 {
		return ""
	}
	rest := body[idx+len("username="):]
	if len(rest) > 0 && rest[0] == '"' {
		end := strings.IndexByte(rest[1:], '"')
		if end >= 0 {
			return rest[1 : 1+end]
		}
	}
	if semi := strings.IndexByte(rest, ','); semi >= 0 {
		rest = rest[:semi]
	}
	return strings.TrimSpace(rest)
}

// deriveIMPI derives a private identity (IMPI) from the public identity
// (IMPU) by stripping the scheme and taking user@realm.
func deriveIMPI(impu string) string {
	impu = strings.TrimSpace(impu)
	for _, pfx := range []string{"sips:", "sip:", "tel:"} {
		if strings.HasPrefix(strings.ToLower(impu), pfx) {
			impu = impu[len(pfx):]
			break
		}
	}
	if at := strings.IndexByte(impu, '@'); at >= 0 {
		return impu
	}
	return impu
}

// Errors
var (
	// ErrUnknownSubscriber is returned by AuthClient when the HSS has no
	// record for the requested IMPI.
	ErrUnknownSubscriber = errors.New("ims_auth: unknown subscriber")
)

// String returns a human-readable status name.
func (s AuthStatus) String() string {
	switch s {
	case AuthOK:
		return "ok"
	case AuthFailed:
		return "failed"
	case AuthNoPending:
		return "no-pending"
	case AuthMalformed:
		return "malformed"
	default:
		return fmt.Sprintf("auth-status(%d)", int(s))
	}
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultAM *AuthModule
)

// DefaultAuth returns the process-wide AuthModule, creating one on first use.
func DefaultAuth() *AuthModule {
	defaultMu.RLock()
	a := defaultAM
	defaultMu.RUnlock()
	if a != nil {
		return a
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultAM == nil {
		defaultAM = NewAuthModule()
	}
	return defaultAM
}

// Init (re)initialises the process-wide AuthModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultAM = NewAuthModule()
}
