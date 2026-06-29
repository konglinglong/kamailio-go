// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS registrar P-CSCF module.
 */

package ims_registrar_pcscf

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
	"github.com/kamailio/kamailio-go/internal/modules/ims_usrloc_pcscf"
)

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

func makeRegister(cseq int) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK776reg\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: reg-call-1@10.0.0.1\r\n" +
		"CSeq: " + itoaCseq(cseq) + " REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>;+sip.instance=\"<urn:uuid:abc>\";q=0.7\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

func makeRegisterWithNAT(cseq int) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK776nat;received=203.0.113.5;rport=54321\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: reg-call-1@10.0.0.1\r\n" +
		"CSeq: " + itoaCseq(cseq) + " REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

func makeDeregister(cseq int) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK776unreg\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: reg-call-2@10.0.0.1\r\n" +
		"CSeq: " + itoaCseq(cseq) + " REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
		"Expires: 0\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

func makeDeregisterStar(cseq int) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK776star\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: reg-call-3@10.0.0.1\r\n" +
		"CSeq: " + itoaCseq(cseq) + " REGISTER\r\n" +
		"Contact: *\r\n" +
		"Expires: 0\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

func make200OK(cseq int) []byte {
	return []byte("SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK776reg\r\n" +
		"To: <sip:alice@example.com>;tag=vrtr\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"Call-ID: reg-call-1@10.0.0.1\r\n" +
		"CSeq: " + itoaCseq(cseq) + " REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>;expires=3600\r\n" +
		"Service-Route: <sip:orig@scscf.ims.example.com;lr>\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

func make200OKNoRoute(cseq int) []byte {
	return []byte("SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK776reg\r\n" +
		"To: <sip:alice@example.com>;tag=vrtr\r\n" +
		"From: <sip:alice@example.com>;tag=ftag1\r\n" +
		"Call-ID: reg-call-1@10.0.0.1\r\n" +
		"CSeq: " + itoaCseq(cseq) + " REGISTER\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

func make403() []byte {
	return []byte("SIP/2.0 403 Forbidden\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK776reg\r\n" +
		"To: <sip:alice@example.com>;tag=vrtr\r\n" +
		"From: <sip:alice@example.com>;tag=ftag1\r\n" +
		"Call-ID: reg-call-1@10.0.0.1\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

func itoaCseq(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

func TestSave_InitialRegister(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	msg := mustParseMsg(t, makeRegister(1))
	r, err := m.Save(msg)
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}
	if r.StatusCode != 200 {
		t.Errorf("expected 200, got %d", r.StatusCode)
	}
	if !m.IsRegistered("sip:alice@example.com") {
		t.Error("expected IsRegistered true after save")
	}
}

func TestSave_StoresContactAndInstanceID(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	msg := mustParseMsg(t, makeRegister(1))
	_, _ = m.Save(msg)
	contacts := m.RegFetchContacts("sip:alice@example.com")
	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	c := contacts[0]
	if c.Contact != "sip:alice@10.0.0.1:5060" {
		t.Errorf("contact = %q", c.Contact)
	}
	if c.InstanceID != "<urn:uuid:abc>" {
		t.Errorf("instance id = %q", c.InstanceID)
	}
	if c.Q != 0.7 {
		t.Errorf("q = %v", c.Q)
	}
}

func TestSave_NATRecordsReceived(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	if !m.natEnabled() {
		t.Skip("NAT not enabled in default config")
	}
	msg := mustParseMsg(t, makeRegisterWithNAT(1))
	_, _ = m.Save(msg)
	contacts := m.RegFetchContacts("sip:alice@example.com")
	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	if contacts[0].Received == "" {
		t.Error("expected Received to be recorded for NAT")
	}
}

func TestSave_DeregisterExpires0(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	_, _ = m.Save(mustParseMsg(t, makeRegister(1)))
	if !m.IsRegistered("sip:alice@example.com") {
		t.Fatal("precondition: should be registered")
	}
	r, err := m.Save(mustParseMsg(t, makeDeregister(2)))
	if err != nil {
		t.Fatalf("deregister error: %v", err)
	}
	if r.StatusCode != 200 {
		t.Errorf("expected 200 on deregister, got %d", r.StatusCode)
	}
	if m.IsRegistered("sip:alice@example.com") {
		t.Error("expected IsRegistered false after deregister")
	}
}

func TestSave_DeregisterContactStar(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	_, _ = m.Save(mustParseMsg(t, makeRegister(1)))
	r, err := m.Save(mustParseMsg(t, makeDeregisterStar(2)))
	if err != nil {
		t.Fatalf("deregister * error: %v", err)
	}
	if r.StatusCode != 200 {
		t.Errorf("expected 200 on deregister *, got %d", r.StatusCode)
	}
	if m.IsRegistered("sip:alice@example.com") {
		t.Error("expected IsRegistered false after deregister *")
	}
}

func TestSave_DeregisterUnknown(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	r, _ := m.Save(mustParseMsg(t, makeDeregister(1)))
	if r.StatusCode != 404 {
		t.Errorf("expected 404 deregistering unknown AOR, got %d", r.StatusCode)
	}
}

func TestSave_NilMessage(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	_, err := m.Save(nil)
	if err == nil {
		t.Error("expected error for nil message")
	}
}

func TestSave_NotRegister(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	invite := []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776inv\r\n" +
		"From: <sip:alice@example.com>;tag=t1\r\n" +
		"To: <sip:bob@example.com>\r\n" +
		"Call-ID: c@10.0.0.1\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n\r\n")
	_, err := m.Save(mustParseMsg(t, invite))
	if err == nil {
		t.Error("expected error for non-REGISTER")
	}
}

func TestSave_NoToHeader(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	msg := []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776noto\r\n" +
		"From: <sip:alice@example.com>;tag=t1\r\n" +
		"Call-ID: c@10.0.0.1\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1>\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n\r\n")
	r, err := m.Save(mustParseMsg(t, msg))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.StatusCode != 400 {
		t.Errorf("expected 400 for missing To, got %d", r.StatusCode)
	}
}

func TestSave_NoContact(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	msg := []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776noc\r\n" +
		"From: <sip:alice@example.com>;tag=t1\r\n" +
		"To: <sip:alice@example.com>\r\n" +
		"Call-ID: c@10.0.0.1\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n\r\n")
	r, err := m.Save(mustParseMsg(t, msg))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.StatusCode != 400 {
		t.Errorf("expected 400 for missing Contact, got %d", r.StatusCode)
	}
}

func TestSaveResponse_CachesServiceRoute(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	// Seed a binding first so the contact update can refresh it.
	_, _ = m.Save(mustParseMsg(t, makeRegister(1)))
	if m.ServiceRouteCount() != 0 {
		t.Fatalf("expected 0 service routes initially, got %d", m.ServiceRouteCount())
	}
	r, err := m.SaveResponse(mustParseMsg(t, make200OK(1)))
	if err != nil {
		t.Fatalf("SaveResponse error: %v", err)
	}
	if r.StatusCode != 200 {
		t.Errorf("expected 200, got %d", r.StatusCode)
	}
	if m.ServiceRouteCount() != 1 {
		t.Errorf("expected 1 service route cached, got %d", m.ServiceRouteCount())
	}
	routes := m.GetServiceRoute("sip:alice@example.com")
	if len(routes) != 1 {
		t.Fatalf("expected 1 service route URI, got %d", len(routes))
	}
	if routes[0] != "sip:orig@scscf.ims.example.com;lr" {
		t.Errorf("service route = %q", routes[0])
	}
}

func TestSaveResponse_NoRoute(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	r, err := m.SaveResponse(mustParseMsg(t, make200OKNoRoute(1)))
	if err != nil {
		t.Fatalf("SaveResponse error: %v", err)
	}
	if r.StatusCode != 200 {
		t.Errorf("expected 200, got %d", r.StatusCode)
	}
	if m.ServiceRouteCount() != 0 {
		t.Errorf("expected 0 service routes, got %d", m.ServiceRouteCount())
	}
}

func TestSaveResponse_Non200(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	r, err := m.SaveResponse(mustParseMsg(t, make403()))
	if err != nil {
		t.Fatalf("SaveResponse error: %v", err)
	}
	if r.StatusCode != 403 {
		t.Errorf("expected 403 propagated, got %d", r.StatusCode)
	}
}

func TestSaveResponse_NilMessage(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	_, err := m.SaveResponse(nil)
	if err == nil {
		t.Error("expected error for nil message")
	}
}

func TestSaveResponse_NotReply(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	_, err := m.SaveResponse(mustParseMsg(t, makeRegister(1)))
	if err == nil {
		t.Error("expected error for non-reply message")
	}
}

func TestLookup(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	_, _ = m.Save(mustParseMsg(t, makeRegister(1)))
	msg := mustParseMsg(t, []byte("INVITE sip:alice@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.2;branch=z9hG4bK776lkp\r\n" +
		"From: <sip:bob@example.com>;tag=t1\r\n" +
		"To: <sip:alice@example.com>\r\n" +
		"Call-ID: c@10.0.0.2\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n\r\n"))
	contacts, err := m.Lookup(msg)
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	if contacts[0] != "sip:alice@10.0.0.1:5060" {
		t.Errorf("contact = %q", contacts[0])
	}
}

func TestLookup_NoRURI(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	_, err := m.Lookup(&parser.SIPMsg{})
	if err == nil {
		t.Error("expected error for message with no R-URI")
	}
}

func TestLookup_NilMessage(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	_, err := m.Lookup(nil)
	if err == nil {
		t.Error("expected error for nil message")
	}
}

func TestIsRegistered(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	if m.IsRegistered("sip:nobody@example.com") {
		t.Error("expected false for unknown AOR")
	}
	_, _ = m.Save(mustParseMsg(t, makeRegister(1)))
	if !m.IsRegistered("sip:alice@example.com") {
		t.Error("expected true after save")
	}
}

func TestRegFetchContacts(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	if c := m.RegFetchContacts("sip:nobody@example.com"); len(c) != 0 {
		t.Errorf("expected 0 contacts, got %d", len(c))
	}
	_, _ = m.Save(mustParseMsg(t, makeRegister(1)))
	if c := m.RegFetchContacts("sip:alice@example.com"); len(c) != 1 {
		t.Errorf("expected 1 contact, got %d", len(c))
	}
}

func TestDeregister(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	_, _ = m.Save(mustParseMsg(t, makeRegister(1)))
	if err := m.Deregister("sip:alice@example.com"); err != nil {
		t.Fatalf("Deregister error: %v", err)
	}
	if m.IsRegistered("sip:alice@example.com") {
		t.Error("expected false after Deregister")
	}
}

func TestDeregister_Unknown(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	if err := m.Deregister("sip:nobody@example.com"); err == nil {
		t.Error("expected error deregistering unknown AOR")
	}
}

func TestDeregisterClearsServiceRoute(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	_, _ = m.Save(mustParseMsg(t, makeRegister(1)))
	_, _ = m.SaveResponse(mustParseMsg(t, make200OK(1)))
	if m.ServiceRouteCount() != 1 {
		t.Fatal("expected 1 service route before deregister")
	}
	if err := m.Deregister("sip:alice@example.com"); err != nil {
		t.Fatalf("Deregister error: %v", err)
	}
	if m.ServiceRouteCount() != 0 {
		t.Errorf("expected 0 service routes after deregister, got %d", m.ServiceRouteCount())
	}
}

func TestSetRealm(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	m.SetRealm("newrealm.example.com")
	if m.Realm() != "newrealm.example.com" {
		t.Errorf("Realm = %q", m.Realm())
	}
}

func TestSetConfig(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	cfg := DefaultConfig()
	cfg.MaxContacts = 5
	cfg.EnableNAT = false
	m.SetConfig(cfg)
	if m.natEnabled() {
		t.Error("expected NAT disabled after SetConfig")
	}
}

func TestSetUsrloc(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	custom := ims_usrloc_pcscf.NewUsrlocPCSCFModule()
	m.SetUsrloc(custom)
	if m.Usrloc() != custom {
		t.Error("SetUsrloc did not install custom backend")
	}
	m.SetUsrloc(nil)
	if m.Usrloc() == nil {
		t.Error("expected non-nil usrloc after nil set")
	}
}

func TestUsrlocAccessor(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	if m.Usrloc() == nil {
		t.Error("expected non-nil usrloc")
	}
}

func TestDefaultSingleton(t *testing.T) {
	if DefaultPCSCFRegistrar() != DefaultPCSCFRegistrar() {
		t.Error("DefaultPCSCFRegistrar should return same instance")
	}
}

func TestInit_ResetsDefault(t *testing.T) {
	first := DefaultPCSCFRegistrar()
	first.SetRealm("before-init.example.com")
	Init()
	second := DefaultPCSCFRegistrar()
	if second.Realm() == "before-init.example.com" {
		t.Error("Init should reset the default registrar")
	}
	if second.Realm() != DefaultConfig().Realm {
		t.Errorf("Realm = %q, want %q", second.Realm(), DefaultConfig().Realm)
	}
}

func TestNewWithConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Realm = "custom.example.com"
	cfg.MaxContacts = 3
	m := NewRegistrarPCSCFModuleWithConfig(cfg)
	if m.Realm() != "custom.example.com" {
		t.Errorf("Realm = %q", m.Realm())
	}
}

func TestIsDeregistration(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want bool
	}{
		{"expires_zero", makeDeregister(1), true},
		{"contact_star", makeDeregisterStar(1), true},
		{"normal_register", makeRegister(1), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := mustParseMsg(t, tc.raw)
			if got := isDeregistration(msg); got != tc.want {
				t.Errorf("isDeregistration = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractAOR(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"<sip:alice@example.com>", "sip:alice@example.com"},
		{"Alice <sip:alice@example.com>", "sip:alice@example.com"},
		{"sip:bob@example.com", "sip:bob@example.com"},
	}
	for _, c := range cases {
		if got := extractAOR(c.in); got != c.want {
			t.Errorf("extractAOR(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractContactURI(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"*", "*"},
		{"<sip:alice@10.0.0.1:5060>", "sip:alice@10.0.0.1:5060"},
		{"<sip:alice@10.0.0.1>;q=0.5", "sip:alice@10.0.0.1"},
	}
	for _, c := range cases {
		if got := extractContactURI(c.in); got != c.want {
			t.Errorf("extractContactURI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractInstanceID(t *testing.T) {
	body := `<sip:alice@10.0.0.1>;+sip.instance="<urn:uuid:abc>"`
	if got := extractInstanceID(body); got != "<urn:uuid:abc>" {
		t.Errorf("extractInstanceID = %q", got)
	}
	if extractInstanceID("") != "" {
		t.Error("expected empty for empty body")
	}
	if got := extractInstanceID("<sip:alice@10.0.0.1>"); got != "" {
		t.Errorf("expected empty when no instance param, got %q", got)
	}
}

func TestExtractQ(t *testing.T) {
	if q, ok := extractQ(`<sip:a@1>;q=0.7`); !ok || q != 0.7 {
		t.Errorf("extractQ = %v ok=%v", q, ok)
	}
	if _, ok := extractQ("<sip:a@1>"); ok {
		t.Error("expected ok=false when no q param")
	}
	if _, ok := extractQ(""); ok {
		t.Error("expected ok=false for empty body")
	}
}

func TestExtractReceived_FromViaBody(t *testing.T) {
	// Construct a ViaBody with received/rport params to test extractReceived.
	via := &parser.ViaBody{
		Host: str.Mk("10.0.0.1"),
		Port: 5060,
	}
	// With Received and RPort set.
	via.Received = &parser.ViaParam{Value: str.Mk("203.0.113.5")}
	via.RPort = &parser.ViaParam{Value: str.Mk("54321")}
	msg := &parser.SIPMsg{Via1: via}
	if got := extractReceived(msg); got != "203.0.113.5:54321" {
		t.Errorf("extractReceived = %q, want 203.0.113.5:54321", got)
	}
	// Without Received, falls back to host:port.
	via2 := &parser.ViaBody{Host: str.Mk("10.0.0.1"), Port: 5060}
	msg2 := &parser.SIPMsg{Via1: via2}
	if got := extractReceived(msg2); got != "10.0.0.1:5060" {
		t.Errorf("extractReceived (fallback) = %q, want 10.0.0.1:5060", got)
	}
}

func TestExtractReceived_Empty(t *testing.T) {
	if got := extractReceived(&parser.SIPMsg{}); got != "" {
		t.Errorf("expected empty received for nil Via1, got %q", got)
	}
}

func TestStatusReason(t *testing.T) {
	msg := mustParseMsg(t, make200OK(1))
	if got := statusReason(msg); got != "OK" {
		t.Errorf("statusReason = %q, want OK", got)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewRegistrarPCSCFModule()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := mustParseMsg(t, makeRegister(n + 1))
			_, _ = m.Save(msg)
		}(i)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.IsRegistered("sip:alice@example.com")
			_ = m.GetServiceRoute("sip:alice@example.com")
			_ = m.RegFetchContacts("sip:alice@example.com")
		}()
	}
	wg.Wait()
}

// silence unused import warnings for time/strings when builds prune them.
var (
	_ = time.Second
	_ = strings.Contains
)
