// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Group module - user group membership.
 * Port of the kamailio group module (src/modules/group).
 *
 * The module maintains group-to-user membership sets and supports
 * add/remove/membership-query operations plus reverse lookups. It is
 * safe for concurrent use.
 */

package group

import "sync"

// GroupModule maintains group membership.
type GroupModule struct {
	mu     sync.RWMutex
	groups map[string]map[string]struct{}
}

// New creates a GroupModule with empty storage.
func New() *GroupModule {
	return &GroupModule{groups: make(map[string]map[string]struct{})}
}

// AddUser adds a user to the given group, creating the group when
// necessary. Adding an existing member is a no-op.
//
//	C: group_add_user()
func (m *GroupModule) AddUser(group, user string) {
	if group == "" || user == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.groups == nil {
		m.groups = make(map[string]map[string]struct{})
	}
	if m.groups[group] == nil {
		m.groups[group] = make(map[string]struct{})
	}
	m.groups[group][user] = struct{}{}
}

// RemoveUser removes a user from the given group. Returns true when the
// user was a member.
//
//	C: group_remove_user()
func (m *GroupModule) RemoveUser(group, user string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	members, ok := m.groups[group]
	if !ok {
		return false
	}
	if _, ok := members[user]; !ok {
		return false
	}
	delete(members, user)
	return true
}

// IsUserInGroup returns true when the user is a member of the group.
//
//	C: is_user_in_group()
func (m *GroupModule) IsUserInGroup(group, user string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	members, ok := m.groups[group]
	if !ok {
		return false
	}
	_, ok = members[user]
	return ok
}

// GetGroups returns the names of all groups that contain the given user.
func (m *GroupModule) GetGroups(user string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []string
	for g, members := range m.groups {
		if _, ok := members[user]; ok {
			out = append(out, g)
		}
	}
	return out
}

// List returns a copy of all groups and their members.
func (m *GroupModule) List() map[string][]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]string, len(m.groups))
	for g, members := range m.groups {
		users := make([]string, 0, len(members))
		for u := range members {
			users = append(users, u)
		}
		out[g] = users
	}
	return out
}

// GroupCount returns the number of groups.
func (m *GroupModule) GroupCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.groups)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *GroupModule
)

// DefaultGroup returns the process-wide GroupModule.
func DefaultGroup() *GroupModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide GroupModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// AddUser is the package-level wrapper around DefaultGroup().AddUser.
func AddUser(group, user string) { DefaultGroup().AddUser(group, user) }

// RemoveUser is the package-level wrapper around DefaultGroup().RemoveUser.
func RemoveUser(group, user string) bool { return DefaultGroup().RemoveUser(group, user) }

// IsUserInGroup is the package-level wrapper around DefaultGroup().IsUserInGroup.
func IsUserInGroup(group, user string) bool { return DefaultGroup().IsUserInGroup(group, user) }

// GetGroups is the package-level wrapper around DefaultGroup().GetGroups.
func GetGroups(user string) []string { return DefaultGroup().GetGroups(user) }

// List is the package-level wrapper around DefaultGroup().List.
func List() map[string][]string { return DefaultGroup().List() }
