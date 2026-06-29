// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the presence_dfks module - DFKS feature key supervision.
 */
package presence_dfks

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

const dndXML = `<?xml version="1.0" encoding="ISO-8859-1"?>
<SetDoNotDisturb xmlns="http://www.ecma-international.org/standards/ecma-323/csta/ed3">
  <device>sip:bob@example.com</device>
  <doNotDisturbOn>true</doNotDisturbOn>
</SetDoNotDisturb>`

const fwdXML = `<?xml version="1.0" encoding="ISO-8859-1"?>
<SetForwarding xmlns="http://www.ecma-international.org/standards/ecma-323/csta/ed3">
  <device>sip:alice@example.com</device>
  <forwardDN>sip:carol@example.com</forwardDN>
  <forwardingType>forwardImmediate</forwardingType>
  <activateForward>true</activateForward>
</SetForwarding>`

const dndEventXML = `<?xml version="1.0" encoding="ISO-8859-1"?>
<DoNotDisturbEvent xmlns="http://www.ecma-international.org/standards/ecma-323/csta/ed3">
  <device>sip:bob@example.com</device>
  <doNotDisturbOn>false</doNotDisturbOn>
</DoNotDisturbEvent>`

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
	msg.AddHeader("Event", "as-feature-event")
	msg.AddHeader("Content-Type", "application/x-as-feature-event+xml")
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
	msg.AddHeader("Event", "as-feature-event")
	return msg
}

func TestParseDFKSDND(t *testing.T) {
	m := New()
	info, err := m.ParseDFKS([]byte(dndXML))
	if err != nil {
		t.Fatalf("ParseDFKS error: %v", err)
	}
	if info.FeatureKeys["DoNotDisturb"] != "true" {
		t.Errorf("DoNotDisturb = %q, want true", info.FeatureKeys["DoNotDisturb"])
	}
	if info.Status != "true" {
		t.Errorf("Status = %q, want true", info.Status)
	}
}

func TestParseDFKSForwarding(t *testing.T) {
	m := New()
	info, err := m.ParseDFKS([]byte(fwdXML))
	if err != nil {
		t.Fatalf("ParseDFKS error: %v", err)
	}
	fwd := info.FeatureKeys["Forwarding"]
	if !strings.Contains(fwd, "forwardImmediate") {
		t.Errorf("Forwarding = %q, want forwardImmediate", fwd)
	}
	if !strings.Contains(fwd, "sip:carol@example.com") {
		t.Errorf("Forwarding = %q, want forwardTo carol", fwd)
	}
}

func TestParseDFKSEvent(t *testing.T) {
	m := New()
	info, err := m.ParseDFKS([]byte(dndEventXML))
	if err != nil {
		t.Fatalf("ParseDFKS error: %v", err)
	}
	if info.FeatureKeys["DoNotDisturb"] != "false" {
		t.Errorf("DoNotDisturb = %q, want false", info.FeatureKeys["DoNotDisturb"])
	}
}

func TestParseDFKSErrors(t *testing.T) {
	m := New()
	if _, err := m.ParseDFKS([]byte("")); err == nil {
		t.Errorf("empty body should error")
	}
	if _, err := m.ParseDFKS([]byte("<presence/>")); err == nil {
		t.Errorf("non-DFKS body should error")
	}
	var nilm *PresenceDFKSModule
	if _, err := nilm.ParseDFKS([]byte(dndXML)); err == nil {
		t.Errorf("nil module should error")
	}
}

func TestBuildDFKSRoundTrip(t *testing.T) {
	m := New()
	info := &DFKSInfo{
		UserURI:     "sip:bob@example.com",
		FeatureKeys: map[string]string{"DoNotDisturb": "true"},
	}
	out, err := m.BuildDFKS(info)
	if err != nil {
		t.Fatalf("BuildDFKS error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "<doNotDisturbOn>true</doNotDisturbOn>") {
		t.Errorf("body missing doNotDisturbOn: %s", s)
	}
	if !strings.Contains(s, "<device>sip:bob@example.com</device>") {
		t.Errorf("body missing device: %s", s)
	}
	// Round-trip parse.
	parsed, err := m.ParseDFKS(out)
	if err != nil {
		t.Fatalf("round-trip parse error: %v", err)
	}
	if parsed.FeatureKeys["DoNotDisturb"] != "true" {
		t.Errorf("round-trip DoNotDisturb = %q", parsed.FeatureKeys["DoNotDisturb"])
	}
}

func TestBuildDFKSForwarding(t *testing.T) {
	m := New()
	info := &DFKSInfo{
		UserURI:     "sip:alice@example.com",
		FeatureKeys: map[string]string{"Forwarding": "forwardImmediate->sip:carol@example.com"},
	}
	out, err := m.BuildDFKS(info)
	if err != nil {
		t.Fatalf("BuildDFKS error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "<forwardingType>forwardImmediate</forwardingType>") {
		t.Errorf("body missing forwardingType: %s", s)
	}
	if !strings.Contains(s, "<forwardTo>sip:carol@example.com</forwardTo>") {
		t.Errorf("body missing forwardTo: %s", s)
	}
}

func TestBuildDFKSNil(t *testing.T) {
	m := New()
	out, err := m.BuildDFKS(nil)
	if err != nil {
		t.Fatalf("BuildDFKS(nil) error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("nil info should yield empty body, got %q", out)
	}
}

func TestHandlePublish(t *testing.T) {
	m := New()
	msg := buildPublishMsg("sip:bob@example.com", dndXML)
	code, err := m.HandlePublish(msg)
	if err != nil {
		t.Fatalf("HandlePublish error: %v", err)
	}
	if code != 200 {
		t.Errorf("HandlePublish code = %d, want 200", code)
	}
	keys, err := m.GetFeatureKeys("sip:bob@example.com")
	if err != nil {
		t.Fatalf("GetFeatureKeys error: %v", err)
	}
	if len(keys) != 1 || keys[0].Name != "DoNotDisturb" {
		t.Errorf("feature keys = %+v", keys)
	}
}

func TestHandlePublishErrors(t *testing.T) {
	m := New()
	// Empty body -> 415.
	code, err := m.HandlePublish(buildPublishMsg("sip:bob@example.com", ""))
	if code != 415 || err == nil {
		t.Errorf("empty body: code=%d err=%v, want 415", code, err)
	}
	// Malformed body -> 400.
	code2, err2 := m.HandlePublish(buildPublishMsg("sip:bob@example.com", "<not-dfks/>"))
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
	msg := buildSubscribeMsg("sip:bob@example.com")
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
	// Empty message -> 400.
	code3, err3 := m.HandleSubscribe(&parser.SIPMsg{})
	if code3 != 400 || err3 == nil {
		t.Errorf("empty msg: code=%d err=%v, want 400", code3, err3)
	}
}

func TestSetGetClearFeatureKey(t *testing.T) {
	m := New()
	if err := m.SetFeatureKey("sip:bob@example.com", "DoNotDisturb", "true"); err != nil {
		t.Fatalf("SetFeatureKey error: %v", err)
	}
	if err := m.SetFeatureKey("sip:bob@example.com", "Forwarding", "forwardImmediate->sip:carol@example.com"); err != nil {
		t.Fatalf("SetFeatureKey 2 error: %v", err)
	}
	keys, err := m.GetFeatureKeys("sip:bob@example.com")
	if err != nil {
		t.Fatalf("GetFeatureKeys error: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("keys len = %d, want 2", len(keys))
	}
	// Clear.
	if err := m.ClearFeatureKey("sip:bob@example.com", "DoNotDisturb"); err != nil {
		t.Fatalf("ClearFeatureKey error: %v", err)
	}
	keys2, _ := m.GetFeatureKeys("sip:bob@example.com")
	if len(keys2) != 1 || keys2[0].Name != "Forwarding" {
		t.Errorf("after clear keys = %+v", keys2)
	}
}

func TestSetClearFeatureKeyErrors(t *testing.T) {
	m := New()
	if err := m.SetFeatureKey("", "k", "v"); err == nil {
		t.Errorf("empty userURI should error")
	}
	if err := m.SetFeatureKey("sip:bob@example.com", "", "v"); err == nil {
		t.Errorf("empty keyName should error")
	}
	if err := m.ClearFeatureKey("sip:missing@example.com", "k"); err == nil {
		t.Errorf("clear on missing user should error")
	}
	if err := m.SetFeatureKey("sip:bob@example.com", "DoNotDisturb", "true"); err != nil {
		t.Fatalf("SetFeatureKey error: %v", err)
	}
	if err := m.ClearFeatureKey("sip:bob@example.com", "Forwarding"); err == nil {
		t.Errorf("clear on missing key should error")
	}
}

func TestGetFeatureKeysMissing(t *testing.T) {
	m := New()
	if _, err := m.GetFeatureKeys("sip:missing@example.com"); err == nil {
		t.Errorf("GetFeatureKeys on missing should error")
	}
	if m.Count() != 0 {
		t.Errorf("Count = %d, want 0", m.Count())
	}
}

func TestDefaultDFKSAndInit(t *testing.T) {
	Init()
	a := DefaultDFKS()
	b := DefaultDFKS()
	if a != b {
		t.Error("DefaultDFKS should return the same instance")
	}
	_ = a.SetFeatureKey("sip:tmp@example.com", "DoNotDisturb", "true")
	if a.Count() != 1 {
		t.Errorf("Count = %d, want 1", a.Count())
	}
	Init()
	c := DefaultDFKS()
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
			_, _ = m.ParseDFKS([]byte(dndXML))
			out, _ := m.BuildDFKS(&DFKSInfo{UserURI: uri, FeatureKeys: map[string]string{"DoNotDisturb": "true"}})
			_, _ = m.ParseDFKS(out)
			_ = m.SetFeatureKey(uri, "DoNotDisturb", "true")
			_, _ = m.GetFeatureKeys(uri)
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
