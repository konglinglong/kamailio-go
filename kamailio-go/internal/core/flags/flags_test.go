// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for flags package
 */

package flags

import (
	"testing"
)

func TestMsgFlags(t *testing.T) {
	mf := NewMsgFlags()

	if mf.IsFlagSet(FL_RED) {
		t.Error("flag should not be set initially")
	}

	ret := mf.SetFlag(FL_RED)
	if ret != 1 {
		t.Errorf("SetFlag returned %d, expected 1", ret)
	}
	if !mf.IsFlagSet(FL_RED) {
		t.Error("flag should be set after SetFlag")
	}

	ret = mf.ResetFlag(FL_RED)
	if ret != 1 {
		t.Errorf("ResetFlag returned %d, expected 1", ret)
	}
	if mf.IsFlagSet(FL_RED) {
		t.Error("flag should not be set after ResetFlag")
	}
}

func TestMsgFlagsOutOfRange(t *testing.T) {
	mf := NewMsgFlags()

	ret := mf.SetFlag(MaxFlag + 1)
	if ret != -1 {
		t.Errorf("SetFlag with out of range should return -1, got %d", ret)
	}

	if mf.IsFlagSet(MaxFlag + 1) {
		t.Error("out of range flag should not be set")
	}
}

func TestXFlags(t *testing.T) {
	mf := NewMsgFlags()

	if mf.IsXFlagSet(0) {
		t.Error("xflag should not be set initially")
	}

	ret := mf.SetXFlag(0)
	if ret != 1 {
		t.Errorf("SetXFlag returned %d, expected 1", ret)
	}
	if !mf.IsXFlagSet(0) {
		t.Error("xflag should be set after SetXFlag")
	}

	ret = mf.ResetXFlag(0)
	if ret != 1 {
		t.Errorf("ResetXFlag returned %d, expected 1", ret)
	}
	if mf.IsXFlagSet(0) {
		t.Error("xflag should not be set after ResetXFlag")
	}
}

func TestScriptFlags(t *testing.T) {
	SetScriptFlagsVal(0)

	if IsScriptFlagSet(FL_GREEN) {
		t.Error("script flag should not be set initially")
	}

	SetScriptFlag(FL_GREEN)
	if !IsScriptFlagSet(FL_GREEN) {
		t.Error("script flag should be set")
	}

	ResetScriptFlag(FL_GREEN)
	if IsScriptFlagSet(FL_GREEN) {
		t.Error("script flag should be reset")
	}

	SetScriptFlagsVal(0xFFFFFFFF)
	if GetScriptFlags() != 0xFFFFFFFF {
		t.Error("script flags val mismatch")
	}
}

func TestBranchFlags(t *testing.T) {
	bf := NewBranchFlags()

	if bf.IsBFlagSet(0) {
		t.Error("branch flag should not be set initially")
	}

	bf.SetBFlag(5)
	if !bf.IsBFlagSet(5) {
		t.Error("branch flag should be set")
	}

	bf.SetBFlagsVal(0xFF)
	if bf.GetBFlagsVal() != 0xFF {
		t.Error("branch flags val mismatch")
	}

	bf.ResetBFlag(0)
	if bf.IsBFlagSet(0) {
		t.Error("branch flag should be reset")
	}
}

func TestFlagInRange(t *testing.T) {
	if !FlagInRange(0) {
		t.Error("flag 0 should be in range")
	}
	if !FlagInRange(MaxFlag) {
		t.Error("MaxFlag should be in range")
	}
	if FlagInRange(MaxFlag + 1) {
		t.Error("MaxFlag+1 should not be in range")
	}
}

func TestNamedFlags(t *testing.T) {
	pos := RegisterFlag("testflag", -1)
	if pos < 0 {
		t.Fatalf("RegisterFlag failed: %d", pos)
	}

	found := GetFlagNo("testflag")
	if found != pos {
		t.Errorf("GetFlagNo = %d, expected %d", found, pos)
	}

	if !CheckFlag(pos) {
		t.Error("CheckFlag should return true for valid flag")
	}

	if GetFlagNo("nonexistent") != -1 {
		t.Error("GetFlagNo for nonexistent should return -1")
	}
}

func TestGetFlags(t *testing.T) {
	mf := NewMsgFlags()
	mf.SetFlag(FL_RED)
	mf.SetFlag(FL_GREEN)

	flags := mf.GetFlags()
	if flags&(1<<FL_RED) == 0 {
		t.Error("FL_RED should be set in GetFlags")
	}
	if flags&(1<<FL_GREEN) == 0 {
		t.Error("FL_GREEN should be set in GetFlags")
	}
}

func TestResetFlags(t *testing.T) {
	mf := NewMsgFlags()
	mf.SetFlag(FL_RED)
	mf.SetFlag(FL_GREEN)
	mf.SetFlag(FL_BLUE)

	mf.ResetFlags((1 << FL_RED) | (1 << FL_GREEN))

	if mf.IsFlagSet(FL_RED) {
		t.Error("FL_RED should be reset")
	}
	if mf.IsFlagSet(FL_GREEN) {
		t.Error("FL_GREEN should be reset")
	}
	if !mf.IsFlagSet(FL_BLUE) {
		t.Error("FL_BLUE should still be set")
	}
}
