// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the I-CSCF S-CSCF capability table and candidate-list logic.
 */

package icscf

import (
	"math"
	"testing"
	"time"
)

func TestMatchScoreMandatoryReject(t *testing.T) {
	c := SCSCFCapability{
		ID: 1, Name: "sip:scscf1.home1.net",
		MandatoryCaps: []int{1, 2},
		OptionalCaps: []int{3, 4, 5},
	}
	// Missing capability 2 -> reject.
	if got := c.MatchScore([]int{1, 3, 4}); got != -1 {
		t.Errorf("MatchScore(missing mandatory) = %d, want -1", got)
	}
	// All mandatory present, 0 optional matched -> score 0.
	if got := c.MatchScore([]int{1, 2, 99}); got != 0 {
		t.Errorf("MatchScore(no optional) = %d, want 0", got)
	}
	// All mandatory + 2 optional matched -> score 2.
	if got := c.MatchScore([]int{1, 2, 3, 4}); got != 2 {
		t.Errorf("MatchScore(2 optional) = %d, want 2", got)
	}
}

func TestMatchScoreAllMatched(t *testing.T) {
	c := SCSCFCapability{
		ID: 1, Name: "sip:scscf1.home1.net",
		MandatoryCaps: []int{1},
		OptionalCaps: []int{2, 3},
	}
	if got := c.MatchScore([]int{1, 2, 3}); got != 2 {
		t.Errorf("MatchScore(all) = %d, want 2", got)
	}
}

func TestMatchScoreNilSafe(t *testing.T) {
	var c *SCSCFCapability
	if got := c.MatchScore(nil); got != -1 {
		t.Errorf("nil MatchScore = %d, want -1", got)
	}
}

func TestSCSCFCapabilityClone(t *testing.T) {
	c := SCSCFCapability{
		ID: 1, Name: "x",
		MandatoryCaps: []int{1, 2},
		OptionalCaps:  []int{3},
	}
	clone := c.clone()
	clone.MandatoryCaps[0] = 999
	if c.MandatoryCaps[0] == 999 {
		t.Errorf("clone() returned a live reference to internal state")
	}
}

func TestSCSCFCapabilityString(t *testing.T) {
	c := SCSCFCapability{ID: 1, Name: "x", MandatoryCaps: []int{1}}
	if c.String() == "" {
		t.Errorf("String() returned empty")
	}
}

// ---------------------------------------------------------------------------
// SCSCFTable — catalogue operations
// ---------------------------------------------------------------------------

func TestLoadSCSCFsReplaces(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.AddSCSCF(SCSCFCapability{ID: 1, Name: "old"})
	tbl.LoadSCSCFs([]SCSCFCapability{
		{ID: 2, Name: "new1"},
		{ID: 3, Name: "new2"},
	})
	if got := tbl.SCSCFCount(); got != 2 {
		t.Errorf("SCSCFCount after Load = %d, want 2", got)
	}
	if tbl.FindSCSCF("old") != nil {
		t.Errorf("old entry should have been replaced")
	}
}

func TestFindSCSCF(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.AddSCSCF(SCSCFCapability{ID: 1, Name: "sip:scscf1.home.net"})
	if tbl.FindSCSCF("sip:scscf1.home.net") == nil {
		t.Errorf("FindSCSCF returned nil for known entry")
	}
	if tbl.FindSCSCF("nope") != nil {
		t.Errorf("FindSCSCF returned non-nil for unknown entry")
	}
}

func TestSCSCFsSnapshotIsolated(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.AddSCSCF(SCSCFCapability{ID: 1, Name: "x", MandatoryCaps: []int{1}})
	got := tbl.SCSCFs()
	got[0].MandatoryCaps[0] = 999
	again := tbl.SCSCFs()
	if again[0].MandatoryCaps[0] == 999 {
		t.Errorf("SCSCFs() returned a live reference")
	}
}

// ---------------------------------------------------------------------------
// BuildCandidateList — ranking logic
// ---------------------------------------------------------------------------

func TestBuildCandidateListWithExplicitServerName(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.LoadSCSCFs([]SCSCFCapability{
		{ID: 1, Name: "sip:scscf1.home.net", MandatoryCaps: []int{1}},
	})
	list := tbl.BuildCandidateList("callid-1", []int{1}, "sip:scscf_assigned.home.net", false)
	if len(list.Candidates) != 1 {
		t.Fatalf("explicit server name should produce 1 candidate, got %d", len(list.Candidates))
	}
	if list.Candidates[0].Name != "sip:scscf_assigned.home.net" {
		t.Errorf("Name = %q", list.Candidates[0].Name)
	}
	if list.Candidates[0].Score != math.MaxInt32 {
		t.Errorf("Score = %d, want MaxInt32", list.Candidates[0].Score)
	}
}

func TestBuildCandidateListScoreOrdering(t *testing.T) {
	tbl := NewSCSCFTable()
	// Two S-CSCFs that both satisfy the mandatory capability.
	tbl.LoadSCSCFs([]SCSCFCapability{
		{ID: 1, Name: "sip:scscf1.home.net", MandatoryCaps: []int{1}, OptionalCaps: []int{2}},
		{ID: 2, Name: "sip:scscf2.home.net", MandatoryCaps: []int{1}, OptionalCaps: []int{3, 4}},
	})
	// Require mandatory=1 plus optional=3 -> scscf2 should score 1, scscf1 should score 0.
	list := tbl.BuildCandidateList("callid-2", []int{1, 3}, "", false)
	if len(list.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(list.Candidates))
	}
	if list.Candidates[0].Name != "sip:scscf2.home.net" {
		t.Errorf("highest-scored candidate = %q, want scscf2", list.Candidates[0].Name)
	}
	if list.Candidates[1].Score != 0 {
		t.Errorf("second candidate Score = %d, want 0", list.Candidates[1].Score)
	}
}

func TestBuildCandidateListRejectsMandatoryMiss(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.LoadSCSCFs([]SCSCFCapability{
		{ID: 1, Name: "sip:scscf1.home.net", MandatoryCaps: []int{1, 2}},
		{ID: 2, Name: "sip:scscf2.home.net", MandatoryCaps: []int{3}},
	})
	// Required = {3} -> scscf1 misses mandatory {1,2} -> reject; scscf2 satisfies.
	list := tbl.BuildCandidateList("callid-3", []int{3}, "", false)
	if len(list.Candidates) != 1 {
		t.Fatalf("expected 1 candidate after mandatory filter, got %d", len(list.Candidates))
	}
	if list.Candidates[0].Name != "sip:scscf2.home.net" {
		t.Errorf("surviving candidate = %q", list.Candidates[0].Name)
	}
}

func TestBuildCandidateListPreferredBonus(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.LoadSCSCFs([]SCSCFCapability{
		{ID: 1, Name: "sip:scscf1.home.net", MandatoryCaps: []int{1}, OptionalCaps: []int{2, 3}},
		{ID: 2, Name: "sip:scscf2.home.net", MandatoryCaps: []int{1}, OptionalCaps: []int{4}},
	})
	// Without preferred: scscf1 wins (score 2 vs 0).
	list := tbl.BuildCandidateList("c", []int{1, 2, 3, 4}, "", false)
	if list.Candidates[0].Name != "sip:scscf1.home.net" {
		t.Errorf("without preferred: top = %s", list.Candidates[0].Name)
	}

	// With preferred=[scscf2]: scscf2 should be ranked first.
	tbl.SetPreferredSCSCFs([]string{"sip:scscf2.home.net"}, true)
	list = tbl.BuildCandidateList("c2", []int{1, 2, 3, 4}, "", false)
	if list.Candidates[0].Name != "sip:scscf2.home.net" {
		t.Errorf("with preferred: top = %s, want scscf2", list.Candidates[0].Name)
	}
	if list.Candidates[0].Score != math.MaxInt32 {
		t.Errorf("preferred score = %d, want MaxInt32", list.Candidates[0].Score)
	}
}

func TestBuildCandidateListOrigSuffix(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.LoadSCSCFs([]SCSCFCapability{
		{ID: 1, Name: "sip:scscf1.home.net", MandatoryCaps: []int{1}},
	})
	list := tbl.BuildCandidateList("callid-orig", []int{1}, "", true)
	if !list.Orig {
		t.Errorf("Orig = false, want true")
	}
	if list.Candidates[0].Name != "sip:scscf1.home.net;orig" {
		t.Errorf("Name = %q, want ;orig suffix", list.Candidates[0].Name)
	}
}

// ---------------------------------------------------------------------------
// Select / DropList
// ---------------------------------------------------------------------------

func TestSelectPopsHighestScore(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.LoadSCSCFs([]SCSCFCapability{
		{ID: 1, Name: "sip:scscf1.home.net", MandatoryCaps: []int{1}, OptionalCaps: []int{2}},
		{ID: 2, Name: "sip:scscf2.home.net", MandatoryCaps: []int{1}, OptionalCaps: []int{3}},
	})
	tbl.BuildCandidateList("callid", []int{1, 2, 3}, "", false)

	c, err := tbl.Select("callid")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	// Both score 1 — stable sort keeps catalogue order (scscf1 first).
	if c.Name != "sip:scscf1.home.net" {
		t.Errorf("first Select = %q", c.Name)
	}
	c2, err := tbl.Select("callid")
	if err != nil {
		t.Fatalf("second Select: %v", err)
	}
	if c2.Name != "sip:scscf2.home.net" {
		t.Errorf("second Select = %q", c2.Name)
	}
	// Third Select should fail (list exhausted).
	_, err = tbl.Select("callid")
	if err != ErrCandidateListEmpty {
		t.Errorf("exhausted Select err = %v, want ErrCandidateListEmpty", err)
	}
}

func TestSelectNoList(t *testing.T) {
	tbl := NewSCSCFTable()
	_, err := tbl.Select("nonexistent")
	if err != ErrNoCandidateList {
		t.Errorf("Select(no list) err = %v, want ErrNoCandidateList", err)
	}
}

func TestDropList(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.BuildCandidateList("callid", []int{}, "server", false)
	tbl.DropList("callid")
	if tbl.CandidateList("callid") != nil {
		t.Errorf("DropList did not remove the list")
	}
}

func TestCandidateListSnapshot(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.LoadSCSCFs([]SCSCFCapability{
		{ID: 1, Name: "x", MandatoryCaps: []int{1}},
	})
	tbl.BuildCandidateList("callid", []int{1}, "", false)
	snap := tbl.CandidateList("callid")
	if snap == nil {
		t.Fatalf("CandidateList returned nil")
	}
	// Mutating the snapshot must not affect the table.
	snap.Candidates[0] = SCSCFCandidate{Name: "tampered"}
	again := tbl.CandidateList("callid")
	if again.Candidates[0].Name == "tampered" {
		t.Errorf("CandidateList returned a live reference")
	}
}

// ---------------------------------------------------------------------------
// Expiry sweep
// ---------------------------------------------------------------------------

func TestSweepExpiredRemovesOldLists(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.SetEntryExpiry(time.Second)
	tbl.BuildCandidateList("c1", []int{}, "s1", false)
	tbl.BuildCandidateList("c2", []int{}, "s2", false)
	// Backdate c1 to be older than the expiry window.
	tbl.mu.Lock()
	if l, ok := tbl.lists["c1"]; ok {
		l.CreatedAt = time.Now().Add(-2 * time.Second)
	}
	tbl.mu.Unlock()

	removed := tbl.SweepExpired(time.Now())
	if removed != 1 {
		t.Errorf("SweepExpired removed %d lists, want 1", removed)
	}
	if tbl.CandidateList("c1") != nil {
		t.Errorf("c1 not removed")
	}
	if tbl.CandidateList("c2") == nil {
		t.Errorf("c2 should still be present")
	}
}

func TestSweepExpiredKeepsFreshLists(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.SetEntryExpiry(time.Hour)
	tbl.BuildCandidateList("fresh", []int{}, "s", false)
	if removed := tbl.SweepExpired(time.Now()); removed != 0 {
		t.Errorf("SweepExpired removed %d fresh lists", removed)
	}
}

// ---------------------------------------------------------------------------
// Preferred list operations
// ---------------------------------------------------------------------------

func TestSetPreferredSCSCFs(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.SetPreferredSCSCFs([]string{"a", "b"}, true)
	if !tbl.UsePreferred() {
		t.Errorf("UsePreferred = false, want true")
	}
	got := tbl.PreferredSCSCFs()
	if len(got) != 2 || got[0] != "a" {
		t.Errorf("PreferredSCSCFs = %v", got)
	}
}

func TestSetEntryExpiry(t *testing.T) {
	tbl := NewSCSCFTable()
	tbl.SetEntryExpiry(5 * time.Minute)
	if got := tbl.EntryExpiry(); got != 5*time.Minute {
		t.Errorf("EntryExpiry = %v, want 5m", got)
	}
}

func TestSetEntryExpiryIgnoresZero(t *testing.T) {
	tbl := NewSCSCFTable()
	original := tbl.EntryExpiry()
	tbl.SetEntryExpiry(0)
	if got := tbl.EntryExpiry(); got != original {
		t.Errorf("SetEntryExpiry(0) changed expiry from %v to %v", original, got)
	}
}

func TestListCount(t *testing.T) {
	tbl := NewSCSCFTable()
	if tbl.ListCount() != 0 {
		t.Errorf("initial ListCount = %d, want 0", tbl.ListCount())
	}
	tbl.BuildCandidateList("c1", []int{}, "s", false)
	tbl.BuildCandidateList("c2", []int{}, "s", false)
	if tbl.ListCount() != 2 {
		t.Errorf("ListCount = %d, want 2", tbl.ListCount())
	}
	tbl.DropList("c1")
	if tbl.ListCount() != 1 {
		t.Errorf("ListCount after Drop = %d, want 1", tbl.ListCount())
	}
}
