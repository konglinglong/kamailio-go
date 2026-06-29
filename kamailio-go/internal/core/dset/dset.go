// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Destination set handling - matching C dset.c
 * Manages SIP forking branches with URI, destination URI, path,
 * q-value, flags, and other branch attributes.
 */

package dset

import (
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/flags"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

const MaxBranches = 12
const MaxURISize = 1024
const MaxPathSize = 4096
const MaxInstanceSize = 256
const MaxRUIDSize = 64
const MaxUASize = 256

const DSFlags = 1
const DSPath = 2

type Branch struct {
	URI             str.Str
	DstURI          str.Str
	Path            str.Str
	Q               int
	ForceSocket     interface{}
	Instance        str.Str
	RegID           uint
	RUID            str.Str
	LocationUA      str.Str
	OTCPID          int
	Flags           flags.Flag_t
}

type BranchData struct {
	URI         str.Str
	DstURI      str.Str
	Q           int
	Path        str.Str
	Flags       uint
	ForceSocket interface{}
	RUID        str.Str
	Instance    str.Str
	LocationUA  str.Str
	OTCPID      int
}

type DestinationSet struct {
	mu        sync.RWMutex
	branches  []*Branch
	ruriIsNew int
	ruriQ     int
	iterator  int
}

func NewDestinationSet() *DestinationSet {
	return &DestinationSet{
		branches:  make([]*Branch, 0, MaxBranches),
		ruriIsNew: 1,
		ruriQ:     1000,
	}
}

func (ds *DestinationSet) GetNrBranches() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return len(ds.branches)
}

func (ds *DestinationSet) GetSIPBranch(idx int) *Branch {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	if idx < 0 || idx >= len(ds.branches) {
		return nil
	}
	return ds.branches[idx]
}

func (ds *DestinationSet) GetAllSIPBranches() []*Branch {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	result := make([]*Branch, len(ds.branches))
	copy(result, ds.branches)
	return result
}

func (ds *DestinationSet) SetAllSIPBranches(branches []*Branch) int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if len(branches) > MaxBranches {
		return -1
	}
	ds.branches = make([]*Branch, len(branches))
	copy(ds.branches, branches)
	return 0
}

func (ds *DestinationSet) DropSIPBranch(idx int) int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if idx < 0 || idx >= len(ds.branches) {
		return -1
	}
	ds.branches = append(ds.branches[:idx], ds.branches[idx+1:]...)
	return 0
}

func (ds *DestinationSet) PushBranch(uri str.Str, dstURI str.Str, path str.Str,
	q int, flags flags.Flag_t, forceSocket interface{},
	instance str.Str, regID uint, ruid str.Str, locationUA str.Str) *Branch {

	ds.mu.Lock()
	defer ds.mu.Unlock()

	if len(ds.branches) >= MaxBranches {
		return nil
	}

	b := &Branch{
		URI:         uri.Clone(),
		DstURI:      dstURI.Clone(),
		Path:        path.Clone(),
		Q:           q,
		ForceSocket: forceSocket,
		Instance:    instance.Clone(),
		RegID:       regID,
		RUID:        ruid.Clone(),
		LocationUA:  locationUA.Clone(),
		Flags:       flags,
	}

	ds.branches = append(ds.branches, b)
	return b
}

func (ds *DestinationSet) AppendBranch(uri str.Str, dstURI str.Str, path str.Str,
	q int, flags flags.Flag_t, forceSocket interface{},
	instance str.Str, regID uint, ruid str.Str, locationUA str.Str) int {

	b := ds.PushBranch(uri, dstURI, path, q, flags, forceSocket,
		instance, regID, ruid, locationUA)
	if b == nil {
		return -1
	}
	return len(ds.branches)
}

func (ds *DestinationSet) InitBranchIterator() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.iterator = 0
}

func (ds *DestinationSet) GetBranchIterator() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.iterator
}

func (ds *DestinationSet) SetBranchIterator(n int) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if n >= 0 && n <= len(ds.branches) {
		ds.iterator = n
	}
}

func (ds *DestinationSet) NextBranch() *Branch {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ds.iterator >= len(ds.branches) {
		return nil
	}
	b := ds.branches[ds.iterator]
	ds.iterator++
	return b
}

func (ds *DestinationSet) GetBranch(i int) *Branch {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	if i < 0 || i >= len(ds.branches) {
		return nil
	}
	return ds.branches[i]
}

func (ds *DestinationSet) GetBranchData(i int) *BranchData {
	b := ds.GetBranch(i)
	if b == nil {
		return nil
	}
	return &BranchData{
		URI:         b.URI,
		DstURI:      b.DstURI,
		Q:           b.Q,
		Path:        b.Path,
		Flags:       uint(b.Flags),
		ForceSocket: b.ForceSocket,
		RUID:        b.RUID,
		Instance:    b.Instance,
		LocationUA:  b.LocationUA,
		OTCPID:      b.OTCPID,
	}
}

func (ds *DestinationSet) NextBranchData() *BranchData {
	b := ds.NextBranch()
	if b == nil {
		return nil
	}
	return &BranchData{
		URI:         b.URI,
		DstURI:      b.DstURI,
		Q:           b.Q,
		Path:        b.Path,
		Flags:       uint(b.Flags),
		ForceSocket: b.ForceSocket,
		RUID:        b.RUID,
		Instance:    b.Instance,
		LocationUA:  b.LocationUA,
		OTCPID:      b.OTCPID,
	}
}

func (ds *DestinationSet) ClearBranches() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.branches = ds.branches[:0]
	ds.iterator = 0
}

func (ds *DestinationSet) PrintDSet(options int) str.Str {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if len(ds.branches) == 0 {
		return str.Str{}
	}

	var result []byte
	for i, b := range ds.branches {
		if i > 0 {
			result = append(result, ',', ' ')
		}
		if options&DSFlags != 0 && b.Flags != 0 {
		}
		result = append(result, '<')
		if !b.URI.IsEmpty() {
			result = append(result, b.URI.Bytes()...)
		}
		result = append(result, '>')
		if !b.Path.IsEmpty() && options&DSPath != 0 {
		}
	}
	return str.MkBytes(result)
}

func (ds *DestinationSet) SetRURIQ(q int) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.ruriQ = q
}

func (ds *DestinationSet) GetRURIQ() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.ruriQ
}

func (ds *DestinationSet) RURIMarkNew() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.ruriIsNew = 1
}

func (ds *DestinationSet) RURIMarkConsumed() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.ruriIsNew = 0
}

func (ds *DestinationSet) RURIGetForkingState() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.ruriIsNew
}

func (ds *DestinationSet) SetBFlag(branch int, flag flags.Flag_t) int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if branch == 0 {
		return 1
	}
	idx := branch - 1
	if idx < 0 || idx >= len(ds.branches) {
		return -1
	}
	ds.branches[idx].Flags |= 1 << flag
	return 1
}

func (ds *DestinationSet) ResetBFlag(branch int, flag flags.Flag_t) int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if branch == 0 {
		return 1
	}
	idx := branch - 1
	if idx < 0 || idx >= len(ds.branches) {
		return -1
	}
	ds.branches[idx].Flags &^= 1 << flag
	return 1
}

func (ds *DestinationSet) IsBFlagSet(branch int, flag flags.Flag_t) bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	if branch == 0 {
		return false
	}
	idx := branch - 1
	if idx < 0 || idx >= len(ds.branches) {
		return false
	}
	return ds.branches[idx].Flags&(1<<flag) != 0
}

func (ds *DestinationSet) GetBFlagsVal(branch int) (flags.Flag_t, int) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	if branch == 0 {
		return 0, 1
	}
	idx := branch - 1
	if idx < 0 || idx >= len(ds.branches) {
		return 0, -1
	}
	return ds.branches[idx].Flags, 1
}

func (ds *DestinationSet) SetBFlagsVal(branch int, val flags.Flag_t) int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if branch == 0 {
		return 1
	}
	idx := branch - 1
	if idx < 0 || idx >= len(ds.branches) {
		return -1
	}
	ds.branches[idx].Flags = val
	return 1
}

var globalDS = NewDestinationSet()

func GlobalDS() *DestinationSet {
	return globalDS
}

func InitDstSet() int {
	globalDS = NewDestinationSet()
	return 0
}

func GetNrBranches() int {
	return globalDS.GetNrBranches()
}

func ClearBranches() {
	globalDS.ClearBranches()
}

func AppendBranch(uri str.Str, dstURI str.Str, path str.Str,
	q int, flags flags.Flag_t, forceSocket interface{},
	instance str.Str, regID uint, ruid str.Str, locationUA str.Str) int {
	return globalDS.AppendBranch(uri, dstURI, path, q, flags, forceSocket,
		instance, regID, ruid, locationUA)
}
