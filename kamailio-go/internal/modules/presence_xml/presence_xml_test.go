// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the presence_xml module - PIDF/XML body parsing and building.
 */
package presence_xml

import (
	"strings"
	"sync"
	"testing"
)

func TestParseBodySingleTuple(t *testing.T) {
	m := NewPresenceXMLModule()
	body := `<?xml version="1.0" encoding="UTF-8"?>
<presence xmlns="urn:ietf:params:xml:ns:pidf" entity="pres:sip:alice@example.com">
  <tuple id="t1">
    <status>
      <basic>open</basic>
    </status>
    <contact>sip:alice@example.com</contact>
    <timestamp>2020-01-01T00:00:00Z</timestamp>
  </tuple>
</presence>`

	docs, err := m.ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody returned error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	d := docs[0]
	if d.Entity != "pres:sip:alice@example.com" {
		t.Errorf("Entity = %q", d.Entity)
	}
	if d.TupleID != "t1" {
		t.Errorf("TupleID = %q, want t1", d.TupleID)
	}
	if d.Basic != "open" {
		t.Errorf("Basic = %q, want open", d.Basic)
	}
	if d.Contact != "sip:alice@example.com" {
		t.Errorf("Contact = %q", d.Contact)
	}
	if d.Timestamp != "2020-01-01T00:00:00Z" {
		t.Errorf("Timestamp = %q", d.Timestamp)
	}
}

func TestParseBodyMultipleTuples(t *testing.T) {
	m := NewPresenceXMLModule()
	body := `<?xml version="1.0" encoding="UTF-8"?>
<presence xmlns="urn:ietf:params:xml:ns:pidf" entity="pres:sip:bob@example.com">
  <tuple id="tuple-a">
    <status><basic>open</basic></status>
    <contact>sip:bob@phone.example.com</contact>
  </tuple>
  <tuple id="tuple-b">
    <status><basic>closed</basic></status>
    <contact>sip:bob@desk.example.com</contact>
  </tuple>
</presence>`

	docs, err := m.ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody returned error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
	if docs[0].TupleID != "tuple-a" || docs[0].Basic != "open" {
		t.Errorf("doc0 = {TupleID:%q Basic:%q}", docs[0].TupleID, docs[0].Basic)
	}
	if docs[1].TupleID != "tuple-b" || docs[1].Basic != "closed" {
		t.Errorf("doc1 = {TupleID:%q Basic:%q}", docs[1].TupleID, docs[1].Basic)
	}
}

func TestBuildBodyRoundTrip(t *testing.T) {
	m := NewPresenceXMLModule()
	docs := []*PresenceDoc{
		{
			Entity:    "pres:sip:carol@example.com",
			TupleID:   "tx1",
			Contact:   "sip:carol@example.com",
			Basic:     "open",
			Timestamp: "2021-05-05T12:00:00Z",
		},
	}
	body := m.BuildBody(docs)
	if !strings.Contains(body, `entity="pres:sip:carol@example.com"`) {
		t.Errorf("body missing entity: %s", body)
	}
	if !strings.Contains(body, `id="tx1"`) {
		t.Errorf("body missing tuple id: %s", body)
	}
	if !strings.Contains(body, "<basic>open</basic>") {
		t.Errorf("body missing basic: %s", body)
	}
	if !strings.Contains(body, "sip:carol@example.com") {
		t.Errorf("body missing contact: %s", body)
	}
	// Round-trip: parse what we built.
	parsed, err := m.ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody(build) error: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("round-trip expected 1 doc, got %d", len(parsed))
	}
	if parsed[0].Basic != "open" || parsed[0].TupleID != "tx1" {
		t.Errorf("round-trip doc = %+v", parsed[0])
	}
}

func TestIsPresent(t *testing.T) {
	m := NewPresenceXMLModule()
	if !m.IsPresent(&PresenceDoc{Basic: "open"}) {
		t.Errorf("IsPresent(open) = false, want true")
	}
	if m.IsPresent(&PresenceDoc{Basic: "closed"}) {
		t.Errorf("IsPresent(closed) = true, want false")
	}
	if m.IsPresent(&PresenceDoc{Basic: ""}) {
		t.Errorf("IsPresent(empty) = true, want false")
	}
	// Case-insensitive.
	if !m.IsPresent(&PresenceDoc{Basic: "OPEN"}) {
		t.Errorf("IsPresent(OPEN) = false, want true (case-insensitive)")
	}
}

func TestGetStatus(t *testing.T) {
	m := NewPresenceXMLModule()
	// Explicit Status wins.
	if got := m.GetStatus(&PresenceDoc{Status: "away", Basic: "open"}); got != "away" {
		t.Errorf("GetStatus = %q, want away", got)
	}
	// Falls back to Basic when Status empty.
	if got := m.GetStatus(&PresenceDoc{Status: "", Basic: "open"}); got != "open" {
		t.Errorf("GetStatus fallback = %q, want open", got)
	}
}

func TestAggregate(t *testing.T) {
	m := NewPresenceXMLModule()
	docs := []*PresenceDoc{
		{Entity: "pres:sip:x@example.com", TupleID: "a", Basic: "closed"},
		{Entity: "pres:sip:x@example.com", TupleID: "b", Basic: "open"},
		{Entity: "pres:sip:x@example.com", TupleID: "c", Basic: "closed"},
	}
	agg := m.Aggregate(docs)
	if agg == nil {
		t.Fatal("Aggregate returned nil")
	}
	if agg.Entity != "pres:sip:x@example.com" {
		t.Errorf("Aggregate Entity = %q", agg.Entity)
	}
	if !m.IsPresent(agg) {
		t.Errorf("Aggregate should be present (any open), Basic = %q", agg.Basic)
	}
	// All closed -> not present.
	allClosed := []*PresenceDoc{
		{Entity: "pres:sip:y@example.com", Basic: "closed"},
		{Entity: "pres:sip:y@example.com", Basic: "closed"},
	}
	agg2 := m.Aggregate(allClosed)
	if m.IsPresent(agg2) {
		t.Errorf("Aggregate of all-closed should not be present, Basic = %q", agg2.Basic)
	}
	// Empty input -> nil.
	if got := m.Aggregate(nil); got != nil {
		t.Errorf("Aggregate(nil) = %v, want nil", got)
	}
}

func TestDefaultPresenceXMLAndInit(t *testing.T) {
	Init()
	d1 := DefaultPresenceXML()
	d2 := DefaultPresenceXML()
	if d1 != d2 {
		t.Error("DefaultPresenceXML should return the same instance")
	}
	// Init resets to a fresh instance.
	Init()
	d3 := DefaultPresenceXML()
	if d3 == d1 {
		t.Error("Init should reset the default instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewPresenceXMLModule()
	body := `<?xml version="1.0"?>
<presence xmlns="urn:ietf:params:xml:ns:pidf" entity="pres:sip:z@example.com">
  <tuple id="z1"><status><basic>open</basic></status></tuple>
</presence>`
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			docs, err := m.ParseBody(body)
			if err != nil || len(docs) != 1 {
				t.Errorf("concurrent ParseBody: docs=%d err=%v", len(docs), err)
			}
			_ = m.BuildBody(docs)
			_ = m.Aggregate(docs)
		}()
	}
	wg.Wait()
}
