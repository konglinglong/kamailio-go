// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Event subscribe/dispatch framework - matching Kamailio core events.c.
 *
 * Provides a registry of event callbacks keyed by event ID. Subscribers
 * are dispatched in descending priority order (highest priority first),
 * with ties broken by registration order (FIFO). The manager is safe
 * for concurrent use.
 */

package event

import (
	"sort"
	"sync"
)

// Event ID constants matching Kamailio's C SREV_* defines. The values
// mirror the classic SER/Kamailio core events.h numeration.
const (
	SREVNetDataIn       = 1  // SREV_NET_DATA_IN
	SREVNetDataOut      = 2  // SREV_NET_DATA_OUT
	SREVCoreStats       = 3  // SREV_CORE_STATS
	SREVCfgRunAction    = 4  // SREV_CFG_RUN_ACTION
	SREVRcvReq          = 5  // SREV_RCV_REQ
	SREVRcvRpl          = 6  // SREV_RCV_RPL
	SREVNetDataInRaw    = 7  // SREV_NET_DATA_IN_RAW
	SREVRunAction       = 8  // SREV_RUN_ACTION
	SREVReqForceRcv     = 9  // SREV_REQ_FORCE_RCV
	SREVRunRoute        = 10 // SREV_RUN_ROUTE
	SREVRunFailureRoute = 11 // SREV_RUN_FAILURE_ROUTE
	SREVRunErrorRoute   = 12 // SREV_RUN_ERROR_ROUTE
	SREVRunStartupRoute = 13 // SREV_RUN_STARTUP_ROUTE
	SREVTmLocalReqIn    = 14 // SREV_TM_LOCAL_REQ_IN
	SREVPkgMemAlloc     = 15 // SREV_PKG_MEM_ALLOC
	SREVShmMemAlloc     = 16 // SREV_SHM_MEM_ALLOC
)

// EventParam mirrors Kamailio's sr_event_param_t. Data is the generic
// payload; the remaining fields are optional and populated as needed by
// the dispatching site. Pointer-typed C fields are exposed as interface{}
// so callers may pass the appropriate Go representation.
type EventParam struct {
	Data    interface{} // void *data
	OBuf    []byte      // str obuf
	Rcv     interface{} // receive_info_t *rcv
	Dst     interface{} // dest_info_t *dst
	Req     interface{} // sip_msg_t *req
	Rpl     interface{} // sip_msg_t *rpl
	RplCode int         // int rplcode
	Mode    int         // int mode
}

// EventCallback is the signature of an event subscriber callback. It
// matches Kamailio's sr_event_cb_f: int (*sr_event_cb_f)(sr_event_param_t *evp).
type EventCallback func(param *EventParam) int

// EventSubscriber represents a single registration of a callback for an
// event. Subscribers are dispatched in descending Priority order.
type EventSubscriber struct {
	ID       int
	EventID  int
	Callback EventCallback
	Priority int
}

// EventManager is a thread-safe registry of event subscribers. It is the
// Go counterpart of Kamailio's sr_event_cb_t plus the
// sr_event_register_cb / sr_event_exec / sr_event_enabled API.
type EventManager struct {
	mu          sync.RWMutex
	subscribers map[int][]*EventSubscriber // eventID -> subscribers sorted by priority desc
	byID        map[int]*EventSubscriber   // subscriber id -> subscriber for O(1) unsubscribe
	nextID      int
}

// NewEventManager creates an empty EventManager.
func NewEventManager() *EventManager {
	return &EventManager{
		subscribers: make(map[int][]*EventSubscriber),
		byID:        make(map[int]*EventSubscriber),
	}
}

// Subscribe registers callback for eventID at the given priority and
// returns a unique, positive subscriber ID. A nil callback is rejected
// with -1, mirroring the failure return of sr_event_register_cb when no
// free slot is available. Subscribers are kept sorted by descending
// priority (highest first); ties preserve registration (FIFO) order.
func (em *EventManager) Subscribe(eventID int, callback EventCallback, priority int) int {
	if callback == nil {
		return -1
	}
	em.mu.Lock()
	defer em.mu.Unlock()
	em.nextID++
	id := em.nextID
	sub := &EventSubscriber{
		ID:       id,
		EventID:  eventID,
		Callback: callback,
		Priority: priority,
	}
	// Append then stable-sort by descending priority. Because the new
	// element is appended last, equal-priority subscribers keep their
	// registration order (FIFO).
	subs := append(em.subscribers[eventID], sub)
	sort.SliceStable(subs, func(i, j int) bool {
		return subs[i].Priority > subs[j].Priority
	})
	em.subscribers[eventID] = subs
	em.byID[id] = sub
	return id
}

// Unsubscribe removes the subscriber identified by subscriberID. It is a
// safe no-op if the ID is unknown or already removed.
func (em *EventManager) Unsubscribe(subscriberID int) {
	em.mu.Lock()
	defer em.mu.Unlock()
	sub, ok := em.byID[subscriberID]
	if !ok {
		return
	}
	subs := em.subscribers[sub.EventID]
	for i, s := range subs {
		if s.ID == subscriberID {
			em.subscribers[sub.EventID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	delete(em.byID, subscriberID)
}

// Dispatch invokes every subscriber registered for eventID, in priority
// order, passing param to each callback. The callbacks are invoked
// outside the manager lock so a callback may itself subscribe or
// unsubscribe without deadlocking. Dispatching an event with no
// subscribers is a no-op.
func (em *EventManager) Dispatch(eventID int, param *EventParam) {
	em.mu.RLock()
	subs := em.subscribers[eventID]
	// Copy the subscriber list so iteration is not affected by concurrent
	// mutations and the lock is not held during callback execution.
	cp := make([]*EventSubscriber, len(subs))
	copy(cp, subs)
	em.mu.RUnlock()
	for _, s := range cp {
		s.Callback(param)
	}
}

// IsEnabled reports whether any subscriber is registered for eventID,
// matching Kamailio's sr_event_enabled.
func (em *EventManager) IsEnabled(eventID int) bool {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return len(em.subscribers[eventID]) > 0
}

// SubscriberCount returns the number of subscribers registered for
// eventID.
func (em *EventManager) SubscriberCount(eventID int) int {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return len(em.subscribers[eventID])
}

// Clear removes every subscriber registered for eventID. Other events
// are unaffected.
func (em *EventManager) Clear(eventID int) {
	em.mu.Lock()
	defer em.mu.Unlock()
	for _, s := range em.subscribers[eventID] {
		delete(em.byID, s.ID)
	}
	delete(em.subscribers, eventID)
}

// ClearAll removes every subscriber for every event.
func (em *EventManager) ClearAll() {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.subscribers = make(map[int][]*EventSubscriber)
	em.byID = make(map[int]*EventSubscriber)
}

// defaultEM is the process-wide EventManager used by the package-level
// helper functions. defaultMu guards the pointer; Init replaces it.
var (
	defaultEM *EventManager
	defaultMu sync.RWMutex
)

// DefaultEventManager returns the process-wide EventManager, creating
// it on first use.
func DefaultEventManager() *EventManager {
	defaultMu.RLock()
	em := defaultEM
	defaultMu.RUnlock()
	if em != nil {
		return em
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultEM == nil {
		defaultEM = NewEventManager()
	}
	return defaultEM
}

// Init (re)initialises the process-wide EventManager to an empty state,
// mirroring Kamailio's sr_event_cb_init. It is safe to call multiple
// times.
func Init() {
	defaultMu.Lock()
	defaultEM = NewEventManager()
	defaultMu.Unlock()
}

// Subscribe registers a callback on the process-wide EventManager. See
// EventManager.Subscribe.
func Subscribe(eventID int, callback EventCallback, priority int) int {
	return DefaultEventManager().Subscribe(eventID, callback, priority)
}

// Unsubscribe removes a subscriber by ID on the process-wide
// EventManager. See EventManager.Unsubscribe.
func Unsubscribe(subscriberID int) {
	DefaultEventManager().Unsubscribe(subscriberID)
}

// Dispatch fires an event on the process-wide EventManager. See
// EventManager.Dispatch.
func Dispatch(eventID int, param *EventParam) {
	DefaultEventManager().Dispatch(eventID, param)
}

// IsEnabled reports whether any subscriber is registered for eventID on
// the process-wide EventManager.
func IsEnabled(eventID int) bool {
	return DefaultEventManager().IsEnabled(eventID)
}
