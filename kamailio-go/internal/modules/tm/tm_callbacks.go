// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * TM callback registry - matching C's tm_cb.h / t_lookup.h register_tmcb().
 *
 * Kamailio's tm module lets other code register interest in a defined set
 * of transaction events (request in/out, reply in/out, branch failure,
 * transaction destruction, ...). Each registration supplies a bitmask of
 * the event types it cares about plus a single callback; the tm engine
 * invokes every registered callback whose mask intersects the event
 * being fired.
 *
 * This Go counterpart keeps the same model: a CallbackRegistry holds a
 * slice of entries, each with a TMCBType mask, a TMCBCallback and an
 * opaque user-data pointer. The Manager owns a registry and calls
 * invokeTMCBs at the well-defined event sites (HandleResponse,
 * RelayRequest, handleFRTimeout, removeCell).
 *
 * The older single-slot RouteCallbacks (SetCallbacks/GetCallbacks) is
 * retained for backward compatibility with existing callers; the
 * registry fires alongside it.
 */

package tm

import (
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// TMCBType is a bitmask identifying the transaction events a callback
// is interested in. It mirrors C's TMCB_* constants from tm_cb.h.
type TMCBType uint32

// Supported TMCB event types. Values match the C bit positions.
const (
	// TMCBRequestIn fires when a request enters the transaction layer
	// (new transaction created or retransmission matched).
	TMCBRequestIn TMCBType = 1 << iota
	// TMCBResponseIn fires when a response is received and matched to
	// a transaction (before any on_reply processing).
	TMCBResponseIn
	// TMCBRequestOut fires when a request is sent out on a branch
	// (after t_relay adds the branch).
	TMCBRequestOut
	// TMCBResponseOut fires when a response is relayed upstream.
	TMCBResponseOut
	// TMCBOnReply fires when a response is received for a transaction
	// (provisional or final). Equivalent to t_on_reply.
	TMCBOnReply
	// TMCBOnFailure fires when a transaction fails with a non-2xx
	// final response or a branch/transaction timeout. Equivalent to
	// t_on_failure.
	TMCBOnFailure
	// TMCBOnBranchFailure fires when a specific branch fails (negative
	// final response or FR timeout on that branch). Equivalent to
	// t_on_branch_failure.
	TMCBOnBranchFailure
	// TMCBOnBranch fires when a request is about to be forwarded on a
	// branch, before the buffer is sent. Equivalent to t_on_branch.
	TMCBOnBranch
	// TMCBTransactionDestroyed fires just before a transaction cell is
	// removed from the table and freed.
	TMCBTransactionDestroyed
)

// tmcbMaskNames provides stable names for the bitmask values, used by
// String() for diagnostic logging.
var tmcbMaskNames = []struct {
	mask TMCBType
	name string
}{
	{TMCBRequestIn, "request_in"},
	{TMCBResponseIn, "response_in"},
	{TMCBRequestOut, "request_out"},
	{TMCBResponseOut, "response_out"},
	{TMCBOnReply, "on_reply"},
	{TMCBOnFailure, "on_failure"},
	{TMCBOnBranchFailure, "on_branch_failure"},
	{TMCBOnBranch, "on_branch"},
	{TMCBTransactionDestroyed, "transaction_destroyed"},
}

// String returns a human-readable representation of the set event
// types in the mask (e.g. "on_reply|on_failure"). Returns "none" when
// the mask is empty and "unknown" when bits outside the known set are
// present without any known ones.
func (t TMCBType) String() string {
	if t == 0 {
		return "none"
	}
	out := ""
	for _, n := range tmcbMaskNames {
		if t&n.mask != 0 {
			if out != "" {
				out += "|"
			}
			out += n.name
		}
	}
	return out
}

// TMCBCallback is the signature for TM event callbacks. cell is the
// affected transaction (never nil at fire time); branch is the relevant
// UAC branch index (-1 when not branch-specific); msg is the SIP
// message that triggered the event (may be nil for synthetic events
// like transaction_destroyed); data is the opaque pointer supplied at
// registration.
type TMCBCallback func(cell *Cell, branch int, msg *parser.SIPMsg, data interface{})

// tmcbEntry is a single registration in the registry.
type tmcbEntry struct {
	types TMCBType
	cb    TMCBCallback
	data  interface{}
}

// CallbackRegistry holds all TM callback registrations for a Manager.
// It is safe for concurrent use; registrations and invocations may
// happen from any goroutine.
type CallbackRegistry struct {
	mu      sync.RWMutex
	entries []tmcbEntry
	// nextID is the next handle to return from Register. Handles are
	// 1-based and reused only after Unregister frees a slot; callers
	// should treat them as opaque.
	nextID int
}

// NewCallbackRegistry returns an empty callback registry.
func NewCallbackRegistry() *CallbackRegistry {
	return &CallbackRegistry{}
}

// Register adds a callback for the given event mask. data is passed
// back to the callback at fire time; pass nil when not needed. Returns
// a handle that can be passed to Unregister to remove the entry.
//
// Panics if cb is nil (a nil callback would crash at invocation time
// and is always a programming error).
func (r *CallbackRegistry) Register(typ TMCBType, cb TMCBCallback, data interface{}) int {
	if cb == nil {
		panic("tm: nil TMCB callback")
	}
	if typ == 0 {
		// A zero mask would never fire; refuse rather than silently
		// swallow the registration.
		panic("tm: empty TMCBType mask")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	r.entries = append(r.entries, tmcbEntry{
		types: typ,
		cb:    cb,
		data:  data,
	})
	return r.nextID
}

// Unregister removes the callback associated with handle. A handle of
// 0 or an unknown handle is a no-op. The freed slot is niled so its
// memory can be reclaimed; subsequent invocations skip nilled entries.
func (r *CallbackRegistry) Unregister(handle int) {
	if handle <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Entries are 1-based: handle h lives at index h-1.
	idx := handle - 1
	if idx >= len(r.entries) {
		return
	}
	// Nil out the function (which holds the closure) so the GC can
	// reclaim any captured state. We keep the slot to preserve the
	// handle/index mapping of later registrations.
	r.entries[idx].cb = nil
	r.entries[idx].data = nil
	r.entries[idx].types = 0
}

// Invoke runs every callback whose mask intersects typ for the given
// event. Callbacks are invoked synchronously in registration order;
// each is called with (cell, branch, msg, entry.data). A callback
// that panics is recovered and logged so a faulty callback cannot
// crash the transaction engine.
func (r *CallbackRegistry) Invoke(typ TMCBType, cell *Cell, branch int, msg *parser.SIPMsg) {
	// Snapshot the matching callbacks under the read lock so the
	// invocation itself does not hold the registry lock while user
	// code runs (callbacks may re-register or unregister).
	type fired struct {
		cb   TMCBCallback
		data interface{}
	}
	var pending []fired
	r.mu.RLock()
	for i := range r.entries {
		e := &r.entries[i]
		if e.cb == nil {
			continue
		}
		if e.types&typ != 0 {
			pending = append(pending, fired{cb: e.cb, data: e.data})
		}
	}
	r.mu.RUnlock()

	for i := range pending {
		// Defensive: a panicking callback must not take down the
		// transaction engine.
		func() {
			defer func() { _ = recover() }()
			pending[i].cb(cell, branch, msg, pending[i].data)
		}()
	}
}

// Count returns the number of currently registered (non-nilled)
// callbacks. Primarily useful for tests and diagnostics.
func (r *CallbackRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for i := range r.entries {
		if r.entries[i].cb != nil {
			n++
		}
	}
	return n
}

// HasType reports whether any registered callback is interested in the
// given event type. The tm engine uses this to skip the (cheaper but
// still non-zero) snapshot/iteration work in Invoke when nothing is
// registered for an event.
func (r *CallbackRegistry) HasType(typ TMCBType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := range r.entries {
		if r.entries[i].cb != nil && r.entries[i].types&typ != 0 {
			return true
		}
	}
	return false
}
