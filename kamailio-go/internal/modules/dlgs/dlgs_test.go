// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Dlgs (dialog store) module.
 */

package dlgs

import (
	"sync"
	"testing"
	"time"
)

func TestCreate(t *testing.T) {
	m := New()

	rec := m.Create("call-1@example.com", "ftag1", "sip:alice@example.com", "sip:bob@example.com", "sip:bob@example.com")
	if rec == nil {
		t.Fatalf("Create() returned nil")
	}
	if rec.ID == "" {
		t.Errorf("ID should be generated, got empty")
	}
	if rec.CallID != "call-1@example.com" {
		t.Errorf("CallID = %q, want %q", rec.CallID, "call-1@example.com")
	}
	if rec.FromTag != "ftag1" {
		t.Errorf("FromTag = %q, want %q", rec.FromTag, "ftag1")
	}
	if rec.FromURI != "sip:alice@example.com" {
		t.Errorf("FromURI = %q", rec.FromURI)
	}
	if rec.ToURI != "sip:bob@example.com" {
		t.Errorf("ToURI = %q", rec.ToURI)
	}
	if rec.RURI != "sip:bob@example.com" {
		t.Errorf("RURI = %q", rec.RURI)
	}
	if rec.State != "early" {
		t.Errorf("State = %q, want %q", rec.State, "early")
	}
	if rec.CreatedAt.IsZero() {
		t.Errorf("CreatedAt should be set")
	}
	if rec.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt should be set")
	}
	if rec.CreatedAt != rec.UpdatedAt {
		t.Errorf("CreatedAt and UpdatedAt should match on create")
	}

	// Two records get distinct IDs.
	rec2 := m.Create("call-2@example.com", "ftag2", "sip:a@b", "sip:c@d", "sip:c@d")
	if rec2.ID == rec.ID {
		t.Errorf("two records share the same ID %q", rec.ID)
	}
}

func TestGet(t *testing.T) {
	m := New()

	rec := m.Create("call-get@example.com", "ftag", "sip:a@b", "sip:c@d", "sip:c@d")
	got := m.Get("call-get@example.com", "ftag")
	if got == nil {
		t.Fatalf("Get() returned nil")
	}
	if got.ID != rec.ID {
		t.Errorf("Get().ID = %q, want %q", got.ID, rec.ID)
	}

	// Unknown -> nil.
	if got := m.Get("nope", "nope"); got != nil {
		t.Errorf("Get(unknown) should return nil")
	}
}

func TestGetByID(t *testing.T) {
	m := New()

	rec := m.Create("call-id@example.com", "ft", "sip:a@b", "sip:c@d", "sip:c@d")
	got := m.GetByID(rec.ID)
	if got == nil {
		t.Fatalf("GetByID() returned nil")
	}
	if got.CallID != rec.CallID {
		t.Errorf("GetByID().CallID = %q, want %q", got.CallID, rec.CallID)
	}

	// Unknown -> nil.
	if got := m.GetByID("does-not-exist"); got != nil {
		t.Errorf("GetByID(unknown) should return nil")
	}
}

func TestUpdate(t *testing.T) {
	m := New()

	rec := m.Create("call-upd@example.com", "ft", "sip:a@b", "sip:c@d", "sip:c@d")
	origUpdated := rec.UpdatedAt
	time.Sleep(2 * time.Millisecond)

	if !m.Update(rec.ID, "confirmed") {
		t.Fatalf("Update() returned false, want true")
	}
	got := m.GetByID(rec.ID)
	if got.State != "confirmed" {
		t.Errorf("State after update = %q, want %q", got.State, "confirmed")
	}
	if !got.UpdatedAt.After(origUpdated) {
		t.Errorf("UpdatedAt should advance after Update")
	}

	// Unknown ID -> false.
	if m.Update("nope", "x") {
		t.Errorf("Update(unknown) should return false")
	}
}

func TestDelete(t *testing.T) {
	m := New()

	rec := m.Create("call-del@example.com", "ft", "sip:a@b", "sip:c@d", "sip:c@d")
	if !m.Delete(rec.ID) {
		t.Fatalf("Delete() returned false, want true")
	}
	if m.GetByID(rec.ID) != nil {
		t.Errorf("GetByID() after delete should return nil")
	}
	if m.Get(rec.CallID, rec.FromTag) != nil {
		t.Errorf("Get() after delete should return nil")
	}

	// Deleting again -> false.
	if m.Delete(rec.ID) {
		t.Errorf("Delete() twice should return false")
	}
	// Deleting unknown -> false.
	if m.Delete("nope") {
		t.Errorf("Delete(unknown) should return false")
	}
}

func TestListAndCount(t *testing.T) {
	m := New()

	m.Create("c1@x", "t1", "sip:a@b", "sip:c@d", "sip:c@d")
	m.Create("c2@x", "t2", "sip:a@b", "sip:c@d", "sip:c@d")
	m.Create("c3@x", "t3", "sip:a@b", "sip:c@d", "sip:c@d")

	if got := m.Count(); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}
	list := m.List()
	if len(list) != 3 {
		t.Errorf("List() returned %d records, want 3", len(list))
	}
	// Each returned record must be non-nil and have a non-empty ID.
	for i, r := range list {
		if r == nil || r.ID == "" {
			t.Errorf("List()[%d] is nil or has empty ID", i)
		}
	}
}

func TestCountByState(t *testing.T) {
	m := New()

	r1 := m.Create("c1@x", "t1", "sip:a@b", "sip:c@d", "sip:c@d")
	r2 := m.Create("c2@x", "t2", "sip:a@b", "sip:c@d", "sip:c@d")
	m.Create("c3@x", "t3", "sip:a@b", "sip:c@d", "sip:c@d")

	m.Update(r1.ID, "confirmed")
	m.Update(r2.ID, "confirmed")

	if got := m.CountByState("confirmed"); got != 2 {
		t.Errorf("CountByState(confirmed) = %d, want 2", got)
	}
	if got := m.CountByState("early"); got != 1 {
		t.Errorf("CountByState(early) = %d, want 1", got)
	}
	if got := m.CountByState("terminated"); got != 0 {
		t.Errorf("CountByState(terminated) = %d, want 0", got)
	}
}

func TestCleanupExpired(t *testing.T) {
	m := New()

	r1 := m.Create("c1@x", "t1", "sip:a@b", "sip:c@d", "sip:c@d")
	m.Create("c2@x", "t2", "sip:a@b", "sip:c@d", "sip:c@d")

	// Age the first record so it is older than a 1-second TTL.
	r1.UpdatedAt = time.Now().Add(-2 * time.Second)

	m.CleanupExpired(1 * time.Second)
	if got := m.Count(); got != 1 {
		t.Errorf("Count() after cleanup = %d, want 1", got)
	}
	if m.GetByID(r1.ID) != nil {
		t.Errorf("expired record should have been cleaned up")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultDlgs()
	if d == nil {
		t.Fatalf("DefaultDlgs() returned nil")
	}
	if d != DefaultDlgs() {
		t.Fatalf("DefaultDlgs() returned different instances after Init()")
	}

	// Package-level wrappers delegate to the default module.
	Init()
	rec := Create("default@x", "dt", "sip:a@b", "sip:c@d", "sip:c@d")
	if rec == nil {
		t.Fatalf("package Create() returned nil")
	}
	if got := Count(); got != 1 {
		t.Errorf("package Count() = %d, want 1", got)
	}
	if got := GetByID(rec.ID); got == nil {
		t.Errorf("package GetByID() returned nil")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultDlgs()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			rec := shared.Create("c"+itoa(i)+"@x", "t"+itoa(i), "sip:a@b", "sip:c@d", "sip:c@d")
			shared.GetByID(rec.ID)
			shared.Update(rec.ID, "confirmed")
			shared.Count()
			shared.CountByState("confirmed")
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != goroutines {
		t.Errorf("Count() after concurrent creates = %d, want %d", got, goroutines)
	}
}

// itoa is a tiny local int->string helper to avoid pulling strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
