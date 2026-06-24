// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the avpops module.
 */

package avpops

import (
	"sync"
	"testing"
)

func TestAVPSetGetExists(t *testing.T) {
	m := NewAVPOpsModule()

	// Fresh AVP does not exist.
	if m.AVPExists("user") {
		t.Error("AVPExists(user) = true on fresh module, want false")
	}
	if _, ok := m.AVPGet("user"); ok {
		t.Error("AVPGet(user) returned ok=true, want false")
	}

	// Set then get.
	if got := m.AVPSet("user", "alice"); got != 1 {
		t.Errorf("AVPSet(user) = %d, want 1", got)
	}
	if !m.AVPExists("user") {
		t.Error("AVPExists(user) = false after set, want true")
	}
	val, ok := m.AVPGet("user")
	if !ok {
		t.Fatal("AVPGet(user) returned ok=false after set")
	}
	if val != "alice" {
		t.Errorf("AVPGet(user) = %q, want alice", val)
	}

	// Set replaces existing value.
	m.AVPSet("user", "bob")
	val, _ = m.AVPGet("user")
	if val != "bob" {
		t.Errorf("AVPGet(user) after re-set = %q, want bob", val)
	}
	if got := m.AVPCount("user"); got != 1 {
		t.Errorf("AVPCount(user) after set = %d, want 1", got)
	}

	// Empty name rejected.
	if got := m.AVPSet("", "x"); got != -1 {
		t.Errorf("AVPSet(empty) = %d, want -1", got)
	}
	if m.AVPExists("  ") {
		t.Error("AVPExists(whitespace) = true, want false")
	}
}

func TestAVPDelete(t *testing.T) {
	m := NewAVPOpsModule()

	m.AVPSet("a", "1")
	m.AVPAppend("a", "2")
	m.AVPAppend("a", "3")
	if got := m.AVPCount("a"); got != 3 {
		t.Fatalf("AVPCount(a) = %d, want 3", got)
	}

	// Delete returns the number of values removed.
	if got := m.AVPDelete("a"); got != 3 {
		t.Errorf("AVPDelete(a) = %d, want 3", got)
	}
	if m.AVPExists("a") {
		t.Error("AVPExists(a) after delete = true, want false")
	}
	if got := m.AVPCount("a"); got != 0 {
		t.Errorf("AVPCount(a) after delete = %d, want 0", got)
	}

	// Deleting a non-existent AVP returns 0.
	if got := m.AVPDelete("missing"); got != 0 {
		t.Errorf("AVPDelete(missing) = %d, want 0", got)
	}
	// Empty name rejected.
	if got := m.AVPDelete(""); got != -1 {
		t.Errorf("AVPDelete(empty) = %d, want -1", got)
	}
}

func TestAVPAppendAndGetAll(t *testing.T) {
	m := NewAVPOpsModule()

	m.AVPAppend("list", "one")
	m.AVPAppend("list", "two")
	m.AVPAppend("list", "three")
	if got := m.AVPCount("list"); got != 3 {
		t.Fatalf("AVPCount(list) = %d, want 3", got)
	}

	all := m.AVPGetAll("list")
	want := []string{"one", "two", "three"}
	if len(all) != len(want) {
		t.Fatalf("AVPGetAll(list) = %v, want %v", all, want)
	}
	for i, v := range want {
		if all[i] != v {
			t.Errorf("AVPGetAll(list)[%d] = %q, want %q", i, all[i], v)
		}
	}

	// AVPGet returns the first value.
	if v, ok := m.AVPGet("list"); !ok || v != "one" {
		t.Errorf("AVPGet(list) = %q,%v, want one,true", v, ok)
	}

	// AVPSet replaces the whole list.
	m.AVPSet("list", "only")
	if all := m.AVPGetAll("list"); len(all) != 1 || all[0] != "only" {
		t.Errorf("AVPGetAll after set = %v, want [only]", all)
	}

	// Empty name rejected.
	if got := m.AVPAppend("", "x"); got != -1 {
		t.Errorf("AVPAppend(empty) = %d, want -1", got)
	}
}

func TestAVPPrintf(t *testing.T) {
	m := NewAVPOpsModule()

	if got := m.AVPPrintf("greeting", "hello %s, you have %d messages", "alice", 5); got != 1 {
		t.Errorf("AVPPrintf = %d, want 1", got)
	}
	val, ok := m.AVPGet("greeting")
	if !ok {
		t.Fatal("AVPGet(greeting) returned ok=false after printf")
	}
	if val != "hello alice, you have 5 messages" {
		t.Errorf("AVPGet(greeting) = %q, want formatted string", val)
	}

	// Printf with no args just stores the format.
	m.AVPPrintf("plain", "no-args")
	if v, _ := m.AVPGet("plain"); v != "no-args" {
		t.Errorf("AVPGet(plain) = %q, want no-args", v)
	}

	// Empty name rejected.
	if got := m.AVPPrintf("", "%s", "x"); got != -1 {
		t.Errorf("AVPPrintf(empty) = %d, want -1", got)
	}
}

func TestAVPCopy(t *testing.T) {
	m := NewAVPOpsModule()

	// Source with multiple values.
	m.AVPAppend("src", "a")
	m.AVPAppend("src", "b")

	// Copy to a fresh destination.
	if got := m.AVPCopy("src", "dst"); got != 2 {
		t.Errorf("AVPCopy(src,dst) = %d, want 2", got)
	}
	dst := m.AVPGetAll("dst")
	if len(dst) != 2 || dst[0] != "a" || dst[1] != "b" {
		t.Errorf("AVPGetAll(dst) = %v, want [a b]", dst)
	}

	// Copy appends to an existing destination.
	m.AVPSet("dst", "pre")
	if got := m.AVPCopy("src", "dst"); got != 2 {
		t.Errorf("AVPCopy(src,dst) append = %d, want 2", got)
	}
	dst = m.AVPGetAll("dst")
	want := []string{"pre", "a", "b"}
	if len(dst) != len(want) {
		t.Fatalf("AVPGetAll(dst) after append = %v, want %v", dst, want)
	}
	for i, v := range want {
		if dst[i] != v {
			t.Errorf("AVPGetAll(dst)[%d] = %q, want %q", i, dst[i], v)
		}
	}

	// Mutating the source after copy does not affect the destination.
	m.AVPSet("src", "changed")
	if v, _ := m.AVPGet("dst"); v != "pre" {
		t.Errorf("dst mutated after source change: %q, want pre", v)
	}

	// Copy from a non-existent source returns 0.
	if got := m.AVPCopy("missing", "dst2"); got != 0 {
		t.Errorf("AVPCopy(missing,...) = %d, want 0", got)
	}
	// Empty names rejected.
	if got := m.AVPCopy("", "dst"); got != -1 {
		t.Errorf("AVPCopy(empty src) = %d, want -1", got)
	}
	if got := m.AVPCopy("src", ""); got != -1 {
		t.Errorf("AVPCopy(empty dst) = %d, want -1", got)
	}
}

func TestAVPClearAndNames(t *testing.T) {
	m := NewAVPOpsModule()
	m.AVPSet("a", "1")
	m.AVPSet("b", "2")
	m.AVPSet("c", "3")

	names := m.AVPNames()
	if len(names) != 3 {
		t.Fatalf("len(AVPNames) = %d, want 3", len(names))
	}

	m.AVPClear()
	if len(m.AVPNames()) != 0 {
		t.Errorf("len(AVPNames) after clear = %d, want 0", len(m.AVPNames()))
	}
	if m.AVPExists("a") {
		t.Error("AVPExists(a) after clear = true, want false")
	}
}

func TestDefaultAndInit(t *testing.T) {
	d := DefaultAVPOps()
	if d == nil {
		t.Fatal("DefaultAVPOps() = nil")
	}
	d.AVPSet("temp", "1")
	if v, ok := d.AVPGet("temp"); !ok || v != "1" {
		t.Errorf("AVPGet(temp) = %q,%v, want 1,true", v, ok)
	}

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	d2 := DefaultAVPOps()
	if d2.AVPExists("temp") {
		t.Error("Init() did not reset the default instance")
	}

	// Package-level wrappers work on the default instance.
	AVPSet("g", "hello")
	if v, ok := AVPGet("g"); !ok || v != "hello" {
		t.Errorf("AVPGet(g) = %q,%v, want hello,true", v, ok)
	}
	if !AVPExists("g") {
		t.Error("AVPExists(g) = false, want true")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewAVPOpsModule()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.AVPAppend("shared", "v")
			m.AVPSet("set", "x")
			_, _ = m.AVPGet("shared")
			_ = m.AVPCount("shared")
			_ = m.AVPExists("shared")
			_ = m.AVPGetAll("shared")
			if i%10 == 0 {
				m.AVPClear()
			}
		}(i)
	}
	wg.Wait()
}
