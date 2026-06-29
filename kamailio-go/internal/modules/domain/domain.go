// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Domain module - local domain registry with per-domain attributes.
 *
 * Port of the kamailio domain module (src/modules/domain). The module
 * keeps the set of domains the local server is responsible for
 * (Kamailio domain table) and a set of named attributes keyed by domain
 * identifier (did) (Kamailio domain_attrs table). IsDomainKnown answers
 * whether a host is one of the local domains.
 *
 * The package is safe for concurrent use.
 */
package domain

import (
	"strings"
	"sync"
	"time"
)

// DomainEntry is one local domain (Kamailio domain table row). Did is the
// domain identifier used to link attributes.
type DomainEntry struct {
	Domain       string
	Did          string
	LastModified time.Time
}

// DomainAttr is one domain attribute (Kamailio domain_attrs table row).
// Type is the attribute value type (0 = string).
type DomainAttr struct {
	Did   string
	Name  string
	Type  int
	Value string
}

// attrKey is the composite key for (did, name).
type attrKey struct {
	did  string
	name string
}

// DomainModule implements the domain module. It is safe for concurrent
// use: all state is guarded by mu.
type DomainModule struct {
	mu       sync.RWMutex
	domains  map[string]*DomainEntry // domain name -> entry
	attrs    map[attrKey]*DomainAttr
	nextID   int
	nextAttr int
}

// NewDomainModule creates a new DomainModule.
func NewDomainModule() *DomainModule {
	return &DomainModule{
		domains: make(map[string]*DomainEntry),
		attrs:   make(map[attrKey]*DomainAttr),
	}
}

// AddDomain registers a local domain with the given did. The domain name
// is matched case-insensitively. Returns the assigned internal ID, or -1
// if domain is empty or already registered.
func (m *DomainModule) AddDomain(domain string, did string) int {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.domains[domain]; exists {
		return -1
	}
	m.nextID++
	id := m.nextID
	m.domains[domain] = &DomainEntry{
		Domain:       domain,
		Did:          did,
		LastModified: time.Now(),
	}
	return id
}

// RemoveDomain deletes the local domain with the given name (matched
// case-insensitively) and any attributes keyed by its did. Returns true
// when a domain was removed.
func (m *DomainModule) RemoveDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.domains[domain]
	if !ok {
		return false
	}
	delete(m.domains, domain)
	// Drop attributes belonging to this domain's did.
	for k := range m.attrs {
		if k.did == entry.Did {
			delete(m.attrs, k)
		}
	}
	return true
}

// IsDomainKnown reports whether domain is a registered local domain
// (matched case-insensitively).
func (m *DomainModule) IsDomainKnown(domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.domains[domain]
	return ok
}

// GetDomain returns a copy of the entry for domain (matched
// case-insensitively), or nil if it is not registered.
func (m *DomainModule) GetDomain(domain string) *DomainEntry {
	domain = strings.ToLower(strings.TrimSpace(domain))
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.domains[domain]
	if !ok {
		return nil
	}
	cp := *entry
	return &cp
}

// SetAttr sets the attribute (did, name) to value, creating it when new
// or updating it when it exists. Returns the assigned internal ID, or -1
// if did or name is empty.
func (m *DomainModule) SetAttr(did string, name string, value string) int {
	did = strings.TrimSpace(did)
	name = strings.TrimSpace(name)
	if did == "" || name == "" {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := attrKey{did, name}
	if _, exists := m.attrs[key]; !exists {
		m.nextAttr++
	}
	m.attrs[key] = &DomainAttr{
		Did:   did,
		Name:  name,
		Type:  0,
		Value: value,
	}
	return m.nextAttr
}

// GetAttr returns the value of the attribute (did, name). The boolean
// result is false when no such attribute exists.
func (m *DomainModule) GetAttr(did string, name string) (string, bool) {
	did = strings.TrimSpace(did)
	name = strings.TrimSpace(name)
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.attrs[attrKey{did, name}]
	if !ok {
		return "", false
	}
	return a.Value, true
}

// ListDomains returns copies of all registered domain entries, sorted by
// domain name for deterministic ordering.
func (m *DomainModule) ListDomains() []*DomainEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*DomainEntry, 0, len(m.domains))
	for _, e := range m.domains {
		cp := *e
		out = append(out, &cp)
	}
	// Sort by domain name for deterministic output.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Domain > out[j].Domain; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// Count returns the number of registered domains.
func (m *DomainModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.domains)
}

// Reload drops all in-memory domains and attributes, mirroring Kamailio's
// reload that re-reads the domain tables from the database. After Reload
// the module is empty until repopulated.
func (m *DomainModule) Reload() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.domains = make(map[string]*DomainEntry)
	m.attrs = make(map[attrKey]*DomainAttr)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultDM *DomainModule
)

// DefaultDomain returns the process-wide DomainModule, creating one on
// first use.
func DefaultDomain() *DomainModule {
	defaultMu.RLock()
	d := defaultDM
	defaultMu.RUnlock()
	if d != nil {
		return d
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDM == nil {
		defaultDM = NewDomainModule()
	}
	return defaultDM
}

// Init (re)initialises the process-wide DomainModule to a fresh state,
// mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDM = NewDomainModule()
}
