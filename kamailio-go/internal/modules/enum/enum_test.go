// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - Enum module tests.
 */
package enum

import (
	"sync"
	"testing"
)

// TestBuildEnumName verifies ENUM domain name construction.
func TestBuildEnumName(t *testing.T) {
	m := NewEnumModule()
	got := m.BuildEnumName("+35831234567")
	want := "7.6.5.4.3.2.1.3.8.5.3.e164.arpa"
	if got != want {
		t.Errorf("BuildEnumName = %q, want %q", got, want)
	}
	// Leading '+' is optional; non-digits are ignored.
	if got := m.BuildEnumName("358-312-34567"); got != want {
		t.Errorf("BuildEnumName = %q, want %q", got, want)
	}
	// Custom suffix via Domain.
	m2 := NewEnumModule()
	m2.config = EnumConfig{Domain: "e164.arpa"}
	if got := m2.BuildEnumName("+1"); got != "1.e164.arpa" {
		t.Errorf("BuildEnumName = %q, want 1.e164.arpa", got)
	}
}

// TestParseNAPTR verifies NAPTR record parsing.
func TestParseNAPTR(t *testing.T) {
	m := NewEnumModule()
	rec, err := m.ParseNAPTR(`100 10 "u" "sip+E2U" "!^.*$!sip:info@example.com!" "."`)
	if err != nil {
		t.Fatalf("ParseNAPTR failed: %v", err)
	}
	if rec.Order != 100 || rec.Preference != 10 {
		t.Errorf("order/pref = %d/%d, want 100/10", rec.Order, rec.Preference)
	}
	if rec.Flags != "u" || rec.Service != "sip+E2U" {
		t.Errorf("flags/service = %q/%q", rec.Flags, rec.Service)
	}
	if rec.Regexp != "!^.*$!sip:info@example.com!" {
		t.Errorf("Regexp = %q", rec.Regexp)
	}
	if rec.Replacement != "." {
		t.Errorf("Replacement = %q", rec.Replacement)
	}
	// Unquoted form also works.
	rec2, err := m.ParseNAPTR(`50 5 u sip+E2U !^.*$!sip:bob@example.com! .`)
	if err != nil {
		t.Fatalf("ParseNAPTR unquoted failed: %v", err)
	}
	if rec2.Order != 50 {
		t.Errorf("Order = %d, want 50", rec2.Order)
	}
	// Too few fields errors.
	if _, err := m.ParseNAPTR(`100 10 u`); err == nil {
		t.Error("expected error for too few fields")
	}
}

// TestApplyRegexp verifies NAPTR regexp application.
func TestApplyRegexp(t *testing.T) {
	m := NewEnumModule()
	rec := &EnumRecord{Regexp: `!^.*$!sip:info@example.com!`}
	got, err := m.ApplyRegexp(rec, "35831234567")
	if err != nil {
		t.Fatalf("ApplyRegexp failed: %v", err)
	}
	if got != "sip:info@example.com" {
		t.Errorf("ApplyRegexp = %q, want sip:info@example.com", got)
	}
	// Backreference conversion: \1 -> $1.
	rec2 := &EnumRecord{Regexp: `!^(.*)$!sip:\1@example.com!`}
	got, err = m.ApplyRegexp(rec2, "1001")
	if err != nil {
		t.Fatalf("ApplyRegexp failed: %v", err)
	}
	if got != "sip:1001@example.com" {
		t.Errorf("ApplyRegexp = %q, want sip:1001@example.com", got)
	}
	// Invalid regexp errors.
	if _, err := m.ApplyRegexp(&EnumRecord{Regexp: ""}, "x"); err == nil {
		t.Error("expected error for empty regexp")
	}
	if _, err := m.ApplyRegexp(nil, "x"); err == nil {
		t.Error("expected error for nil record")
	}
}

// TestLookupAndQuery verifies record storage, lookup and query.
func TestLookupAndQuery(t *testing.T) {
	m := NewEnumModule()
	m.AddRecord("+35831234567", &EnumRecord{
		Order: 100, Preference: 10, Flags: "u", Service: "sip+E2U",
		Regexp: `!^.*$!sip:info@example.com!`, Replacement: ".",
	})
	uri, err := m.Lookup("+35831234567")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if uri != "sip:info@example.com" {
		t.Errorf("Lookup = %q, want sip:info@example.com", uri)
	}
	// Query returns the stored records.
	recs, err := m.Query("+35831234567", "")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("Query returned %d records, want 1", len(recs))
	}
	if recs[0].Service != "sip+E2U" {
		t.Errorf("Service = %q, want sip+E2U", recs[0].Service)
	}
	// Mutating the returned copy must not affect the module.
	recs[0].Service = "mutated"
	if got, _ := m.Query("+35831234567", ""); got[0].Service == "mutated" {
		t.Fatal("expected isolation from Query copy")
	}
	// Lookup for an unknown number errors.
	if _, err := m.Lookup("+999999"); err == nil {
		t.Error("expected error for unknown number")
	}
}

// TestLookupOrdering verifies records are ordered by (order, preference).
func TestLookupOrdering(t *testing.T) {
	m := NewEnumModule()
	// Higher order tried later; the lower-order record wins.
	m.AddRecord("+123", &EnumRecord{
		Order: 200, Preference: 5, Regexp: `!^.*$!sip:second@example.com!`,
	})
	m.AddRecord("+123", &EnumRecord{
		Order: 100, Preference: 50, Regexp: `!^.*$!sip:first@example.com!`,
	})
	uri, err := m.Lookup("+123")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if uri != "sip:first@example.com" {
		t.Errorf("Lookup = %q, want sip:first@example.com (lowest order)", uri)
	}
}

// TestIsEnumURI verifies ENUM URI detection.
func TestIsEnumURI(t *testing.T) {
	m := NewEnumModule()
	if !m.IsEnumURI("sip:7.6.5.4.3.2.1.3.8.5.3.e164.arpa") {
		t.Error("expected ENUM URI to be detected")
	}
	if m.IsEnumURI("sip:alice@example.com") {
		t.Error("expected non-ENUM URI to be rejected")
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	Init()
	e := DefaultEnum()
	if e == nil {
		t.Fatal("expected non-nil default enum")
	}
	e.AddRecord("+111", &EnumRecord{Regexp: `!^.*$!sip:global@example.com!`})
	uri, err := e.Lookup("+111")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if uri != "sip:global@example.com" {
		t.Errorf("Lookup = %q, want sip:global@example.com", uri)
	}
}

// TestConcurrentAccess exercises the module under the race detector.
func TestConcurrentAccess(t *testing.T) {
	m := NewEnumModule()
	m.AddRecord("+12345", &EnumRecord{Regexp: `!^.*$!sip:info@example.com!`})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Lookup("+12345")
			_, _ = m.Query("+12345", "")
			_ = m.BuildEnumName("+12345")
			_ = m.IsEnumURI("sip:5.4.3.2.1.e164.arpa")
		}()
	}
	wg.Wait()
}
