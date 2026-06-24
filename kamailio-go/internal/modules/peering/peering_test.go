// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - peering module tests.
 */

package peering

import (
	"sort"
	"testing"
)

func TestAddCheckRemove(t *testing.T) {
	m := New()
	m.AddPeer("peer1.example.com", "tok1")
	m.AddPeer("peer2.example.com", "tok2")
	if !m.CheckPeer("peer1.example.com") {
		t.Error("CheckPeer(peer1) = false, want true")
	}
	if m.CheckPeer("unknown.example.com") {
		t.Error("CheckPeer(unknown) = true, want false")
	}
	if !m.RemovePeer("peer1.example.com") {
		t.Error("RemovePeer(peer1) = false, want true")
	}
	if m.CheckPeer("peer1.example.com") {
		t.Error("CheckPeer after remove should be false")
	}
	if m.RemovePeer("peer1.example.com") {
		t.Error("RemovePeer twice = true, want false")
	}
}

func TestValidateToken(t *testing.T) {
	m := New()
	m.AddPeer("peer.example.com", "secret")
	if !m.ValidateToken("peer.example.com", "secret") {
		t.Error("ValidateToken(correct) = false, want true")
	}
	if m.ValidateToken("peer.example.com", "wrong") {
		t.Error("ValidateToken(wrong) = true, want false")
	}
	if m.ValidateToken("unknown", "secret") {
		t.Error("ValidateToken(unknown) = true, want false")
	}
}

func TestListPeers(t *testing.T) {
	m := New()
	m.AddPeer("a.example.com", "1")
	m.AddPeer("b.example.com", "2")
	lst := m.ListPeers()
	sort.Strings(lst)
	if len(lst) != 2 || lst[0] != "a.example.com" || lst[1] != "b.example.com" {
		t.Errorf("ListPeers = %v, want [a b]", lst)
	}
}
