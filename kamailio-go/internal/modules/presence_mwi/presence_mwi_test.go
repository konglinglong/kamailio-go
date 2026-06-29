// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the presence_mwi module - Message Waiting Indication.
 */
package presence_mwi

import (
	"strings"
	"sync"
	"testing"
)

func TestParseMWIBody(t *testing.T) {
	m := NewMWIModule()
	body := "Messages-Waiting: yes\r\n" +
		"Message-Account: sip:alice@example.com\r\n" +
		"Voice-Message: 2/1 (1/0)\r\n"

	info, err := m.ParseMWIBody(body)
	if err != nil {
		t.Fatalf("ParseMWIBody error: %v", err)
	}
	if info.MessageAccount != "sip:alice@example.com" {
		t.Errorf("MessageAccount = %q", info.MessageAccount)
	}
	if info.NewMessages != 2 {
		t.Errorf("NewMessages = %d, want 2", info.NewMessages)
	}
	if info.OldMessages != 1 {
		t.Errorf("OldMessages = %d, want 1", info.OldMessages)
	}
	if info.NewUrgentMessages != 1 {
		t.Errorf("NewUrgentMessages = %d, want 1", info.NewUrgentMessages)
	}
}

func TestBuildMWIBodyRoundTrip(t *testing.T) {
	m := NewMWIModule()
	info := &MWIInfo{
		Mailbox:           "sip:bob@example.com",
		MessageAccount:    "sip:bob@example.com",
		NewMessages:       3,
		OldMessages:       2,
		NewUrgentMessages: 1,
		VoiceMessageURL:   "sip:vm@example.com",
	}
	body := m.BuildMWIBody(info)
	if !strings.Contains(body, "Messages-Waiting: yes") {
		t.Errorf("body missing Messages-Waiting yes: %s", body)
	}
	if !strings.Contains(body, "Message-Account: sip:bob@example.com") {
		t.Errorf("body missing Message-Account: %s", body)
	}
	if !strings.Contains(body, "Voice-Message: 3/2 (1/0)") {
		t.Errorf("body missing Voice-Message: %s", body)
	}
	// Round-trip parse.
	parsed, err := m.ParseMWIBody(body)
	if err != nil {
		t.Fatalf("ParseMWIBody(build) error: %v", err)
	}
	if parsed.NewMessages != 3 || parsed.OldMessages != 2 {
		t.Errorf("round-trip counts = %d/%d", parsed.NewMessages, parsed.OldMessages)
	}
	if parsed.NewUrgentMessages != 1 {
		t.Errorf("round-trip NewUrgent = %d", parsed.NewUrgentMessages)
	}
	if parsed.MessageAccount != "sip:bob@example.com" {
		t.Errorf("round-trip MessageAccount = %q", parsed.MessageAccount)
	}
}

func TestBuildMWIBodyNoWaiting(t *testing.T) {
	m := NewMWIModule()
	info := &MWIInfo{
		MessageAccount: "sip:empty@example.com",
		NewMessages:    0,
		OldMessages:    5,
	}
	body := m.BuildMWIBody(info)
	if !strings.Contains(body, "Messages-Waiting: no") {
		t.Errorf("body should say Messages-Waiting: no when NewMessages=0: %s", body)
	}
}

func TestNotifyAndGetMWI(t *testing.T) {
	m := NewMWIModule()
	info := &MWIInfo{
		Mailbox:        "sip:carol@example.com",
		MessageAccount: "sip:carol@example.com",
		NewMessages:    4,
		OldMessages:    0,
	}
	if err := m.Notify("sip:carol@example.com", info); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if got := m.Count(); got != 1 {
		t.Errorf("Count = %d, want 1", got)
	}
	got := m.GetMWI("sip:carol@example.com")
	if got == nil {
		t.Fatal("GetMWI returned nil")
	}
	if got.NewMessages != 4 {
		t.Errorf("NewMessages = %d, want 4", got.NewMessages)
	}
	// Notify again updates the record.
	info2 := &MWIInfo{
		Mailbox:        "sip:carol@example.com",
		MessageAccount: "sip:carol@example.com",
		NewMessages:    0,
		OldMessages:    4,
	}
	if err := m.Notify("sip:carol@example.com", info2); err != nil {
		t.Fatal(err)
	}
	if got := m.Count(); got != 1 {
		t.Errorf("Count after re-notify = %d, want 1", got)
	}
	got2 := m.GetMWI("sip:carol@example.com")
	if got2.NewMessages != 0 || got2.OldMessages != 4 {
		t.Errorf("after re-notify = %+v", got2)
	}
}

func TestClearMWI(t *testing.T) {
	m := NewMWIModule()
	m.Notify("sip:dave@example.com", &MWIInfo{Mailbox: "sip:dave@example.com", NewMessages: 1})
	if !m.ClearMWI("sip:dave@example.com") {
		t.Error("ClearMWI returned false for existing")
	}
	if m.ClearMWI("sip:dave@example.com") {
		t.Error("ClearMWI returned true for missing")
	}
	if got := m.GetMWI("sip:dave@example.com"); got != nil {
		t.Errorf("GetMWI after clear = %v, want nil", got)
	}
	if got := m.Count(); got != 0 {
		t.Errorf("Count after clear = %d, want 0", got)
	}
}

func TestNotifyInvalid(t *testing.T) {
	m := NewMWIModule()
	if err := m.Notify("", &MWIInfo{NewMessages: 1}); err == nil {
		t.Errorf("Notify with empty mailbox should error")
	}
	if err := m.Notify("sip:x@example.com", nil); err == nil {
		t.Errorf("Notify with nil info should error")
	}
}

func TestDefaultMWIAndInit(t *testing.T) {
	Init()
	a := DefaultMWI()
	b := DefaultMWI()
	if a != b {
		t.Error("DefaultMWI should return the same instance")
	}
	Init()
	c := DefaultMWI()
	if c == a {
		t.Error("Init should reset the default instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewMWIModule()
	body := "Messages-Waiting: yes\r\nMessage-Account: sip:z@example.com\r\nVoice-Message: 1/0 (0/0)\r\n"
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			mbox := "sip:m" + itoa(i) + "@example.com"
			info, _ := m.ParseMWIBody(body)
			if info != nil {
				_ = m.BuildMWIBody(info)
			}
			_ = m.Notify(mbox, &MWIInfo{Mailbox: mbox, NewMessages: i})
			_ = m.GetMWI(mbox)
			if i%2 == 0 {
				m.ClearMWI(mbox)
			}
		}()
	}
	wg.Wait()
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
