// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Flag handling - matching C flags.c
 * Includes message flags, script flags, branch flags, and extended flags.
 */

package flags

import (
	"sync"
)

type Flag_t uint32

const (
	FL_WHITE Flag_t = 1 + iota
	FL_YELLOW
	FL_GREEN
	FL_RED
	FL_BLUE
	FL_MAGENTA
	FL_BROWN
	FL_BLACK
	FL_ACC
	FL_MAX
)

const MaxFlag = Flag_t(31)

const KSRXFlagsSize = 2
const KSRMaxXFlag = Flag_t(KSRXFlagsSize*2*32 - 1)

type MsgFlags struct {
	mu     sync.RWMutex
	flags  Flag_t
	xflags [KSRXFlagsSize]Flag_t
}

func NewMsgFlags() *MsgFlags {
	return &MsgFlags{}
}

func (mf *MsgFlags) SetFlag(flag Flag_t) int {
	if flag > MaxFlag {
		return -1
	}
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.flags |= 1 << flag
	return 1
}

func (mf *MsgFlags) ResetFlag(flag Flag_t) int {
	if flag > MaxFlag {
		return -1
	}
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.flags &^= 1 << flag
	return 1
}

func (mf *MsgFlags) ResetFlags(flags Flag_t) int {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.flags &^= flags
	return 1
}

func (mf *MsgFlags) IsFlagSet(flag Flag_t) bool {
	if flag > MaxFlag {
		return false
	}
	mf.mu.RLock()
	defer mf.mu.RUnlock()
	return mf.flags&(1<<flag) != 0
}

func (mf *MsgFlags) GetFlags() Flag_t {
	mf.mu.RLock()
	defer mf.mu.RUnlock()
	return mf.flags
}

func (mf *MsgFlags) SetXFlag(flag Flag_t) int {
	if flag > KSRMaxXFlag {
		return -1
	}
	idx := flag / 32
	bit := flag % 32
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.xflags[idx] |= 1 << bit
	return 1
}

func (mf *MsgFlags) ResetXFlag(flag Flag_t) int {
	if flag > KSRMaxXFlag {
		return -1
	}
	idx := flag / 32
	bit := flag % 32
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.xflags[idx] &^= 1 << bit
	return 1
}

func (mf *MsgFlags) IsXFlagSet(flag Flag_t) bool {
	if flag > KSRMaxXFlag {
		return false
	}
	idx := flag / 32
	bit := flag % 32
	mf.mu.RLock()
	defer mf.mu.RUnlock()
	return mf.xflags[idx]&(1<<bit) != 0
}

type ScriptFlags struct {
	mu    sync.RWMutex
	flags Flag_t
}

var globalScriptFlags = &ScriptFlags{}

func SetScriptFlag(flag Flag_t) int {
	if flag > MaxFlag {
		return -1
	}
	globalScriptFlags.mu.Lock()
	defer globalScriptFlags.mu.Unlock()
	globalScriptFlags.flags |= 1 << flag
	return 1
}

func ResetScriptFlag(flag Flag_t) int {
	if flag > MaxFlag {
		return -1
	}
	globalScriptFlags.mu.Lock()
	defer globalScriptFlags.mu.Unlock()
	globalScriptFlags.flags &^= 1 << flag
	return 1
}

func IsScriptFlagSet(flag Flag_t) bool {
	if flag > MaxFlag {
		return false
	}
	globalScriptFlags.mu.RLock()
	defer globalScriptFlags.mu.RUnlock()
	return globalScriptFlags.flags&(1<<flag) != 0
}

func SetScriptFlagsVal(val Flag_t) int {
	globalScriptFlags.mu.Lock()
	defer globalScriptFlags.mu.Unlock()
	globalScriptFlags.flags = val
	return 1
}

func GetScriptFlags() Flag_t {
	globalScriptFlags.mu.RLock()
	defer globalScriptFlags.mu.RUnlock()
	return globalScriptFlags.flags
}

type BranchFlags struct {
	mu    sync.RWMutex
	flags Flag_t
}

func NewBranchFlags() *BranchFlags {
	return &BranchFlags{}
}

func (bf *BranchFlags) SetBFlag(flag Flag_t) int {
	if flag > MaxFlag {
		return -1
	}
	bf.mu.Lock()
	defer bf.mu.Unlock()
	bf.flags |= 1 << flag
	return 1
}

func (bf *BranchFlags) ResetBFlag(flag Flag_t) int {
	if flag > MaxFlag {
		return -1
	}
	bf.mu.Lock()
	defer bf.mu.Unlock()
	bf.flags &^= 1 << flag
	return 1
}

func (bf *BranchFlags) IsBFlagSet(flag Flag_t) bool {
	if flag > MaxFlag {
		return false
	}
	bf.mu.RLock()
	defer bf.mu.RUnlock()
	return bf.flags&(1<<flag) != 0
}

func (bf *BranchFlags) GetBFlagsVal() Flag_t {
	bf.mu.RLock()
	defer bf.mu.RUnlock()
	return bf.flags
}

func (bf *BranchFlags) SetBFlagsVal(val Flag_t) int {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	bf.flags = val
	return 1
}

func FlagInRange(flag Flag_t) bool {
	return flag <= MaxFlag
}

type NamedFlag struct {
	Name string
	Pos  int
}

var namedFlags struct {
	mu    sync.RWMutex
	flags map[string]int
}

func init() {
	namedFlags.flags = make(map[string]int)
}

func RegisterFlag(name string, pos int) int {
	namedFlags.mu.Lock()
	defer namedFlags.mu.Unlock()
	if pos < 0 {
		for i := 0; i <= int(MaxFlag); i++ {
			found := false
			for _, p := range namedFlags.flags {
				if p == i {
					found = true
					break
				}
			}
			if !found {
				namedFlags.flags[name] = i
				return i
			}
		}
		return -1
	}
	if Flag_t(pos) > MaxFlag {
		return -1
	}
	namedFlags.flags[name] = pos
	return pos
}

func GetFlagNo(name string) int {
	namedFlags.mu.RLock()
	defer namedFlags.mu.RUnlock()
	pos, ok := namedFlags.flags[name]
	if !ok {
		return -1
	}
	return pos
}

func CheckFlag(pos int) bool {
	return Flag_t(pos) <= MaxFlag
}
