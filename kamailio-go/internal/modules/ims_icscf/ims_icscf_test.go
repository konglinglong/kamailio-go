// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS I-CSCF module.
 */

package ims_icscf

import (
	"sync"
	"testing"
)

func TestRegisterAndCount(t *testing.T) {
	m := NewICSCFModule()
	if m.CountSCSCFs() != 0 {
		t.Errorf("CountSCSCFs = %d, want 0", m.CountSCSCFs())
	}
	m.RegisterSCSCF("scscf1@example.com", 100, 1)
	m.RegisterSCSCF("scscf2@example.com", 200, 1)
	if got := m.CountSCSCFs(); got != 2 {
		t.Errorf("CountSCSCFs = %d, want 2", got)
	}
	// Registering the same name replaces the entry.
	m.RegisterSCSCF("scscf1@example.com", 500, 0)
	if got := m.CountSCSCFs(); got != 2 {
		t.Errorf("CountSCSCFs after re-register = %d, want 2", got)
	}
	// Empty name is ignored.
	m.RegisterSCSCF("", 1, 1)
	if got := m.CountSCSCFs(); got != 2 {
		t.Errorf("CountSCSCFs after empty register = %d, want 2", got)
	}
}

func TestUnregister(t *testing.T) {
	m := NewICSCFModule()
	m.RegisterSCSCF("scscf1@example.com", 100, 1)
	m.RegisterSCSCF("scscf2@example.com", 200, 1)
	m.UnregisterSCSCF("scscf1@example.com")
	if got := m.CountSCSCFs(); got != 1 {
		t.Errorf("CountSCSCFs after unregister = %d, want 1", got)
	}
	if got := m.ListSCSCFs(); len(got) != 1 || got[0].SCSCF != "scscf2@example.com" {
		t.Errorf("ListSCSCFs after unregister = %v", got)
	}
}

func TestSelectSCSCF(t *testing.T) {
	m := NewICSCFModule()
	if got := m.SelectSCSCF(); got != "" {
		t.Errorf("SelectSCSCF on empty module = %q, want empty", got)
	}
	// Lower priority value = higher precedence.
	m.RegisterSCSCF("low@example.com", 1000, 5)
	m.RegisterSCSCF("high@example.com", 10, 1)
	if got := m.SelectSCSCF(); got != "high@example.com" {
		t.Errorf("SelectSCSCF = %q, want high@example.com", got)
	}
	// Equal priority: highest capacity wins.
	m2 := NewICSCFModule()
	m2.RegisterSCSCF("small@example.com", 50, 1)
	m2.RegisterSCSCF("big@example.com", 500, 1)
	if got := m2.SelectSCSCF(); got != "big@example.com" {
		t.Errorf("SelectSCSCF = %q, want big@example.com", got)
	}
}

func TestAssignSCSCF(t *testing.T) {
	m := NewICSCFModule()
	if _, err := m.AssignSCSCF("sub1"); err == nil {
		t.Errorf("AssignSCSCF with no S-CSCF should error")
	}
	if _, err := m.AssignSCSCF(""); err == nil {
		t.Errorf("AssignSCSCF with empty subscriber should error")
	}
	m.RegisterSCSCF("scscf1@example.com", 100, 1)
	m.RegisterSCSCF("scscf2@example.com", 200, 1)

	name, err := m.AssignSCSCF("sub1")
	if err != nil {
		t.Fatalf("AssignSCSCF failed: %v", err)
	}
	if name != "scscf2@example.com" {
		t.Errorf("AssignSCSCF = %q, want scscf2@example.com (highest capacity)", name)
	}
	// Re-assigning returns the same S-CSCF.
	name2, err := m.AssignSCSCF("sub1")
	if err != nil {
		t.Fatalf("AssignSCSCF second call failed: %v", err)
	}
	if name2 != name {
		t.Errorf("AssignSCSCF second call = %q, want %q", name2, name)
	}
	if got := m.GetSCSCF("sub1"); got != name {
		t.Errorf("GetSCSCF = %q, want %q", got, name)
	}
}

func TestAssignSCSCFStaleAssignment(t *testing.T) {
	m := NewICSCFModule()
	m.RegisterSCSCF("scscf1@example.com", 100, 1)
	m.RegisterSCSCF("scscf2@example.com", 200, 1)
	name, _ := m.AssignSCSCF("sub1")
	if name != "scscf2@example.com" {
		t.Fatalf("initial assignment = %q, want scscf2@example.com", name)
	}
	// Unregister the assigned S-CSCF: the assignment becomes stale and
	// a new one must be selected.
	m.UnregisterSCSCF("scscf2@example.com")
	name2, err := m.AssignSCSCF("sub1")
	if err != nil {
		t.Fatalf("AssignSCSCF after stale: %v", err)
	}
	if name2 != "scscf1@example.com" {
		t.Errorf("AssignSCSCF after stale = %q, want scscf1@example.com", name2)
	}
}

func TestListSCSCFs(t *testing.T) {
	m := NewICSCFModule()
	m.RegisterSCSCF("scscf1@example.com", 100, 1)
	m.RegisterSCSCF("scscf2@example.com", 200, 2)
	list := m.ListSCSCFs()
	if len(list) != 2 {
		t.Fatalf("ListSCSCFs len = %d, want 2", len(list))
	}
	seen := map[string]bool{}
	for _, r := range list {
		seen[r.SCSCF] = true
		if r.Capacity <= 0 {
			t.Errorf("SCSCF %q capacity = %d, want > 0", r.SCSCF, r.Capacity)
		}
		if r.RegisteredAt.IsZero() {
			t.Errorf("SCSCF %q RegisteredAt is zero", r.SCSCF)
		}
	}
	if !seen["scscf1@example.com"] || !seen["scscf2@example.com"] {
		t.Errorf("ListSCSCFs missing entries: %v", seen)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewICSCFModule()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.RegisterSCSCF("scscf1@example.com", 100, 1)
			m.SelectSCSCF()
			m.AssignSCSCF("sub")
			m.GetSCSCF("sub")
			m.CountSCSCFs()
			m.ListSCSCFs()
		}(i)
	}
	wg.Wait()
	if m.CountSCSCFs() != 1 {
		t.Errorf("CountSCSCFs after concurrent access = %d, want 1", m.CountSCSCFs())
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	im := DefaultICSCF()
	if im == nil {
		t.Fatal("expected non-nil default I-CSCF module")
	}
	if im.CountSCSCFs() != 0 {
		t.Errorf("CountSCSCFs = %d, want 0 after Init", im.CountSCSCFs())
	}
}
