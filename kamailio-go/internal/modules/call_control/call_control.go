// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * call_control - concurrent call limiting per user.
 *
 * Tracks the number of active calls per user and per Call-ID so that a
 * script can enforce concurrent-call limits. Mirrors the kamailio
 * call_control module.
 */

package call_control

import "sync"

// CallControlModule tracks active calls per user.
type CallControlModule struct {
	mu     sync.Mutex
	active map[string]int    // user -> active call count
	calls  map[string]string // callID -> user
}

// New returns a new CallControlModule.
func New() *CallControlModule {
	return &CallControlModule{
		active: make(map[string]int),
		calls:  make(map[string]string),
	}
}

// CheckLimit reports whether user may start another call without
// exceeding limit (i.e. active calls < limit).
func (m *CallControlModule) CheckLimit(user string, limit int) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit < 0 {
		return false
	}
	return m.active[user] < limit
}

// GetActiveCalls returns the number of currently active calls for user.
func (m *CallControlModule) GetActiveCalls(user string) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active[user]
}

// StartCall registers a new active call for user keyed by callID. If the
// callID is already known this is a no-op.
func (m *CallControlModule) StartCall(user, callID string) {
	if m == nil || user == "" || callID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.calls[callID]; ok {
		return
	}
	m.calls[callID] = user
	m.active[user]++
}

// EndCall terminates the call identified by callID. Returns true if a
// call was actually ended.
func (m *CallControlModule) EndCall(callID string) bool {
	if m == nil || callID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.calls[callID]
	if !ok {
		return false
	}
	delete(m.calls, callID)
	if m.active[user] > 0 {
		m.active[user]--
		if m.active[user] == 0 {
			delete(m.active, user)
		}
	}
	return true
}
