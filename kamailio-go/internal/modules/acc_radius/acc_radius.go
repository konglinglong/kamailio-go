// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * acc_radius - RADIUS accounting backend.
 *
 * A simulated RADIUS client: it does not perform real network I/O but
 * tracks connection state and the CDRs that would be sent. This mirrors
 * the kamailio acc_radius module's accounting interface.
 */

package acc_radius

import (
	"errors"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/acc"
)

// AccRadiusModule is the RADIUS accounting backend.
type AccRadiusModule struct {
	mu        sync.Mutex
	server    string
	secret    string
	connected bool
	records   []*acc.CDR
}

// New returns a new AccRadiusModule.
func New() *AccRadiusModule {
	return &AccRadiusModule{}
}

// Init configures the RADIUS server and shared secret and marks the
// backend as connected. An empty server leaves the backend disconnected.
func (m *AccRadiusModule) Init(server, secret string) error {
	if m == nil {
		return errors.New("acc_radius: nil module")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.server = server
	m.secret = secret
	m.connected = server != ""
	return nil
}

// WriteCDR records a CDR for transmission to the RADIUS server. Returns
// an error if the backend is not connected or the CDR is nil.
func (m *AccRadiusModule) WriteCDR(cdr *acc.CDR) error {
	if m == nil {
		return errors.New("acc_radius: nil module")
	}
	if cdr == nil {
		return errors.New("acc_radius: nil cdr")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("acc_radius: not connected")
	}
	m.records = append(m.records, cdr)
	return nil
}

// IsConnected reports whether the backend has been initialised with a
// server and is ready to accept CDRs.
func (m *AccRadiusModule) IsConnected() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

// Count returns the number of CDRs accepted so far.
func (m *AccRadiusModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}
