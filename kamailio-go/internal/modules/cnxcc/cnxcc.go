// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * cnxcc - credit control / prepaid call charging.
 *
 * Maintains a per-user credit balance and supports checking and deducting
 * credit as calls progress. Mirrors the kamailio cnxcc module.
 */

package cnxcc

import "sync"

// CNXCCModule tracks prepaid credit balances per user.
type CNXCCModule struct {
	mu       sync.Mutex
	balances map[string]int64
}

// New returns a new CNXCCModule.
func New() *CNXCCModule {
	return &CNXCCModule{balances: make(map[string]int64)}
}

// SetCredit sets the credit balance for user, overwriting any previous value.
func (m *CNXCCModule) SetCredit(user string, credit int64) {
	if m == nil || user == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balances[user] = credit
}

// CheckCredit reports whether user has at least cost credit remaining.
func (m *CNXCCModule) CheckCredit(user string, cost int64) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.balances[user] >= cost
}

// DeductCredit subtracts amount from the user's balance, flooring at zero,
// and returns the new balance. If the user is unknown the balance is 0.
func (m *CNXCCModule) DeductCredit(user string, amount int64) int64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.balances[user]
	cur -= amount
	if cur < 0 {
		cur = 0
	}
	m.balances[user] = cur
	return cur
}

// GetBalance returns the current credit balance for user (0 if unknown).
func (m *CNXCCModule) GetBalance(user string) int64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.balances[user]
}
