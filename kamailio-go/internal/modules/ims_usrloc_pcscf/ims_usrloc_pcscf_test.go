// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS usrloc P-CSCF module.
 */

package ims_usrloc_pcscf

import (
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

var registerBytes = []byte("REGISTER sip:example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776reg\r\n" +
	"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
	"To: Alice <sip:alice@example.com>\r\n" +
	"Call-ID: reg-pcscf-1@10.0.0.1\r\n" +
	"CSeq: 1 REGISTER\r\n" +
	"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
	"Expires: 3600\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

var unregisterBytes = []byte("REGISTER sip:example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776unreg\r\n" +
	"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
	"To: Alice <sip:alice@example.com>\r\n" +
	"Call-ID: reg-pcscf-2@10.0.0.1\r\n" +
	"CSeq: 2 REGISTER\r\n" +
	"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
	"Expires: 0\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func TestSaveAndGetContact(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	c := &PCCContact{
		AOR:      "sip:alice@example.com",
		Contact:  "sip:alice@10.0.0.1",
		Expires:  time.Now().Add(time.Hour),
		RegState: RegStateRegistered,
	}
	if err := m.SaveContact(c); err != nil {
		t.Fatalf("SaveContact failed: %v", err)
	}
	list := m.GetContacts("sip:alice@example.com")
	if len(list) != 1 {
		t.Fatalf("GetContacts len = %d, want 1", len(list))
	}
	if list[0].Contact != "sip:alice@10.0.0.1" {
		t.Errorf("Contact = %q", list[0].Contact)
	}
	if !m.IsRegistered("sip:alice@example.com") {
		t.Error("IsRegistered should be true")
	}
}

func TestSaveContactErrors(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	if err := m.SaveContact(nil); err == nil {
		t.Error("SaveContact(nil) should error")
	}
	if err := m.SaveContact(&PCCContact{Contact: "sip:x"}); err == nil {
		t.Error("SaveContact with empty AOR should error")
	}
	if err := m.SaveContact(&PCCContact{AOR: "sip:x"}); err == nil {
		t.Error("SaveContact with empty Contact should error")
	}
}

func TestSaveContactReplacesExisting(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@1.1.1.1", Expires: time.Now().Add(time.Hour), RegState: RegStateRegistered})
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@1.1.1.1", Expires: time.Now().Add(2 * time.Hour), RegState: RegStateRegistered})
	if got := len(m.GetContacts("sip:a@e.com")); got != 1 {
		t.Errorf("len = %d, want 1 (replace)", got)
	}
}

func TestSaveContactMaxLimit(t *testing.T) {
	m := NewUsrlocPCSCFModuleWithConfig(Config{MaxContacts: 2, DefaultExpires: 60})
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@1", RegState: RegStateRegistered})
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@2", RegState: RegStateRegistered})
	if err := m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@3", RegState: RegStateRegistered}); err == nil {
		t.Error("SaveContact over max should error")
	}
}

func TestRemoveContactAndAOR(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@1", RegState: RegStateRegistered})
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@2", RegState: RegStateRegistered})
	if err := m.RemoveContact("sip:a@e.com", "sip:a@1"); err != nil {
		t.Fatalf("RemoveContact failed: %v", err)
	}
	if got := len(m.GetContacts("sip:a@e.com")); got != 1 {
		t.Errorf("len = %d, want 1", got)
	}
	if err := m.RemoveContact("sip:a@e.com", "missing"); err == nil {
		t.Error("RemoveContact for missing should error")
	}
	if err := m.RemoveAOR("sip:a@e.com"); err != nil {
		t.Fatalf("RemoveAOR failed: %v", err)
	}
	if m.IsRegistered("sip:a@e.com") {
		t.Error("IsRegistered after RemoveAOR should be false")
	}
	if err := m.RemoveAOR("missing"); err == nil {
		t.Error("RemoveAOR for missing should error")
	}
}

func TestUpdateContact(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@1", Expires: time.Now().Add(time.Hour), RegState: RegStateRegistered})
	newExp := time.Now().Add(2 * time.Hour)
	if err := m.UpdateContact("sip:a@e.com", "sip:a@1", newExp); err != nil {
		t.Fatalf("UpdateContact failed: %v", err)
	}
	c := m.GetContacts("sip:a@e.com")[0]
	if !c.Expires.Equal(newExp) {
		t.Errorf("Expires not updated")
	}
	if err := m.UpdateContact("sip:a@e.com", "missing", newExp); err == nil {
		t.Error("UpdateContact for missing should error")
	}
}

func TestCleanupExpired(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@1", Expires: time.Now().Add(-time.Hour), RegState: RegStateRegistered})
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@2", Expires: time.Now().Add(time.Hour), RegState: RegStateRegistered})
	purged := m.CleanupExpired()
	if purged != 1 {
		t.Errorf("purged = %d, want 1", purged)
	}
	if got := len(m.GetContacts("sip:a@e.com")); got != 1 {
		t.Errorf("len after cleanup = %d, want 1", got)
	}
}

func TestGetAORList(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@1", RegState: RegStateRegistered})
	m.SaveContact(&PCCContact{AOR: "sip:b@e.com", Contact: "sip:b@1", RegState: RegStateRegistered})
	if got := len(m.GetAORList()); got != 2 {
		t.Errorf("GetAORList len = %d, want 2", got)
	}
	if m.AORCount() != 2 {
		t.Errorf("AORCount = %d, want 2", m.AORCount())
	}
}

func TestHandleRegister(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	msg := mustParseMsg(t, registerBytes)
	code, err := m.HandleRegister(msg)
	if err != nil {
		t.Fatalf("HandleRegister failed: %v", err)
	}
	if code != 200 {
		t.Errorf("code = %d, want 200", code)
	}
	if !m.IsRegistered("sip:alice@example.com") {
		t.Error("should be registered after HandleRegister")
	}
	list := m.GetContacts("sip:alice@example.com")
	if len(list) != 1 {
		t.Fatalf("contacts len = %d, want 1", len(list))
	}
	if list[0].Contact != "sip:alice@10.0.0.1:5060" {
		t.Errorf("Contact = %q", list[0].Contact)
	}
}

func TestHandleRegisterErrors(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	if _, err := m.HandleRegister(nil); err == nil {
		t.Error("HandleRegister(nil) should error")
	}
	// Non-REGISTER.
	invite := []byte("INVITE sip:alice@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776\r\n" +
		"From: <sip:a@e.com>;tag=t\r\n" +
		"To: <sip:b@e.com>\r\n" +
		"Call-ID: c@h\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n\r\n")
	if code, _ := m.HandleRegister(mustParseMsg(t, invite)); code != 405 {
		t.Errorf("code for non-REGISTER = %d, want 405", code)
	}
	// No Contact.
	noContact := []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776\r\n" +
		"From: <sip:a@e.com>;tag=t\r\n" +
		"To: <sip:a@e.com>\r\n" +
		"Call-ID: c@h\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Content-Length: 0\r\n\r\n")
	if code, _ := m.HandleRegister(mustParseMsg(t, noContact)); code != 400 {
		t.Errorf("code for no-contact = %d, want 400", code)
	}
}

func TestHandleUnregister(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	reg := mustParseMsg(t, registerBytes)
	m.HandleRegister(reg)
	if !m.IsRegistered("sip:alice@example.com") {
		t.Fatal("expected registered first")
	}
	unreg := mustParseMsg(t, unregisterBytes)
	code, err := m.HandleUnregister(unreg)
	if err != nil {
		t.Fatalf("HandleUnregister failed: %v", err)
	}
	if code != 200 {
		t.Errorf("code = %d, want 200", code)
	}
	if m.IsRegistered("sip:alice@example.com") {
		t.Error("should not be registered after unregister")
	}
	// Unregister missing returns 404.
	if code, _ := m.HandleUnregister(unreg); code != 404 {
		t.Errorf("code for missing unregister = %d, want 404", code)
	}
}

func TestHandleUnregisterStar(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@1", RegState: RegStateRegistered})
	m.SaveContact(&PCCContact{AOR: "sip:a@e.com", Contact: "sip:a@2", RegState: RegStateRegistered})
	star := []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776star\r\n" +
		"From: <sip:a@e.com>;tag=t\r\n" +
		"To: <sip:a@e.com>\r\n" +
		"Call-ID: c@h\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Contact: *\r\n" +
		"Expires: 0\r\n" +
		"Content-Length: 0\r\n\r\n")
	code, err := m.HandleUnregister(mustParseMsg(t, star))
	if err != nil {
		t.Fatalf("HandleUnregister star failed: %v", err)
	}
	if code != 200 {
		t.Errorf("code = %d, want 200", code)
	}
	if m.IsRegistered("sip:a@e.com") {
		t.Error("should not be registered after star unregister")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewUsrlocPCSCFModule()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.SaveContact(&PCCContact{
				AOR:      "sip:alice@example.com",
				Contact:  "sip:alice@10.0.0.1",
				Expires:  time.Now().Add(time.Hour),
				RegState: RegStateRegistered,
			})
			m.GetContacts("sip:alice@example.com")
			m.IsRegistered("sip:alice@example.com")
			m.GetAORList()
			m.AORCount()
		}(i)
	}
	wg.Wait()
	if got := len(m.GetContacts("sip:alice@example.com")); got != 1 {
		t.Errorf("contacts len = %d, want 1", got)
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	p := DefaultUsrlocPCSCF()
	if p == nil {
		t.Fatal("expected non-nil default P-CSCF module")
	}
	if p.AORCount() != 0 {
		t.Errorf("AORCount = %d, want 0 after Init", p.AORCount())
	}
	if p2 := DefaultUsrlocPCSCF(); p != p2 {
		t.Error("DefaultUsrlocPCSCF should return the same instance")
	}
}
