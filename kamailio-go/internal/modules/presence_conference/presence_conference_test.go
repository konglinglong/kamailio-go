// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the presence_conference module - conference state presence.
 */
package presence_conference

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

const confXML = `<?xml version="1.0" encoding="UTF-8"?>
<conference-info xmlns="urn:ietf:params:xml:ns:conference-info" entity="sips:conf233@example.com" state="full" version="1">
  <users>
    <user entity="sip:bob@example.com" state="full">
      <display-text>Bob Hoskins</display-text>
      <endpoint entity="sip:bob@pc33.example.com">
        <display-text>Bob's Laptop</display-text>
        <status>disconnected</status>
      </endpoint>
    </user>
    <user entity="sip:alice@example.com">
      <display-text>Alice</display-text>
      <endpoint entity="sip:alice@pc1.example.com">
        <status>connected</status>
      </endpoint>
    </user>
  </users>
</conference-info>`

func buildPublishMsg(body string) *parser.SIPMsg {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type: parser.MsgRequest,
		Req: &parser.RequestLine{
			Method:      str.Mk("PUBLISH"),
			URI:         str.Mk("sips:conf233@example.com"),
			MethodValue: parser.MethodPublish,
		},
	}
	msg.AddHeader("From", "<sips:conf233@example.com>;tag=t1")
	msg.AddHeader("Event", "conference")
	msg.AddHeader("Content-Type", "application/conference-info+xml")
	msg.Body = []byte(body)
	return msg
}

func buildSubscribeMsg(uri string) *parser.SIPMsg {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type: parser.MsgRequest,
		Req: &parser.RequestLine{
			Method:      str.Mk("SUBSCRIBE"),
			URI:         str.Mk(uri),
			MethodValue: parser.MethodSubscribe,
		},
	}
	msg.AddHeader("From", "<"+uri+">;tag=s1")
	msg.AddHeader("Event", "conference")
	msg.AddHeader("Expires", "3600")
	return msg
}

func TestParseConferenceInfo(t *testing.T) {
	m := New()
	info, err := m.ParseConferenceInfo([]byte(confXML))
	if err != nil {
		t.Fatalf("ParseConferenceInfo error: %v", err)
	}
	if info.ConferenceURI != "sips:conf233@example.com" {
		t.Errorf("ConferenceURI = %q", info.ConferenceURI)
	}
	if info.State != "full" {
		t.Errorf("State = %q, want full", info.State)
	}
	if len(info.Participants) != 2 {
		t.Fatalf("Participants len = %d, want 2", len(info.Participants))
	}
	bob := info.Participants[0]
	if bob.URI != "sip:bob@example.com" {
		t.Errorf("bob URI = %q", bob.URI)
	}
	if bob.Status != "disconnected" {
		t.Errorf("bob Status = %q", bob.Status)
	}
	if bob.DisplayText != "Bob Hoskins" {
		t.Errorf("bob DisplayText = %q", bob.DisplayText)
	}
	alice := info.Participants[1]
	if alice.Status != "connected" {
		t.Errorf("alice Status = %q", alice.Status)
	}
	if info.UserRoles["sip:bob@example.com"] != "full" {
		t.Errorf("UserRoles[bob] = %q", info.UserRoles["sip:bob@example.com"])
	}
}

func TestParseConferenceInfoErrors(t *testing.T) {
	m := New()
	if _, err := m.ParseConferenceInfo([]byte("")); err == nil {
		t.Errorf("empty body should error")
	}
	if _, err := m.ParseConferenceInfo([]byte("<presence/>")); err == nil {
		t.Errorf("non-conference body should error")
	}
	// nil module
	var nilm *PresenceConferenceModule
	if _, err := nilm.ParseConferenceInfo([]byte(confXML)); err == nil {
		t.Errorf("nil module should error")
	}
}

func TestBuildConferenceInfoRoundTrip(t *testing.T) {
	m := New()
	info := &ConferenceInfo{
		ConferenceURI: "sips:conf42@example.com",
		State:         "full",
		UserRoles:     map[string]string{"sip:carol@example.com": "active"},
		Participants: []Participant{
			{URI: "sip:carol@example.com", Status: "connected", DisplayText: "Carol"},
		},
	}
	out, err := m.BuildConferenceInfo(info)
	if err != nil {
		t.Fatalf("BuildConferenceInfo error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `entity="sips:conf42@example.com"`) {
		t.Errorf("body missing entity: %s", s)
	}
	if !strings.Contains(s, "<status>connected</status>") {
		t.Errorf("body missing status: %s", s)
	}
	if !strings.Contains(s, "<display-text>Carol</display-text>") {
		t.Errorf("body missing display-text: %s", s)
	}
	// Round-trip parse.
	parsed, err := m.ParseConferenceInfo(out)
	if err != nil {
		t.Fatalf("round-trip parse error: %v", err)
	}
	if parsed.ConferenceURI != "sips:conf42@example.com" {
		t.Errorf("round-trip ConferenceURI = %q", parsed.ConferenceURI)
	}
	if len(parsed.Participants) != 1 || parsed.Participants[0].URI != "sip:carol@example.com" {
		t.Errorf("round-trip participants = %+v", parsed.Participants)
	}
}

func TestBuildConferenceInfoNil(t *testing.T) {
	m := New()
	out, err := m.BuildConferenceInfo(nil)
	if err != nil {
		t.Fatalf("BuildConferenceInfo(nil) error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("nil info should yield empty body, got %q", out)
	}
}

func TestHandlePublish(t *testing.T) {
	m := New()
	msg := buildPublishMsg(confXML)
	code, err := m.HandlePublish(msg)
	if err != nil {
		t.Fatalf("HandlePublish error: %v", err)
	}
	if code != 200 {
		t.Errorf("HandlePublish code = %d, want 200", code)
	}
	info, err := m.GetConference("sips:conf233@example.com")
	if err != nil {
		t.Fatalf("GetConference error: %v", err)
	}
	if len(info.Participants) != 2 {
		t.Errorf("cached Participants len = %d, want 2", len(info.Participants))
	}
}

func TestHandlePublishErrors(t *testing.T) {
	m := New()
	// Empty body -> 415.
	msg := buildPublishMsg("")
	code, err := m.HandlePublish(msg)
	if code != 415 || err == nil {
		t.Errorf("empty body: code=%d err=%v, want 415", code, err)
	}
	// Malformed body -> 400.
	msg2 := buildPublishMsg("<not-conference/>")
	code2, err2 := m.HandlePublish(msg2)
	if code2 != 400 || err2 == nil {
		t.Errorf("malformed body: code=%d err=%v, want 400", code2, err2)
	}
	// nil message.
	code3, err3 := m.HandlePublish(nil)
	if code3 != 400 || err3 == nil {
		t.Errorf("nil msg: code=%d err=%v, want 400", code3, err3)
	}
}

func TestHandleSubscribe(t *testing.T) {
	m := New()
	msg := buildSubscribeMsg("sips:conf233@example.com")
	code, err := m.HandleSubscribe(msg)
	if err != nil {
		t.Fatalf("HandleSubscribe error: %v", err)
	}
	if code != 200 {
		t.Errorf("HandleSubscribe code = %d, want 200", code)
	}
	// nil message -> 400.
	code2, err2 := m.HandleSubscribe(nil)
	if code2 != 400 || err2 == nil {
		t.Errorf("nil msg: code=%d err=%v, want 400", code2, err2)
	}
	// Message without From or R-URI -> 400.
	empty := &parser.SIPMsg{}
	code3, err3 := m.HandleSubscribe(empty)
	if code3 != 400 || err3 == nil {
		t.Errorf("empty msg: code=%d err=%v, want 400", code3, err3)
	}
}

func TestAddRemoveParticipant(t *testing.T) {
	m := New()
	p := &Participant{URI: "sip:dave@example.com", Status: "connected", DisplayText: "Dave"}
	if err := m.AddParticipant("sips:conf1@example.com", p); err != nil {
		t.Fatalf("AddParticipant error: %v", err)
	}
	info, err := m.GetConference("sips:conf1@example.com")
	if err != nil {
		t.Fatalf("GetConference error: %v", err)
	}
	if len(info.Participants) != 1 || info.Participants[0].URI != "sip:dave@example.com" {
		t.Errorf("participants = %+v", info.Participants)
	}
	// Add another.
	if err := m.AddParticipant("sips:conf1@example.com", &Participant{URI: "sip:eve@example.com"}); err != nil {
		t.Fatalf("AddParticipant 2 error: %v", err)
	}
	info2, _ := m.GetConference("sips:conf1@example.com")
	if len(info2.Participants) != 2 {
		t.Errorf("participants len = %d, want 2", len(info2.Participants))
	}
	// Remove.
	if err := m.RemoveParticipant("sips:conf1@example.com", "sip:dave@example.com"); err != nil {
		t.Fatalf("RemoveParticipant error: %v", err)
	}
	info3, _ := m.GetConference("sips:conf1@example.com")
	if len(info3.Participants) != 1 || info3.Participants[0].URI != "sip:eve@example.com" {
		t.Errorf("after remove participants = %+v", info3.Participants)
	}
	// Remove missing participant.
	if err := m.RemoveParticipant("sips:conf1@example.com", "sip:missing@example.com"); err == nil {
		t.Errorf("removing missing participant should error")
	}
	// Remove from missing conference.
	if err := m.RemoveParticipant("sips:nope@example.com", "sip:x@example.com"); err == nil {
		t.Errorf("removing from missing conference should error")
	}
}

func TestAddParticipantErrors(t *testing.T) {
	m := New()
	if err := m.AddParticipant("", &Participant{}); err == nil {
		t.Errorf("empty confURI should error")
	}
	if err := m.AddParticipant("sips:c@example.com", nil); err == nil {
		t.Errorf("nil participant should error")
	}
}

func TestGetConferenceMissing(t *testing.T) {
	m := New()
	if _, err := m.GetConference("sips:missing@example.com"); err == nil {
		t.Errorf("GetConference on missing should error")
	}
}

func TestListConferences(t *testing.T) {
	m := New()
	_ = m.AddParticipant("sips:z@example.com", &Participant{URI: "sip:a@example.com"})
	_ = m.AddParticipant("sips:a@example.com", &Participant{URI: "sip:b@example.com"})
	_ = m.AddParticipant("sips:m@example.com", &Participant{URI: "sip:c@example.com"})
	list := m.ListConferences()
	if len(list) != 3 {
		t.Fatalf("ListConferences len = %d, want 3", len(list))
	}
	if list[0].ConferenceURI != "sips:a@example.com" {
		t.Errorf("ListConferences not sorted: %s", list[0].ConferenceURI)
	}
	if m.Count() != 3 {
		t.Errorf("Count = %d, want 3", m.Count())
	}
}

func TestDefaultPresenceAndInit(t *testing.T) {
	Init()
	a := DefaultPresence()
	b := DefaultPresence()
	if a != b {
		t.Error("DefaultPresence should return the same instance")
	}
	_ = a.AddParticipant("sips:tmp@example.com", &Participant{URI: "sip:x@example.com"})
	if a.Count() != 1 {
		t.Errorf("Count = %d, want 1", a.Count())
	}
	Init()
	c := DefaultPresence()
	if c == a {
		t.Error("Init should reset the default instance")
	}
	if c.Count() != 0 {
		t.Errorf("Count after Init = %d, want 0", c.Count())
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			uri := "sips:conf" + itoa(n) + "@example.com"
			_ = m.AddParticipant(uri, &Participant{URI: "sip:user@example.com"})
			_, _ = m.ParseConferenceInfo([]byte(confXML))
			out, _ := m.BuildConferenceInfo(&ConferenceInfo{ConferenceURI: uri})
			_, _ = m.ParseConferenceInfo(out)
			_, _ = m.GetConference(uri)
			_ = m.ListConferences()
			_ = m.Count()
		}(i)
	}
	wg.Wait()
}

// itoa is a tiny dependency-free int->string.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
