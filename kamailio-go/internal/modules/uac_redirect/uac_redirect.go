// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * UAC redirect module - processing of 3xx redirect replies.
 * Port of the kamailio uac_redirect module (src/modules/uac_redirect).
 *
 * The uac_redirect module processes 3xx redirect responses received by a
 * UAC: it extracts the Contact entries offered by the redirector, sorts
 * them by q-value, selects the best target and reports the redirect type
 * (301, 302, 305, ...).
 *
 * It is safe for concurrent use: the module holds no mutable state after
 * construction and the process-wide singleton is guarded by a mutex.
 */

package uac_redirect

import (
	"sort"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// RedirectEntry describes a single Contact offered by a 3xx redirect.
type RedirectEntry struct {
	Contact string
	Q       float64
	Expires int
	Flags   int
}

// UACRedirectModule implements the uac_redirect module functionality.
// C: struct module uac_redirect
type UACRedirectModule struct {
	mu sync.RWMutex
}

// New creates a UACRedirectModule instance.
func New() *UACRedirectModule {
	return &UACRedirectModule{}
}

// ProcessRedirect extracts the Contact entries from a 3xx redirect reply
// and returns them as RedirectEntry values sorted by descending q-value.
// Returns an empty slice when msg is nil, is not a reply, is not a 3xx, or
// has no Contact header.
//
//	C: redirect() / get_redirect()
func (m *UACRedirectModule) ProcessRedirect(msg *parser.SIPMsg) []*RedirectEntry {
	out := []*RedirectEntry{}
	if msg == nil || !m.IsRedirect(msg) || msg.Contact == nil {
		return out
	}
	contacts, err := parser.ParseContactList(msg.Contact.Body)
	if err != nil || len(contacts) == 0 {
		return out
	}
	for _, c := range contacts {
		entry := &RedirectEntry{
			Contact: c.GetURI(),
			Q:       float64(c.QValue),
			Expires: c.Expires,
		}
		out = append(out, entry)
	}
	// Return them sorted by q-value, matching the C module's behaviour of
	// presenting the highest-q target first.
	return m.SortByQ(out)
}

// SelectBest returns the entry with the highest q-value, or nil when the
// slice is empty. Ties are broken in favour of the earlier entry.
//
//	C: select_best() analogue
func (m *UACRedirectModule) SelectBest(entries []*RedirectEntry) *RedirectEntry {
	if len(entries) == 0 {
		return nil
	}
	best := entries[0]
	for _, e := range entries[1:] {
		if e.Q > best.Q {
			best = e
		}
	}
	return best
}

// SortByQ returns a copy of entries sorted by descending q-value. Entries
// with equal q-values keep their relative order (stable sort).
//
//	C: sort_contacts() analogue
func (m *UACRedirectModule) SortByQ(entries []*RedirectEntry) []*RedirectEntry {
	out := make([]*RedirectEntry, len(entries))
	copy(out, entries)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Q > out[j].Q
	})
	return out
}

// Count returns the number of entries in the slice.
//
//	C: contacts_count() analogue
func (m *UACRedirectModule) Count(entries []*RedirectEntry) int {
	return len(entries)
}

// IsRedirect reports whether msg is a 3xx redirect reply (status code in
// the range 300..399).
//
//	C: is_redirect() analogue
func (m *UACRedirectModule) IsRedirect(msg *parser.SIPMsg) bool {
	if msg == nil {
		return false
	}
	code := msg.StatusCode()
	return code >= 300 && code < 400
}

// GetRedirectType returns the status code of a 3xx redirect reply (e.g.
// 301, 302, 305), or 0 when msg is not a redirect.
//
//	C: get_redirect_type() analogue
func (m *UACRedirectModule) GetRedirectType(msg *parser.SIPMsg) int {
	if !m.IsRedirect(msg) {
		return 0
	}
	return int(msg.StatusCode())
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu          sync.RWMutex
	defaultUACRedirect *UACRedirectModule
)

// DefaultUACRedirect returns the process-wide UACRedirectModule, creating it
// on first use.
func DefaultUACRedirect() *UACRedirectModule {
	defaultMu.RLock()
	m := defaultUACRedirect
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultUACRedirect == nil {
		defaultUACRedirect = New()
	}
	return defaultUACRedirect
}

// Init (re)initialises the process-wide UACRedirectModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultUACRedirect = New()
}
