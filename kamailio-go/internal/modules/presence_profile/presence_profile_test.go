// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the presence_profile module - RPID rich presence.
 */
package presence_profile

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

const rpidXML = `<?xml version="1.0" encoding="UTF-8"?>
<presence xmlns="urn:ietf:params:xml:ns:pidf" xmlns:rpid="urn:ietf:params:xml:ns:pidf:rpid" entity="sip:alice@example.com">
  <tuple id="t1">
    <status><basic>open</basic></status>
    <rpid:activities>
      <rpid:away/>
      <rpid:on-the-phone/>
    </rpid:activities>
    <rpid:mood>
      <rpid:happy/>
      <rpid:text>Feeling great</rpid:text>
    </rpid:mood>
    <rpid:sphere>work</rpid:sphere>
    <rpid:icon>http://example.com/alice.png</rpid:icon>
    <timestamp>2026-06-25T10:00:00Z</timestamp>
  </tuple>
</presence>`

func buildPublishMsg(uri, body string) *parser.SIPMsg {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type: parser.MsgRequest,
		Req: &parser.RequestLine{
			Method:      str.Mk("PUBLISH"),
			URI:         str.Mk(uri),
			MethodValue: parser.MethodPublish,
		},
	}
	msg.AddHeader("From", "<"+uri+">;tag=t1")
	msg.AddHeader("Event", "presence")
	msg.AddHeader("Content-Type", "application/pidf+xml")
	msg.Body = []byte(body)
	return msg
}

func TestParseRPID(t *testing.T) {
	m := New()
	info, err := m.ParseRPID([]byte(rpidXML))
	if err != nil {
		t.Fatalf("ParseRPID error: %v", err)
	}
	if info.UserURI != "sip:alice@example.com" {
		t.Errorf("UserURI = %q", info.UserURI)
	}
	if len(info.Activities) != 2 {
		t.Fatalf("Activities len = %d, want 2", len(info.Activities))
	}
	if info.Activities[0].Type != "away" {
		t.Errorf("Activities[0].Type = %q, want away", info.Activities[0].Type)
	}
	if info.Activities[1].Type != "on-the-phone" {
		t.Errorf("Activities[1].Type = %q, want on-the-phone", info.Activities[1].Type)
	}
	if len(info.Mood.Moods) != 1 || info.Mood.Moods[0] != "happy" {
		t.Errorf("Mood.Moods = %v, want [happy]", info.Mood.Moods)
	}
	if info.Mood.Text != "Feeling great" {
		t.Errorf("Mood.Text = %q", info.Mood.Text)
	}
	if info.Sphere != "work" {
		t.Errorf("Sphere = %q, want work", info.Sphere)
	}
	if info.Icon != "http://example.com/alice.png" {
		t.Errorf("Icon = %q", info.Icon)
	}
	if info.Timestamp != "2026-06-25T10:00:00Z" {
		t.Errorf("Timestamp = %q", info.Timestamp)
	}
}

func TestParseRPIDActivityWithDescription(t *testing.T) {
	m := New()
	body := `<?xml version="1.0"?>
<presence xmlns="urn:ietf:params:xml:ns:pidf" xmlns:rpid="urn:ietf:params:xml:ns:pidf:rpid" entity="sip:bob@example.com">
  <tuple id="t1">
    <rpid:activities>
      <rpid:busy>In a meeting</rpid:busy>
    </rpid:activities>
  </tuple>
</presence>`
	info, err := m.ParseRPID([]byte(body))
	if err != nil {
		t.Fatalf("ParseRPID error: %v", err)
	}
	if len(info.Activities) != 1 {
		t.Fatalf("Activities len = %d, want 1", len(info.Activities))
	}
	if info.Activities[0].Type != "busy" {
		t.Errorf("Type = %q, want busy", info.Activities[0].Type)
	}
	if info.Activities[0].Description != "In a meeting" {
		t.Errorf("Description = %q, want 'In a meeting'", info.Activities[0].Description)
	}
}

func TestParseRPIDErrors(t *testing.T) {
	m := New()
	if _, err := m.ParseRPID([]byte("")); err == nil {
		t.Errorf("empty body should error")
	}
	if _, err := m.ParseRPID([]byte("<not-presence/>")); err == nil {
		t.Errorf("non-presence body should error")
	}
	var nilm *PresenceProfileModule
	if _, err := nilm.ParseRPID([]byte(rpidXML)); err == nil {
		t.Errorf("nil module should error")
	}
}

func TestBuildRPIDRoundTrip(t *testing.T) {
	m := New()
	info := &ProfileInfo{
		UserURI:    "sip:carol@example.com",
		Activities: []Activity{{Type: "away"}, {Type: "on-the-phone"}},
		Mood:       MoodInfo{Moods: []string{"sad"}, Text: "Tired"},
		Sphere:     "home",
		Icon:       "http://example.com/c.png",
		Timestamp:  "2026-06-25T11:00:00Z",
	}
	out, err := m.BuildRPID(info)
	if err != nil {
		t.Fatalf("BuildRPID error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `entity="sip:carol@example.com"`) {
		t.Errorf("body missing entity: %s", s)
	}
	if !strings.Contains(s, "<rpid:away/>") {
		t.Errorf("body missing away activity: %s", s)
	}
	if !strings.Contains(s, "<rpid:sad/>") {
		t.Errorf("body missing sad mood: %s", s)
	}
	if !strings.Contains(s, "<rpid:sphere>home</rpid:sphere>") {
		t.Errorf("body missing sphere: %s", s)
	}
	// Round-trip parse.
	parsed, err := m.ParseRPID(out)
	if err != nil {
		t.Fatalf("round-trip parse error: %v", err)
	}
	if parsed.UserURI != "sip:carol@example.com" {
		t.Errorf("round-trip UserURI = %q", parsed.UserURI)
	}
	if len(parsed.Activities) != 2 {
		t.Errorf("round-trip Activities len = %d, want 2", len(parsed.Activities))
	}
	if parsed.Sphere != "home" {
		t.Errorf("round-trip Sphere = %q", parsed.Sphere)
	}
}

func TestBuildRPIDNil(t *testing.T) {
	m := New()
	out, err := m.BuildRPID(nil)
	if err != nil {
		t.Fatalf("BuildRPID(nil) error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("nil info should yield empty body, got %q", out)
	}
}

func TestHandlePublish(t *testing.T) {
	m := New()
	msg := buildPublishMsg("sip:alice@example.com", rpidXML)
	code, err := m.HandlePublish(msg)
	if err != nil {
		t.Fatalf("HandlePublish error: %v", err)
	}
	if code != 200 {
		t.Errorf("HandlePublish code = %d, want 200", code)
	}
	info, err := m.GetProfile("sip:alice@example.com")
	if err != nil {
		t.Fatalf("GetProfile error: %v", err)
	}
	if len(info.Activities) != 2 {
		t.Errorf("cached Activities len = %d, want 2", len(info.Activities))
	}
}

func TestHandlePublishErrors(t *testing.T) {
	m := New()
	// Empty body -> 415.
	code, err := m.HandlePublish(buildPublishMsg("sip:alice@example.com", ""))
	if code != 415 || err == nil {
		t.Errorf("empty body: code=%d err=%v, want 415", code, err)
	}
	// Malformed body -> 400.
	code2, err2 := m.HandlePublish(buildPublishMsg("sip:alice@example.com", "<not-presence/>"))
	if code2 != 400 || err2 == nil {
		t.Errorf("malformed body: code=%d err=%v, want 400", code2, err2)
	}
	// nil message.
	code3, err3 := m.HandlePublish(nil)
	if code3 != 400 || err3 == nil {
		t.Errorf("nil msg: code=%d err=%v, want 400", code3, err3)
	}
}

func TestSetActivity(t *testing.T) {
	m := New()
	if err := m.SetActivity("sip:bob@example.com", &Activity{Type: "busy", Description: "In a call"}); err != nil {
		t.Fatalf("SetActivity error: %v", err)
	}
	info, err := m.GetProfile("sip:bob@example.com")
	if err != nil {
		t.Fatalf("GetProfile error: %v", err)
	}
	if len(info.Activities) != 1 || info.Activities[0].Type != "busy" {
		t.Errorf("activities = %+v", info.Activities)
	}
	// SetActivity replaces existing activities.
	if err := m.SetActivity("sip:bob@example.com", &Activity{Type: "away"}); err != nil {
		t.Fatalf("SetActivity 2 error: %v", err)
	}
	info2, _ := m.GetProfile("sip:bob@example.com")
	if len(info2.Activities) != 1 || info2.Activities[0].Type != "away" {
		t.Errorf("after reset activities = %+v", info2.Activities)
	}
}

func TestSetMood(t *testing.T) {
	m := New()
	mood := &MoodInfo{Moods: []string{"happy", "excited"}, Text: "Great day"}
	if err := m.SetMood("sip:eve@example.com", mood); err != nil {
		t.Fatalf("SetMood error: %v", err)
	}
	info, err := m.GetProfile("sip:eve@example.com")
	if err != nil {
		t.Fatalf("GetProfile error: %v", err)
	}
	if len(info.Mood.Moods) != 2 {
		t.Errorf("Moods len = %d, want 2", len(info.Mood.Moods))
	}
	if info.Mood.Text != "Great day" {
		t.Errorf("Mood.Text = %q", info.Mood.Text)
	}
}

func TestSetSphere(t *testing.T) {
	m := New()
	if err := m.SetSphere("sip:dave@example.com", "private"); err != nil {
		t.Fatalf("SetSphere error: %v", err)
	}
	info, err := m.GetProfile("sip:dave@example.com")
	if err != nil {
		t.Fatalf("GetProfile error: %v", err)
	}
	if info.Sphere != "private" {
		t.Errorf("Sphere = %q, want private", info.Sphere)
	}
}

func TestSetErrors(t *testing.T) {
	m := New()
	if err := m.SetActivity("", &Activity{Type: "away"}); err == nil {
		t.Errorf("empty userURI should error")
	}
	if err := m.SetActivity("sip:bob@example.com", nil); err == nil {
		t.Errorf("nil activity should error")
	}
	if err := m.SetMood("", &MoodInfo{}); err == nil {
		t.Errorf("empty userURI SetMood should error")
	}
	if err := m.SetMood("sip:bob@example.com", nil); err == nil {
		t.Errorf("nil mood should error")
	}
	if err := m.SetSphere("", "work"); err == nil {
		t.Errorf("empty userURI SetSphere should error")
	}
}

func TestClearProfile(t *testing.T) {
	m := New()
	_ = m.SetSphere("sip:tmp@example.com", "work")
	if err := m.ClearProfile("sip:tmp@example.com"); err != nil {
		t.Fatalf("ClearProfile error: %v", err)
	}
	if _, err := m.GetProfile("sip:tmp@example.com"); err == nil {
		t.Errorf("GetProfile after clear should error")
	}
	if err := m.ClearProfile("sip:missing@example.com"); err == nil {
		t.Errorf("ClearProfile on missing should error")
	}
}

func TestGetProfileMissing(t *testing.T) {
	m := New()
	if _, err := m.GetProfile("sip:missing@example.com"); err == nil {
		t.Errorf("GetProfile on missing should error")
	}
	if m.Count() != 0 {
		t.Errorf("Count = %d, want 0", m.Count())
	}
}

func TestDefaultProfileAndInit(t *testing.T) {
	Init()
	a := DefaultProfile()
	b := DefaultProfile()
	if a != b {
		t.Error("DefaultProfile should return the same instance")
	}
	_ = a.SetSphere("sip:tmp@example.com", "work")
	if a.Count() != 1 {
		t.Errorf("Count = %d, want 1", a.Count())
	}
	Init()
	c := DefaultProfile()
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
			uri := "sip:user" + itoa(n) + "@example.com"
			_, _ = m.ParseRPID([]byte(rpidXML))
			out, _ := m.BuildRPID(&ProfileInfo{UserURI: uri, Sphere: "work"})
			_, _ = m.ParseRPID(out)
			_ = m.SetActivity(uri, &Activity{Type: "away"})
			_ = m.SetMood(uri, &MoodInfo{Moods: []string{"happy"}})
			_ = m.SetSphere(uri, "home")
			_, _ = m.GetProfile(uri)
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
