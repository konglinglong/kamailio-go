// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the rls module - Resource List Server subscriptions.
 */
package rls

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateSubscriptionAndGet(t *testing.T) {
	m := NewRLSModule()
	resources := []string{"sip:alice@example.com", "sip:bob@example.com"}
	sub := m.CreateSubscription("sip:watcher@example.com", resources, 3600)
	if sub == nil {
		t.Fatal("CreateSubscription returned nil")
	}
	if sub.WatcherURI != "sip:watcher@example.com" {
		t.Errorf("WatcherURI = %q", sub.WatcherURI)
	}
	if len(sub.ResourceList) != 2 {
		t.Errorf("ResourceList len = %d, want 2", len(sub.ResourceList))
	}
	if sub.Version != 1 {
		t.Errorf("Version = %d, want 1", sub.Version)
	}
	if sub.Expires.IsZero() || time.Now().After(sub.Expires) {
		t.Errorf("Expires = %v, want future", sub.Expires)
	}
	if got := m.GetSubscription("sip:watcher@example.com"); got != sub {
		t.Errorf("GetSubscription returned %p, want %p", got, sub)
	}
	if got := m.GetSubscription("sip:missing@example.com"); got != nil {
		t.Errorf("GetSubscription(missing) = %v, want nil", got)
	}
	if got := m.Count(); got != 1 {
		t.Errorf("Count = %d, want 1", got)
	}
}

func TestUpdateSubscriptionBumpsVersion(t *testing.T) {
	m := NewRLSModule()
	m.CreateSubscription("sip:w@example.com", []string{"sip:a@example.com"}, 300)
	before := m.GetSubscription("sip:w@example.com").Version
	resources := []RLSResource{
		{URI: "sip:a@example.com", State: "active", Body: "<pidf/>"},
	}
	if err := m.UpdateSubscription("sip:w@example.com", resources); err != nil {
		t.Fatalf("UpdateSubscription error: %v", err)
	}
	after := m.GetSubscription("sip:w@example.com").Version
	if after != before+1 {
		t.Errorf("Version = %d, want %d", after, before+1)
	}
	// Updating a missing subscription is an error.
	if err := m.UpdateSubscription("sip:ghost@example.com", resources); err == nil {
		t.Errorf("UpdateSubscription on missing should return error")
	}
}

func TestDeleteSubscription(t *testing.T) {
	m := NewRLSModule()
	m.CreateSubscription("sip:w2@example.com", []string{"sip:a@example.com"}, 300)
	if !m.DeleteSubscription("sip:w2@example.com") {
		t.Error("DeleteSubscription returned false for existing")
	}
	if m.DeleteSubscription("sip:w2@example.com") {
		t.Error("DeleteSubscription returned true for missing")
	}
	if got := m.GetSubscription("sip:w2@example.com"); got != nil {
		t.Errorf("GetSubscription after delete = %v, want nil", got)
	}
}

func TestCountAndList(t *testing.T) {
	m := NewRLSModule()
	for _, w := range []string{"sip:w1@example.com", "sip:w2@example.com", "sip:w3@example.com"} {
		m.CreateSubscription(w, []string{"sip:r@example.com"}, 60)
	}
	if got := m.Count(); got != 3 {
		t.Errorf("Count = %d, want 3", got)
	}
	list := m.List()
	if len(list) != 3 {
		t.Errorf("List len = %d, want 3", len(list))
	}
	seen := map[string]bool{}
	for _, s := range list {
		seen[s.WatcherURI] = true
	}
	for _, w := range []string{"sip:w1@example.com", "sip:w2@example.com", "sip:w3@example.com"} {
		if !seen[w] {
			t.Errorf("List missing %q", w)
		}
	}
}

func TestBuildRLSBody(t *testing.T) {
	m := NewRLSModule()
	sub := m.CreateSubscription("sip:watcher@example.com", []string{"sip:alice@example.com", "sip:bob@example.com"}, 300)
	resources := []RLSResource{
		{URI: "sip:alice@example.com", State: "active", Body: "<presence-alice/>"},
		{URI: "sip:bob@example.com", State: "terminated", Body: "<presence-bob/>"},
	}
	body := m.BuildRLSBody(sub, resources)
	if body == "" {
		t.Fatal("BuildRLSBody returned empty body")
	}
	// RLMI part present.
	if !strings.Contains(body, "application/rlmi+xml") {
		t.Errorf("body missing RLMI content type")
	}
	// Multipart boundary.
	if !strings.Contains(body, "multipart/related") {
		t.Errorf("body missing multipart/related")
	}
	// Version reflected.
	if !strings.Contains(body, `version="1"`) {
		t.Errorf("body missing version attribute")
	}
	// Each resource URI present.
	if !strings.Contains(body, "sip:alice@example.com") {
		t.Errorf("body missing alice resource")
	}
	if !strings.Contains(body, "sip:bob@example.com") {
		t.Errorf("body missing bob resource")
	}
	// Each resource body present.
	if !strings.Contains(body, "<presence-alice/>") {
		t.Errorf("body missing alice presence body")
	}
	if !strings.Contains(body, "<presence-bob/>") {
		t.Errorf("body missing bob presence body")
	}
	// Resource states present.
	if !strings.Contains(body, "active") {
		t.Errorf("body missing active state")
	}
	if !strings.Contains(body, "terminated") {
		t.Errorf("body missing terminated state")
	}
}

func TestCleanupExpired(t *testing.T) {
	m := NewRLSModule()
	m.CreateSubscription("sip:fresh@example.com", []string{"sip:r@example.com"}, 3600)
	// Manually plant an expired subscription.
	m.mu.Lock()
	m.subs["sip:expired@example.com"] = &RLSSubscription{
		WatcherURI:   "sip:expired@example.com",
		ResourceList: []string{"sip:r@example.com"},
		Expires:      time.Now().Add(-1 * time.Minute),
		Version:      1,
	}
	m.mu.Unlock()
	if got := m.Count(); got != 2 {
		t.Fatalf("Count before cleanup = %d, want 2", got)
	}
	m.CleanupExpired()
	if got := m.Count(); got != 1 {
		t.Errorf("Count after cleanup = %d, want 1", got)
	}
	if m.GetSubscription("sip:expired@example.com") != nil {
		t.Errorf("expired subscription should be removed")
	}
	if m.GetSubscription("sip:fresh@example.com") == nil {
		t.Errorf("fresh subscription should remain")
	}
}

func TestDefaultRLSAndInit(t *testing.T) {
	Init()
	a := DefaultRLS()
	b := DefaultRLS()
	if a != b {
		t.Error("DefaultRLS should return the same instance")
	}
	Init()
	c := DefaultRLS()
	if c == a {
		t.Error("Init should reset the default instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewRLSModule()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			w := "sip:cw" + itoa(i) + "@example.com"
			sub := m.CreateSubscription(w, []string{"sip:r@example.com"}, 60)
			res := []RLSResource{{URI: "sip:r@example.com", State: "active", Body: "<p/>"}}
			_ = m.UpdateSubscription(w, res)
			_ = m.BuildRLSBody(sub, res)
			_ = m.GetSubscription(w)
			_ = m.List()
			if i%2 == 0 {
				m.DeleteSubscription(w)
			}
		}()
	}
	wg.Wait()
	m.CleanupExpired()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
