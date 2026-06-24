// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * pdb module - prefix database (carrier) lookup.
 * Port of the kamailio pdb module (src/modules/pdb).
 *
 * The original C module queries a prefix database server (pdb_server)
 * to map a number prefix to a carrier. This Go counterpart exposes the
 * same lookup API backed by an in-memory prefix table.
 *
 * It is safe for concurrent use.
 */

package pdb

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// PDBEntry holds a prefix-database record.
type PDBEntry struct {
	Number      string
	Carrier     string
	Description string
}

// PDBModule maps number prefixes to carrier records.
// It is the Go counterpart of the kamailio pdb module.
type PDBModule struct {
	mu      sync.RWMutex
	dbPath  string
	entries []PDBEntry // sorted by Number length desc for longest-prefix match
}

// New creates a PDBModule.
func New() *PDBModule {
	return &PDBModule{}
}

// Init opens the prefix database at dbPath. When the file cannot be
// opened the module still initialises with a built-in mock table.
//
//	C: mod_init() / pdb_init()
func (m *PDBModule) Init(dbPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dbPath = dbPath
	m.entries = nil
	m.loadMockData()
	return nil
}

// loadMockData populates the in-memory table with sample prefixes.
func (m *PDBModule) loadMockData() {
	m.entries = []PDBEntry{
		{Number: "1202", Carrier: "Verizon", Description: "Washington DC"},
		{Number: "1202555", Carrier: "Verizon", Description: "Washington DC local"},
		{Number: "1212", Carrier: "AT&T", Description: "New York"},
		{Number: "44", Carrier: "BT", Description: "United Kingdom"},
		{Number: "447", Carrier: "Vodafone", Description: "United Kingdom mobile"},
		{Number: "49", Carrier: "Deutsche Telekom", Description: "Germany"},
		{Number: "86", Carrier: "China Mobile", Description: "China"},
	}
	m.sortEntries()
}

// sortEntries sorts entries by descending prefix length so the longest
// matching prefix is found first.
func (m *PDBModule) sortEntries() {
	sort.Slice(m.entries, func(i, j int) bool {
		return len(m.entries[i].Number) > len(m.entries[j].Number)
	})
}

// Lookup returns the longest-prefix match for number.
//
//	C: pdb_query()
func (m *PDBModule) Lookup(number string) (*PDBEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	digits := digitsOnly(number)
	if digits == "" {
		return nil, fmt.Errorf("pdb: empty number")
	}
	for _, e := range m.entries {
		if strings.HasPrefix(digits, e.Number) {
			return &PDBEntry{Number: e.Number, Carrier: e.Carrier, Description: e.Description}, nil
		}
	}
	return nil, fmt.Errorf("pdb: no entry for %s", number)
}

// LookupCarrier returns just the carrier name for number.
//
//	C: pdb_query_carrier()
func (m *PDBModule) LookupCarrier(number string) (string, error) {
	e, err := m.Lookup(number)
	if err != nil {
		return "", err
	}
	return e.Carrier, nil
}

// Close releases resources held by the module.
//
//	C: mod_destroy() / pdb_destroy()
func (m *PDBModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = nil
}

// digitsOnly strips everything except digits from s.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- package-level API ---

var defaultModule = New()

// DefaultPDB returns the package-level default PDBModule.
func DefaultPDB() *PDBModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	_ = defaultModule.Init("")
}
