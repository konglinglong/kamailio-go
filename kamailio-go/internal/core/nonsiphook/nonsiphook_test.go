// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the nonsiphook package - matching C nonsip_hooks.c / nonsip_hooks.h.
 */

package nonsiphook

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// makeMsg creates a minimal SIPMsg for testing.
func makeMsg(t *testing.T) *parser.SIPMsg {
	t.Helper()
	raw := []byte("OPTIONS sip:test@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1\r\n" +
		"From: <sip:a@b>;tag=1\r\n" +
		"To: <sip:a@b>\r\n" +
		"Call-ID: test@localhost\r\n" +
		"CSeq: 1 OPTIONS\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("ParseMsg error: %v", err)
	}
	return msg
}

// ============================================================
// HookType
// ============================================================

// TestHookTypeString verifies the String() method.
func TestHookTypeString(t *testing.T) {
	tests := []struct {
		ht   HookType
		want string
	}{
		{HookPre, "pre"},
		{HookPost, "post"},
		{HookError, "error"},
		{HookType(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.ht.String(); got != tt.want {
			t.Errorf("HookType(%d).String() = %q, want %q", int(tt.ht), got, tt.want)
		}
	}
}

// ============================================================
// Register
// ============================================================

// TestRegister verifies that a hook is added and sorted by priority.
func TestRegister(t *testing.T) {
	m := New()

	h1 := &Hook{Name: "h1", Type: HookPre, Priority: 10, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }}
	h2 := &Hook{Name: "h2", Type: HookPre, Priority: 20, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }}
	h3 := &Hook{Name: "h3", Type: HookPre, Priority: 5, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }}

	if err := m.Register(h1); err != nil {
		t.Fatalf("Register h1 error: %v", err)
	}
	if err := m.Register(h2); err != nil {
		t.Fatalf("Register h2 error: %v", err)
	}
	if err := m.Register(h3); err != nil {
		t.Fatalf("Register h3 error: %v", err)
	}

	// Must be sorted by descending priority: h2(20), h1(10), h3(5).
	list := m.ListType(HookPre)
	if len(list) != 3 {
		t.Fatalf("ListType count = %d, want 3", len(list))
	}
	if list[0].Name != "h2" {
		t.Errorf("list[0].Name = %q, want %q", list[0].Name, "h2")
	}
	if list[1].Name != "h1" {
		t.Errorf("list[1].Name = %q, want %q", list[1].Name, "h1")
	}
	if list[2].Name != "h3" {
		t.Errorf("list[2].Name = %q, want %q", list[2].Name, "h3")
	}
}

// TestRegisterSamePriorityFIFO verifies that hooks with the same priority
// preserve registration (FIFO) order.
func TestRegisterSamePriorityFIFO(t *testing.T) {
	m := New()

	for _, name := range []string{"a", "b", "c", "d"} {
		h := &Hook{Name: name, Type: HookPost, Priority: 100, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }}
		if err := m.Register(h); err != nil {
			t.Fatalf("Register %s error: %v", name, err)
		}
	}

	list := m.ListType(HookPost)
	if len(list) != 4 {
		t.Fatalf("ListType count = %d, want 4", len(list))
	}
	for i, want := range []string{"a", "b", "c", "d"} {
		if list[i].Name != want {
			t.Errorf("list[%d].Name = %q, want %q", i, list[i].Name, want)
		}
	}
}

// TestRegisterNilHook verifies that a nil hook is rejected.
func TestRegisterNilHook(t *testing.T) {
	m := New()
	if err := m.Register(nil); err == nil {
		t.Error("Register(nil) should return error")
	}
}

// TestRegisterEmptyName verifies that an empty name is rejected.
func TestRegisterEmptyName(t *testing.T) {
	m := New()
	h := &Hook{Name: "", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }}
	if err := m.Register(h); err == nil {
		t.Error("Register with empty name should return error")
	}
}

// TestRegisterNilCallback verifies that a nil callback is rejected.
func TestRegisterNilCallback(t *testing.T) {
	m := New()
	h := &Hook{Name: "test", Type: HookPre, Priority: 0, Callback: nil}
	if err := m.Register(h); err == nil {
		t.Error("Register with nil callback should return error")
	}
}

// TestRegisterDifferentTypes verifies that hooks of different types are
// stored independently.
func TestRegisterDifferentTypes(t *testing.T) {
	m := New()

	m.Register(&Hook{Name: "pre", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	m.Register(&Hook{Name: "post", Type: HookPost, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	m.Register(&Hook{Name: "err", Type: HookError, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})

	if m.CountType(HookPre) != 1 {
		t.Errorf("HookPre count = %d, want 1", m.CountType(HookPre))
	}
	if m.CountType(HookPost) != 1 {
		t.Errorf("HookPost count = %d, want 1", m.CountType(HookPost))
	}
	if m.CountType(HookError) != 1 {
		t.Errorf("HookError count = %d, want 1", m.CountType(HookError))
	}
	if m.Count() != 3 {
		t.Errorf("total count = %d, want 3", m.Count())
	}
}

// ============================================================
// Execute
// ============================================================

// TestExecuteNoHooks verifies that Execute returns NONSIPMsgDrop when no
// hooks are registered (matching C's default).
func TestExecuteNoHooks(t *testing.T) {
	m := New()
	msg := makeMsg(t)
	if ret := m.Execute(HookPre, msg); ret != NONSIPMsgDrop {
		t.Errorf("Execute with no hooks = %d, want %d", ret, NONSIPMsgDrop)
	}
}

// TestExecuteAllPass verifies that when all hooks return PASS, the final
// result is PASS.
func TestExecuteAllPass(t *testing.T) {
	m := New()
	m.Register(&Hook{Name: "p1", Type: HookPre, Priority: 10, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	m.Register(&Hook{Name: "p2", Type: HookPre, Priority: 5, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})

	msg := makeMsg(t)
	if ret := m.Execute(HookPre, msg); ret != NONSIPMsgPass {
		t.Errorf("Execute all-pass = %d, want %d", ret, NONSIPMsgPass)
	}
}

// TestExecuteShortCircuit verifies that a hook returning DROP stops the
// chain and that subsequent hooks are NOT called.
func TestExecuteShortCircuit(t *testing.T) {
	m := New()

	var called []string
	var mu sync.Mutex

	m.Register(&Hook{Name: "first", Type: HookPre, Priority: 20, Callback: func(msg *parser.SIPMsg) int {
		mu.Lock()
		called = append(called, "first")
		mu.Unlock()
		return NONSIPMsgPass
	}})
	m.Register(&Hook{Name: "second", Type: HookPre, Priority: 10, Callback: func(msg *parser.SIPMsg) int {
		mu.Lock()
		called = append(called, "second")
		mu.Unlock()
		return NONSIPMsgDrop
	}})
	m.Register(&Hook{Name: "third", Type: HookPre, Priority: 5, Callback: func(msg *parser.SIPMsg) int {
		mu.Lock()
		called = append(called, "third")
		mu.Unlock()
		return NONSIPMsgAccept
	}})

	msg := makeMsg(t)
	ret := m.Execute(HookPre, msg)

	if ret != NONSIPMsgDrop {
		t.Errorf("Execute = %d, want %d (DROP)", ret, NONSIPMsgDrop)
	}
	if len(called) != 2 {
		t.Errorf("called = %v, want [first second]", called)
	}
	if called[0] != "first" || called[1] != "second" {
		t.Errorf("called order = %v, want [first second]", called)
	}
}

// TestExecutePriorityOrder verifies that hooks run in descending priority
// order.
func TestExecutePriorityOrder(t *testing.T) {
	m := New()

	var order []string
	var mu sync.Mutex

	addHook := func(name string, priority int) {
		m.Register(&Hook{
			Name:     name,
			Type:     HookPost,
			Priority: priority,
			Callback: func(msg *parser.SIPMsg) int {
				mu.Lock()
				order = append(order, name)
				mu.Unlock()
				return NONSIPMsgPass
			},
		})
	}

	addHook("low", 1)
	addHook("high", 100)
	addHook("mid", 50)

	msg := makeMsg(t)
	m.Execute(HookPost, msg)

	want := []string{"high", "mid", "low"}
	if len(order) != 3 {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i, w := range want {
		if order[i] != w {
			t.Errorf("order[%d] = %q, want %q", i, order[i], w)
		}
	}
}

// TestExecuteAccept verifies that ACCEPT stops the chain.
func TestExecuteAccept(t *testing.T) {
	m := New()

	var called int32

	m.Register(&Hook{Name: "accept", Type: HookPre, Priority: 20, Callback: func(msg *parser.SIPMsg) int {
		atomic.AddInt32(&called, 1)
		return NONSIPMsgAccept
	}})
	m.Register(&Hook{Name: "should-not-run", Type: HookPre, Priority: 10, Callback: func(msg *parser.SIPMsg) int {
		atomic.AddInt32(&called, 1)
		return NONSIPMsgPass
	}})

	msg := makeMsg(t)
	ret := m.Execute(HookPre, msg)

	if ret != NONSIPMsgAccept {
		t.Errorf("Execute = %d, want %d (ACCEPT)", ret, NONSIPMsgAccept)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("called count = %d, want 1 (second hook should not run)", atomic.LoadInt32(&called))
	}
}

// TestExecuteIsolatesTypes verifies that Execute only runs hooks of the
// specified type.
func TestExecuteIsolatesTypes(t *testing.T) {
	m := New()

	var preCalled, postCalled int32

	m.Register(&Hook{Name: "pre", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int {
		atomic.AddInt32(&preCalled, 1)
		return NONSIPMsgPass
	}})
	m.Register(&Hook{Name: "post", Type: HookPost, Priority: 0, Callback: func(msg *parser.SIPMsg) int {
		atomic.AddInt32(&postCalled, 1)
		return NONSIPMsgPass
	}})

	msg := makeMsg(t)
	m.Execute(HookPre, msg)

	if atomic.LoadInt32(&preCalled) != 1 {
		t.Errorf("preCalled = %d, want 1", preCalled)
	}
	if atomic.LoadInt32(&postCalled) != 0 {
		t.Errorf("postCalled = %d, want 0", postCalled)
	}
}

// TestExecuteNilMsg verifies that Execute handles a nil message without
// panicking (the callback decides what to do with it).
func TestExecuteNilMsg(t *testing.T) {
	m := New()
	m.Register(&Hook{Name: "nil-test", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int {
		if msg == nil {
			return NONSIPMsgDrop
		}
		return NONSIPMsgPass
	}})

	if ret := m.Execute(HookPre, nil); ret != NONSIPMsgDrop {
		t.Errorf("Execute with nil msg = %d, want %d", ret, NONSIPMsgDrop)
	}
}

// ============================================================
// Unregister
// ============================================================

// TestUnregister verifies that a hook is removed by name.
func TestUnregister(t *testing.T) {
	m := New()
	m.Register(&Hook{Name: "target", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	m.Register(&Hook{Name: "other", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})

	if !m.Unregister("target") {
		t.Error("Unregister returned false for existing hook")
	}
	if m.Count() != 1 {
		t.Errorf("Count after unregister = %d, want 1", m.Count())
	}

	// Unregister a non-existent hook.
	if m.Unregister("nonexistent") {
		t.Error("Unregister returned true for non-existent hook")
	}

	// Unregister with empty name.
	if m.Unregister("") {
		t.Error("Unregister with empty name should return false")
	}
}

// TestUnregisterType verifies type-scoped unregistration.
func TestUnregisterType(t *testing.T) {
	m := New()
	m.Register(&Hook{Name: "dup", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	m.Register(&Hook{Name: "dup", Type: HookPost, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})

	// Remove only the HookPre variant.
	if !m.UnregisterType(HookPre, "dup") {
		t.Error("UnregisterType returned false for existing hook")
	}
	if m.CountType(HookPre) != 0 {
		t.Errorf("HookPre count = %d, want 0", m.CountType(HookPre))
	}
	if m.CountType(HookPost) != 1 {
		t.Errorf("HookPost count = %d, want 1", m.CountType(HookPost))
	}
}

// TestUnregisterAfterExecute verifies that unregistering after Execute
// works correctly (no stale references).
func TestUnregisterAfterExecute(t *testing.T) {
	m := New()
	m.Register(&Hook{Name: "h", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})

	msg := makeMsg(t)
	m.Execute(HookPre, msg)
	m.Unregister("h")

	if m.Execute(HookPre, msg) != NONSIPMsgDrop {
		t.Error("Execute after unregister should return NONSIPMsgDrop (no hooks)")
	}
}

// ============================================================
// List / Count / Clear
// ============================================================

// TestList verifies that List returns all hooks sorted by type then priority.
func TestList(t *testing.T) {
	m := New()
	m.Register(&Hook{Name: "post1", Type: HookPost, Priority: 10, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	m.Register(&Hook{Name: "pre1", Type: HookPre, Priority: 10, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	m.Register(&Hook{Name: "pre2", Type: HookPre, Priority: 20, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})

	list := m.List()
	if len(list) != 3 {
		t.Fatalf("List count = %d, want 3", len(list))
	}

	// HookPre (type 0) should come before HookPost (type 1).
	if list[0].Type != HookPre {
		t.Errorf("list[0].Type = %v, want HookPre", list[0].Type)
	}
	// Within HookPre, higher priority first.
	if list[0].Name != "pre2" {
		t.Errorf("list[0].Name = %q, want %q", list[0].Name, "pre2")
	}
	if list[1].Name != "pre1" {
		t.Errorf("list[1].Name = %q, want %q", list[1].Name, "pre1")
	}
	if list[2].Type != HookPost {
		t.Errorf("list[2].Type = %v, want HookPost", list[2].Type)
	}
}

// TestListReturnsCopy verifies that mutating the returned slice does not
// affect the manager.
func TestListReturnsCopy(t *testing.T) {
	m := New()
	m.Register(&Hook{Name: "h1", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})

	list := m.List()
	list[0] = nil // mutate the returned slice

	// The manager must be unaffected.
	list2 := m.List()
	if list2[0] == nil {
		t.Error("mutating List() result affected the manager")
	}
}

// TestCount verifies the Count method.
func TestCount(t *testing.T) {
	m := New()
	if m.Count() != 0 {
		t.Errorf("Count on empty = %d, want 0", m.Count())
	}

	m.Register(&Hook{Name: "a", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	m.Register(&Hook{Name: "b", Type: HookPost, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})

	if m.Count() != 2 {
		t.Errorf("Count = %d, want 2", m.Count())
	}
}

// TestClear verifies that Clear removes hooks of a specific type.
func TestClear(t *testing.T) {
	m := New()
	m.Register(&Hook{Name: "pre", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	m.Register(&Hook{Name: "post", Type: HookPost, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})

	m.Clear(HookPre)
	if m.CountType(HookPre) != 0 {
		t.Errorf("HookPre count after Clear = %d, want 0", m.CountType(HookPre))
	}
	if m.CountType(HookPost) != 1 {
		t.Errorf("HookPost count after Clear = %d, want 1", m.CountType(HookPost))
	}
}

// TestClearAll verifies that ClearAll removes all hooks.
func TestClearAll(t *testing.T) {
	m := New()
	m.Register(&Hook{Name: "pre", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	m.Register(&Hook{Name: "post", Type: HookPost, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})

	m.ClearAll()
	if m.Count() != 0 {
		t.Errorf("Count after ClearAll = %d, want 0", m.Count())
	}
}

// ============================================================
// Singleton (DefaultManager / Init)
// ============================================================

// TestDefaultManager verifies the singleton lifecycle.
func TestDefaultManager(t *testing.T) {
	Init()

	m1 := DefaultManager()
	if m1 == nil {
		t.Fatal("DefaultManager returned nil")
	}
	m2 := DefaultManager()
	if m1 != m2 {
		t.Error("DefaultManager returned different instances")
	}

	// Register and verify via package helpers.
	Register(&Hook{Name: "default-hook", Type: HookPre, Priority: 0, Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass }})
	if Count := len(List()); Count != 1 {
		t.Errorf("List count = %d, want 1", Count)
	}

	// Init must reset.
	Init()
	if len(List()) != 0 {
		t.Error("List after Init should be empty")
	}
}

// TestPackageHelpers verifies the package-level Register/Execute/Unregister.
func TestPackageHelpers(t *testing.T) {
	Init()

	var called int32
	Register(&Hook{
		Name:     "pkg-test",
		Type:     HookPre,
		Priority: 0,
		Callback: func(msg *parser.SIPMsg) int {
			atomic.AddInt32(&called, 1)
			return NONSIPMsgAccept
		},
	})

	msg := makeMsg(t)
	ret := Execute(HookPre, msg)
	if ret != NONSIPMsgAccept {
		t.Errorf("Execute = %d, want %d", ret, NONSIPMsgAccept)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("called = %d, want 1", called)
	}

	if !Unregister("pkg-test") {
		t.Error("Unregister returned false")
	}
	if Execute(HookPre, msg) != NONSIPMsgDrop {
		t.Error("Execute after unregister should return NONSIPMsgDrop")
	}
}

// ============================================================
// Concurrency tests (run with -race)
// ============================================================

// TestConcurrentRegisterExecute exercises Register and Execute concurrently.
func TestConcurrentRegisterExecute(t *testing.T) {
	m := New()

	var wg sync.WaitGroup
	const goroutines = 50

	// Pre-register some hooks.
	for i := 0; i < 10; i++ {
		m.Register(&Hook{
			Name:     "pre-" + itoa(i),
			Type:     HookPre,
			Priority: i,
			Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass },
		})
	}

	msg := makeMsg(t)

	// Concurrent writers and readers.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			if n%3 == 0 {
				m.Register(&Hook{
					Name:     "concurrent-" + itoa(n),
					Type:     HookPre,
					Priority: n,
					Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass },
				})
			} else if n%3 == 1 {
				m.Execute(HookPre, msg)
			} else {
				m.List()
				m.Count()
			}
		}(i)
	}
	wg.Wait()

	// After the storm, the manager must still be usable.
	m.ClearAll()
	if m.Count() != 0 {
		t.Errorf("Count after ClearAll = %d, want 0", m.Count())
	}
}

// TestConcurrentUnregister verifies that concurrent unregister is safe.
func TestConcurrentUnregister(t *testing.T) {
	m := New()

	// Register many hooks.
	for i := 0; i < 100; i++ {
		m.Register(&Hook{
			Name:     "hook-" + itoa(i),
			Type:     HookPre,
			Priority: 0,
			Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass },
		})
	}

	var wg sync.WaitGroup
	const goroutines = 100

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			m.Unregister("hook-" + itoa(n))
		}(i)
	}
	wg.Wait()

	// All hooks should be removed.
	if m.Count() != 0 {
		t.Errorf("Count after concurrent unregister = %d, want 0", m.Count())
	}
}

// TestConcurrentExecuteNoDeadlock verifies that Execute does not deadlock
// even when callbacks themselves register new hooks.
func TestConcurrentExecuteNoDeadlock(t *testing.T) {
	m := New()

	var wg sync.WaitGroup
	const goroutines = 20

	for i := 0; i < goroutines; i++ {
		m.Register(&Hook{
			Name:     "base-" + itoa(i),
			Type:     HookPre,
			Priority: 0,
			Callback: func(msg *parser.SIPMsg) int { return NONSIPMsgPass },
		})
	}

	msg := makeMsg(t)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			// Execute takes a snapshot and runs callbacks outside the lock,
			// so registering inside a callback is safe.
			m.Execute(HookPre, msg)
		}(i)
	}
	wg.Wait()
}

// ============================================================
// Helpers
// ============================================================

// itoa is a local strconv.Itoa to avoid importing strconv in the test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
