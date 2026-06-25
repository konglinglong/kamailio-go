// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS registrar P-CSCF module - REGISTER handling on the Proxy-CSCF.
 * Port of the kamailio ims_registrar_pcscf module (src/modules/ims_registrar_pcscf).
 *
 * The P-CSCF is the first IMS contact point for the UE. Unlike the
 * S-CSCF it does not authenticate subscribers (that is the S-CSCF's job
 * via the HSS); instead it:
 *
 *  - records the UE's contact binding so subsequent requests for the
 *    same Address of Record can be routed back to the UE;
 *  - caches the Service-Route header learned from the S-CSCF 200 OK so
 *    that outbound requests toward the home network are routed through
 *    the S-CSCF;
 *  - records the received IP:port for NAT traversal when EnableNAT is
 *    set;
 *  - forwards REGISTER requests toward the I-CSCF (the actual forwarding
 *    is performed by the routing core; this module only manages state).
 *
 * Contact storage is delegated to the ims_usrloc_pcscf module.
 *
 * It is safe for concurrent use.
 */

package ims_registrar_pcscf

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
	"github.com/kamailio/kamailio-go/internal/ims"
	"github.com/kamailio/kamailio-go/internal/modules/ims_usrloc_pcscf"
)

// SaveResult mirrors Kamailio's save() return: the SIP status code, the
// reason phrase, and any response headers to add to the reply.
type SaveResult struct {
	StatusCode   uint16
	StatusReason string
	Headers      map[string]str.Str
}

// Config holds the P-CSCF registrar configuration.
type Config struct {
	Realm          string
	DefaultExpires int  // seconds applied when no Expires present
	MaxContacts    int  // per-AOR contact cap (0 = unlimited)
	EnableNAT      bool // record received IP:port on bindings
	NATPingInterval int // seconds between NAT keepalive pings
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		Realm:           "ims.example.com",
		DefaultExpires:  3600,
		MaxContacts:     10,
		EnableNAT:       true,
		NATPingInterval: 60,
	}
}

// RegistrarPCSCFModule orchestrates P-CSCF registration state. It composes
// an ims_usrloc_pcscf storage backend and caches Service-Route per AOR.
type RegistrarPCSCFModule struct {
	mu            sync.RWMutex
	config        Config
	usrloc        *ims_usrloc_pcscf.UsrlocPCSCFModule
	serviceRoutes map[string][]string // AOR -> Service-Route URIs
}

// NewRegistrarPCSCFModule creates a module with default storage and config.
func NewRegistrarPCSCFModule() *RegistrarPCSCFModule {
	cfg := DefaultConfig()
	return &RegistrarPCSCFModule{
		config:        cfg,
		usrloc:        ims_usrloc_pcscf.NewUsrlocPCSCFModule(),
		serviceRoutes: make(map[string][]string),
	}
}

// NewRegistrarPCSCFModuleWithConfig creates a module with the supplied
// configuration. Storage is a default in-memory usrloc instance.
func NewRegistrarPCSCFModuleWithConfig(cfg Config) *RegistrarPCSCFModule {
	m := NewRegistrarPCSCFModule()
	m.config = cfg
	m.usrloc.SetConfig(ims_usrloc_pcscf.Config{
		MaxContacts:    cfg.MaxContacts,
		DefaultExpires: cfg.DefaultExpires,
		EnableNAT:      cfg.EnableNAT,
		PingInterval:   cfg.NATPingInterval,
	})
	return m
}

// SetUsrloc replaces the storage backend.
func (m *RegistrarPCSCFModule) SetUsrloc(u *ims_usrloc_pcscf.UsrlocPCSCFModule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u == nil {
		u = ims_usrloc_pcscf.NewUsrlocPCSCFModule()
	}
	m.usrloc = u
}

// Usrloc returns the underlying storage backend.
func (m *RegistrarPCSCFModule) Usrloc() *ims_usrloc_pcscf.UsrlocPCSCFModule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.usrloc
}

// SetRealm overrides the configured realm.
//
//	C: assign_realm()
func (m *RegistrarPCSCFModule) SetRealm(realm string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Realm = realm
}

// Realm returns the currently configured realm.
func (m *RegistrarPCSCFModule) Realm() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Realm
}

// SetConfig replaces the module configuration and propagates it to usrloc.
func (m *RegistrarPCSCFModule) SetConfig(cfg Config) {
	m.mu.Lock()
	m.config = cfg
	u := m.usrloc
	m.mu.Unlock()
	if u != nil {
		u.SetConfig(ims_usrloc_pcscf.Config{
			MaxContacts:    cfg.MaxContacts,
			DefaultExpires: cfg.DefaultExpires,
			EnableNAT:      cfg.EnableNAT,
			PingInterval:   cfg.NATPingInterval,
		})
	}
}

// Save processes a SIP REGISTER request, recording the local binding.
// On a deregistration (Expires: 0 or Contact: *) it removes the binding.
// The P-CSCF does not authenticate; it accepts the binding as presented
// and lets the S-CSCF challenge the request downstream.
//
//	C: save()
func (m *RegistrarPCSCFModule) Save(msg *parser.SIPMsg) (*SaveResult, error) {
	if msg == nil {
		return nil, errors.New("ims_registrar_pcscf: nil message")
	}
	if msg.Method() != parser.MethodRegister {
		return nil, errors.New("ims_registrar_pcscf: not a REGISTER")
	}
	impu := extractAOR(headerBody(msg, msg.To, parser.HdrTo))
	if impu == "" {
		return &SaveResult{StatusCode: 400, StatusReason: "Bad Request"}, nil
	}
	if isDeregistration(msg) {
		return m.handleDeregister(msg, impu)
	}

	contactURI := extractContactURI(headerBody(msg, msg.Contact, parser.HdrContact))
	if contactURI == "" {
		return &SaveResult{StatusCode: 400, StatusReason: "Bad Request"}, nil
	}
	expires := m.computeExpiry(msg)
	path := headerBody(msg, msg.Path, parser.HdrPath)
	contact := &ims_usrloc_pcscf.PCCContact{
		AOR:         impu,
		Contact:     contactURI,
		Path:        path,
		Expires:     expires,
		RegState:    ims_usrloc_pcscf.RegStateRegistered,
		LastRegTime: time.Now(),
		InstanceID:  extractInstanceID(headerBody(msg, msg.Contact, parser.HdrContact)),
	}
	if m.natEnabled() {
		contact.Received = extractReceived(msg)
	}
	if q, ok := extractQ(headerBody(msg, msg.Contact, parser.HdrContact)); ok {
		contact.Q = q
	}
	if err := m.saveContact(contact); err != nil {
		return &SaveResult{StatusCode: 503, StatusReason: "Service Unavailable"}, err
	}
	return &SaveResult{StatusCode: 200, StatusReason: "OK"}, nil
}

// SaveResponse processes a 200 OK received from the S-CSCF in response to
// a REGISTER. It refreshes the contact binding's expiry and caches the
// Service-Route header(s) for subsequent outbound requests.
//
//	C: save() / follow_save() on the reply path
func (m *RegistrarPCSCFModule) SaveResponse(msg *parser.SIPMsg) (*SaveResult, error) {
	if msg == nil {
		return nil, errors.New("ims_registrar_pcscf: nil message")
	}
	if !msg.IsReply() {
		return nil, errors.New("ims_registrar_pcscf: not a reply")
	}
	if msg.StatusCode() != 200 {
		return &SaveResult{StatusCode: msg.StatusCode(), StatusReason: statusReason(msg)}, nil
	}
	impu := extractAOR(headerBody(msg, msg.To, parser.HdrTo))
	if impu == "" {
		return &SaveResult{StatusCode: 200, StatusReason: "OK"}, nil
	}

	// Refresh the contact expiry from the 200 OK Contact header (if present).
	if msg.Contact != nil {
		contactURI := extractContactURI(msg.Contact.Body.String())
		if contactURI != "" && contactURI != "*" {
			expires := m.computeExpiry(msg)
			_ = m.updateContact(impu, contactURI, expires)
		}
	}

	// Cache Service-Route header(s).
	routes := m.extractServiceRoutes(msg)
	if len(routes) > 0 {
		m.storeServiceRoute(impu, routes)
	}
	return &SaveResult{StatusCode: 200, StatusReason: "OK"}, nil
}

// handleDeregister removes the binding and the cached Service-Route.
func (m *RegistrarPCSCFModule) handleDeregister(msg *parser.SIPMsg, impu string) (*SaveResult, error) {
	contactBody := headerBody(msg, msg.Contact, parser.HdrContact)
	if strings.TrimSpace(contactBody) == "*" {
		if err := m.removeAOR(impu); err != nil {
			return &SaveResult{StatusCode: 404, StatusReason: "Not Found"}, nil
		}
		m.clearServiceRoute(impu)
		return &SaveResult{StatusCode: 200, StatusReason: "OK"}, nil
	}
	contactURI := extractContactURI(contactBody)
	if contactURI == "" {
		return &SaveResult{StatusCode: 400, StatusReason: "Bad Request"}, nil
	}
	if err := m.removeContact(impu, contactURI); err != nil {
		return &SaveResult{StatusCode: 404, StatusReason: "Not Found"}, nil
	}
	return &SaveResult{StatusCode: 200, StatusReason: "OK"}, nil
}

// Lookup finds the contact bindings for the AOR in the request URI.
//
//	C: lookup()
func (m *RegistrarPCSCFModule) Lookup(msg *parser.SIPMsg) ([]string, error) {
	if msg == nil {
		return nil, errors.New("ims_registrar_pcscf: nil message")
	}
	aor := ""
	if msg.FirstLine != nil && msg.FirstLine.Req != nil {
		aor = msg.FirstLine.Req.URI.String()
	}
	if aor == "" {
		return nil, errors.New("ims_registrar_pcscf: no R-URI")
	}
	contacts := m.getContacts(aor)
	out := make([]string, 0, len(contacts))
	for _, c := range contacts {
		if c.RegState == ims_usrloc_pcscf.RegStateRegistered && !c.IsExpired() {
			out = append(out, c.Contact)
		}
	}
	return out, nil
}

// IsRegistered reports whether the given AOR has at least one
// non-expired registered contact.
//
//	C: is_registered()
func (m *RegistrarPCSCFModule) IsRegistered(impu string) bool {
	u := m.Usrloc()
	if u == nil {
		return false
	}
	return u.IsRegistered(impu)
}

// GetServiceRoute returns the cached Service-Route URIs for the given AOR,
// or nil when none have been recorded.
//
//	C: pcscf_get_service_route()
func (m *RegistrarPCSCFModule) GetServiceRoute(impu string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, len(m.serviceRoutes[impu]))
	copy(out, m.serviceRoutes[impu])
	return out
}

// RegFetchContacts returns a snapshot of all contacts for the given AOR.
//
//	C: reg_fetch_contacts()
func (m *RegistrarPCSCFModule) RegFetchContacts(aor string) []*ims_usrloc_pcscf.PCCContact {
	u := m.Usrloc()
	if u == nil {
		return nil
	}
	return u.GetContacts(aor)
}

// Deregister forcibly removes all bindings and cached Service-Route for
// the given AOR.
//
//	C: unregister() / delete_pcontact()
func (m *RegistrarPCSCFModule) Deregister(impu string) error {
	if err := m.removeAOR(impu); err != nil {
		return err
	}
	m.clearServiceRoute(impu)
	return nil
}

// ServiceRouteCount returns the number of AORs with a cached Service-Route.
func (m *RegistrarPCSCFModule) ServiceRouteCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.serviceRoutes)
}

// ---------------------------------------------------------------------------
// internal helpers (storage wrappers, kept lock-light)
// ---------------------------------------------------------------------------

func (m *RegistrarPCSCFModule) natEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.EnableNAT
}

func (m *RegistrarPCSCFModule) computeExpiry(msg *parser.SIPMsg) time.Time {
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

func (m *RegistrarPCSCFModule) saveContact(c *ims_usrloc_pcscf.PCCContact) error {
	u := m.Usrloc()
	if u == nil {
		return errors.New("ims_registrar_pcscf: no usrloc backend")
	}
	return u.SaveContact(c)
}

func (m *RegistrarPCSCFModule) removeContact(aor, contact string) error {
	u := m.Usrloc()
	if u == nil {
		return errors.New("ims_registrar_pcscf: no usrloc backend")
	}
	return u.RemoveContact(aor, contact)
}

func (m *RegistrarPCSCFModule) removeAOR(aor string) error {
	u := m.Usrloc()
	if u == nil {
		return errors.New("ims_registrar_pcscf: no usrloc backend")
	}
	return u.RemoveAOR(aor)
}

func (m *RegistrarPCSCFModule) updateContact(aor, contact string, expires time.Time) error {
	u := m.Usrloc()
	if u == nil {
		return errors.New("ims_registrar_pcscf: no usrloc backend")
	}
	return u.UpdateContact(aor, contact, expires)
}

func (m *RegistrarPCSCFModule) getContacts(aor string) []*ims_usrloc_pcscf.PCCContact {
	u := m.Usrloc()
	if u == nil {
		return nil
	}
	return u.GetContacts(aor)
}

func (m *RegistrarPCSCFModule) storeServiceRoute(impu string, routes []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.serviceRoutes == nil {
		m.serviceRoutes = make(map[string][]string)
	}
	cp := make([]string, len(routes))
	copy(cp, routes)
	m.serviceRoutes[impu] = cp
}

func (m *RegistrarPCSCFModule) clearServiceRoute(impu string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.serviceRoutes, impu)
}

// extractServiceRoutes returns the Service-Route URIs from all
// Service-Route headers on the message (there may be several).
func (m *RegistrarPCSCFModule) extractServiceRoutes(msg *parser.SIPMsg) []string {
	if msg == nil {
		return nil
	}
	hdrs := msg.GetAllHeadersByType(parser.HdrServiceRoute)
	var routes []string
	for _, h := range hdrs {
		if info, err := ims.ParseServiceRoute(h); err == nil && info != nil {
			routes = append(routes, info.URIs...)
		}
	}
	return routes
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

// extractInstanceID returns the +sip.instance parameter from a Contact
// header body, or the empty string when absent.
func extractInstanceID(body string) string {
	if body == "" {
		return ""
	}
	lower := strings.ToLower(body)
	idx := strings.Index(lower, "+sip.instance=")
	if idx < 0 {
		return ""
	}
	rest := body[idx+len("+sip.instance="):]
	rest = strings.TrimSpace(rest)
	if len(rest) == 0 {
		return ""
	}
	if rest[0] == '"' {
		end := strings.IndexByte(rest[1:], '"')
		if end >= 0 {
			return rest[1 : 1+end]
		}
	}
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		rest = rest[:semi]
	}
	return strings.TrimSpace(rest)
}

// extractQ returns the q-value parameter from a Contact header body.
func extractQ(body string) (float32, bool) {
	if body == "" {
		return 0, false
	}
	lower := strings.ToLower(body)
	idx := strings.Index(lower, "q=")
	if idx < 0 {
		return 0, false
	}
	rest := body[idx+len("q="):]
	end := 0
	for end < len(rest) {
		c := rest[end]
		if (c >= '0' && c <= '9') || c == '.' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return 0, false
	}
	var q float32
	if n, err := fmt.Sscanf(rest[:end], "%f", &q); err == nil && n == 1 {
		return q, true
	}
	return 0, false
}

// extractReceived returns the source IP:port from the top Via header's
// received/rport parameters, falling back to the Via host. Used to record
// the NAT'ed source for outbound NAT traversal.
func extractReceived(msg *parser.SIPMsg) string {
	if msg == nil || msg.Via1 == nil {
		return ""
	}
	via := msg.Via1
	if via.Received != nil && via.Received.Value.Len > 0 {
		host := via.Received.Value.String()
		if via.RPort != nil && via.RPort.Value.Len > 0 {
			return host + ":" + via.RPort.Value.String()
		}
		return host
	}
	host := via.Host.String()
	if via.Port > 0 {
		return fmt.Sprintf("%s:%d", host, via.Port)
	}
	return host
}

// statusReason extracts the reason phrase from a reply's first line.
func statusReason(msg *parser.SIPMsg) string {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Reply == nil {
		return ""
	}
	return msg.FirstLine.Reply.Reason.String()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultPM *RegistrarPCSCFModule
)

// DefaultPCSCFRegistrar returns the process-wide RegistrarPCSCFModule,
// creating one on first use.
func DefaultPCSCFRegistrar() *RegistrarPCSCFModule {
	defaultMu.RLock()
	r := defaultPM
	defaultMu.RUnlock()
	if r != nil {
		return r
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPM == nil {
		defaultPM = NewRegistrarPCSCFModule()
	}
	return defaultPM
}

// Init (re)initialises the process-wide RegistrarPCSCFModule to a fresh
// state, mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPM = NewRegistrarPCSCFModule()
}
