// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Script callback framework - matching C script_cb.c
 *
 * Provides pre-/post-script callbacks for request and reply processing.
 * Mirrors register_script_cb() / exec_pre_script_cb() /
 * exec_post_script_cb() from the C core, exposed as a thread-safe Go
 * CallbackManager plus package-level helpers backed by a default
 * singleton manager.
 */

package scriptcb

import (
	"sort"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Callback type constants matching C script_cb.h.
const (
	REQ_CB_TYPE_PRE  CallbackType = 1
	REQ_CB_TYPE_POST CallbackType = 2
	REQ_CB_TYPE_MISS CallbackType = 3
	RPL_CB_TYPE_PRE  CallbackType = 4
	RPL_CB_TYPE_POST CallbackType = 5
)

// CallbackType identifies a script callback slot.
type CallbackType int

// SIPMsg is an alias for the parsed SIP message used by callbacks.
type SIPMsg = *parser.SIPMsg

// ScriptCallback is the function signature invoked for a script callback.
type ScriptCallback func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int

// CallbackEntry is a single registered callback.
type CallbackEntry struct {
	ID       int
	Type     CallbackType
	Callback ScriptCallback
	Priority int
	Extra    interface{}
}

// CallbackManager manages script callbacks.
// C counterpart: the pre_script_cb[] / post_script_cb[] arrays plus
// register_script_cb() / exec_pre_script_cb() / exec_post_script_cb().
type CallbackManager struct {
	mu      sync.RWMutex
	nextID  int
	entries map[CallbackType][]CallbackEntry
}

// NewCallbackManager creates a new callback manager.
func NewCallbackManager() *CallbackManager {
	return &CallbackManager{
		nextID:  1,
		entries: make(map[CallbackType][]CallbackEntry),
	}
}

// Register adds a callback for the given type and returns its ID.
// A nil callback is rejected with -1. Higher priority values execute
// first; ties preserve registration (FIFO) order.
func (cm *CallbackManager) Register(cbType CallbackType, callback ScriptCallback, priority int, extra interface{}) int {
	if callback == nil {
		return -1
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	id := cm.nextID
	cm.nextID++
	cm.entries[cbType] = append(cm.entries[cbType], CallbackEntry{
		ID:       id,
		Type:     cbType,
		Callback: callback,
		Priority: priority,
		Extra:    extra,
	})
	return id
}

// Unregister removes the callback with the given ID. Removing an unknown
// or already-removed ID is a safe no-op.
func (cm *CallbackManager) Unregister(cbID int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for ct, list := range cm.entries {
		for i, e := range list {
			if e.ID == cbID {
				cm.entries[ct] = append(list[:i], list[i+1:]...)
				return
			}
		}
	}
}

// Execute runs all callbacks registered for cbType in priority order
// (highest first, FIFO for ties) and returns each callback's result in
// execution order. A type with no callbacks yields an empty slice.
func (cm *CallbackManager) Execute(cbType CallbackType, msg *parser.SIPMsg) []int {
	cm.mu.RLock()
	list := cm.entries[cbType]
	// Copy so the snapshot is unaffected by concurrent mutation and so
	// we never sort the live slice.
	ordered := make([]CallbackEntry, len(list))
	copy(ordered, list)
	cm.mu.RUnlock()

	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Priority > ordered[j].Priority
	})

	results := make([]int, 0, len(ordered))
	for _, e := range ordered {
		results = append(results, e.Callback(msg, cbType, e.Extra))
	}
	return results
}

// Count returns the number of callbacks registered for cbType.
func (cm *CallbackManager) Count(cbType CallbackType) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.entries[cbType])
}

// Clear removes all callbacks registered for cbType.
func (cm *CallbackManager) Clear(cbType CallbackType) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.entries, cbType)
}

// ClearAll removes all callbacks of every type.
func (cm *CallbackManager) ClearAll() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.entries = make(map[CallbackType][]CallbackEntry)
}

var (
	defaultMu      sync.RWMutex
	defaultManager *CallbackManager
)

// DefaultCallbackManager returns the process-wide callback manager.
func DefaultCallbackManager() *CallbackManager {
	defaultMu.RLock()
	dm := defaultManager
	defaultMu.RUnlock()
	if dm == nil {
		defaultMu.Lock()
		if defaultManager == nil {
			defaultManager = NewCallbackManager()
		}
		dm = defaultManager
		defaultMu.Unlock()
	}
	return dm
}

// Init (re)initializes the default callback manager.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultManager = NewCallbackManager()
}

// Register adds a callback to the default manager.
func Register(cbType CallbackType, callback ScriptCallback, priority int, extra interface{}) int {
	return DefaultCallbackManager().Register(cbType, callback, priority, extra)
}

// Unregister removes a callback from the default manager.
func Unregister(cbID int) {
	DefaultCallbackManager().Unregister(cbID)
}

// Execute runs callbacks of the given type on the default manager.
func Execute(cbType CallbackType, msg *parser.SIPMsg) []int {
	return DefaultCallbackManager().Execute(cbType, msg)
}
