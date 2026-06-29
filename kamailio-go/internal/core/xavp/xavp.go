// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * xavp - eXtended AVP with nested structures - matching C xavp.c
 * Supports multi-level attribute-value pairs with sub-AVPs,
 * allowing hierarchical data structures.
 */

package xavp

import (
	"fmt"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/str"
)

type XAVPType int

const (
	XAVPTypeNull XAVPType = iota
	XAVPTypeStr
	XAVPTypeInt
	XAVPTypeXAVP
)

type XAVP struct {
	Name     str.Str
	Type     XAVPType
	Flags    uint16
	StrVal   str.Str
	IntVal   int64
	Children []*XAVP
	Next     *XAVP
	mu       sync.RWMutex
}

type XAVPList struct {
	head *XAVP
	tail *XAVP
	mu   sync.RWMutex
}

func NewXAVP(name str.Str) *XAVP {
	return &XAVP{
		Name:     name.Clone(),
		Type:     XAVPTypeNull,
		Children: make([]*XAVP, 0),
	}
}

func NewXAVPStr(name str.Str, val str.Str) *XAVP {
	x := NewXAVP(name)
	x.Type = XAVPTypeStr
	x.StrVal = val.Clone()
	return x
}

func NewXAVPInt(name str.Str, val int64) *XAVP {
	x := NewXAVP(name)
	x.Type = XAVPTypeInt
	x.IntVal = val
	return x
}

func NewXAVPXAVP(name str.Str) *XAVP {
	x := NewXAVP(name)
	x.Type = XAVPTypeXAVP
	return x
}

func (x *XAVP) SetStr(val str.Str) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.Type = XAVPTypeStr
	x.StrVal = val.Clone()
}

func (x *XAVP) SetInt(val int64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.Type = XAVPTypeInt
	x.IntVal = val
}

func (x *XAVP) GetStr() str.Str {
	x.mu.RLock()
	defer x.mu.RUnlock()
	if x.Type != XAVPTypeStr {
		return str.Str{}
	}
	return x.StrVal.Clone()
}

func (x *XAVP) GetInt() (int64, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	if x.Type != XAVPTypeInt {
		return 0, false
	}
	return x.IntVal, true
}

func (x *XAVP) AddChild(child *XAVP) {
	if child == nil {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()

	if x.Type != XAVPTypeXAVP {
		x.Type = XAVPTypeXAVP
	}

	if len(x.Children) > 0 {
		x.Children[len(x.Children)-1].Next = child
	}
	x.Children = append(x.Children, child)
}

func (x *XAVP) GetChild(name str.Str) *XAVP {
	x.mu.RLock()
	defer x.mu.RUnlock()

	nameStr := name.String()
	for _, child := range x.Children {
		if child.Name.String() == nameStr {
			return child
		}
	}
	return nil
}

func (x *XAVP) GetAllChildren(name str.Str) []*XAVP {
	x.mu.RLock()
	defer x.mu.RUnlock()

	nameStr := name.String()
	var result []*XAVP
	for _, child := range x.Children {
		if child.Name.String() == nameStr {
			result = append(result, child)
		}
	}
	return result
}

func (x *XAVP) RemoveChild(name str.Str) bool {
	x.mu.Lock()
	defer x.mu.Unlock()

	nameStr := name.String()
	found := false
	var newChildren []*XAVP
	for _, child := range x.Children {
		if child.Name.String() == nameStr {
			found = true
		} else {
			newChildren = append(newChildren, child)
		}
	}
	x.Children = newChildren

	if len(x.Children) > 0 {
		for i := 0; i < len(x.Children)-1; i++ {
			x.Children[i].Next = x.Children[i+1]
		}
		x.Children[len(x.Children)-1].Next = nil
	}

	return found
}

func (x *XAVP) ChildrenCount() int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return len(x.Children)
}

func (x *XAVP) Clone() *XAVP {
	x.mu.RLock()
	defer x.mu.RUnlock()

	nx := &XAVP{
		Name:     x.Name.Clone(),
		Type:     x.Type,
		Flags:    x.Flags,
		StrVal:   x.StrVal.Clone(),
		IntVal:   x.IntVal,
		Children: make([]*XAVP, 0, len(x.Children)),
	}

	for _, child := range x.Children {
		nc := child.Clone()
		if len(nx.Children) > 0 {
			nx.Children[len(nx.Children)-1].Next = nc
		}
		nx.Children = append(nx.Children, nc)
	}

	return nx
}

func (x *XAVP) String() string {
	x.mu.RLock()
	defer x.mu.RUnlock()

	switch x.Type {
	case XAVPTypeStr:
		return x.StrVal.String()
	case XAVPTypeInt:
		return fmt.Sprintf("%d", x.IntVal)
	case XAVPTypeXAVP:
		return fmt.Sprintf("[xavp with %d children]", len(x.Children))
	default:
		return "<null>"
	}
}

func NewXAVPList() *XAVPList {
	return &XAVPList{}
}

func (l *XAVPList) Add(x *XAVP) {
	if x == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.head == nil {
		l.head = x
		l.tail = x
	} else {
		l.tail.Next = x
		l.tail = x
	}
}

func (l *XAVPList) Get(name str.Str) *XAVP {
	l.mu.RLock()
	defer l.mu.RUnlock()

	nameStr := name.String()
	for cur := l.head; cur != nil; cur = cur.Next {
		if cur.Name.String() == nameStr {
			return cur
		}
	}
	return nil
}

func (l *XAVPList) GetAll(name str.Str) []*XAVP {
	l.mu.RLock()
	defer l.mu.RUnlock()

	nameStr := name.String()
	var result []*XAVP
	for cur := l.head; cur != nil; cur = cur.Next {
		if cur.Name.String() == nameStr {
			result = append(result, cur)
		}
	}
	return result
}

func (l *XAVPList) Remove(name str.Str) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	nameStr := name.String()
	found := false

	var prev *XAVP
	cur := l.head
	for cur != nil {
		if cur.Name.String() == nameStr {
			found = true
			if prev == nil {
				l.head = cur.Next
			} else {
				prev.Next = cur.Next
			}
			if cur == l.tail {
				l.tail = prev
			}
		} else {
			prev = cur
		}
		cur = cur.Next
	}

	return found
}

func (l *XAVPList) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	count := 0
	for cur := l.head; cur != nil; cur = cur.Next {
		count++
	}
	return count
}

func (l *XAVPList) Head() *XAVP {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.head
}

func (l *XAVPList) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.head = nil
	l.tail = nil
}

type XAVPStore struct {
	lists map[string]*XAVPList
	mu    sync.RWMutex
}

func NewXAVPStore() *XAVPStore {
	return &XAVPStore{
		lists: make(map[string]*XAVPList),
	}
}

func (s *XAVPStore) GetList(name str.Str) *XAVPList {
	s.mu.RLock()
	key := name.String()
	list, ok := s.lists[key]
	s.mu.RUnlock()

	if ok {
		return list
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list, ok = s.lists[key]
	if !ok {
		list = NewXAVPList()
		s.lists[key] = list
	}
	return list
}

func (s *XAVPStore) AddXAVP(name str.Str, x *XAVP) {
	list := s.GetList(name)
	list.Add(x)
}

func (s *XAVPStore) GetXAVP(name str.Str) *XAVP {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := name.String()
	list, ok := s.lists[key]
	if !ok {
		return nil
	}
	return list.Head()
}

func (s *XAVPStore) RemoveList(name str.Str) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := name.String()
	if _, ok := s.lists[key]; !ok {
		return false
	}
	delete(s.lists, key)
	return true
}

func XAVPCheckName(x *XAVP, name str.Str) bool {
	if x == nil {
		return false
	}
	return x.Name.String() == name.String()
}

func XAVPSetValue(x *XAVP, val interface{}) int {
	if x == nil {
		return -1
	}

	switch v := val.(type) {
	case str.Str:
		x.SetStr(v)
		return 0
	case string:
		x.SetStr(str.Mk(v))
		return 0
	case int64:
		x.SetInt(v)
		return 0
	case int:
		x.SetInt(int64(v))
		return 0
	default:
		return -1
	}
}

func XAVPGetValue(x *XAVP) interface{} {
	if x == nil {
		return nil
	}

	x.mu.RLock()
	defer x.mu.RUnlock()

	switch x.Type {
	case XAVPTypeStr:
		return x.StrVal.Clone()
	case XAVPTypeInt:
		return x.IntVal
	case XAVPTypeXAVP:
		return x.Children
	default:
		return nil
	}
}
