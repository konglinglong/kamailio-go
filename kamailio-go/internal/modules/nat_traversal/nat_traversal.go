// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * nat_traversal - NAT detection and keep-alive tracking.
 *
 * Detects whether a SIP message traverses a NAT (received address differs
 * from the address advertised in Contact/Via) and tracks per-contact
 * keep-alive timestamps. Mirrors the kamailio nat_traversal module.
 */

package nat_traversal

import (
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// NATTraversalModule detects NAT and tracks keep-alives.
type NATTraversalModule struct {
	mu        sync.Mutex
	keepAlive map[string]time.Time
}

// New returns a new NATTraversalModule.
func New() *NATTraversalModule {
	return &NATTraversalModule{keepAlive: make(map[string]time.Time)}
}

// CheckNAT reports whether the message shows signs of NAT traversal. A
// simple heuristic: the Contact header host is a private IP while the Via
// received parameter (or absence of one) indicates a different public
// address. Here we treat presence of a private IP in Contact as a NAT
// indicator.
func (m *NATTraversalModule) CheckNAT(msg *parser.SIPMsg) bool {
	if m == nil || msg == nil {
		return false
	}
	if msg.Contact == nil {
		return false
	}
	body := msg.Contact.Body.String()
	return strings.Contains(body, "192.168.") ||
		strings.Contains(body, "10.") ||
		strings.Contains(body, "172.16.")
}

// KeepAlive records a keep-alive for the given contact and returns nil on
// success. An empty contact returns an error.
func (m *NATTraversalModule) KeepAlive(contact string) error {
	if m == nil {
		return nil
	}
	if contact == "" {
		return errEmptyContact
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keepAlive[contact] = time.Now()
	return nil
}

// IsKeepAliveNeeded reports whether the contact requires a keep-alive,
// i.e. it shows NAT signs and has not been kept alive recently (within
// the default interval).
func (m *NATTraversalModule) IsKeepAliveNeeded(msg *parser.SIPMsg) bool {
	if m == nil || msg == nil {
		return false
	}
	if !m.CheckNAT(msg) {
		return false
	}
	contact := contactString(msg)
	if contact == "" {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	last, ok := m.keepAlive[contact]
	if !ok {
		return true
	}
	return time.Since(last) > defaultKeepAliveInterval
}

// ProcessKeepAlive refreshes the keep-alive timestamp for contact.
func (m *NATTraversalModule) ProcessKeepAlive(contact string) {
	if m == nil || contact == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keepAlive[contact] = time.Now()
}

// LastKeepAlive returns the last keep-alive time for contact (zero if none).
func (m *NATTraversalModule) LastKeepAlive(contact string) time.Time {
	if m == nil {
		return time.Time{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keepAlive[contact]
}

const defaultKeepAliveInterval = 30 * time.Second

var errEmptyContact = &natError{"nat_traversal: empty contact"}

type natError struct{ msg string }

func (e *natError) Error() string { return e.msg }

func contactString(msg *parser.SIPMsg) string {
	if msg == nil || msg.Contact == nil {
		return ""
	}
	return msg.Contact.Body.String()
}
