// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS usrloc P-CSCF module - contact registration management for the
 * Proxy-CSCF.
 * Port of the kamailio ims_usrloc_pcscf module (src/modules/ims_usrloc_pcscf).
 *
 * The P-CSCF is the first contact point for the UE in the IMS core. It
 * stores the registration bindings learned from REGISTER requests so
 * that subsequent requests for the same Address of Record can be routed
 * to the registered contact. Each AOR may carry multiple contacts with
 * independent expiry, q-value, Path header and GRUU instance id.
 *
 * It is safe for concurrent use.
 */

package ims_usrloc_pcscf

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

// Config holds the P-CSCF usrloc configuration.
type Config struct {
	MaxContacts    int
	DefaultExpires int
	DBUrl          string
	EnableNAT      bool
	PingInterval   int
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		MaxContacts:    10,
		DefaultExpires: 3600,
		DBUrl:          "",
		EnableNAT:      true,
		PingInterval:   60,
	}
}

// PCCContact represents a single P-CSCF registration binding.
type PCCContact struct {
	AOR        string // Address of Record
	Contact    string // Contact URI
	Received   string // received IP:port
	Path       string // Path header
	Expires    time.Time
	Q          float32
	InstanceID string // GRUU +sip.instance
	RegState   string // "registered", "unregistered", "expired"
	LastRegTime time.Time
	Flags      uint32
}

// IsExpired returns true when the contact has passed its expiry time.
func (c *PCCContact) IsExpired() bool {
	if c.Expires.IsZero() {
		return false
	}
	return time.Now().After(c.Expires)
}

// UsrlocPCSCFModule maintains the P-CSCF contact bindings keyed by AOR.
type UsrlocPCSCFModule struct {
	mu       sync.RWMutex
	config   Config
	contacts map[string][]*PCCContact // AOR -> contacts
}

// NewUsrlocPCSCFModule creates a UsrlocPCSCFModule with the default
// configuration and empty contact storage.
func NewUsrlocPCSCFModule() *UsrlocPCSCFModule {
	return &UsrlocPCSCFModule{
		config:   DefaultConfig(),
		contacts: make(map[string][]*PCCContact),
	}
}

// NewUsrlocPCSCFModuleWithConfig creates a UsrlocPCSCFModule with the
// supplied configuration.
func NewUsrlocPCSCFModuleWithConfig(cfg Config) *UsrlocPCSCFModule {
	m := NewUsrlocPCSCFModule()
	m.config = cfg
	return m
}

// SetConfig replaces the module configuration.
func (m *UsrlocPCSCFModule) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
}

// GetConfig returns a copy of the current configuration.
func (m *UsrlocPCSCFModule) GetConfig() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// SaveContact adds or updates a contact binding. When a contact with the
// same Contact URI already exists for the AOR it is replaced. The
// MaxContacts limit is enforced for new AORs. Returns an error when the
// AOR or Contact URI is empty.
//
//	C: save_contacts() / insert_pcontact()
func (m *UsrlocPCSCFModule) SaveContact(contact *PCCContact) error {
	if contact == nil {
		return errors.New("ims_usrloc_pcscf: nil contact")
	}
	if contact.AOR == "" {
		return errors.New("ims_usrloc_pcscf: empty AOR")
	}
	if contact.Contact == "" {
		return errors.New("ims_usrloc_pcscf: empty contact URI")
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
		m.contacts = make(map[string][]*PCCContact)
	}
	list := m.contacts[contact.AOR]
	for i, c := range list {
		if c.Contact == contact.Contact {
			list[i] = contact
			return nil
		}
	}
	if m.config.MaxContacts > 0 && len(list) >= m.config.MaxContacts {
		return fmt.Errorf("ims_usrloc_pcscf: max contacts (%d) reached for AOR %q", m.config.MaxContacts, contact.AOR)
	}
	m.contacts[contact.AOR] = append(list, contact)
	return nil
}

// GetContacts returns a snapshot of the contacts for the given AOR.
func (m *UsrlocPCSCFModule) GetContacts(aor string) []*PCCContact {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.contacts[aor]
	out := make([]*PCCContact, len(list))
	copy(out, list)
	return out
}

// RemoveContact removes the contact identified by its URI from the AOR.
// Returns an error when no such contact exists.
func (m *UsrlocPCSCFModule) RemoveContact(aor, contact string) error {
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
	return fmt.Errorf("ims_usrloc_pcscf: no contact %q for AOR %q", contact, aor)
}

// RemoveAOR removes all contacts for the given AOR. Returns true when
// the AOR existed.
func (m *UsrlocPCSCFModule) RemoveAOR(aor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.contacts[aor]; !ok {
		return fmt.Errorf("ims_usrloc_pcscf: no AOR %q", aor)
	}
	delete(m.contacts, aor)
	return nil
}

// UpdateContact sets the expiry time of the contact identified by its
// URI within the AOR. Returns an error when no such contact exists.
func (m *UsrlocPCSCFModule) UpdateContact(aor, contact string, expires time.Time) error {
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
	return fmt.Errorf("ims_usrloc_pcscf: no contact %q for AOR %q", contact, aor)
}

// CleanupExpired removes all expired contacts and marks their state as
// expired before deletion. Returns the number of removed contacts.
//
//	C: timer_pcontact()
func (m *UsrlocPCSCFModule) CleanupExpired() int {
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

// IsRegistered reports whether the given AOR has at least one non-expired
// registered contact.
func (m *UsrlocPCSCFModule) IsRegistered(aor string) bool {
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
func (m *UsrlocPCSCFModule) GetAORList() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.contacts))
	for aor := range m.contacts {
		out = append(out, aor)
	}
	return out
}

// AORCount returns the number of AORs tracked.
func (m *UsrlocPCSCFModule) AORCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.contacts)
}

// HandleRegister processes a SIP REGISTER request, extracting the AOR
// from the To header and the contact binding from the Contact header.
// Returns the SIP status code (200 on success, 4xx/5xx on error).
//
//	C: save_contacts() / registrar PCSCF
func (m *UsrlocPCSCFModule) HandleRegister(msg *parser.SIPMsg) (int, error) {
	if msg == nil {
		return 400, errors.New("ims_usrloc_pcscf: nil message")
	}
	if msg.Method() != parser.MethodRegister {
		return 405, errors.New("ims_usrloc_pcscf: not a REGISTER")
	}
	aor := extractAOR(headerBody(msg, msg.To, parser.HdrTo))
	if aor == "" {
		return 400, errors.New("ims_usrloc_pcscf: no AOR in To header")
	}
	contactURI := extractContactURI(headerBody(msg, msg.Contact, parser.HdrContact))
	if contactURI == "" {
		return 400, errors.New("ims_usrloc_pcscf: no Contact header")
	}
	expires := m.defaultExpiry()
	if msg.Expires != nil {
		if eb, err := parser.ParseExpiresBody(msg.Expires.Body); err == nil {
			expires = time.Now().Add(time.Duration(eb.Value) * time.Second)
		}
	}
	path := headerBody(msg, msg.Path, parser.HdrPath)
	contact := &PCCContact{
		AOR:        aor,
		Contact:    contactURI,
		Path:       path,
		Expires:    expires,
		RegState:   RegStateRegistered,
		LastRegTime: time.Now(),
	}
	if err := m.SaveContact(contact); err != nil {
		return 503, err
	}
	return 200, nil
}

// HandleUnregister processes a REGISTER with Expires: 0 (or Contact: *),
// removing the binding. Returns the SIP status code.
//
//	C: unregister() / delete_pcontact()
func (m *UsrlocPCSCFModule) HandleUnregister(msg *parser.SIPMsg) (int, error) {
	if msg == nil {
		return 400, errors.New("ims_usrloc_pcscf: nil message")
	}
	if msg.Method() != parser.MethodRegister {
		return 405, errors.New("ims_usrloc_pcscf: not a REGISTER")
	}
	aor := extractAOR(headerBody(msg, msg.To, parser.HdrTo))
	if aor == "" {
		return 400, errors.New("ims_usrloc_pcscf: no AOR in To header")
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
		return 400, errors.New("ims_usrloc_pcscf: no Contact header")
	}
	if err := m.RemoveContact(aor, contactURI); err != nil {
		return 404, err
	}
	return 200, nil
}

func (m *UsrlocPCSCFModule) defaultExpiry() time.Time {
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

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultPM *UsrlocPCSCFModule
)

// DefaultUsrlocPCSCF returns the process-wide UsrlocPCSCFModule,
// creating one on first use.
func DefaultUsrlocPCSCF() *UsrlocPCSCFModule {
	defaultMu.RLock()
	p := defaultPM
	defaultMu.RUnlock()
	if p != nil {
		return p
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPM == nil {
		defaultPM = NewUsrlocPCSCFModule()
	}
	return defaultPM
}

// Init (re)initialises the process-wide UsrlocPCSCFModule to a fresh
// state, mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPM = NewUsrlocPCSCFModule()
}
