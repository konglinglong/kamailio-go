// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS I-CSCF module - Serving-CSCF selection during registration.
 * Port of the kamailio ims_icscf module (src/modules/ims_icscf).
 *
 * The Interrogating-CSCF selects a Serving-CSCF to handle a subscriber's
 * registration. Candidate S-CSCFs advertise a capacity and a priority;
 * the I-CSCF picks the one with the highest capacity among the highest
 * priority (lowest numeric priority value = highest precedence, matching
 * Kamailio's convention). The assignment is remembered per subscriber
 * so that subsequent requests for the same subscriber are routed to the
 * same S-CSCF.
 *
 * It is safe for concurrent use.
 */

package ims_icscf

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// ICSCFRegistration describes a registered Serving-CSCF.
type ICSCFRegistration struct {
	Subscriber   string
	SCSCF        string
	Capacity     int
	Priority     int
	RegisteredAt time.Time
}

// ICSCFModule maintains the set of registered S-CSCFs and the
// subscriber-to-S-CSCF assignments.
type ICSCFModule struct {
	mu          sync.RWMutex
	scscfs      map[string]*ICSCFRegistration
	assignments map[string]string // subscriber -> S-CSCF name
}

// NewICSCFModule creates an ICSCFModule with empty storage.
func NewICSCFModule() *ICSCFModule {
	return &ICSCFModule{
		scscfs:      make(map[string]*ICSCFRegistration),
		assignments: make(map[string]string),
	}
}

// RegisterSCSCF registers (or replaces) a Serving-CSCF with the given
// capacity and priority. A newly registered S-CSCF is not yet assigned
// to any subscriber.
//
//	C: icscf_register_scscf()
func (m *ICSCFModule) RegisterSCSCF(name string, capacity int, priority int) {
	if name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.scscfs == nil {
		m.scscfs = make(map[string]*ICSCFRegistration)
	}
	m.scscfs[name] = &ICSCFRegistration{
		Subscriber:   "",
		SCSCF:        name,
		Capacity:     capacity,
		Priority:     priority,
		RegisteredAt: time.Now(),
	}
}

// UnregisterSCSCF removes a Serving-CSCF and clears any assignment that
// pointed to it.
func (m *ICSCFModule) UnregisterSCSCF(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.scscfs, name)
	for sub, scscf := range m.assignments {
		if scscf == name {
			delete(m.assignments, sub)
		}
	}
}

// SelectSCSCF returns the best available S-CSCF: the one with the lowest
// priority value, breaking ties by the highest capacity. Returns the
// empty string when no S-CSCF is registered.
//
//	C: icscf_select_scscf()
func (m *ICSCFModule) SelectSCSCF() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.scscfs) == 0 {
		return ""
	}
	ranked := make([]*ICSCFRegistration, 0, len(m.scscfs))
	for _, r := range m.scscfs {
		ranked = append(ranked, r)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Priority != ranked[j].Priority {
			return ranked[i].Priority < ranked[j].Priority
		}
		return ranked[i].Capacity > ranked[j].Capacity
	})
	return ranked[0].SCSCF
}

// AssignSCSCF selects an S-CSCF for subscriber and remembers the
// assignment so that subsequent lookups return the same S-CSCF. Returns
// an error when no S-CSCF is available.
//
//	C: icscf_assign_scscf()
func (m *ICSCFModule) AssignSCSCF(subscriber string) (string, error) {
	if subscriber == "" {
		return "", errors.New("ims_icscf: empty subscriber")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.assignments == nil {
		m.assignments = make(map[string]string)
	}
	// Return the existing assignment if the S-CSCF is still registered.
	if name, ok := m.assignments[subscriber]; ok {
		if _, alive := m.scscfs[name]; alive {
			return name, nil
		}
		// Stale assignment: clear it and re-select.
		delete(m.assignments, subscriber)
	}
	if len(m.scscfs) == 0 {
		return "", errors.New("ims_icscf: no S-CSCF registered")
	}
	ranked := make([]*ICSCFRegistration, 0, len(m.scscfs))
	for _, r := range m.scscfs {
		ranked = append(ranked, r)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Priority != ranked[j].Priority {
			return ranked[i].Priority < ranked[j].Priority
		}
		return ranked[i].Capacity > ranked[j].Capacity
	})
	name := ranked[0].SCSCF
	m.assignments[subscriber] = name
	return name, nil
}

// GetSCSCF returns the S-CSCF currently assigned to subscriber, or the
// empty string when no assignment exists.
func (m *ICSCFModule) GetSCSCF(subscriber string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.assignments[subscriber]
}

// ListSCSCFs returns a snapshot of all registered S-CSCFs.
func (m *ICSCFModule) ListSCSCFs() []ICSCFRegistration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ICSCFRegistration, 0, len(m.scscfs))
	for _, r := range m.scscfs {
		out = append(out, *r)
	}
	return out
}

// CountSCSCFs returns the number of registered S-CSCFs.
func (m *ICSCFModule) CountSCSCFs() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.scscfs)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultIM *ICSCFModule
)

// DefaultICSCF returns the process-wide ICSCFModule, creating one on
// first use.
func DefaultICSCF() *ICSCFModule {
	defaultMu.RLock()
	im := defaultIM
	defaultMu.RUnlock()
	if im != nil {
		return im
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultIM == nil {
		defaultIM = NewICSCFModule()
	}
	return defaultIM
}

// Init (re)initialises the process-wide ICSCFModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultIM = NewICSCFModule()
}
