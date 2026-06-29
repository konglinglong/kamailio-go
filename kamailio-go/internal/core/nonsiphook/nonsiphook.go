// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Non-SIP message hook framework - matching C nonsip_hooks.c / nonsip_hooks.h.
 *
 * In the C core, non-SIP hooks are invoked whenever a message with a SIP-like
 * first line but a protocol/version other than SIP/2.0 is received (e.g. HTTP,
 * MSRP, STUN). Each registered hook receives the parsed message and returns
 * one of NONSIP_MSG_DROP (0), NONSIP_MSG_PASS (1), or NONSIP_MSG_ACCEPT (2).
 * The first hook that does not return PASS short-circuits the chain.
 *
 * The Go port generalises this into a priority-ordered, type-tagged hook
 * manager (HookPre / HookPost / HookError) that is safe for concurrent use.
 * It follows the project New() / Default*() / Init() convention.
 */

package nonsiphook

import (
	"fmt"
	"sort"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// HookType identifies the phase at which a hook is invoked.
type HookType int

const (
	HookPre   HookType = iota // before message processing
	HookPost                  // after message processing
	HookError                 // on error during processing
)

// String returns a human-readable name for the hook type.
func (ht HookType) String() string {
	switch ht {
	case HookPre:
		return "pre"
	case HookPost:
		return "post"
	case HookError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", int(ht))
	}
}

// Return values for hook callbacks, matching C's enum nonsip_msg_returns.
const (
	NONSIPMsgError  = -1 // error (drop message)
	NONSIPMsgDrop   = 0  // drop message immediately
	NONSIPMsgPass   = 1  // continue with other hooks
	NONSIPMsgAccept = 2  // accept message
)

// HookFunc is the signature of a non-SIP hook callback. It receives the
// parsed message and returns one of the NONSIPMsg* constants.
//
// C counterpart: int (*on_nonsip_req)(struct sip_msg *msg)
type HookFunc func(msg *parser.SIPMsg) int

// Hook represents a single registered non-SIP hook.
type Hook struct {
	Name     string
	Type     HookType
	Priority int
	Callback HookFunc
}

// Manager is a thread-safe registry of non-SIP hooks, keyed by HookType.
// Hooks within the same type are dispatched in descending priority order
// (highest first); ties preserve registration (FIFO) order.
//
// C counterpart: the nonsip_hooks array plus register_nonsip_msg_hook() /
// nonsip_msg_run_hooks() / init_nonsip_hooks() / destroy_nonsip_hooks().
type Manager struct {
	mu    sync.RWMutex
	hooks map[HookType][]*Hook
}

// New creates an empty hook manager.
func New() *Manager {
	return &Manager{
		hooks: make(map[HookType][]*Hook),
	}
}

// Register adds a hook to the manager. A nil callback or an empty name is
// rejected with an error. Hooks are kept sorted by descending priority
// (highest first); ties preserve registration (FIFO) order.
func (m *Manager) Register(h *Hook) error {
	if h == nil {
		return fmt.Errorf("nonsiphook: hook is nil")
	}
	if h.Name == "" {
		return fmt.Errorf("nonsiphook: hook name is empty")
	}
	if h.Callback == nil {
		return fmt.Errorf("nonsiphook: hook callback is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	list := append(m.hooks[h.Type], h)
	// Stable sort by descending priority so the highest-priority hook
	// runs first. Because the new element is appended last, equal-priority
	// hooks keep their registration order (FIFO).
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].Priority > list[j].Priority
	})
	m.hooks[h.Type] = list
	return nil
}

// Execute runs every hook registered for ht in priority order, passing msg
// to each callback. The callbacks are invoked outside the manager lock so a
// callback may itself register or unregister hooks without deadlocking.
//
// The return value follows the C nonsip_msg_run_hooks() contract:
//   - If no hooks are registered, returns NONSIPMsgDrop (the C default).
//   - Otherwise, returns the result of the first hook whose return value
//     is not NONSIPMsgPass; if all hooks return PASS, returns PASS.
func (m *Manager) Execute(ht HookType, msg *parser.SIPMsg) int {
	m.mu.RLock()
	list := m.hooks[ht]
	// Snapshot so execution is unaffected by concurrent mutation and so
	// we never hold the lock while running user callbacks.
	snapshot := make([]*Hook, len(list))
	copy(snapshot, list)
	m.mu.RUnlock()

	// C default: if no hook installed, drop.
	ret := NONSIPMsgDrop
	for _, h := range snapshot {
		ret = h.Callback(msg)
		if ret != NONSIPMsgPass {
			break
		}
	}
	return ret
}

// Unregister removes the first hook with the given name (across all hook
// types). Returns true if a hook was found and removed, false otherwise.
func (m *Manager) Unregister(name string) bool {
	if name == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for ht, list := range m.hooks {
		for i, h := range list {
			if h.Name == name {
				m.hooks[ht] = append(list[:i], list[i+1:]...)
				return true
			}
		}
	}
	return false
}

// UnregisterType removes the first hook with the given name within the
// specified hook type. Returns true if a hook was found and removed.
func (m *Manager) UnregisterType(ht HookType, name string) bool {
	if name == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.hooks[ht]
	for i, h := range list {
		if h.Name == name {
			m.hooks[ht] = append(list[:i], list[i+1:]...)
			return true
		}
	}
	return false
}

// List returns a snapshot of all registered hooks across all types, sorted
// by hook type then by descending priority. The returned slice is a copy;
// callers may freely mutate it without affecting the manager's state.
func (m *Manager) List() []*Hook {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect all hook types in order.
	var types []HookType
	for ht := range m.hooks {
		types = append(types, ht)
	}
	sort.Slice(types, func(i, j int) bool {
		return types[i] < types[j]
	})

	var result []*Hook
	for _, ht := range types {
		result = append(result, m.hooks[ht]...)
	}
	return result
}

// ListType returns a snapshot of all hooks registered for the given type,
// in priority order.
func (m *Manager) ListType(ht HookType) []*Hook {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.hooks[ht]
	result := make([]*Hook, len(list))
	copy(result, list)
	return result
}

// Count returns the total number of hooks across all types.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, list := range m.hooks {
		n += len(list)
	}
	return n
}

// CountType returns the number of hooks registered for the given type.
func (m *Manager) CountType(ht HookType) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.hooks[ht])
}

// Clear removes all hooks of the given type.
func (m *Manager) Clear(ht HookType) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.hooks, ht)
}

// ClearAll removes every hook of every type.
func (m *Manager) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks = make(map[HookType][]*Hook)
}

// ============================================================
// Process-wide singleton (Default*() / Init())
// ============================================================

var (
	defaultMgr *Manager
	defaultMu  sync.RWMutex
)

// DefaultManager returns the process-wide hook manager, creating it on
// first use (lazy initialisation with double-checked locking).
func DefaultManager() *Manager {
	defaultMu.RLock()
	m := defaultMgr
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultMgr == nil {
		defaultMgr = New()
	}
	return defaultMgr
}

// Init (re)initialises the process-wide hook manager to an empty state.
// It is safe to call multiple times and is intended for test isolation.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultMgr = New()
}

// Register adds a hook to the default manager.
func Register(h *Hook) error {
	return DefaultManager().Register(h)
}

// Execute runs hooks of the given type on the default manager.
func Execute(ht HookType, msg *parser.SIPMsg) int {
	return DefaultManager().Execute(ht, msg)
}

// Unregister removes a hook by name from the default manager.
func Unregister(name string) bool {
	return DefaultManager().Unregister(name)
}

// List returns all hooks from the default manager.
func List() []*Hook {
	return DefaultManager().List()
}
