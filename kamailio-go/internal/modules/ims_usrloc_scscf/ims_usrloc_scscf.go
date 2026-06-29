// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS usrloc S-CSCF module - contact registration and Cx interface data
 * management for the Serving-CSCF.
 * Port of the kamailio ims_usrloc_scscf module (src/modules/ims_usrloc_scscf).
 *
 * The S-CSCF holds the authoritative registration bindings for a
 * subscriber. In addition to the contact bindings (keyed by AOR) it
 * stores the subscriber's user profile as received from the HSS over the
 * Cx reference point: the IMS Public/Private identities, the service
 * profile and the Initial Filter Criteria used to route sessions to
 * Application Servers. Each contact carries the IMS-specific fields
 * (IMPI/IMPU, charging identifier, Service-Route and associated URIs).
 *
 * It is safe for concurrent use.
 */

package ims_usrloc_scscf

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Registration state values.
const (
	RegStateRegistered   = "registered"
	RegStateUnregistered = "unregistered"
	RegStateExpired      = "expired"
)

// Default IFC default handling values.
const (
	DefaultHandlingContinue  = "continue"
	DefaultHandlingTerminate = "terminate"
)

// Config holds the S-CSCF usrloc configuration.
type Config struct {
	MaxContacts     int
	DefaultExpires  int
	DBUrl           string
	EnableCharging  bool
	CxTimeout       time.Duration
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		MaxContacts:    10,
		DefaultExpires: 3600,
		DBUrl:          "",
		EnableCharging: true,
		CxTimeout:      5 * time.Second,
	}
}

// IFC describes a single Initial Filter Criteria entry (3GPP TS 23.218).
type IFC struct {
	Priority        int
	TriggerPoint    string
	ApplicationServer string
	DefaultHandling string
}

// UserProfile holds the subscriber profile received from the HSS over Cx.
type UserProfile struct {
	IMPU                string // IMS Public Identity
	IMPI                string // IMS Private Identity
	ServiceProfile      string
	InitialFilterCriteria []IFC
	ChargingInfo        string
}

// SCCContact represents a single S-CSCF registration binding.
type SCCContact struct {
	AOR           string
	Contact       string
	Received      string
	Path          string
	Expires       time.Time
	Q             float32
	InstanceID    string
	RegState      string
	LastRegTime   time.Time
	IMSPrivateID  string   // IMPI
	IMPublicID    string   // IMPU
	ChargingID    string
	ServiceRoute  []string
	AssociatedURI []string
}

// IsExpired returns true when the contact has passed its expiry time.
func (c *SCCContact) IsExpired() bool {
	if c.Expires.IsZero() {
		return false
	}
	return time.Now().After(c.Expires)
}

// UsrlocSCSCFModule maintains the S-CSCF contact bindings and the
// subscriber user profiles received from the HSS.
type UsrlocSCSCFModule struct {
	mu           sync.RWMutex
	config       Config
	contacts     map[string][]*SCCContact // AOR -> contacts
	userProfiles map[string]*UserProfile  // IMPU -> profile
}

// NewUsrlocSCSCFModule creates a UsrlocSCSCFModule with the default
// configuration and empty storage.
func NewUsrlocSCSCFModule() *UsrlocSCSCFModule {
	return &UsrlocSCSCFModule{
		config:       DefaultConfig(),
		contacts:     make(map[string][]*SCCContact),
		userProfiles: make(map[string]*UserProfile),
	}
}

// NewUsrlocSCSCFModuleWithConfig creates a UsrlocSCSCFModule with the
// supplied configuration.
func NewUsrlocSCSCFModuleWithConfig(cfg Config) *UsrlocSCSCFModule {
	m := NewUsrlocSCSCFModule()
	m.config = cfg
	return m
}

// SetConfig replaces the module configuration.
func (m *UsrlocSCSCFModule) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
}

// GetConfig returns a copy of the current configuration.
func (m *UsrlocSCSCFModule) GetConfig() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// SaveContact adds or updates a contact binding. When a contact with the
// same Contact URI already exists for the AOR it is replaced. The
// MaxContacts limit is enforced for new contacts. Returns an error when
// the AOR or Contact URI is empty.
//
//	C: insert_scontact() / save_contacts()
func (m *UsrlocSCSCFModule) SaveContact(contact *SCCContact) error {
	if contact == nil {
		return errors.New("ims_usrloc_scscf: nil contact")
	}
	if contact.AOR == "" {
		return errors.New("ims_usrloc_scscf: empty AOR")
	}
	if contact.Contact == "" {
		return errors.New("ims_usrloc_scscf: empty contact URI")
	}
	if contact.RegState == "" {
		contact.RegState = RegStateRegistered
	}
	if contact.LastRegTime.IsZero() {
		contact.LastRegTime = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.contacts == nil {
		m.contacts = make(map[string][]*SCCContact)
	}
	list := m.contacts[contact.AOR]
	for i, c := range list {
		if c.Contact == contact.Contact {
			list[i] = contact
			return nil
		}
	}
	if m.config.MaxContacts > 0 && len(list) >= m.config.MaxContacts {
		return fmt.Errorf("ims_usrloc_scscf: max contacts (%d) reached for AOR %q", m.config.MaxContacts, contact.AOR)
	}
	m.contacts[contact.AOR] = append(list, contact)
	return nil
}

// GetContacts returns a snapshot of the contacts for the given AOR.
func (m *UsrlocSCSCFModule) GetContacts(aor string) []*SCCContact {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.contacts[aor]
	out := make([]*SCCContact, len(list))
	copy(out, list)
	return out
}

// RemoveContact removes the contact identified by its URI from the AOR.
// Returns an error when no such contact exists.
func (m *UsrlocSCSCFModule) RemoveContact(aor, contact string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.contacts[aor]
	for i, c := range list {
		if c.Contact == contact {
			m.contacts[aor] = append(list[:i], list[i+1:]...)
			if len(m.contacts[aor]) == 0 {
				delete(m.contacts, aor)
			}
			return nil
		}
	}
	return fmt.Errorf("ims_usrloc_scscf: no contact %q for AOR %q", contact, aor)
}

// RemoveAOR removes all contacts for the given AOR. Returns an error
// when the AOR does not exist.
func (m *UsrlocSCSCFModule) RemoveAOR(aor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.contacts[aor]; !ok {
		return fmt.Errorf("ims_usrloc_scscf: no AOR %q", aor)
	}
	delete(m.contacts, aor)
	return nil
}

// UpdateContact sets the expiry time of the contact identified by its
// URI within the AOR. Returns an error when no such contact exists.
func (m *UsrlocSCSCFModule) UpdateContact(aor, contact string, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.contacts[aor] {
		if c.Contact == contact {
			c.Expires = expires
			c.LastRegTime = time.Now()
			if expires.IsZero() || time.Now().After(expires) {
				c.RegState = RegStateExpired
			} else {
				c.RegState = RegStateRegistered
			}
			return nil
		}
	}
	return fmt.Errorf("ims_usrloc_scscf: no contact %q for AOR %q", contact, aor)
}

// CleanupExpired removes all expired contacts and marks their state as
// expired before deletion. Returns the number of removed contacts.
//
//	C: timer_impurecord()
func (m *UsrlocSCSCFModule) CleanupExpired() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	purged := 0
	for aor, list := range m.contacts {
		remaining := list[:0]
		for _, c := range list {
			if !c.Expires.IsZero() && now.After(c.Expires) {
				c.RegState = RegStateExpired
				purged++
				continue
			}
			remaining = append(remaining, c)
		}
		if len(remaining) == 0 {
			delete(m.contacts, aor)
		} else {
			m.contacts[aor] = remaining
		}
	}
	return purged
}

// IsRegistered reports whether the given AOR has at least one
// non-expired registered contact.
func (m *UsrlocSCSCFModule) IsRegistered(aor string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.contacts[aor] {
		if c.RegState == RegStateRegistered && !c.IsExpired() {
			return true
		}
	}
	return false
}

// GetAORList returns a snapshot of all registered AORs.
func (m *UsrlocSCSCFModule) GetAORList() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.contacts))
	for aor := range m.contacts {
		out = append(out, aor)
	}
	return out
}

// AORCount returns the number of AORs tracked.
func (m *UsrlocSCSCFModule) AORCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.contacts)
}

// HandleRegister processes a SIP REGISTER request, extracting the AOR
// from the To header and the contact binding from the Contact header.
// Returns the SIP status code (200 on success, 4xx/5xx on error).
//
//	C: save_contacts() / S-CSCF registrar
func (m *UsrlocSCSCFModule) HandleRegister(msg *parser.SIPMsg) (int, error) {
	if msg == nil {
		return 400, errors.New("ims_usrloc_scscf: nil message")
	}
	if msg.Method() != parser.MethodRegister {
		return 405, errors.New("ims_usrloc_scscf: not a REGISTER")
	}
	aor := extractAOR(headerBody(msg, msg.To, parser.HdrTo))
	if aor == "" {
		return 400, errors.New("ims_usrloc_scscf: no AOR in To header")
	}
	contactURI := extractContactURI(headerBody(msg, msg.Contact, parser.HdrContact))
	if contactURI == "" {
		return 400, errors.New("ims_usrloc_scscf: no Contact header")
	}
	expires := m.defaultExpiry()
	if msg.Expires != nil {
		if eb, err := parser.ParseExpiresBody(msg.Expires.Body); err == nil {
			expires = time.Now().Add(time.Duration(eb.Value) * time.Second)
		}
	}
	path := headerBody(msg, msg.Path, parser.HdrPath)
	impi := extractAuthUser(headerBody(msg, msg.Authorization, parser.HdrAuthorization))
	contact := &SCCContact{
		AOR:          aor,
		Contact:      contactURI,
		Path:         path,
		Expires:      expires,
		RegState:     RegStateRegistered,
		LastRegTime:  time.Now(),
		IMPublicID:   aor,
		IMSPrivateID: impi,
		ServiceRoute: []string{},
	}
	if err := m.SaveContact(contact); err != nil {
		return 503, err
	}
	return 200, nil
}

// HandleUnregister processes a REGISTER with Expires: 0 (or Contact: *),
// removing the binding. Returns the SIP status code.
//
//	C: unregister() / delete_scontact()
func (m *UsrlocSCSCFModule) HandleUnregister(msg *parser.SIPMsg) (int, error) {
	if msg == nil {
		return 400, errors.New("ims_usrloc_scscf: nil message")
	}
	if msg.Method() != parser.MethodRegister {
		return 405, errors.New("ims_usrloc_scscf: not a REGISTER")
	}
	aor := extractAOR(headerBody(msg, msg.To, parser.HdrTo))
	if aor == "" {
		return 400, errors.New("ims_usrloc_scscf: no AOR in To header")
	}
	contactBody := headerBody(msg, msg.Contact, parser.HdrContact)
	if strings.TrimSpace(contactBody) == "*" {
		if err := m.RemoveAOR(aor); err != nil {
			return 404, err
		}
		return 200, nil
	}
	contactURI := extractContactURI(contactBody)
	if contactURI == "" {
		return 400, errors.New("ims_usrloc_scscf: no Contact header")
	}
	if err := m.RemoveContact(aor, contactURI); err != nil {
		return 404, err
	}
	return 200, nil
}

// GetUserProfile returns the user profile for the given IMPU, or nil.
//
//	C: Cx SAA / get_impurecord()
func (m *UsrlocSCSCFModule) GetUserProfile(impu string) *UserProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.userProfiles[impu]
}

// SaveUserProfile stores or replaces the user profile for the IMPU
// carried on the profile. Returns an error when the profile is nil or
// has no IMPU.
//
//	C: Cx SAR/SAA / update_impurecord()
func (m *UsrlocSCSCFModule) SaveUserProfile(profile *UserProfile) error {
	if profile == nil {
		return errors.New("ims_usrloc_scscf: nil profile")
	}
	if profile.IMPU == "" {
		return errors.New("ims_usrloc_scscf: empty IMPU")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.userProfiles == nil {
		m.userProfiles = make(map[string]*UserProfile)
	}
	m.userProfiles[profile.IMPU] = profile
	return nil
}

// GetIFCList returns the Initial Filter Criteria for the given IMPU,
// sorted by priority (ascending). Returns nil when no profile exists.
func (m *UsrlocSCSCFModule) GetIFCList(impu string) []IFC {
	p := m.GetUserProfile(impu)
	if p == nil {
		return nil
	}
	out := make([]IFC, len(p.InitialFilterCriteria))
	copy(out, p.InitialFilterCriteria)
	// Sort by priority ascending (highest precedence first).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Priority > out[j].Priority; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// ProfileCount returns the number of stored user profiles.
func (m *UsrlocSCSCFModule) ProfileCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.userProfiles)
}

func (m *UsrlocSCSCFModule) defaultExpiry() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	secs := m.config.DefaultExpires
	if secs <= 0 {
		secs = 3600
	}
	return time.Now().Add(time.Duration(secs) * time.Second)
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// headerBody returns the body string of a header, looking it up by quick
// reference first, then by type.
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

// extractAuthUser returns the username from a Digest Authorization
// header body, or the empty string when absent.
func extractAuthUser(body string) string {
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

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultSM *UsrlocSCSCFModule
)

// DefaultUsrlocSCSCF returns the process-wide UsrlocSCSCFModule,
// creating one on first use.
func DefaultUsrlocSCSCF() *UsrlocSCSCFModule {
	defaultMu.RLock()
	s := defaultSM
	defaultMu.RUnlock()
	if s != nil {
		return s
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSM == nil {
		defaultSM = NewUsrlocSCSCFModule()
	}
	return defaultSM
}

// Init (re)initialises the process-wide UsrlocSCSCFModule to a fresh
// state, mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSM = NewUsrlocSCSCFModule()
}
