// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * acc_diameter - Diameter accounting backend.
 *
 * A simulated Diameter client: it tracks connection state and the CDRs
 * that would be sent over the Diameter interface. Mirrors the kamailio
 * acc_diameter module's accounting interface.
 */

package acc_diameter

import (
	"errors"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/acc"
)

// AccDiameterModule is the Diameter accounting backend.
type AccDiameterModule struct {
	mu        sync.Mutex
	server    string
	connected bool
	records   []*acc.CDR
}

// New returns a new AccDiameterModule.
func New() *AccDiameterModule {
	return &AccDiameterModule{}
}

// Init configures the Diameter server and marks the backend as connected.
// An empty server leaves the backend disconnected.
func (m *AccDiameterModule) Init(server string) error {
	if m == nil {
		return errors.New("acc_diameter: nil module")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.server = server
	m.connected = server != ""
	return nil
}

// WriteCDR records a CDR for transmission to the Diameter server. Returns
// an error if the backend is not connected or the CDR is nil.
func (m *AccDiameterModule) WriteCDR(cdr *acc.CDR) error {
	if m == nil {
		return errors.New("acc_diameter: nil module")
	}
	if cdr == nil {
		return errors.New("acc_diameter: nil cdr")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("acc_diameter: not connected")
	}
	m.records = append(m.records, cdr)
	return nil
}

// IsConnected reports whether the backend has been initialised with a
// server and is ready to accept CDRs.
func (m *AccDiameterModule) IsConnected() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

// Count returns the number of CDRs accepted so far.
func (m *AccDiameterModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}
