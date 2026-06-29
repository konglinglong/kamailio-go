// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the event package - matching Kamailio core events.c
 */

package event

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestSubscribe verifies that Subscribe returns unique, positive IDs and
// that SubscriberCount reflects the registrations. Registering a nil
// callback must fail and return -1 without changing the count.
func TestSubscribe(t *testing.T) {
	em := NewEventManager()

	id1 := em.Subscribe(SREVNetDataIn, func(param *EventParam) int {
		return 1
	}, 0)
	if id1 <= 0 {
		t.Fatalf("first Subscribe returned non-positive id %d", id1)
	}

	id2 := em.Subscribe(SREVNetDataIn, func(param *EventParam) int {
		return 2
	}, 0)
	if id2 <= 0 {
		t.Fatalf("second Subscribe returned non-positive id %d", id2)
	}
	if id1 == id2 {
		t.Fatalf("Subscribe returned duplicate ids: %d", id1)
	}

	if got := em.SubscriberCount(SREVNetDataIn); got != 2 {
		t.Fatalf("SubscriberCount after 2 subscribes = %d, want 2", got)
	}

	// Subscribing a nil callback must fail and return -1 without
	// changing the count.
	badID := em.Subscribe(SREVNetDataIn, nil, 0)
	if badID != -1 {
		t.Fatalf("Subscribe(nil) returned %d, want -1", badID)
	}
	if got := em.SubscriberCount(SREVNetDataIn); got != 2 {
		t.Fatalf("SubscriberCount after failed subscribe = %d, want 2", got)
	}
}

// TestUnsubscribe verifies that Unsubscribe removes a subscriber by ID
// and that unregistering an unknown ID is a safe no-op.
func TestUnsubscribe(t *testing.T) {
	em := NewEventManager()

	var calls []int
	var mu sync.Mutex

	id1 := em.Subscribe(SREVNetDataOut, func(param *EventParam) int {
		mu.Lock()
		calls = append(calls, 1)
		mu.Unlock()
		return 1
	}, 0)
	id2 := em.Subscribe(SREVNetDataOut, func(param *EventParam) int {
		mu.Lock()
		calls = append(calls, 2)
		mu.Unlock()
		return 2
	}, 0)

	if got := em.SubscriberCount(SREVNetDataOut); got != 2 {
		t.Fatalf("SubscriberCount = %d, want 2", got)
	}

	em.Unsubscribe(id1)
	if got := em.SubscriberCount(SREVNetDataOut); got != 1 {
		t.Fatalf("SubscriberCount after unsubscribe id1 = %d, want 1", got)
	}

	// Remaining callback must still execute.
	calls = nil
	em.Dispatch(SREVNetDataOut, &EventParam{Data: "x"})
	mu.Lock()
	if len(calls) != 1 || calls[0] != 2 {
		t.Fatalf("Dispatch after unsubscribe = %v, want [2]", calls)
	}
	mu.Unlock()

	em.Unsubscribe(id2)
	if got := em.SubscriberCount(SREVNetDataOut); got != 0 {
		t.Fatalf("SubscriberCount after unsubscribe id2 = %d, want 0", got)
	}

	// Unregistering an already-removed or unknown id is a no-op.
	em.Unsubscribe(id1)
	em.Unsubscribe(99999)
}

// TestDispatch verifies that Dispatch invokes every subscriber for the
// event, passing the EventParam through, and that dispatching an event
// with no subscribers is a safe no-op.
func TestDispatch(t *testing.T) {
	em := NewEventManager()

	var seen []interface{}
	var mu sync.Mutex

	em.Subscribe(SREVCoreStats, func(param *EventParam) int {
		mu.Lock()
		seen = append(seen, param.Data)
		mu.Unlock()
		return 0
	}, 0)
	em.Subscribe(SREVCoreStats, func(param *EventParam) int {
		mu.Lock()
		seen = append(seen, param.Data)
		mu.Unlock()
		return 0
	}, 0)

	// Dispatching an event with no subscribers must not panic.
	em.Dispatch(SREVNetDataIn, &EventParam{Data: "none"})

	em.Dispatch(SREVCoreStats, &EventParam{Data: "hello"})
	mu.Lock()
	if len(seen) != 2 || seen[0] != "hello" || seen[1] != "hello" {
		t.Fatalf("Dispatch seen = %v, want [hello hello]", seen)
	}
	mu.Unlock()
}

// TestIsEnabled verifies that IsEnabled reports whether any subscribers
// are registered for an event.
func TestIsEnabled(t *testing.T) {
	em := NewEventManager()

	if em.IsEnabled(SREVRunRoute) {
		t.Fatal("IsEnabled = true before any subscribe, want false")
	}

	id := em.Subscribe(SREVRunRoute, func(param *EventParam) int {
		return 0
	}, 0)
	if !em.IsEnabled(SREVRunRoute) {
		t.Fatal("IsEnabled = false after subscribe, want true")
	}

	em.Unsubscribe(id)
	if em.IsEnabled(SREVRunRoute) {
		t.Fatal("IsEnabled = true after unsubscribe, want false")
	}
}

// TestSubscriberCount verifies that SubscriberCount is tracked per
// event and is isolated between events.
func TestSubscriberCount(t *testing.T) {
	em := NewEventManager()

	em.Subscribe(SREVNetDataIn, func(param *EventParam) int { return 0 }, 0)
	em.Subscribe(SREVNetDataIn, func(param *EventParam) int { return 0 }, 0)
	em.Subscribe(SREVNetDataOut, func(param *EventParam) int { return 0 }, 0)

	if got := em.SubscriberCount(SREVNetDataIn); got != 2 {
		t.Errorf("SubscriberCount(SREVNetDataIn) = %d, want 2", got)
	}
	if got := em.SubscriberCount(SREVNetDataOut); got != 1 {
		t.Errorf("SubscriberCount(SREVNetDataOut) = %d, want 1", got)
	}
	if got := em.SubscriberCount(SREVCoreStats); got != 0 {
		t.Errorf("SubscriberCount(SREVCoreStats) = %d, want 0", got)
	}
}

// TestClear verifies that Clear removes all subscribers for one event
// without affecting other events, and that ClearAll removes every
// subscriber.
func TestClear(t *testing.T) {
	em := NewEventManager()

	em.Subscribe(SREVNetDataIn, func(param *EventParam) int { return 0 }, 0)
	em.Subscribe(SREVNetDataIn, func(param *EventParam) int { return 0 }, 0)
	em.Subscribe(SREVNetDataOut, func(param *EventParam) int { return 0 }, 0)

	em.Clear(SREVNetDataIn)

	if got := em.SubscriberCount(SREVNetDataIn); got != 0 {
		t.Errorf("SubscriberCount(SREVNetDataIn) after Clear = %d, want 0", got)
	}
	if got := em.SubscriberCount(SREVNetDataOut); got != 1 {
		t.Errorf("SubscriberCount(SREVNetDataOut) after Clear = %d, want 1 (other event must survive)", got)
	}

	// Clearing an already-empty event is a no-op.
	em.Clear(SREVNetDataIn)

	// ClearAll removes every subscriber across all events.
	em.Subscribe(SREVRunRoute, func(param *EventParam) int { return 0 }, 0)
	em.ClearAll()
	if got := em.SubscriberCount(SREVNetDataOut); got != 0 {
		t.Errorf("SubscriberCount(SREVNetDataOut) after ClearAll = %d, want 0", got)
	}
	if got := em.SubscriberCount(SREVRunRoute); got != 0 {
		t.Errorf("SubscriberCount(SREVRunRoute) after ClearAll = %d, want 0", got)
	}
}

// TestPriority verifies that subscribers are dispatched in descending
// priority order (highest priority first) and that ties are broken by
// registration order (FIFO).
func TestPriority(t *testing.T) {
	em := NewEventManager()

	var order []int
	var mu sync.Mutex

	record := func(tag int) EventCallback {
		return func(param *EventParam) int {
			mu.Lock()
			order = append(order, tag)
			mu.Unlock()
			return tag
		}
	}

	// Register out of priority order.
	em.Subscribe(SREVRunAction, record(10), 10)
	em.Subscribe(SREVRunAction, record(30), 30)
	em.Subscribe(SREVRunAction, record(20), 20)

	em.Dispatch(SREVRunAction, &EventParam{})
	mu.Lock()
	want := []int{30, 20, 10}
	if len(order) != len(want) {
		t.Fatalf("dispatch order = %v, want %v", order, want)
	}
	for i, v := range want {
		if order[i] != v {
			t.Fatalf("dispatch order[%d] = %d, want %d (full order %v)", i, order[i], v, order)
		}
	}
	mu.Unlock()

	// Ties: same priority must preserve registration (FIFO) order.
	em2 := NewEventManager()
	order = nil
	em2.Subscribe(SREVRunAction, record(1), 5)
	em2.Subscribe(SREVRunAction, record(2), 5)
	em2.Subscribe(SREVRunAction, record(3), 5)

	em2.Dispatch(SREVRunAction, &EventParam{})
	mu.Lock()
	wantFIFO := []int{1, 2, 3}
	for i, v := range wantFIFO {
		if order[i] != v {
			t.Fatalf("FIFO tie[%d] = %d, want %d (full order %v)", i, order[i], v, order)
		}
	}
	mu.Unlock()
}

// TestMultipleEvents verifies that subscribers registered under different
// events are isolated: dispatching one event never runs another event's
// subscribers.
func TestMultipleEvents(t *testing.T) {
	em := NewEventManager()

	hits := make(map[int]int)
	var mu sync.Mutex

	register := func(eventID int) {
		em.Subscribe(eventID, func(param *EventParam) int {
			mu.Lock()
			hits[eventID]++
			mu.Unlock()
			return eventID
		}, 0)
	}

	register(SREVNetDataIn)
	register(SREVNetDataOut)
	register(SREVCoreStats)
	register(SREVRunRoute)
	register(SREVRunAction)

	em.Dispatch(SREVNetDataIn, &EventParam{})
	em.Dispatch(SREVRunAction, &EventParam{})

	mu.Lock()
	defer mu.Unlock()
	if hits[SREVNetDataIn] != 1 {
		t.Errorf("SREVNetDataIn hit %d times, want 1", hits[SREVNetDataIn])
	}
	if hits[SREVRunAction] != 1 {
		t.Errorf("SREVRunAction hit %d times, want 1", hits[SREVRunAction])
	}
	for _, ev := range []int{SREVNetDataOut, SREVCoreStats, SREVRunRoute} {
		if hits[ev] != 0 {
			t.Errorf("event %d hit %d times, want 0 (must be isolated)", ev, hits[ev])
		}
	}
}

// TestConcurrentDispatch exercises the manager under concurrent
// subscription, dispatch and unsubscription to validate the race-free
// locking (run with -race).
func TestConcurrentDispatch(t *testing.T) {
	em := NewEventManager()

	var dispatched int64

	cb := func(param *EventParam) int {
		atomic.AddInt64(&dispatched, 1)
		return 1
	}

	// Pre-populate so Dispatch always has something to run.
	for i := 0; i < 10; i++ {
		em.Subscribe(SREVNetDataIn, cb, i)
	}

	var wg sync.WaitGroup
	const goroutines = 50

	// Writers: subscribe and unsubscribe.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(prio int) {
			defer wg.Done()
			id := em.Subscribe(SREVNetDataIn, cb, prio)
			em.Dispatch(SREVNetDataIn, &EventParam{Data: "w"})
			em.Unsubscribe(id)
		}(i)
	}

	// Readers: dispatch and count concurrently.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			em.Dispatch(SREVNetDataIn, &EventParam{Data: "r"})
			em.SubscriberCount(SREVNetDataIn)
			em.IsEnabled(SREVNetDataIn)
		}()
	}

	wg.Wait()

	// The manager must remain usable after the storm.
	em.Clear(SREVNetDataIn)
	if got := em.SubscriberCount(SREVNetDataIn); got != 0 {
		t.Errorf("SubscriberCount after Clear = %d, want 0", got)
	}
	if dispatched <= 0 {
		t.Error("no callbacks dispatched during concurrent run")
	}
}
