// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go — XAVP (Extended AVP) store.
 *
 * Thread-safe storage for extended attribute-value pairs, indexed by
 * root name and key.  Each root may hold multiple instances (FIFO list);
 * index 0 is the newest.
 */

package script

import "sync"

// XAVPValue holds either a string or integer XAVP value.
type XAVPValue struct {
	Str   string
	Int   int
	IsInt bool
}

// XAVPStore is a thread-safe store for XAVPs.
type XAVPStore struct {
	mu    sync.RWMutex
	roots map[string][]map[string]XAVPValue
}

// NewXAVPStore creates an initialised XAVPStore.
func NewXAVPStore() *XAVPStore {
	return &XAVPStore{roots: make(map[string][]map[string]XAVPValue)}
}

// SetStr stores a string value for root[key] in the newest instance.
func (s *XAVPStore) SetStr(root, key, val string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.roots[root]) == 0 {
		s.roots[root] = []map[string]XAVPValue{{}}
	}
	s.roots[root][0][key] = XAVPValue{Str: val}
}

// SetInt stores an integer value for root[key] in the newest instance.
func (s *XAVPStore) SetInt(root, key string, val int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.roots[root]) == 0 {
		s.roots[root] = []map[string]XAVPValue{{}}
	}
	s.roots[root][0][key] = XAVPValue{Int: val, IsInt: true}
}

// GetStr retrieves the string value for root[key] from the newest instance.
func (s *XAVPStore) GetStr(root, key string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	insts := s.roots[root]
	if len(insts) == 0 {
		return "", false
	}
	v, ok := insts[0][key]
	if !ok || v.IsInt {
		return "", false
	}
	return v.Str, true
}

// GetInt retrieves the integer value for root[key] from the newest instance.
func (s *XAVPStore) GetInt(root, key string) (int, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	insts := s.roots[root]
	if len(insts) == 0 {
		return 0, false
	}
	v, ok := insts[0][key]
	if !ok || !v.IsInt {
		return 0, false
	}
	return v.Int, true
}

// Del removes all instances of a root.
func (s *XAVPStore) Del(root string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.roots, root)
}
