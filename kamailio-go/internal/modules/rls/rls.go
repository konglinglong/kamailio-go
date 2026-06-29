// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * rls module - Resource List Server.
 *
 * Port of the kamailio rls module (src/modules/rls). An RLSModule
 * manages back-end subscriptions on behalf of a watcher that subscribes
 * to a resource list (RFC 4662). Each RLSSubscription records the
 * watcher, the list of resource URIs being watched, an expiry and a
 * version number that is bumped whenever the resource states change.
 *
 * BuildRLSBody assembles the multipart/related NOTIFY body: an RLMI
 * (Resource List Meta-Information) part followed by one presence body
 * per resource.
 *
 * The module is safe for concurrent use.
 */
package rls

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// RLSSubscription represents a watcher's subscription to a resource
// list. ResourceList holds the URIs of the resources being watched,
// Expires is the absolute expiry time and Version is incremented on
// every state change.
type RLSSubscription struct {
	WatcherURI   string
	ResourceList []string
	Expires      time.Time
	Version      int
}

// RLSResource describes the state of a single resource within a NOTIFY
// body. Body carries the presence document for the resource.
type RLSResource struct {
	URI   string
	State string
	Body  string
}

// RLSModule stores resource-list subscriptions keyed by the watcher URI.
type RLSModule struct {
	mu   sync.RWMutex
	subs map[string]*RLSSubscription
}

// NewRLSModule creates an RLSModule with empty subscription storage.
func NewRLSModule() *RLSModule {
	return &RLSModule{subs: make(map[string]*RLSSubscription)}
}

// CreateSubscription registers a new subscription for watcherURI
// watching the given resources with expires (in seconds). The initial
// version is 1. If a subscription for the watcher already exists it is
// replaced.
func (m *RLSModule) CreateSubscription(watcherURI string, resources []string, expires int) *RLSSubscription {
	if expires <= 0 {
		expires = 3600
	}
	sub := &RLSSubscription{
		WatcherURI:   watcherURI,
		ResourceList: append([]string(nil), resources...),
		Expires:      time.Now().Add(time.Duration(expires) * time.Second),
		Version:      1,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[watcherURI] = sub
	return sub
}

// GetSubscription returns the subscription for watcherURI, or nil if
// none exists.
func (m *RLSModule) GetSubscription(watcherURI string) *RLSSubscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subs[watcherURI]
}

// UpdateSubscription records new resource states for the subscription
// identified by watcherURI, bumping its version. Returns an error if
// the subscription does not exist.
func (m *RLSModule) UpdateSubscription(watcherURI string, resources []RLSResource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sub, ok := m.subs[watcherURI]
	if !ok || sub == nil {
		return fmt.Errorf("rls: no subscription for %q", watcherURI)
	}
	sub.Version++
	return nil
}

// DeleteSubscription removes the subscription for watcherURI. Returns
// true if a subscription was removed.
func (m *RLSModule) DeleteSubscription(watcherURI string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.subs[watcherURI]; !ok {
		return false
	}
	delete(m.subs, watcherURI)
	return true
}

// Count returns the number of tracked subscriptions.
func (m *RLSModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.subs)
}

// List returns a snapshot of all subscriptions. The order is
// unspecified.
func (m *RLSModule) List() []*RLSSubscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*RLSSubscription, 0, len(m.subs))
	for _, s := range m.subs {
		out = append(out, s)
	}
	return out
}

// BuildRLSBody builds the multipart/related NOTIFY body for a NOTIFY to
// the watcher: an RLMI part describing every resource and its state,
// followed by one part per resource carrying its presence body. The
// subscription's version is embedded in the RLMI list element.
func (m *RLSModule) BuildRLSBody(sub *RLSSubscription, resources []RLSResource) string {
	if sub == nil {
		return ""
	}
	boundary := fmt.Sprintf("rls-boundary-%d", time.Now().UnixNano())
	var b strings.Builder

	// Self-describing Content-Type so the boundary is known to the
	// caller without a separate SIP header.
	b.WriteString(fmt.Sprintf(
		"Content-Type: multipart/related;boundary=\"%s\";type=\"application/rlmi+xml\"\r\n\r\n", boundary))

	// RLMI part.
	b.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	b.WriteString("Content-Type: application/rlmi+xml\r\n\r\n")
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\r\n")
	b.WriteString(fmt.Sprintf(
		`<list xmlns="urn:ietf:params:xml:ns:rlmi" uri="%s" version="%d" fullState="true">`+"\r\n",
		escapeXML(sub.WatcherURI), sub.Version))
	for i, r := range resources {
		cid := fmt.Sprintf("res%d@%d.rls", i+1, time.Now().UnixNano())
		state := r.State
		if state == "" {
			state = "active"
		}
		b.WriteString(fmt.Sprintf("  <resource uri=\"%s\">\r\n", escapeXML(r.URI)))
		b.WriteString(fmt.Sprintf("    <instance id=\"%d\" state=\"%s\">\r\n", i+1, escapeXML(state)))
		b.WriteString(fmt.Sprintf("      <cid>&lt;%s&gt;</cid>\r\n", cid))
		b.WriteString("    </instance>\r\n")
		b.WriteString("  </resource>\r\n")
	}
	b.WriteString("</list>\r\n")

	// One presence body per resource.
	for i, r := range resources {
		if r.Body == "" {
			continue
		}
		cid := fmt.Sprintf("res%d@%d.rls", i+1, time.Now().UnixNano())
		b.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		b.WriteString("Content-Type: application/pidf+xml\r\n")
		b.WriteString(fmt.Sprintf("Content-ID: &lt;%s&gt;\r\n\r\n", cid))
		b.WriteString(r.Body + "\r\n")
	}

	b.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	return b.String()
}

// CleanupExpired removes every subscription whose Expires time is in
// the past.
func (m *RLSModule) CleanupExpired() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for w, sub := range m.subs {
		if sub == nil || now.After(sub.Expires) {
			delete(m.subs, w)
		}
	}
}

// escapeXML escapes the standard XML special characters.
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu  sync.RWMutex
	defaultRLS *RLSModule
)

// DefaultRLS returns the process-wide RLSModule, creating one on first
// use.
func DefaultRLS() *RLSModule {
	defaultMu.RLock()
	r := defaultRLS
	defaultMu.RUnlock()
	if r != nil {
		return r
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRLS == nil {
		defaultRLS = NewRLSModule()
	}
	return defaultRLS
}

// Init (re)initialises the process-wide RLSModule to a fresh state,
// mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRLS = NewRLSModule()
}
