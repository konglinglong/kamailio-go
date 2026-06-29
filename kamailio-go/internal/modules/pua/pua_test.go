// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the pua module - Presence User Agent publish/subscribe.
 */
package pua

import (
	"sync"
	"testing"
	"time"
)

func TestSendPublishAndGetRecord(t *testing.T) {
	m := NewPUAModule()
	if got := m.Count(); got != 0 {
		t.Fatalf("initial Count = %d, want 0", got)
	}
	if err := m.SendPublish("sip:alice@example.com", "<pidf/>", 3600); err != nil {
		t.Fatalf("SendPublish error: %v", err)
	}
	if got := m.Count(); got != 1 {
		t.Errorf("Count after publish = %d, want 1", got)
	}
	rec := m.GetRecord("sip:alice@example.com")
	if rec == nil {
		t.Fatal("GetRecord returned nil")
	}
	if rec.PresURI != "sip:alice@example.com" {
		t.Errorf("PresURI = %q", rec.PresURI)
	}
	if rec.Body != "<pidf/>" {
		t.Errorf("Body = %q", rec.Body)
	}
	if rec.Event != "presence" {
		t.Errorf("Event = %q, want presence", rec.Event)
	}
	if rec.ETag == "" {
		t.Errorf("ETag should be assigned on publish")
	}
	if rec.Expires.IsZero() || time.Now().After(rec.Expires) {
		t.Errorf("Expires = %v, want future", rec.Expires)
	}
}

func TestSendPublishReplacesExisting(t *testing.T) {
	m := NewPUAModule()
	if err := m.SendPublish("sip:bob@example.com", "body1", 60); err != nil {
		t.Fatal(err)
	}
	first := m.GetRecord("sip:bob@example.com")
	if err := m.SendPublish("sip:bob@example.com", "body2", 120); err != nil {
		t.Fatal(err)
	}
	if got := m.Count(); got != 1 {
		t.Errorf("Count after re-publish = %d, want 1", got)
	}
	second := m.GetRecord("sip:bob@example.com")
	if second.Body != "body2" {
		t.Errorf("Body = %q, want body2", second.Body)
	}
	if second.Expires.Before(first.Expires) {
		t.Errorf("Expires should be extended on re-publish")
	}
}

func TestSendSubscribeAndUnsubscribe(t *testing.T) {
	m := NewPUAModule()
	if err := m.SendSubscribe("sip:watcher@example.com", "sip:carol@example.com", "presence", 300); err != nil {
		t.Fatalf("SendSubscribe error: %v", err)
	}
	rec := m.GetRecord("sip:carol@example.com")
	if rec == nil {
		t.Fatal("GetRecord returned nil after subscribe")
	}
	if rec.WatcherURI != "sip:watcher@example.com" {
		t.Errorf("WatcherURI = %q", rec.WatcherURI)
	}
	if rec.Event != "presence" {
		t.Errorf("Event = %q", rec.Event)
	}
	if rec.State != "active" {
		t.Errorf("State = %q, want active", rec.State)
	}
	// Unsubscribe removes the record.
	if err := m.SendUnsubscribe("sip:watcher@example.com", "sip:carol@example.com"); err != nil {
		t.Fatalf("SendUnsubscribe error: %v", err)
	}
	if rec := m.GetRecord("sip:carol@example.com"); rec != nil {
		t.Errorf("GetRecord after unsubscribe = %v, want nil", rec)
	}
	if got := m.Count(); got != 0 {
		t.Errorf("Count after unsubscribe = %d, want 0", got)
	}
}

func TestSendUnsubscribeMissing(t *testing.T) {
	m := NewPUAModule()
	// Unsubscribing a non-existent record is not an error.
	if err := m.SendUnsubscribe("sip:nope@example.com", "sip:ghost@example.com"); err != nil {
		t.Errorf("SendUnsubscribe missing returned error: %v", err)
	}
}

func TestUpdateRecordAndDeleteRecord(t *testing.T) {
	m := NewPUAModule()
	rec := &PUARecord{
		PresURI:    "sip:dave@example.com",
		WatcherURI: "sip:w@example.com",
		ETag:       "etag-1",
		Expires:    time.Now().Add(1 * time.Hour),
		Body:       "<body/>",
		Event:      "presence",
		State:      "active",
	}
	if !m.UpdateRecord(rec) {
		t.Fatal("UpdateRecord returned false for new record")
	}
	if got := m.GetRecord("sip:dave@example.com"); got == nil || got.ETag != "etag-1" {
		t.Errorf("GetRecord after UpdateRecord = %+v", got)
	}
	// Update existing.
	rec.Body = "<updated/>"
	if !m.UpdateRecord(rec) {
		t.Error("UpdateRecord returned false for existing record")
	}
	if got := m.GetRecord("sip:dave@example.com"); got.Body != "<updated/>" {
		t.Errorf("Body after update = %q", got.Body)
	}
	// DeleteRecord.
	if !m.DeleteRecord("sip:dave@example.com") {
		t.Error("DeleteRecord returned false for existing record")
	}
	if m.DeleteRecord("sip:dave@example.com") {
		t.Error("DeleteRecord returned true for missing record")
	}
}

func TestListAndCount(t *testing.T) {
	m := NewPUAModule()
	for _, uri := range []string{"sip:a@example.com", "sip:b@example.com", "sip:c@example.com"} {
		if err := m.SendPublish(uri, "x", 60); err != nil {
			t.Fatal(err)
		}
	}
	if got := m.Count(); got != 3 {
		t.Errorf("Count = %d, want 3", got)
	}
	list := m.List()
	if len(list) != 3 {
		t.Errorf("List len = %d, want 3", len(list))
	}
	seen := map[string]bool{}
	for _, r := range list {
		seen[r.PresURI] = true
	}
	for _, uri := range []string{"sip:a@example.com", "sip:b@example.com", "sip:c@example.com"} {
		if !seen[uri] {
			t.Errorf("List missing %q", uri)
		}
	}
}

func TestCleanupExpired(t *testing.T) {
	m := NewPUAModule()
	// One fresh, one expired.
	if err := m.SendPublish("sip:fresh@example.com", "b", 3600); err != nil {
		t.Fatal(err)
	}
	expired := &PUARecord{
		PresURI: "sip:expired@example.com",
		ETag:    "e",
		Expires: time.Now().Add(-1 * time.Minute),
		Event:   "presence",
	}
	m.UpdateRecord(expired)
	if got := m.Count(); got != 2 {
		t.Fatalf("Count before cleanup = %d, want 2", got)
	}
	m.CleanupExpired()
	if got := m.Count(); got != 1 {
		t.Errorf("Count after cleanup = %d, want 1", got)
	}
	if rec := m.GetRecord("sip:expired@example.com"); rec != nil {
		t.Errorf("expired record should have been removed")
	}
	if rec := m.GetRecord("sip:fresh@example.com"); rec == nil {
		t.Errorf("fresh record should remain")
	}
}

func TestDefaultPUAAndInit(t *testing.T) {
	Init()
	a := DefaultPUA()
	b := DefaultPUA()
	if a != b {
		t.Error("DefaultPUA should return the same instance")
	}
	Init()
	c := DefaultPUA()
	if c == a {
		t.Error("Init should reset the default instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewPUAModule()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			uri := "sip:u" + itoa(i) + "@example.com"
			_ = m.SendPublish(uri, "body", 60)
			_ = m.GetRecord(uri)
			_ = m.List()
			if i%2 == 0 {
				_ = m.DeleteRecord(uri)
			}
		}()
	}
	wg.Wait()
	m.CleanupExpired()
}

// itoa is a tiny local int->string helper to avoid importing strconv in
// the test goroutine hot path.
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
