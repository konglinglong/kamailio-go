// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - imc module tests.
 */

package imc

import (
	"sync"
	"testing"
	"time"
)

func TestCreateAndDeleteRoom(t *testing.T) {
	m := New()
	r := m.CreateRoom("room1")
	if r == nil {
		t.Fatal("CreateRoom returned nil")
	}
	if r.Name != "room1" {
		t.Errorf("Name = %q", r.Name)
	}
	if r.CreatedAt.IsZero() || time.Now().Before(r.CreatedAt) {
		t.Errorf("CreatedAt = %v, want ~now", r.CreatedAt)
	}
	// Creating the same room returns the existing one.
	r2 := m.CreateRoom("room1")
	if r2 != r {
		t.Errorf("CreateRoom should return existing room")
	}
	if !m.DeleteRoom("room1") {
		t.Errorf("DeleteRoom should return true for existing room")
	}
	if m.DeleteRoom("room1") {
		t.Errorf("DeleteRoom should return false for missing room")
	}
	if m.CreateRoom("") != nil {
		t.Errorf("CreateRoom with empty name should return nil")
	}
}

func TestJoinAndLeaveRoom(t *testing.T) {
	m := New()
	if !m.JoinRoom("room", "alice") {
		t.Fatal("JoinRoom should succeed")
	}
	m.JoinRoom("room", "bob")
	m.JoinRoom("room", "alice") // duplicate is a no-op success
	r := m.CreateRoom("room")
	if len(r.Members) != 2 {
		t.Errorf("Members = %v, want 2", r.Members)
	}
	if !m.LeaveRoom("room", "alice") {
		t.Errorf("LeaveRoom should return true for member")
	}
	if m.LeaveRoom("room", "alice") {
		t.Errorf("LeaveRoom should return false for non-member")
	}
	// Last member leaving removes the room.
	if !m.LeaveRoom("room", "bob") {
		t.Errorf("LeaveRoom should return true for member")
	}
	if m.CreateRoom("room") == nil {
		t.Errorf("room should have been removed when empty")
	}
	// Edge cases.
	if m.JoinRoom("", "u") {
		t.Errorf("JoinRoom with empty room should fail")
	}
	if m.JoinRoom("room", "") {
		t.Errorf("JoinRoom with empty user should fail")
	}
	if m.LeaveRoom("missing", "u") {
		t.Errorf("LeaveRoom on missing room should fail")
	}
}

func TestListRooms(t *testing.T) {
	m := New()
	m.CreateRoom("a")
	m.CreateRoom("b")
	m.CreateRoom("c")
	list := m.ListRooms()
	if len(list) != 3 {
		t.Fatalf("ListRooms len = %d, want 3", len(list))
	}
	seen := map[string]bool{}
	for _, r := range list {
		seen[r.Name] = true
	}
	for _, n := range []string{"a", "b", "c"} {
		if !seen[n] {
			t.Errorf("ListRooms missing %q", n)
		}
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultIMC()
	b := DefaultIMC()
	if a != b {
		t.Fatal("DefaultIMC should return the same instance")
	}
	a.CreateRoom("default")
	Init()
	c := DefaultIMC()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
	if len(c.ListRooms()) != 0 {
		t.Errorf("reset default should have no rooms")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	rooms := []string{"r1", "r2", "r3"}
	for _, room := range rooms {
		wg.Add(1)
		room := room
		go func() {
			defer wg.Done()
			m.CreateRoom(room)
			m.JoinRoom(room, "alice")
			m.JoinRoom(room, "bob")
			m.LeaveRoom(room, "alice")
			_ = m.ListRooms()
		}()
	}
	wg.Wait()
}
