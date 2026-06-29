// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the presence_dialoginfo module - dialog-info XML handling.
 */
package presence_dialoginfo

import (
	"strings"
	"sync"
	"testing"
)

func TestParseDialogInfo(t *testing.T) {
	m := NewDialogInfoModule()
	body := `<?xml version="1.0" encoding="UTF-8"?>
<dialog-info xmlns="urn:ietf:params:xml:ns:dialog-info" version="1" state="full" entity="sip:alice@example.com">
  <dialog id="dlg1" call-id="call-123@host" direction="initiator" local-tag="lt-1" remote-tag="rt-1">
    <state>confirmed</state>
    <local>
      <identity>sip:alice@example.com</identity>
    </local>
    <remote>
      <identity>sip:bob@example.com</identity>
    </remote>
  </dialog>
</dialog-info>`

	info, err := m.ParseDialogInfo(body)
	if err != nil {
		t.Fatalf("ParseDialogInfo error: %v", err)
	}
	if info.Entity != "sip:alice@example.com" {
		t.Errorf("Entity = %q", info.Entity)
	}
	if info.State != "confirmed" {
		t.Errorf("State = %q, want confirmed", info.State)
	}
	if info.Direction != "initiator" {
		t.Errorf("Direction = %q, want initiator", info.Direction)
	}
	if info.CallID != "call-123@host" {
		t.Errorf("CallID = %q", info.CallID)
	}
	if info.LocalURI != "sip:alice@example.com" {
		t.Errorf("LocalURI = %q", info.LocalURI)
	}
	if info.RemoteURI != "sip:bob@example.com" {
		t.Errorf("RemoteURI = %q", info.RemoteURI)
	}
	if info.LocalTag != "lt-1" {
		t.Errorf("LocalTag = %q", info.LocalTag)
	}
	if info.RemoteTag != "rt-1" {
		t.Errorf("RemoteTag = %q", info.RemoteTag)
	}
}

func TestBuildDialogInfoRoundTrip(t *testing.T) {
	m := NewDialogInfoModule()
	info := &DialogInfo{
		Entity:    "sip:carol@example.com",
		State:     "early",
		Direction: "recipient",
		LocalURI:  "sip:carol@example.com",
		RemoteURI: "sip:dave@example.com",
		CallID:    "call-456@host",
		LocalTag:  "lt-9",
		RemoteTag: "rt-9",
	}
	body := m.BuildDialogInfo(info)
	if !strings.Contains(body, `entity="sip:carol@example.com"`) {
		t.Errorf("body missing entity: %s", body)
	}
	if !strings.Contains(body, "<state>early</state>") {
		t.Errorf("body missing state: %s", body)
	}
	if !strings.Contains(body, `direction="recipient"`) {
		t.Errorf("body missing direction: %s", body)
	}
	if !strings.Contains(body, `call-id="call-456@host"`) {
		t.Errorf("body missing call-id: %s", body)
	}
	if !strings.Contains(body, "sip:carol@example.com") {
		t.Errorf("body missing local identity: %s", body)
	}
	if !strings.Contains(body, "sip:dave@example.com") {
		t.Errorf("body missing remote identity: %s", body)
	}
	// Round-trip parse.
	parsed, err := m.ParseDialogInfo(body)
	if err != nil {
		t.Fatalf("ParseDialogInfo(build) error: %v", err)
	}
	if parsed.State != "early" || parsed.Entity != "sip:carol@example.com" {
		t.Errorf("round-trip = %+v", parsed)
	}
	if parsed.CallID != "call-456@host" {
		t.Errorf("round-trip CallID = %q", parsed.CallID)
	}
	if parsed.LocalTag != "lt-9" || parsed.RemoteTag != "rt-9" {
		t.Errorf("round-trip tags = %q/%q", parsed.LocalTag, parsed.RemoteTag)
	}
}

func TestStatePriority(t *testing.T) {
	m := NewDialogInfoModule()
	// terminated < early < confirmed < proceeding
	if !(m.StatePriority("terminated") < m.StatePriority("early")) {
		t.Errorf("terminated should have lower priority than early")
	}
	if !(m.StatePriority("early") < m.StatePriority("confirmed")) {
		t.Errorf("early should have lower priority than confirmed")
	}
	if !(m.StatePriority("confirmed") < m.StatePriority("proceeding")) {
		t.Errorf("confirmed should have lower priority than proceeding")
	}
	// Unknown states have the lowest priority.
	if m.StatePriority("nonsense") > m.StatePriority("terminated") {
		t.Errorf("unknown state should not outrank terminated")
	}
}

func TestAggregateDialogsPicksHighest(t *testing.T) {
	m := NewDialogInfoModule()
	dialogs := []*DialogInfo{
		{Entity: "sip:x@example.com", State: "terminated", CallID: "c1"},
		{Entity: "sip:x@example.com", State: "confirmed", CallID: "c2"},
		{Entity: "sip:x@example.com", State: "early", CallID: "c3"},
	}
	agg := m.AggregateDialogs(dialogs)
	if agg == nil {
		t.Fatal("AggregateDialogs returned nil")
	}
	if agg.State != "confirmed" {
		t.Errorf("Aggregate State = %q, want confirmed (highest of the set)", agg.State)
	}
	// proceeding outranks confirmed.
	dialogs2 := []*DialogInfo{
		{Entity: "sip:y@example.com", State: "proceeding", CallID: "p1"},
		{Entity: "sip:y@example.com", State: "confirmed", CallID: "p2"},
	}
	agg2 := m.AggregateDialogs(dialogs2)
	if agg2.State != "proceeding" {
		t.Errorf("Aggregate State = %q, want proceeding", agg2.State)
	}
}

func TestAggregateDialogsEmpty(t *testing.T) {
	m := NewDialogInfoModule()
	if got := m.AggregateDialogs(nil); got != nil {
		t.Errorf("AggregateDialogs(nil) = %v, want nil", got)
	}
	if got := m.AggregateDialogs([]*DialogInfo{}); got != nil {
		t.Errorf("AggregateDialogs(empty) = %v, want nil", got)
	}
	// Single dialog returns a copy with that state.
	one := []*DialogInfo{{Entity: "sip:z@example.com", State: "terminated", CallID: "z1"}}
	agg := m.AggregateDialogs(one)
	if agg == nil || agg.State != "terminated" {
		t.Errorf("Aggregate single = %+v", agg)
	}
}

func TestDefaultDialogInfoAndInit(t *testing.T) {
	Init()
	a := DefaultDialogInfo()
	b := DefaultDialogInfo()
	if a != b {
		t.Error("DefaultDialogInfo should return the same instance")
	}
	Init()
	c := DefaultDialogInfo()
	if c == a {
		t.Error("Init should reset the default instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewDialogInfoModule()
	body := `<?xml version="1.0"?>
<dialog-info xmlns="urn:ietf:params:xml:ns:dialog-info" entity="sip:q@example.com">
  <dialog id="d" call-id="c" direction="initiator" local-tag="l" remote-tag="r">
    <state>confirmed</state>
    <local><identity>sip:q@example.com</identity></local>
    <remote><identity>sip:r@example.com</identity></remote>
  </dialog>
</dialog-info>`
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info, err := m.ParseDialogInfo(body)
			if err != nil || info == nil {
				t.Errorf("concurrent Parse: info=%v err=%v", info, err)
			}
			if info != nil {
				_ = m.BuildDialogInfo(info)
				_ = m.AggregateDialogs([]*DialogInfo{info})
				_ = m.StatePriority(info.State)
			}
		}()
	}
	wg.Wait()
}
