// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS registrar S-CSCF module - REGISTER transaction orchestration.
 * Port of the kamailio ims_registrar_scscf module (src/modules/ims_registrar_scscf).
 *
 * The S-CSCF registrar drives the registration state machine described in
 * 3GPP TS 24.229:
 *
 *  1. UE -> P-CSCF -> I-CSCF -> S-CSCF: REGISTER (no auth)
 *  2. S-CSCF -> HSS: MAR (Multimedia-Auth-Request)
 *  3. S-CSCF <- HSS: MAA with Authentication Vector
 *  4. S-CSCF -> UE: 401 Unauthorized + WWW-Authenticate (AKA challenge)
 *  5. UE -> S-CSCF: REGISTER + Authorization (AKA response)
 *  6. S-CSCF verifies RES == XRES
 *  7. S-CSCF -> HSS: SAR (Server-Assignment-Request)
 *  8. S-CSCF <- HSS: SAA with User Profile + Initial Filter Criteria
 *  9. S-CSCF -> UE: 200 OK + Service-Route + P-Associated-URI
 *
 * This module orchestrates the flow. Contact binding storage is delegated
 * to the ims_usrloc_scscf module; HSS communication is delegated to an
 * injectable CxClient so tests can substitute an in-memory HSS.
 *
 * It is safe for concurrent use.
 */

package ims_registrar_scscf

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
	"github.com/kamailio/kamailio-go/internal/ims"
	"github.com/kamailio/kamailio-go/internal/ims/auth"
	"github.com/kamailio/kamailio-go/internal/modules/ims_usrloc_scscf"
)

// SaveResult mirrors Kamailio's save() return: the SIP status code, the
// reason phrase, and any response headers that must be added to the reply.
type SaveResult struct {
	StatusCode   uint16
	StatusReason string
	Headers     map[string]str.Str
}

// Config holds the S-CSCF registrar configuration.
type Config struct {
	Realm            string        // home realm (used in WWW-Authenticate)
	DefaultExpires   int           // seconds applied when Expires header is absent
	MaxContacts      int           // per-AOR contact cap (0 = unlimited)
	MaxAuthAttempts  int           // challenges before 403 Forbidden
	ServiceRouteHost string        // host used to build the Service-Route header
	EnableCharging   bool          // include charging metadata in saved contacts
	CxTimeout        time.Duration // HSS query timeout
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		Realm:            "ims.example.com",
		DefaultExpires:   3600,
		MaxContacts:      10,
		MaxAuthAttempts:  3,
		ServiceRouteHost: "scscf.ims.example.com",
		EnableCharging:   true,
		CxTimeout:        5 * time.Second,
	}
}

// CxClient is the HSS Cx reference point client used by the registrar.
// Implementations retrieve authentication vectors (MAR/MAA) and user
// profiles (SAR/SAA) from the HSS.
//
//	C: CxDiameter.c / cxdx.c
type CxClient interface {
	// MAR retrieves an authentication vector for the given IMPI.
	MAR(impi, realm string) (*auth.AuthVector, error)
	// SAR retrieves the user profile for the given IMPI/IMPU after a
	// successful authentication. serverName identifies the S-CSCF.
	SAR(impi, impu, serverName string) (*ims_usrloc_scscf.UserProfile, error)
}

// InMemoryCxClient is a CxClient backed by in-memory maps. It is the
// default when no real HSS connection is configured and is useful for
// tests. Seed it with auth vectors and user profiles before use.
type InMemoryCxClient struct {
	mu       sync.RWMutex
	vectors  map[string]*auth.AuthVector            // IMPI -> vector
	profiles map[string]*ims_usrloc_scscf.UserProfile // IMPU -> profile
}

// NewInMemoryCxClient creates an empty InMemoryCxClient.
func NewInMemoryCxClient() *InMemoryCxClient {
	return &InMemoryCxClient{
		vectors:  make(map[string]*auth.AuthVector),
		profiles: make(map[string]*ims_usrloc_scscf.UserProfile),
	}
}

// SetAuthVector seeds an authentication vector for the given IMPI.
func (c *InMemoryCxClient) SetAuthVector(impi string, av *auth.AuthVector) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vectors[impi] = av
}

// SetUserProfile seeds a user profile for the given IMPU.
func (c *InMemoryCxClient) SetUserProfile(impu string, p *ims_usrloc_scscf.UserProfile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.profiles[impu] = p
}

// MAR returns a clone of the seeded vector, or ErrUnknownSubscriber.
func (c *InMemoryCxClient) MAR(impi, realm string) (*auth.AuthVector, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	av, ok := c.vectors[impi]
	if !ok {
		return nil, ErrUnknownSubscriber
	}
	// Clone so callers cannot mutate the seed.
	out := &auth.AuthVector{
		RAND: append([]byte(nil), av.RAND...),
		XRES: append([]byte(nil), av.XRES...),
		CK:   append([]byte(nil), av.CK...),
		IK:   append([]byte(nil), av.IK...),
		AUTN: append([]byte(nil), av.AUTN...),
	}
	return out, nil
}

// SAR returns the seeded profile for the IMPU, or ErrUnknownSubscriber.
func (c *InMemoryCxClient) SAR(impi, impu, serverName string) (*ims_usrloc_scscf.UserProfile, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.profiles[impu]
	if !ok {
		return nil, ErrUnknownSubscriber
	}
	return p, nil
}

// pendingAuth tracks an in-flight 401 challenge for one IMPU.
type pendingAuth struct {
	vector    *auth.AuthVector
	challenge *auth.AKAChallenge
	opaque    string
	nonce     string
	impi      string
	attempts  int
	createdAt time.Time
}

// RegistrarSCSCFModule orchestrates the S-CSCF registration flow. It
// composes a usrloc storage backend and a CxClient for HSS access.
type RegistrarSCSCFModule struct {
	mu          sync.RWMutex
	config      Config
	usrloc      *ims_usrloc_scscf.UsrlocSCSCFModule
	cx          CxClient
	pending     map[string]*pendingAuth // IMPU -> pending challenge
	serverName  string
}

// NewRegistrarSCSCFModule creates a module with the default in-memory
// CxClient and the default usrloc backend.
func NewRegistrarSCSCFModule() *RegistrarSCSCFModule {
	cfg := DefaultConfig()
	return &RegistrarSCSCFModule{
		config:     cfg,
		usrloc:     ims_usrloc_scscf.NewUsrlocSCSCFModule(),
		cx:         NewInMemoryCxClient(),
		pending:    make(map[string]*pendingAuth),
		serverName: "scscf@" + cfg.ServiceRouteHost,
	}
}

// NewRegistrarSCSCFModuleWithConfig creates a module with the supplied
// configuration. Storage and CxClient are default in-memory instances.
func NewRegistrarSCSCFModuleWithConfig(cfg Config) *RegistrarSCSCFModule {
	m := NewRegistrarSCSCFModule()
	m.config = cfg
	m.serverName = "scscf@" + cfg.ServiceRouteHost
	if m.usrloc != nil {
		m.usrloc.SetConfig(ims_usrloc_scscf.Config{
			MaxContacts:    cfg.MaxContacts,
			DefaultExpires: cfg.DefaultExpires,
			EnableCharging: cfg.EnableCharging,
			CxTimeout:      cfg.CxTimeout,
		})
	}
	return m
}

// SetCxClient replaces the HSS Cx client. Intended for dependency injection
// in tests; must not be called once traffic is flowing.
func (m *RegistrarSCSCFModule) SetCxClient(c CxClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c == nil {
		c = NewInMemoryCxClient()
	}
	m.cx = c
}

// SetUsrloc replaces the storage backend.
func (m *RegistrarSCSCFModule) SetUsrloc(u *ims_usrloc_scscf.UsrlocSCSCFModule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u == nil {
		u = ims_usrloc_scscf.NewUsrlocSCSCFModule()
	}
	m.usrloc = u
}

// Usrloc returns the underlying storage backend.
func (m *RegistrarSCSCFModule) Usrloc() *ims_usrloc_scscf.UsrlocSCSCFModule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.usrloc
}

// CxClient returns the HSS client.
func (m *RegistrarSCSCFModule) CxClient() CxClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cx
}

// SetRealm overrides the configured realm.
//
//	C: assign_realm()
func (m *RegistrarSCSCFModule) SetRealm(realm string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Realm = realm
}

// Realm returns the currently configured realm.
func (m *RegistrarSCSCFModule) Realm() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Realm
}

// InMemoryCx returns the CxClient as an *InMemoryCxClient, or nil when a
// custom client is configured. Convenient for seeding test data.
func (m *RegistrarSCSCFModule) InMemoryCx() *InMemoryCxClient {
	if c, ok := m.CxClient().(*InMemoryCxClient); ok {
		return c
	}
	return nil
}

// Save processes a SIP REGISTER request, driving the full S-CSCF flow.
// On a REGISTER without Authorization it issues a 401 challenge (after
// fetching an auth vector from the HSS). On a REGISTER with a valid
// Authorization it completes the registration, persists the binding via
// usrloc, and returns 200 OK with Service-Route + P-Associated-URI.
//
// Deregistration (Expires: 0 or Contact: *) removes the binding.
//
//	C: save()
func (m *RegistrarSCSCFModule) Save(msg *parser.SIPMsg) (*SaveResult, error) {
	if msg == nil {
		return nil, errors.New("ims_registrar_scscf: nil message")
	}
	if msg.Method() != parser.MethodRegister {
		return nil, errors.New("ims_registrar_scscf: not a REGISTER")
	}
	impu := extractAOR(headerBody(msg, msg.To, parser.HdrTo))
	if impu == "" {
		return &SaveResult{StatusCode: 400, StatusReason: "Bad Request"}, nil
	}
	// Deregistration is handled without authentication challenge when a
	// binding already exists for the IMPU.
	if isDeregistration(msg) {
		return m.handleDeregister(msg, impu)
	}

	if msg.Authorization != nil {
		return m.handleAuthResponse(msg, impu)
	}
	// Initial registration: challenge.
	return m.sendChallenge(msg, impu, "")
}

// sendChallenge fetches an auth vector from the HSS (MAR), stores the
// pending auth state, and returns a 401 with WWW-Authenticate. When impi
// is empty it is derived from the message (Authorization username or the
// IMPU); a non-empty impi is used as-is (used by the re-challenge path so
// the same subscriber identity is queried on retry).
func (m *RegistrarSCSCFModule) sendChallenge(msg *parser.SIPMsg, impu, impi string) (*SaveResult, error) {
	realm := m.Realm()
	if impi == "" {
		impi = extractAuthUsername(headerBody(msg, msg.Authorization, parser.HdrAuthorization))
		if impi == "" {
			impi = deriveIMPI(impu)
		}
	}

	av, err := m.fetchAuthVector(impi, realm)
	if err != nil {
		return &SaveResult{StatusCode: 403, StatusReason: "Forbidden"}, err
	}

	var callID, cseq string
	if msg.CallID != nil {
		callID = msg.CallID.Body.String()
	}
	if msg.CSeq != nil {
		if cb, perr := parser.ParseCSeqHeader(msg.CSeq); perr == nil && cb != nil {
			cseq = fmt.Sprintf("%d", cb.Number)
		}
	}
	opaque := auth.GenerateOpaque(callID, cseq)
	challenge := auth.BuildChallenge(av, realm, opaque)

	m.mu.Lock()
	m.pending[impu] = &pendingAuth{
		vector:    av,
		challenge: challenge,
		opaque:    opaque,
		nonce:     challenge.Nonce,
		impi:      impi,
		createdAt: time.Now(),
	}
	maxAttempts := m.config.MaxAuthAttempts
	m.mu.Unlock()
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	wwwAuth := auth.BuildWWWAuthenticate(challenge)
	return &SaveResult{
		StatusCode:   401,
		StatusReason: "Unauthorized",
		Headers:      map[string]str.Str{"WWW-Authenticate": wwwAuth},
	}, nil
}

// handleAuthResponse verifies the AKA response, runs SAR to fetch the user
// profile, persists the binding, and returns 200 OK.
func (m *RegistrarSCSCFModule) handleAuthResponse(msg *parser.SIPMsg, impu string) (*SaveResult, error) {
	m.mu.Lock()
	pending, ok := m.pending[impu]
	maxAttempts := m.config.MaxAuthAttempts
	m.mu.Unlock()
	if !ok || pending == nil {
		return &SaveResult{StatusCode: 403, StatusReason: "Forbidden"}, errors.New("ims_registrar_scscf: no pending auth state")
	}

	akaResp, err := auth.ParseAuthorization(msg.Authorization.Body.String())
	if err != nil {
		return &SaveResult{StatusCode: 400, StatusReason: "Bad Request"}, err
	}
	if akaResp.Opaque != "" && akaResp.Opaque != pending.opaque {
		return &SaveResult{StatusCode: 403, StatusReason: "Forbidden"}, errors.New("ims_registrar_scscf: opaque mismatch")
	}

	if !auth.VerifyResponse(pending.vector, akaResp) {
		m.mu.Lock()
		pending.attempts++
		attempts := pending.attempts
		m.mu.Unlock()
		if attempts >= maxAttempts {
			m.clearPending(impu)
			return &SaveResult{StatusCode: 403, StatusReason: "Forbidden"}, nil
		}
		// Re-challenge, reusing the original IMPI so the HSS query
		// resolves to the same auth vector.
		return m.sendChallenge(msg, impu, pending.impi)
	}

	// Authenticated. Fetch user profile from HSS (SAR).
	profile, err := m.fetchUserProfile(pending.impi, impu, m.getServerName())
	if err != nil {
		// Auth succeeded but HSS has no profile: still allow registration.
		profile = &ims_usrloc_scscf.UserProfile{IMPU: impu, IMPI: pending.impi}
	}

	// Persist the contact binding.
	expires := m.computeExpiry(msg)
	contactURI := extractContactURI(headerBody(msg, msg.Contact, parser.HdrContact))
	path := headerBody(msg, msg.Path, parser.HdrPath)
	contact := &ims_usrloc_scscf.SCCContact{
		AOR:          impu,
		Contact:      contactURI,
		Path:         path,
		Expires:      expires,
		RegState:     ims_usrloc_scscf.RegStateRegistered,
		LastRegTime:  time.Now(),
		IMPublicID:   impu,
		IMSPrivateID: pending.impi,
		ServiceRoute:  m.buildServiceRoute(),
		AssociatedURI: m.associatedURIs(profile, impu),
	}
	if m.config.EnableCharging && profile != nil && profile.ChargingInfo != "" {
		contact.ChargingID = profile.ChargingInfo
	}
	if uerr := m.saveContact(contact); uerr != nil {
		return &SaveResult{StatusCode: 503, StatusReason: "Service Unavailable"}, uerr
	}

	// Build the 200 OK response headers.
	headers := map[string]str.Str{
		"Service-Route":     ims.BuildServiceRoute(m.buildServiceRoute()),
		"P-Associated-URI":  str.Mk(fmt.Sprintf("<%s>", impu)),
	}
	if path != "" {
		// Echo back the Path the UE/P-CSCF sent.
		headers["Path"] = str.Mk(path)
	}

	m.clearPending(impu)
	return &SaveResult{
		StatusCode:   200,
		StatusReason: "OK",
		Headers:      headers,
	}, nil
}

// handleDeregister removes the binding and returns 200 OK.
func (m *RegistrarSCSCFModule) handleDeregister(msg *parser.SIPMsg, impu string) (*SaveResult, error) {
	contactBody := headerBody(msg, msg.Contact, parser.HdrContact)
	if strings.TrimSpace(contactBody) == "*" {
		if err := m.removeAOR(impu); err != nil {
			return &SaveResult{StatusCode: 404, StatusReason: "Not Found"}, nil
		}
		m.clearPending(impu)
		return &SaveResult{StatusCode: 200, StatusReason: "OK"}, nil
	}
	contactURI := extractContactURI(contactBody)
	if contactURI == "" {
		return &SaveResult{StatusCode: 400, StatusReason: "Bad Request"}, nil
	}
	if err := m.removeContact(impu, contactURI); err != nil {
		return &SaveResult{StatusCode: 404, StatusReason: "Not Found"}, nil
	}
	m.clearPending(impu)
	return &SaveResult{StatusCode: 200, StatusReason: "OK"}, nil
}

// Lookup finds the contact bindings for the AOR in the request URI and
// returns them as a slice of contact URIs. Used to route subsequent
// requests to a registered UE.
//
//	C: lookup()
func (m *RegistrarSCSCFModule) Lookup(msg *parser.SIPMsg) ([]string, error) {
	if msg == nil {
		return nil, errors.New("ims_registrar_scscf: nil message")
	}
	aor := ""
	if msg.FirstLine != nil && msg.FirstLine.Req != nil {
		aor = msg.FirstLine.Req.URI.String()
	}
	if aor == "" {
		return nil, errors.New("ims_registrar_scscf: no R-URI")
	}
	contacts := m.getContacts(aor)
	out := make([]string, 0, len(contacts))
	for _, c := range contacts {
		if c.RegState == ims_usrloc_scscf.RegStateRegistered && !c.IsExpired() {
			out = append(out, c.Contact)
		}
	}
	return out, nil
}

// IsRegistered reports whether the given IMPU has at least one
// non-expired registered contact.
//
//	C: is_registered()
func (m *RegistrarSCSCFModule) IsRegistered(impu string) bool {
	u := m.Usrloc()
	if u == nil {
		return false
	}
	return u.IsRegistered(impu)
}

// IsAuthorised reports whether the given REGISTER carries a valid
// Authorization header. It does not verify the credentials against a
// pending challenge; use Save() for that.
//
//	C: is_authorised()
func (m *RegistrarSCSCFModule) IsAuthorised(msg *parser.SIPMsg) bool {
	if msg == nil || msg.Authorization == nil {
		return false
	}
	body := strings.TrimSpace(msg.Authorization.Body.String())
	if body == "" {
		return false
	}
	lower := strings.ToLower(body)
	return strings.HasPrefix(lower, "digest")
}

// AssignRealm overrides the configured realm.
//
//	C: assign_realm()
func (m *RegistrarSCSCFModule) AssignRealm(realm string) {
	m.SetRealm(realm)
}

// AddPAssertedIdentity builds a P-Asserted-Identity header value for the
// supplied URI (no display name). The caller is responsible for applying
// the returned value to the in-flight message.
//
//	C: add_p_asserted_id()
func (m *RegistrarSCSCFModule) AddPAssertedIdentity(uri string) (str.Str, error) {
	if uri == "" {
		return str.Str{}, errors.New("ims_registrar_scscf: empty URI")
	}
	return ims.BuildPAI(uri, ""), nil
}

// RegFetchContacts returns a snapshot of all contacts for the given AOR.
//
//	C: reg_fetch_contacts()
func (m *RegistrarSCSCFModule) RegFetchContacts(aor string) []*ims_usrloc_scscf.SCCContact {
	u := m.Usrloc()
	if u == nil {
		return nil
	}
	return u.GetContacts(aor)
}

// Deregister forcibly removes all bindings for the given IMPU.
//
//	C: unregister() / delete impurecord
func (m *RegistrarSCSCFModule) Deregister(impu string) error {
	if err := m.removeAOR(impu); err != nil {
		return err
	}
	m.clearPending(impu)
	return nil
}

// PendingCount returns the number of in-flight 401 challenges.
func (m *RegistrarSCSCFModule) PendingCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pending)
}

// CleanupStalePending removes pending auth entries older than maxAge.
func (m *RegistrarSCSCFModule) CleanupStalePending(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	removed := 0
	for impu, p := range m.pending {
		if now.Sub(p.createdAt) > maxAge {
			delete(m.pending, impu)
			removed++
		}
	}
	return removed
}

// ---------------------------------------------------------------------------
// internal helpers (storage / HSS wrappers, kept lock-light)
// ---------------------------------------------------------------------------

func (m *RegistrarSCSCFModule) getServerName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.serverName
}

func (m *RegistrarSCSCFModule) fetchAuthVector(impi, realm string) (*auth.AuthVector, error) {
	cx := m.CxClient()
	if cx == nil {
		return auth.GenerateAuthVector()
	}
	av, err := cx.MAR(impi, realm)
	if err != nil {
		return nil, err
	}
	return av, nil
}

func (m *RegistrarSCSCFModule) fetchUserProfile(impi, impu, serverName string) (*ims_usrloc_scscf.UserProfile, error) {
	cx := m.CxClient()
	if cx == nil {
		return nil, ErrUnknownSubscriber
	}
	return cx.SAR(impi, impu, serverName)
}

func (m *RegistrarSCSCFModule) saveContact(c *ims_usrloc_scscf.SCCContact) error {
	u := m.Usrloc()
	if u == nil {
		return errors.New("ims_registrar_scscf: no usrloc backend")
	}
	return u.SaveContact(c)
}

func (m *RegistrarSCSCFModule) removeContact(aor, contact string) error {
	u := m.Usrloc()
	if u == nil {
		return errors.New("ims_registrar_scscf: no usrloc backend")
	}
	return u.RemoveContact(aor, contact)
}

func (m *RegistrarSCSCFModule) removeAOR(aor string) error {
	u := m.Usrloc()
	if u == nil {
		return errors.New("ims_registrar_scscf: no usrloc backend")
	}
	return u.RemoveAOR(aor)
}

func (m *RegistrarSCSCFModule) getContacts(aor string) []*ims_usrloc_scscf.SCCContact {
	u := m.Usrloc()
	if u == nil {
		return nil
	}
	return u.GetContacts(aor)
}

func (m *RegistrarSCSCFModule) clearPending(impu string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pending, impu)
}

func (m *RegistrarSCSCFModule) computeExpiry(msg *parser.SIPMsg) time.Time {
	m.mu.RLock()
	defaultExp := m.config.DefaultExpires
	m.mu.RUnlock()
	if defaultExp <= 0 {
		defaultExp = 3600
	}
	if msg.Expires != nil {
		if eb, err := parser.ParseExpiresBody(msg.Expires.Body); err == nil && eb != nil && eb.Value > 0 {
			return time.Now().Add(time.Duration(eb.Value) * time.Second)
		}
	}
	return time.Now().Add(time.Duration(defaultExp) * time.Second)
}

func (m *RegistrarSCSCFModule) buildServiceRoute() []string {
	host := m.config.ServiceRouteHost
	if host == "" {
		host = m.Realm()
	}
	return []string{fmt.Sprintf("sip:orig@%s", host)}
}

func (m *RegistrarSCSCFModule) associatedURIs(profile *ims_usrloc_scscf.UserProfile, impu string) []string {
	if profile != nil && profile.IMPU != "" {
		return []string{profile.IMPU}
	}
	return []string{impu}
}

// ---------------------------------------------------------------------------
// package-level helpers
// ---------------------------------------------------------------------------

// isDeregistration reports whether the REGISTER removes bindings.
func isDeregistration(msg *parser.SIPMsg) bool {
	if msg.Expires != nil {
		if eb, err := parser.ParseExpiresBody(msg.Expires.Body); err == nil && eb != nil && eb.Value == 0 {
			return true
		}
	}
	if msg.Contact != nil {
		body := strings.TrimSpace(msg.Contact.Body.String())
		if body == "*" {
			return true
		}
		// Contact with ;expires=0
		if strings.Contains(strings.ToLower(body), "expires=0") {
			return true
		}
	}
	return false
}

// headerBody returns the body string of a header, preferring the quick
// reference and falling back to a by-type lookup.
func headerBody(msg *parser.SIPMsg, quick *parser.HdrField, ht parser.HdrType) string {
	if quick != nil {
		return quick.Body.String()
	}
	if msg != nil {
		if h := msg.GetHeaderByType(ht); h != nil {
			return h.Body.String()
		}
	}
	return ""
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

// extractContactURI returns the contact URI from a Contact header body.
func extractContactURI(body string) string {
	if body == "" {
		return ""
	}
	if strings.TrimSpace(body) == "*" {
		return "*"
	}
	inner := body
	if idx := strings.Index(inner, "<"); idx >= 0 {
		end := strings.Index(inner[idx:], ">")
		if end >= 0 {
			inner = inner[idx+1 : idx+end]
		} else {
			inner = inner[idx+1:]
		}
	} else if semi := strings.Index(inner, ";"); semi >= 0 {
		inner = inner[:semi]
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
		return impu // already user@host
	}
	return impu
}

// Errors
var (
	// ErrUnknownSubscriber is returned by CxClient when the HSS has no
	// record for the requested identity.
	ErrUnknownSubscriber = errors.New("ims_registrar_scscf: unknown subscriber")
)

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultRM *RegistrarSCSCFModule
)

// DefaultSCSCFRegistrar returns the process-wide RegistrarSCSCFModule,
// creating one on first use.
func DefaultSCSCFRegistrar() *RegistrarSCSCFModule {
	defaultMu.RLock()
	r := defaultRM
	defaultMu.RUnlock()
	if r != nil {
		return r
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRM == nil {
		defaultRM = NewRegistrarSCSCFModule()
	}
	return defaultRM
}

// Init (re)initialises the process-wide RegistrarSCSCFModule to a fresh
// state, mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRM = NewRegistrarSCSCFModule()
}
