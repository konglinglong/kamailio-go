// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * qos - per-call QoS / bandwidth tracking.
 *
 * Stores downstream and upstream bandwidth limits keyed by Call-ID so that
 * the media layer can apply them when relaying RTP. Mirrors the kamailio
 * qos module.
 */

package qos

import (
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// QoSInfo holds the bandwidth limits for one call.
type QoSInfo struct {
	Downstream int
	Upstream   int
}

// QoSModule tracks per-call QoS settings.
type QoSModule struct {
	mu   sync.Mutex
	data map[string]*QoSInfo
}

// New returns a new QoSModule.
func New() *QoSModule {
	return &QoSModule{data: make(map[string]*QoSInfo)}
}

// SetQoS records the bandwidth for the given direction on the call
// identified by msg's Call-ID. Returns the bandwidth that was set, or 0 if
// the message has no Call-ID.
func (m *QoSModule) SetQoS(msg *parser.SIPMsg, direction string, bandwidth int) int {
	if m == nil || msg == nil {
		return 0
	}
	callID := callIDOf(msg)
	if callID == "" {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.data[callID]
	if !ok {
		info = &QoSInfo{}
		m.data[callID] = info
	}
	switch direction {
	case "downstream":
		info.Downstream = bandwidth
	case "upstream":
		info.Upstream = bandwidth
	default:
		info.Downstream = bandwidth
	}
	return bandwidth
}

// GetQoS returns the (downstream, upstream) bandwidth for the call
// identified by msg's Call-ID. Returns (0, 0) if unknown.
func (m *QoSModule) GetQoS(msg *parser.SIPMsg) (int, int) {
	if m == nil || msg == nil {
		return 0, 0
	}
	callID := callIDOf(msg)
	if callID == "" {
		return 0, 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.data[callID]
	if !ok {
		return 0, 0
	}
	return info.Downstream, info.Upstream
}

// RemoveQoS removes the QoS entry for the call identified by msg's Call-ID.
// Returns 1 if an entry was removed, 0 otherwise.
func (m *QoSModule) RemoveQoS(msg *parser.SIPMsg) int {
	if m == nil || msg == nil {
		return 0
	}
	callID := callIDOf(msg)
	if callID == "" {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[callID]; !ok {
		return 0
	}
	delete(m.data, callID)
	return 1
}

// callIDOf extracts the Call-ID from a parsed SIP message.
func callIDOf(msg *parser.SIPMsg) string {
	if msg == nil {
		return ""
	}
	if msg.CallID != nil {
		return msg.CallID.Body.String()
	}
	h := msg.GetHeaderByType(parser.HdrCallID)
	if h != nil {
		return h.Body.String()
	}
	return ""
}
