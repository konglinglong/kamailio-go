// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * imc module - Instant Messaging Conferencing.
 *
 * Manages chat rooms and their members. Rooms are keyed by name; each
 * room tracks an ordered list of member URIs and a creation timestamp.
 * The module is safe for concurrent use.
 */

package imc

import (
	"sync"
	"time"
)

// IMCRoom is a single conferencing room.
type IMCRoom struct {
	Name      string
	Members   []string
	CreatedAt time.Time
}

// IMCModule manages instant messaging conference rooms.
type IMCModule struct {
	mu    sync.RWMutex
	rooms map[string]*IMCRoom
}

// New creates an IMCModule with no rooms.
func New() *IMCModule {
	return &IMCModule{rooms: make(map[string]*IMCRoom)}
}

// CreateRoom creates a new room named name and returns it. If a room
// with that name already exists it is returned unchanged.
//
//	C: imc_create_room()
func (m *IMCModule) CreateRoom(name string) *IMCRoom {
	if name == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rooms[name]; ok {
		return r
	}
	r := &IMCRoom{Name: name, CreatedAt: time.Now()}
	m.rooms[name] = r
	return r
}

// DeleteRoom removes the room named name. Returns true when a room was
// removed.
//
//	C: imc_delete_room()
func (m *IMCModule) DeleteRoom(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rooms[name]; !ok {
		return false
	}
	delete(m.rooms, name)
	return true
}

// JoinRoom adds user to room, creating the room when missing. Returns
// false when user is empty.
//
//	C: imc_join_room()
func (m *IMCModule) JoinRoom(room, user string) bool {
	if room == "" || user == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[room]
	if !ok {
		r = &IMCRoom{Name: room, CreatedAt: time.Now()}
		m.rooms[room] = r
	}
	for _, mem := range r.Members {
		if mem == user {
			return true // already a member
		}
	}
	r.Members = append(r.Members, user)
	return true
}

// LeaveRoom removes user from room. Returns true when the user was a
// member. The room is removed when it becomes empty.
//
//	C: imc_leave_room()
func (m *IMCModule) LeaveRoom(room, user string) bool {
	if room == "" || user == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[room]
	if !ok {
		return false
	}
	for i, mem := range r.Members {
		if mem == user {
			r.Members = append(r.Members[:i], r.Members[i+1:]...)
			if len(r.Members) == 0 {
				delete(m.rooms, room)
			}
			return true
		}
	}
	return false
}

// ListRooms returns a snapshot of all rooms.
//
//	C: imc_list_rooms()
func (m *IMCModule) ListRooms() []*IMCRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*IMCRoom, 0, len(m.rooms))
	for _, r := range m.rooms {
		out = append(out, r)
	}
	return out
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu  sync.RWMutex
	defaultIMC *IMCModule
)

// DefaultIMC returns the process-wide IMCModule, creating it on first use.
func DefaultIMC() *IMCModule {
	defaultMu.RLock()
	m := defaultIMC
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultIMC == nil {
		defaultIMC = New()
	}
	return defaultIMC
}

// Init (re)initialises the process-wide IMCModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultIMC = New()
}
