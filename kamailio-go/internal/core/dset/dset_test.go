// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for dset package
 */

package dset

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/flags"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

func TestNewDestinationSet(t *testing.T) {
	ds := NewDestinationSet()
	if ds.GetNrBranches() != 0 {
		t.Errorf("expected 0 branches, got %d", ds.GetNrBranches())
	}
}

func TestAppendBranch(t *testing.T) {
	ds := NewDestinationSet()
	uri := str.Mk("sip:alice@example.com")
	dstURI := str.Mk("sip:192.168.1.1:5060")

	ret := ds.AppendBranch(uri, dstURI, str.Str{}, 1000, 0, nil,
		str.Str{}, 0, str.Str{}, str.Str{})
	if ret < 0 {
		t.Fatalf("AppendBranch failed: %d", ret)
	}

	if ds.GetNrBranches() != 1 {
		t.Errorf("expected 1 branch, got %d", ds.GetNrBranches())
	}
}

func TestGetSIPBranch(t *testing.T) {
	ds := NewDestinationSet()
	uri := str.Mk("sip:alice@example.com")

	ds.AppendBranch(uri, str.Str{}, str.Str{}, 1000, 0, nil,
		str.Str{}, 0, str.Str{}, str.Str{})

	b := ds.GetSIPBranch(0)
	if b == nil {
		t.Fatal("GetSIPBranch(0) returned nil")
	}
	if !b.URI.Equal(uri) {
		t.Errorf("URI mismatch: %s vs %s", b.URI.String(), uri.String())
	}
}

func TestDropSIPBranch(t *testing.T) {
	ds := NewDestinationSet()

	for i := 0; i < 3; i++ {
		uri := str.Mk("sip:user" + string(rune('0'+i)) + "@example.com")
		ds.AppendBranch(uri, str.Str{}, str.Str{}, 1000, 0, nil,
			str.Str{}, 0, str.Str{}, str.Str{})
	}

	if ds.GetNrBranches() != 3 {
		t.Fatalf("expected 3 branches, got %d", ds.GetNrBranches())
	}

	ret := ds.DropSIPBranch(1)
	if ret != 0 {
		t.Errorf("DropSIPBranch failed: %d", ret)
	}

	if ds.GetNrBranches() != 2 {
		t.Errorf("expected 2 branches after drop, got %d", ds.GetNrBranches())
	}
}

func TestBranchIterator(t *testing.T) {
	ds := NewDestinationSet()

	for i := 0; i < 3; i++ {
		uri := str.Mk("sip:user" + string(rune('0'+i)) + "@example.com")
		ds.AppendBranch(uri, str.Str{}, str.Str{}, 1000, 0, nil,
			str.Str{}, 0, str.Str{}, str.Str{})
	}

	ds.InitBranchIterator()

	count := 0
	for {
		b := ds.NextBranch()
		if b == nil {
			break
		}
		count++
	}

	if count != 3 {
		t.Errorf("expected 3 branches via iterator, got %d", count)
	}
}

func TestClearBranches(t *testing.T) {
	ds := NewDestinationSet()

	for i := 0; i < 5; i++ {
		uri := str.Mk("sip:user@example.com")
		ds.AppendBranch(uri, str.Str{}, str.Str{}, 1000, 0, nil,
			str.Str{}, 0, str.Str{}, str.Str{})
	}

	if ds.GetNrBranches() != 5 {
		t.Fatalf("expected 5 branches, got %d", ds.GetNrBranches())
	}

	ds.ClearBranches()

	if ds.GetNrBranches() != 0 {
		t.Errorf("expected 0 branches after clear, got %d", ds.GetNrBranches())
	}
}

func TestPrintDSet(t *testing.T) {
	ds := NewDestinationSet()

	uri1 := str.Mk("sip:alice@example.com")
	uri2 := str.Mk("sip:bob@example.com")

	ds.AppendBranch(uri1, str.Str{}, str.Str{}, 1000, 0, nil,
		str.Str{}, 0, str.Str{}, str.Str{})
	ds.AppendBranch(uri2, str.Str{}, str.Str{}, 1000, 0, nil,
		str.Str{}, 0, str.Str{}, str.Str{})

	result := ds.PrintDSet(0)
	if result.IsEmpty() {
		t.Error("PrintDSet returned empty")
	}
	resultStr := result.String()
	if len(resultStr) == 0 {
		t.Error("PrintDSet result is empty string")
	}
}

func TestRURIQ(t *testing.T) {
	ds := NewDestinationSet()

	ds.SetRURIQ(500)
	if ds.GetRURIQ() != 500 {
		t.Errorf("expected ruri q=500, got %d", ds.GetRURIQ())
	}
}

func TestRURIMark(t *testing.T) {
	ds := NewDestinationSet()

	if ds.RURIGetForkingState() != 1 {
		t.Error("ruri should be new initially")
	}

	ds.RURIMarkConsumed()
	if ds.RURIGetForkingState() != 0 {
		t.Error("ruri should be consumed after mark")
	}

	ds.RURIMarkNew()
	if ds.RURIGetForkingState() != 1 {
		t.Error("ruri should be new after re-mark")
	}
}

func TestBranchFlags(t *testing.T) {
	ds := NewDestinationSet()
	uri := str.Mk("sip:alice@example.com")

	ds.AppendBranch(uri, str.Str{}, str.Str{}, 1000, 0, nil,
		str.Str{}, 0, str.Str{}, str.Str{})

	ret := ds.SetBFlag(1, flags.FL_RED)
	if ret != 1 {
		t.Errorf("SetBFlag failed: %d", ret)
	}

	if !ds.IsBFlagSet(1, flags.FL_RED) {
		t.Error("branch flag should be set")
	}

	ret = ds.ResetBFlag(1, flags.FL_RED)
	if ret != 1 {
		t.Errorf("ResetBFlag failed: %d", ret)
	}

	if ds.IsBFlagSet(1, flags.FL_RED) {
		t.Error("branch flag should be reset")
	}
}

func TestSetAllSIPBranches(t *testing.T) {
	ds := NewDestinationSet()

	branches := []*Branch{
		{URI: str.Mk("sip:alice@example.com"), Q: 1000},
		{URI: str.Mk("sip:bob@example.com"), Q: 900},
	}

	ret := ds.SetAllSIPBranches(branches)
	if ret != 0 {
		t.Errorf("SetAllSIPBranches failed: %d", ret)
	}

	if ds.GetNrBranches() != 2 {
		t.Errorf("expected 2 branches, got %d", ds.GetNrBranches())
	}
}

func TestGetBranchData(t *testing.T) {
	ds := NewDestinationSet()
	uri := str.Mk("sip:alice@example.com")

	ds.AppendBranch(uri, str.Str{}, str.Str{}, 1000, 0, nil,
		str.Str{}, 0, str.Str{}, str.Str{})

	bd := ds.GetBranchData(0)
	if bd == nil {
		t.Fatal("GetBranchData returned nil")
	}
	if !bd.URI.Equal(uri) {
		t.Error("URI mismatch in branch data")
	}
}

func TestGlobalDS(t *testing.T) {
	ds := GlobalDS()
	if ds == nil {
		t.Fatal("GlobalDS returned nil")
	}

	InitDstSet()
	ds2 := GlobalDS()
	if ds2.GetNrBranches() != 0 {
		t.Error("global ds should be empty after init")
	}
}
