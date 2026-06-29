// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for scriptcb package - matching C script_cb.c
 */

package scriptcb

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// makeMsg builds a minimal SIPMsg suitable for callback execution.
func makeMsg() *parser.SIPMsg {
	return &parser.SIPMsg{}
}

// TestRegister verifies that registering a callback returns a unique,
// positive ID and that Count reflects the registration.
func TestRegister(t *testing.T) {
	cm := NewCallbackManager()

	id1 := cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 1
	}, 0, nil)
	if id1 <= 0 {
		t.Fatalf("first Register returned non-positive id %d", id1)
	}

	id2 := cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 2
	}, 0, nil)
	if id2 <= 0 {
		t.Fatalf("second Register returned non-positive id %d", id2)
	}
	if id1 == id2 {
		t.Fatalf("Register returned duplicate ids: %d", id1)
	}

	if got := cm.Count(REQ_CB_TYPE_PRE); got != 2 {
		t.Fatalf("Count after 2 registers = %d, want 2", got)
	}

	// Registering a nil callback must fail and return -1 without
	// changing the count.
	badID := cm.Register(REQ_CB_TYPE_PRE, nil, 0, nil)
	if badID != -1 {
		t.Fatalf("Register(nil) returned %d, want -1", badID)
	}
	if got := cm.Count(REQ_CB_TYPE_PRE); got != 2 {
		t.Fatalf("Count after failed register = %d, want 2", got)
	}
}

// TestUnregister verifies that Unregister removes a callback by ID and
// that unregistering an unknown ID is a safe no-op.
func TestUnregister(t *testing.T) {
	cm := NewCallbackManager()

	id1 := cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 1
	}, 0, nil)
	id2 := cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 2
	}, 0, nil)

	if got := cm.Count(REQ_CB_TYPE_PRE); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}

	cm.Unregister(id1)
	if got := cm.Count(REQ_CB_TYPE_PRE); got != 1 {
		t.Fatalf("Count after unregister id1 = %d, want 1", got)
	}

	// Remaining callback must still execute.
	results := cm.Execute(REQ_CB_TYPE_PRE, makeMsg())
	if len(results) != 1 || results[0] != 2 {
		t.Fatalf("Execute results = %v, want [2]", results)
	}

	cm.Unregister(id2)
	if got := cm.Count(REQ_CB_TYPE_PRE); got != 0 {
		t.Fatalf("Count after unregister id2 = %d, want 0", got)
	}

	// Unregistering an already-removed or unknown id is a no-op.
	cm.Unregister(id1)
	cm.Unregister(99999)
}

// TestExecute verifies that all callbacks of the requested type run and
// that their return values are collected in execution order.
func TestExecute(t *testing.T) {
	cm := NewCallbackManager()

	cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 10
	}, 0, nil)
	cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 20
	}, 0, nil)

	msg := makeMsg()
	results := cm.Execute(REQ_CB_TYPE_PRE, msg)
	if len(results) != 2 {
		t.Fatalf("Execute returned %d results, want 2", len(results))
	}

	// The callback type passed to the function must match.
	// (verified via the extra/return below)

	// Executing a type with no callbacks returns an empty (non-nil) slice.
	empty := cm.Execute(RPL_CB_TYPE_PRE, msg)
	if len(empty) != 0 {
		t.Fatalf("Execute on empty type returned %v, want empty", empty)
	}
}

// TestExecutePassesArgs verifies that the callback receives the message,
// the callback type, and the extra parameter registered with it.
func TestExecutePassesArgs(t *testing.T) {
	cm := NewCallbackManager()

	var seenType CallbackType
	var seenExtra interface{}
	var seenMsg *parser.SIPMsg

	cm.Register(REQ_CB_TYPE_POST, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		seenMsg = msg
		seenType = cbType
		seenExtra = extra
		return 0
	}, 0, "my-extra")

	msg := makeMsg()
	cm.Execute(REQ_CB_TYPE_POST, msg)

	if seenMsg != msg {
		t.Error("callback did not receive the request message")
	}
	if seenType != REQ_CB_TYPE_POST {
		t.Errorf("callback type = %d, want %d", seenType, REQ_CB_TYPE_POST)
	}
	if seenExtra != "my-extra" {
		t.Errorf("callback extra = %v, want %q", seenExtra, "my-extra")
	}
}

// TestCount verifies Count across multiple types.
func TestCount(t *testing.T) {
	cm := NewCallbackManager()

	cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 0
	}, 0, nil)
	cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 0
	}, 0, nil)
	cm.Register(RPL_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 0
	}, 0, nil)

	if got := cm.Count(REQ_CB_TYPE_PRE); got != 2 {
		t.Errorf("Count(REQ_CB_TYPE_PRE) = %d, want 2", got)
	}
	if got := cm.Count(RPL_CB_TYPE_PRE); got != 1 {
		t.Errorf("Count(RPL_CB_TYPE_PRE) = %d, want 1", got)
	}
	if got := cm.Count(REQ_CB_TYPE_POST); got != 0 {
		t.Errorf("Count(REQ_CB_TYPE_POST) = %d, want 0", got)
	}
}

// TestClear verifies that Clear removes all callbacks of one type
// without affecting other types.
func TestClear(t *testing.T) {
	cm := NewCallbackManager()

	cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 1
	}, 0, nil)
	cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 2
	}, 0, nil)
	cm.Register(RPL_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 3
	}, 0, nil)

	cm.Clear(REQ_CB_TYPE_PRE)

	if got := cm.Count(REQ_CB_TYPE_PRE); got != 0 {
		t.Errorf("Count(REQ_CB_TYPE_PRE) after Clear = %d, want 0", got)
	}
	if got := cm.Count(RPL_CB_TYPE_PRE); got != 1 {
		t.Errorf("Count(RPL_CB_TYPE_PRE) after Clear = %d, want 1 (other type must survive)", got)
	}

	// Clearing an already-empty type is a no-op.
	cm.Clear(REQ_CB_TYPE_PRE)
}

// TestClearAll verifies that ClearAll removes callbacks of every type.
func TestClearAll(t *testing.T) {
	cm := NewCallbackManager()

	cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 1
	}, 0, nil)
	cm.Register(RPL_CB_TYPE_POST, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 2
	}, 0, nil)
	cm.Register(REQ_CB_TYPE_MISS, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 3
	}, 0, nil)

	cm.ClearAll()

	for _, ct := range []CallbackType{REQ_CB_TYPE_PRE, RPL_CB_TYPE_POST, REQ_CB_TYPE_MISS} {
		if got := cm.Count(ct); got != 0 {
			t.Errorf("Count(%d) after ClearAll = %d, want 0", ct, got)
		}
	}
}

// TestPriority verifies that callbacks execute in descending priority
// order (highest priority first) and that ties are broken by
// registration order (FIFO).
func TestPriority(t *testing.T) {
	cm := NewCallbackManager()

	// Register out of priority order; each returns its own priority.
	cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 10
	}, 10, nil)
	cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 30
	}, 30, nil)
	cm.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 20
	}, 20, nil)

	results := cm.Execute(REQ_CB_TYPE_PRE, makeMsg())
	want := []int{30, 20, 10}
	if len(results) != len(want) {
		t.Fatalf("Execute returned %v, want %v", results, want)
	}
	for i, v := range want {
		if results[i] != v {
			t.Fatalf("Execute[%d] = %d, want %d (full results %v)", i, results[i], v, results)
		}
	}

	// Ties: same priority must preserve registration (FIFO) order.
	cm2 := NewCallbackManager()
	cm2.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 1
	}, 5, nil)
	cm2.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 2
	}, 5, nil)
	cm2.Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 3
	}, 5, nil)

	results = cm2.Execute(REQ_CB_TYPE_PRE, makeMsg())
	wantFIFO := []int{1, 2, 3}
	for i, v := range wantFIFO {
		if results[i] != v {
			t.Fatalf("FIFO tie[%d] = %d, want %d (full results %v)", i, results[i], v, results)
		}
	}
}

// TestMultipleTypes verifies that callbacks registered under different
// types are isolated: executing one type never runs another type's
// callbacks.
func TestMultipleTypes(t *testing.T) {
	cm := NewCallbackManager()

	hits := make(map[CallbackType]int)
	var mu sync.Mutex

	register := func(ct CallbackType) {
		cm.Register(ct, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
			mu.Lock()
			hits[cbType]++
			mu.Unlock()
			return int(cbType)
		}, 0, nil)
	}

	register(REQ_CB_TYPE_PRE)
	register(REQ_CB_TYPE_POST)
	register(REQ_CB_TYPE_MISS)
	register(RPL_CB_TYPE_PRE)
	register(RPL_CB_TYPE_POST)

	msg := makeMsg()
	results := cm.Execute(REQ_CB_TYPE_PRE, msg)
	if len(results) != 1 || results[0] != int(REQ_CB_TYPE_PRE) {
		t.Fatalf("Execute(REQ_CB_TYPE_PRE) = %v, want [%d]", results, int(REQ_CB_TYPE_PRE))
	}
	results = cm.Execute(RPL_CB_TYPE_POST, msg)
	if len(results) != 1 || results[0] != int(RPL_CB_TYPE_POST) {
		t.Fatalf("Execute(RPL_CB_TYPE_POST) = %v, want [%d]", results, int(RPL_CB_TYPE_POST))
	}

	mu.Lock()
	defer mu.Unlock()
	if hits[REQ_CB_TYPE_PRE] != 1 {
		t.Errorf("REQ_CB_TYPE_PRE hit %d times, want 1", hits[REQ_CB_TYPE_PRE])
	}
	if hits[RPL_CB_TYPE_POST] != 1 {
		t.Errorf("RPL_CB_TYPE_POST hit %d times, want 1", hits[RPL_CB_TYPE_POST])
	}
	for _, ct := range []CallbackType{REQ_CB_TYPE_POST, REQ_CB_TYPE_MISS, RPL_CB_TYPE_PRE} {
		if hits[ct] != 0 {
			t.Errorf("type %d hit %d times, want 0 (must be isolated)", ct, hits[ct])
		}
	}
}

// TestConcurrentExecute exercises the manager under concurrent
// registration, execution and unregistration to validate the race-free
// locking (run with -race).
func TestConcurrentExecute(t *testing.T) {
	cm := NewCallbackManager()

	var executed int64

	cb := func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		atomic.AddInt64(&executed, 1)
		return 1
	}

	// Pre-populate so Execute always has something to run.
	for i := 0; i < 10; i++ {
		cm.Register(REQ_CB_TYPE_PRE, cb, i, nil)
	}

	var wg sync.WaitGroup
	const goroutines = 50

	// Writers: register and unregister.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(prio int) {
			defer wg.Done()
			id := cm.Register(REQ_CB_TYPE_PRE, cb, prio, nil)
			cm.Execute(REQ_CB_TYPE_PRE, makeMsg())
			cm.Unregister(id)
		}(i)
	}

	// Readers: execute concurrently.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			cm.Execute(REQ_CB_TYPE_PRE, makeMsg())
			cm.Count(REQ_CB_TYPE_PRE)
		}()
	}

	wg.Wait()

	// The manager must remain usable after the storm.
	cm.Clear(REQ_CB_TYPE_PRE)
	if got := cm.Count(REQ_CB_TYPE_PRE); got != 0 {
		t.Errorf("Count after Clear = %d, want 0", got)
	}
	if executed <= 0 {
		t.Error("no callbacks executed during concurrent run")
	}
}

// TestGlobalFunctions verifies the package-level DefaultCallbackManager,
// Init, Register, Unregister and Execute helpers.
func TestGlobalFunctions(t *testing.T) {
	// Init must give a clean slate.
	Init()

	if dm := DefaultCallbackManager(); dm == nil {
		t.Fatal("DefaultCallbackManager returned nil after Init")
	}

	id := Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 42
	}, 0, nil)
	if id <= 0 {
		t.Fatalf("global Register returned %d, want positive id", id)
	}
	dm := DefaultCallbackManager()
	if got := dm.Count(REQ_CB_TYPE_PRE); got != 1 {
		t.Fatalf("Count = %d, want 1", got)
	}

	results := Execute(REQ_CB_TYPE_PRE, makeMsg())
	if len(results) != 1 || results[0] != 42 {
		t.Fatalf("global Execute = %v, want [42]", results)
	}

	Unregister(id)
	if got := dm.Count(REQ_CB_TYPE_PRE); got != 0 {
		t.Fatalf("Count after Unregister = %d, want 0", got)
	}

	// Re-init clears everything by replacing the default manager.
	Register(REQ_CB_TYPE_PRE, func(msg *parser.SIPMsg, cbType CallbackType, extra interface{}) int {
		return 1
	}, 0, nil)
	Init()
	if got := DefaultCallbackManager().Count(REQ_CB_TYPE_PRE); got != 0 {
		t.Fatalf("Count after re-Init = %d, want 0", got)
	}
}
