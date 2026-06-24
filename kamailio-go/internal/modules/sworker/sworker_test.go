// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SWorker module tests.
 */
package sworker

import (
	"sync"
	"testing"
)

func TestSubmitAndGetResult(t *testing.T) {
	m := NewSWorkerModule()
	id := m.Submit("hello")
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
	if m.PendingCount() != 1 {
		t.Errorf("PendingCount = %d, want 1", m.PendingCount())
	}
	res, ok := m.GetResult(id)
	if !ok {
		t.Fatal("expected result available")
	}
	if res != "processed:hello" {
		t.Errorf("result = %q, want processed:hello", res)
	}
	// After retrieval the task is no longer pending.
	if m.PendingCount() != 0 {
		t.Errorf("PendingCount after GetResult = %d, want 0", m.PendingCount())
	}
	// A second retrieval fails.
	if _, ok := m.GetResult(id); ok {
		t.Error("expected second GetResult to fail")
	}
	// Unknown id.
	if _, ok := m.GetResult(99999); ok {
		t.Error("expected GetResult(unknown) to fail")
	}
}

func TestStartStop(t *testing.T) {
	m := NewSWorkerModule()
	if m.IsRunning() {
		t.Fatal("expected not running initially")
	}
	m.Start(4)
	if !m.IsRunning() {
		t.Error("expected running after Start")
	}
	// Start while running is a no-op (does not panic or spawn extra).
	m.Start(2)
	if !m.IsRunning() {
		t.Error("expected still running after second Start")
	}
	m.Stop()
	if m.IsRunning() {
		t.Error("expected not running after Stop")
	}
	// Stop when not running is a no-op.
	m.Stop()
}

func TestSubmitWhileRunning(t *testing.T) {
	m := NewSWorkerModule()
	m.Start(2)
	defer m.Stop()
	var ids []int
	for i := 0; i < 5; i++ {
		ids = append(ids, m.Submit("task"))
	}
	if m.PendingCount() != 5 {
		t.Errorf("PendingCount = %d, want 5", m.PendingCount())
	}
	for i, id := range ids {
		res, ok := m.GetResult(id)
		if !ok {
			t.Errorf("GetResult(%d) failed", i)
			continue
		}
		if res != "processed:task" {
			t.Errorf("result %d = %q", i, res)
		}
	}
}

func TestConcurrentSWorker(t *testing.T) {
	m := NewSWorkerModule()
	m.Start(4)
	defer m.Stop()
	var wg sync.WaitGroup
	const goroutines = 20
	const perG = 10
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				id := m.Submit("x")
				_, _ = m.GetResult(id)
			}
		}()
	}
	wg.Wait()
	if m.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0", m.PendingCount())
	}
}
