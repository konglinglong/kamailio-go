// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * CallObj module - call object registry.
 * Port of the kamailio call_obj module (src/modules/call_obj).
 *
 * call_obj keeps a registry of CallObject values keyed by Call-ID so
 * scripts can attach routing state (from/to URI, call state) to a call.
 * Create makes a new object; Get fetches it; Delete removes it; Count
 * reports the registry size.
 *
 * The module is safe for concurrent use.
 */

package call_obj

import "sync"

// CallObject holds per-call state.
type CallObject struct {
	CallID  string
	FromURI string
	ToURI   string
	State   string
}

// CallObjModule is a registry of CallObject values.
type CallObjModule struct {
	mu      sync.RWMutex
	objects map[string]*CallObject
}

// New creates an empty CallObjModule.
func New() *CallObjModule {
	return &CallObjModule{objects: make(map[string]*CallObject)}
}

// Create registers a new CallObject for callID and returns it. If an
// object for callID already exists it is replaced.
func (m *CallObjModule) Create(callID string) *CallObject {
	m.mu.Lock()
	defer m.mu.Unlock()
	obj := &CallObject{CallID: callID, State: "init"}
	m.objects[callID] = obj
	return obj
}

// Get returns the CallObject for callID, or nil when absent.
func (m *CallObjModule) Get(callID string) *CallObject {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.objects[callID]
}

// Delete removes the CallObject for callID. Returns true when an object
// was removed.
func (m *CallObjModule) Delete(callID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[callID]; !ok {
		return false
	}
	delete(m.objects, callID)
	return true
}

// Count returns the number of registered call objects.
func (m *CallObjModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.objects)
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *CallObjModule
)

// DefaultCallObj returns the process-wide module, creating it on first use.
func DefaultCallObj() *CallObjModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide module to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Create is the package-level wrapper.
func Create(callID string) *CallObject { return DefaultCallObj().Create(callID) }

// Get is the package-level wrapper.
func Get(callID string) *CallObject { return DefaultCallObj().Get(callID) }

// Delete is the package-level wrapper.
func Delete(callID string) bool { return DefaultCallObj().Delete(callID) }

// Count is the package-level wrapper.
func Count() int { return DefaultCallObj().Count() }
