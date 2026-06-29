// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the presence_reginfo module - RFC 3680 registration event.
 */
package presence_reginfo

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

const reginfoXML = `<?xml version="1.0" encoding="UTF-8"?>
<reginfo xmlns="urn:ietf:params:xml:ns:reginfo" version="1" state="full">
  <registration aor="sip:alice@example.com" id="reg1" state="active">
    <contact id="c1" state="registered" event="registered" expires="3600">
      <uri>sip:alice@192.0.2.1</uri>
      <timestamp>2026-06-25T10:00:00Z</timestamp>
    </contact>
    <contact id="c2" state="registered" event="created" expires="7200">
      <uri>sip:alice@192.0.2.2</uri>
    </contact>
  </registration>
</reginfo>`

const reginfoEmptyXML = `<?xml version="1.0" encoding="UTF-8"?>
<reginfo xmlns="urn:ietf:params:xml:ns:reginfo" version="0" state="full">
</reginfo>`

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
	msg.AddHeader("Event", "reg")
	msg.AddHeader("Content-Type", "application/reginfo+xml")
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
	msg.AddHeader("Event", "reg")
	return msg
}

func TestParseRegInfo(t *testing.T) {
	m := New()
	info, err := m.ParseRegInfo([]byte(reginfoXML))
	if err != nil {
		t.Fatalf("ParseRegInfo error: %v", err)
	}
	if info.AOR != "sip:alice@example.com" {
		t.Errorf("AOR = %q, want sip:alice@example.com", info.AOR)
	}
	if len(info.Contacts) != 2 {
		t.Fatalf("Contacts len = %d, want 2", len(info.Contacts))
	}
	c1 := info.Contacts[0]
	if c1.URI != "sip:alice@192.0.2.1" {
		t.Errorf("Contacts[0].URI = %q", c1.URI)
	}
	if c1.State != "registered" {
		t.Errorf("Contacts[0].State = %q, want registered", c1.State)
	}
	if c1.Event != "registered" {
		t.Errorf("Contacts[0].Event = %q, want registered", c1.Event)
	}
	if c1.Expires != 3600 {
		t.Errorf("Contacts[0].Expires = %d, want 3600", c1.Expires)
	}
	if c1.Timestamp != "2026-06-25T10:00:00Z" {
		t.Errorf("Contacts[0].Timestamp = %q", c1.Timestamp)
	}
	c2 := info.Contacts[1]
	if c2.Event != "created" {
		t.Errorf("Contacts[1].Event = %q, want created", c2.Event)
	}
	if c2.Expires != 7200 {
		t.Errorf("Contacts[1].Expires = %d, want 7200", c2.Expires)
	}
}

func TestParseRegInfoEmpty(t *testing.T) {
	m := New()
	info, err := m.ParseRegInfo([]byte(reginfoEmptyXML))
	if err != nil {
		t.Fatalf("ParseRegInfo error: %v", err)
	}
	if info.AOR != "" {
		t.Errorf("AOR = %q, want empty", info.AOR)
	}
	if len(info.Contacts) != 0 {
		t.Errorf("Contacts len = %d, want 0", len(info.Contacts))
	}
}

func TestParseRegInfoErrors(t *testing.T) {
	m := New()
	if _, err := m.ParseRegInfo([]byte("")); err == nil {
		t.Errorf("empty body should error")
	}
	if _, err := m.ParseRegInfo([]byte("<not-reginfo/>")); err == nil {
		t.Errorf("non-reginfo body should error")
	}
	var nilm *PresenceReginfoModule
	if _, err := nilm.ParseRegInfo([]byte(reginfoXML)); err == nil {
		t.Errorf("nil module should error")
	}
}

func TestBuildRegInfoRoundTrip(t *testing.T) {
	m := New()
	info := &RegInfo{
		AOR: "sip:carol@example.com",
		Contacts: []RegContact{
			{
				URI:       "sip:carol@192.0.2.3",
				State:     "registered",
				Event:     "registered",
				Expires:   3600,
				Timestamp: "2026-06-25T11:00:00Z",
			},
		},
	}
	out, err := m.BuildRegInfo(info)
	if err != nil {
		t.Fatalf("BuildRegInfo error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `aor="sip:carol@example.com"`) {
		t.Errorf("body missing aor: %s", s)
	}
	if !strings.Contains(s, "<uri>sip:carol@192.0.2.3</uri>") {
		t.Errorf("body missing uri: %s", s)
	}
	if !strings.Contains(s, `expires="3600"`) {
		t.Errorf("body missing expires: %s", s)
	}
	// Round-trip parse.
	parsed, err := m.ParseRegInfo(out)
	if err != nil {
		t.Fatalf("round-trip parse error: %v", err)
	}
	if parsed.AOR != "sip:carol@example.com" {
		t.Errorf("round-trip AOR = %q", parsed.AOR)
	}
	if len(parsed.Contacts) != 1 {
		t.Fatalf("round-trip Contacts len = %d, want 1", len(parsed.Contacts))
	}
	if parsed.Contacts[0].URI != "sip:carol@192.0.2.3" {
		t.Errorf("round-trip URI = %q", parsed.Contacts[0].URI)
	}
	if parsed.Contacts[0].Expires != 3600 {
		t.Errorf("round-trip Expires = %d, want 3600", parsed.Contacts[0].Expires)
	}
}

func TestBuildRegInfoNil(t *testing.T) {
	m := New()
	out, err := m.BuildRegInfo(nil)
	if err != nil {
		t.Fatalf("BuildRegInfo(nil) error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("nil info should yield empty body, got %q", out)
	}
	var nilm *PresenceReginfoModule
	if _, err := nilm.BuildRegInfo(&RegInfo{}); err == nil {
		t.Errorf("nil module should error")
	}
}

func TestHandlePublish(t *testing.T) {
	m := New()
	msg := buildPublishMsg("sip:alice@example.com", reginfoXML)
	code, err := m.HandlePublish(msg)
	if err != nil {
		t.Fatalf("HandlePublish error: %v", err)
	}
	if code != 200 {
		t.Errorf("HandlePublish code = %d, want 200", code)
	}
	info, err := m.GetRegInfo("sip:alice@example.com")
	if err != nil {
		t.Fatalf("GetRegInfo error: %v", err)
	}
	if len(info.Contacts) != 2 {
		t.Errorf("cached Contacts len = %d, want 2", len(info.Contacts))
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
	code2, err2 := m.HandlePublish(buildPublishMsg("sip:alice@example.com", "<not-reginfo/>"))
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
	msg := buildSubscribeMsg("sip:alice@example.com")
	code, err := m.HandleSubscribe(msg)
	if err != nil {
		t.Fatalf("HandleSubscribe error: %v", err)
	}
	if code != 200 {
		t.Errorf("HandleSubscribe code = %d, want 200", code)
	}
}

func TestHandleSubscribeErrors(t *testing.T) {
	m := New()
	// nil message.
	code, err := m.HandleSubscribe(nil)
	if code != 400 || err == nil {
		t.Errorf("nil msg: code=%d err=%v, want 400", code, err)
	}
	// Message with no entity (no From, no request URI).
	msg := &parser.SIPMsg{}
	code2, err2 := m.HandleSubscribe(msg)
	if code2 != 400 || err2 == nil {
		t.Errorf("no entity: code=%d err=%v, want 400", code2, err2)
	}
}

func TestAddRemoveContact(t *testing.T) {
	m := New()
	contact := &RegContact{
		URI:     "sip:bob@192.0.2.4",
		State:   "registered",
		Event:   "created",
		Expires: 3600,
	}
	if err := m.AddContact("sip:bob@example.com", contact); err != nil {
		t.Fatalf("AddContact error: %v", err)
	}
	info, err := m.GetRegInfo("sip:bob@example.com")
	if err != nil {
		t.Fatalf("GetRegInfo error: %v", err)
	}
	if len(info.Contacts) != 1 {
		t.Fatalf("Contacts len = %d, want 1", len(info.Contacts))
	}
	if info.Contacts[0].URI != "sip:bob@192.0.2.4" {
		t.Errorf("URI = %q", info.Contacts[0].URI)
	}
	// Add a second contact.
	if err := m.AddContact("sip:bob@example.com", &RegContact{URI: "sip:bob@192.0.2.5"}); err != nil {
		t.Fatalf("AddContact 2 error: %v", err)
	}
	info2, _ := m.GetRegInfo("sip:bob@example.com")
	if len(info2.Contacts) != 2 {
		t.Errorf("Contacts len = %d, want 2", len(info2.Contacts))
	}
	// Remove the first contact.
	if err := m.RemoveContact("sip:bob@example.com", "sip:bob@192.0.2.4"); err != nil {
		t.Fatalf("RemoveContact error: %v", err)
	}
	info3, _ := m.GetRegInfo("sip:bob@example.com")
	if len(info3.Contacts) != 1 {
		t.Errorf("after remove Contacts len = %d, want 1", len(info3.Contacts))
	}
	if info3.Contacts[0].URI != "sip:bob@192.0.2.5" {
		t.Errorf("remaining URI = %q", info3.Contacts[0].URI)
	}
}

func TestAddContactErrors(t *testing.T) {
	m := New()
	if err := m.AddContact("", &RegContact{URI: "sip:x"}); err == nil {
		t.Errorf("empty AOR should error")
	}
	if err := m.AddContact("sip:x@example.com", nil); err == nil {
		t.Errorf("nil contact should error")
	}
}

func TestRemoveContactErrors(t *testing.T) {
	m := New()
	// Missing AOR.
	if err := m.RemoveContact("sip:missing@example.com", "sip:x"); err == nil {
		t.Errorf("missing AOR should error")
	}
	// Existing AOR, missing contact.
	_ = m.AddContact("sip:bob@example.com", &RegContact{URI: "sip:bob@1"})
	if err := m.RemoveContact("sip:bob@example.com", "sip:missing"); err == nil {
		t.Errorf("missing contact should error")
	}
}

func TestUpdateContactState(t *testing.T) {
	m := New()
	_ = m.AddContact("sip:eve@example.com", &RegContact{URI: "sip:eve@1", State: "registered"})
	if err := m.UpdateContactState("sip:eve@example.com", "sip:eve@1", "unregistered"); err != nil {
		t.Fatalf("UpdateContactState error: %v", err)
	}
	info, _ := m.GetRegInfo("sip:eve@example.com")
	if info.Contacts[0].State != "unregistered" {
		t.Errorf("State = %q, want unregistered", info.Contacts[0].State)
	}
}

func TestUpdateContactStateErrors(t *testing.T) {
	m := New()
	if err := m.UpdateContactState("sip:missing@example.com", "sip:x", "registered"); err == nil {
		t.Errorf("missing AOR should error")
	}
	_ = m.AddContact("sip:eve@example.com", &RegContact{URI: "sip:eve@1"})
	if err := m.UpdateContactState("sip:eve@example.com", "sip:missing", "registered"); err == nil {
		t.Errorf("missing contact should error")
	}
}

func TestGetRegInfoMissing(t *testing.T) {
	m := New()
	if _, err := m.GetRegInfo("sip:missing@example.com"); err == nil {
		t.Errorf("GetRegInfo on missing should error")
	}
	if m.Count() != 0 {
		t.Errorf("Count = %d, want 0", m.Count())
	}
}

func TestListAORs(t *testing.T) {
	m := New()
	_ = m.AddContact("sip:charlie@example.com", &RegContact{URI: "sip:charlie@1"})
	_ = m.AddContact("sip:alice@example.com", &RegContact{URI: "sip:alice@1"})
	_ = m.AddContact("sip:bob@example.com", &RegContact{URI: "sip:bob@1"})
	aors := m.ListAORs()
	if len(aors) != 3 {
		t.Fatalf("ListAORs len = %d, want 3", len(aors))
	}
	if aors[0] != "sip:alice@example.com" {
		t.Errorf("ListAORs[0] = %q, want sip:alice@example.com", aors[0])
	}
	if aors[1] != "sip:bob@example.com" {
		t.Errorf("ListAORs[1] = %q, want sip:bob@example.com", aors[1])
	}
	if aors[2] != "sip:charlie@example.com" {
		t.Errorf("ListAORs[2] = %q, want sip:charlie@example.com", aors[2])
	}
}

func TestDefaultReginfoAndInit(t *testing.T) {
	Init()
	a := DefaultReginfo()
	b := DefaultReginfo()
	if a != b {
		t.Error("DefaultReginfo should return the same instance")
	}
	_ = a.AddContact("sip:tmp@example.com", &RegContact{URI: "sip:tmp@1"})
	if a.Count() != 1 {
		t.Errorf("Count = %d, want 1", a.Count())
	}
	Init()
	c := DefaultReginfo()
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
			aor := "sip:user" + itoa(n) + "@example.com"
			_, _ = m.ParseRegInfo([]byte(reginfoXML))
			out, _ := m.BuildRegInfo(&RegInfo{AOR: aor, Contacts: []RegContact{{URI: "sip:user@" + itoa(n)}}})
			_, _ = m.ParseRegInfo(out)
			_ = m.AddContact(aor, &RegContact{URI: "sip:contact@" + itoa(n), State: "registered"})
			_ = m.UpdateContactState(aor, "sip:contact@"+itoa(n), "unregistered")
			_, _ = m.GetRegInfo(aor)
			_ = m.RemoveContact(aor, "sip:contact@"+itoa(n))
			_ = m.ListAORs()
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
