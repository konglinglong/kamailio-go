// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - cplc module tests.
 *
 * These tests exercise the CPL interpreter against parsed scripts and SIP
 * messages, including concurrent access.
 */

package cplc

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// A SIP INVITE with a Priority and Accept-Language header.
var inviteWithPriority = []byte("INVITE sip:bob@sip.example.org SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP kamailio;branch=z9hG4bK-test\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: <sip:alice@sip.example.org>;tag=fromtag\r\n" +
	"To: <sip:bob@sip.example.org>\r\n" +
	"Call-ID: test@kamailio\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Priority: emergency\r\n" +
	"Accept-Language: en\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

var invitePlain = []byte("INVITE sip:bob@sip.example.org SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP kamailio;branch=z9hG4bK-test\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: <sip:alice@sip.example.org>;tag=fromtag\r\n" +
	"To: <sip:bob@sip.example.org>\r\n" +
	"Call-ID: test@kamailio\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

var sipReply = []byte("SIP/2.0 200 OK\r\n" +
	"Via: SIP/2.0/UDP kamailio;branch=z9hG4bK-test\r\n" +
	"From: <sip:alice@sip.example.org>;tag=fromtag\r\n" +
	"To: <sip:bob@sip.example.org>;tag=totag\r\n" +
	"Call-ID: test@kamailio\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func mustParseSIP(t *testing.T, b []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(b)
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	return msg
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.DBTable != "cpl" {
		t.Errorf("db table = %q", cfg.DBTable)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Errorf("default config: %v", err)
	}
	if err := (&Config{}).Validate(); err == nil {
		t.Error("empty config expected error")
	}
}

func TestParseScriptEmpty(t *testing.T) {
	if _, err := ParseScript(""); err == nil {
		t.Error("ParseScript('') expected error")
	}
}

func TestParseScriptBadRoot(t *testing.T) {
	if _, err := ParseScript("<notcpl/>"); err == nil {
		t.Error("ParseScript(bad root) expected error")
	}
}

func TestParseScriptBadXML(t *testing.T) {
	if _, err := ParseScript("<cpl><incoming></notcpl>"); err == nil {
		t.Error("ParseScript(bad xml) expected error")
	}
}

func TestParseScriptProxy(t *testing.T) {
	xml := `<cpl><incoming><proxy/></incoming></cpl>`
	script, err := ParseScript(xml)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	if script.Incoming == nil {
		t.Fatal("incoming nil")
	}
	msg := mustParseSIP(t, invitePlain)
	code, err := script.Incoming.Execute(msg)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if code != CPLProxy {
		t.Errorf("code = %d, want %d", code, CPLProxy)
	}
}

func TestParseScriptReject(t *testing.T) {
	xml := `<cpl><incoming><reject status="486" reason="Busy Here"/></incoming></cpl>`
	script, err := ParseScript(xml)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	msg := mustParseSIP(t, invitePlain)
	code, _ := script.Incoming.Execute(msg)
	if code != CPLReject {
		t.Errorf("code = %d, want %d", code, CPLReject)
	}
}

func TestParseScriptRedirect(t *testing.T) {
	xml := `<cpl><incoming><redirect permanent="yes"><location url="sip:voicemail@example.org"/></redirect></incoming></cpl>`
	script, err := ParseScript(xml)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	msg := mustParseSIP(t, invitePlain)
	code, _ := script.Incoming.Execute(msg)
	if code != CPLRedirect {
		t.Errorf("code = %d, want %d", code, CPLRedirect)
	}
}

func TestParseScriptPrioritySwitch(t *testing.T) {
	xml := `<cpl><incoming>
		<priority-switch>
			<greater than="3"><reject status="486"/></greater>
			<less than="3"><proxy/></less>
		</priority-switch>
	</incoming></cpl>`
	script, err := ParseScript(xml)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	// emergency (4) > 3 -> reject
	msg := mustParseSIP(t, inviteWithPriority)
	code, _ := script.Incoming.Execute(msg)
	if code != CPLReject {
		t.Errorf("emergency code = %d, want %d (reject)", code, CPLReject)
	}
	// plain invite (no priority = 0) < 3 -> proxy
	msg2 := mustParseSIP(t, invitePlain)
	code, _ = script.Incoming.Execute(msg2)
	if code != CPLProxy {
		t.Errorf("plain code = %d, want %d (proxy)", code, CPLProxy)
	}
}

func TestParseScriptLanguageSwitch(t *testing.T) {
	xml := `<cpl><incoming>
		<language-switch>
			<language matches="en"><proxy/></language>
			<language matches="fr"><reject status="486"/></language>
		</language-switch>
	</incoming></cpl>`
	script, err := ParseScript(xml)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	msg := mustParseSIP(t, inviteWithPriority)
	code, _ := script.Incoming.Execute(msg)
	if code != CPLProxy {
		t.Errorf("en code = %d, want %d (proxy)", code, CPLProxy)
	}
}

func TestParseScriptAddressSwitch(t *testing.T) {
	xml := `<cpl><incoming>
		<address-switch field="from" sub-field="address">
			<present><proxy/></present>
			<absent><reject status="486"/></absent>
		</address-switch>
	</incoming></cpl>`
	script, err := ParseScript(xml)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	msg := mustParseSIP(t, invitePlain)
	code, _ := script.Incoming.Execute(msg)
	if code != CPLProxy {
		t.Errorf("present code = %d, want %d (proxy)", code, CPLProxy)
	}
}

func TestParseScriptLogAndProxy(t *testing.T) {
	xml := `<cpl><incoming><log name="incoming"><comment>call</comment></log><proxy/></incoming></cpl>`
	script, err := ParseScript(xml)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	msg := mustParseSIP(t, invitePlain)
	code, _ := script.Incoming.Execute(msg)
	if code != CPLProxy {
		t.Errorf("code = %d, want %d (proxy after log)", code, CPLProxy)
	}
}

func TestParseScriptOutgoing(t *testing.T) {
	xml := `<cpl><outgoing><proxy/></outgoing></cpl>`
	script, err := ParseScript(xml)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	if script.Outgoing == nil {
		t.Fatal("outgoing nil")
	}
	msg := mustParseSIP(t, sipReply)
	code, _ := script.Outgoing.Execute(msg)
	if code != CPLProxy {
		t.Errorf("code = %d, want %d", code, CPLProxy)
	}
}

func TestLoadScriptFromStore(t *testing.T) {
	m := New()
	m.SetScript("alice", `<cpl><incoming><proxy/></incoming></cpl>`)
	script, err := m.LoadScript("alice")
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if script == nil {
		t.Fatal("LoadScript returned nil")
	}
	msg := mustParseSIP(t, invitePlain)
	code, _ := m.Execute(script, msg)
	if code != CPLProxy {
		t.Errorf("code = %d, want %d", code, CPLProxy)
	}
}

func TestLoadScriptCached(t *testing.T) {
	m := New()
	m.SetScript("alice", `<cpl><incoming><proxy/></incoming></cpl>`)
	s1, _ := m.LoadScript("alice")
	s2, _ := m.LoadScript("alice")
	if s1 != s2 {
		t.Error("LoadScript did not return cached script")
	}
}

func TestLoadScriptMissing(t *testing.T) {
	m := New()
	script, err := m.LoadScript("nobody")
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if script != nil {
		t.Error("LoadScript(missing) expected nil")
	}
}

func TestLoadScriptEmptyUser(t *testing.T) {
	m := New()
	if _, err := m.LoadScript(""); err == nil {
		t.Error("LoadScript('') expected error")
	}
}

func TestExecuteNilScript(t *testing.T) {
	m := New()
	msg := mustParseSIP(t, invitePlain)
	if _, err := m.Execute(nil, msg); err == nil {
		t.Error("Execute(nil,...) expected error")
	}
}

func TestExecuteNilMessage(t *testing.T) {
	m := New()
	script := &CPLScript{}
	if _, err := m.Execute(script, nil); err == nil {
		t.Error("Execute(...,nil) expected error")
	}
}

func TestExecuteNoBranch(t *testing.T) {
	m := New()
	// Script with only outgoing; an INVITE (request) has no incoming branch.
	script, _ := ParseScript(`<cpl><outgoing><proxy/></outgoing></cpl>`)
	msg := mustParseSIP(t, invitePlain)
	code, err := m.Execute(script, msg)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if code != CPLDefault {
		t.Errorf("code = %d, want %d (default, no incoming)", code, CPLDefault)
	}
}

func TestInitResetsCache(t *testing.T) {
	m := New()
	m.SetScript("alice", `<cpl><incoming><proxy/></incoming></cpl>`)
	m.LoadScript("alice")
	cfg := *DefaultConfig()
	if err := m.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.ExecCount() != 0 {
		t.Errorf("ExecCount = %d after Init, want 0", m.ExecCount())
	}
}

func TestDefaultAndInit(t *testing.T) {
	cfg := *DefaultConfig()
	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	d := DefaultCPL()
	if d == nil {
		t.Fatal("DefaultCPL nil")
	}
	d.SetScript("pkg", `<cpl><incoming><proxy/></incoming></cpl>`)
	script, err := LoadScript("pkg")
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	msg := mustParseSIP(t, invitePlain)
	code, _ := Execute(script, msg)
	if code != CPLProxy {
		t.Errorf("code = %d, want %d", code, CPLProxy)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.SetScript("alice", `<cpl><incoming><proxy/></incoming></cpl>`)
	m.SetScript("bob", `<cpl><incoming><reject status="486"/></incoming></cpl>`)
	msg := mustParseSIP(t, invitePlain)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			user := "alice"
			if i%2 == 0 {
				user = "bob"
			}
			script, err := m.LoadScript(user)
			if err != nil {
				t.Errorf("LoadScript %d: %v", i, err)
				return
			}
			code, err := m.Execute(script, msg)
			if err != nil {
				t.Errorf("Execute %d: %v", i, err)
				return
			}
			if user == "alice" && code != CPLProxy {
				t.Errorf("alice code %d = %d, want %d", i, code, CPLProxy)
			}
			if user == "bob" && code != CPLReject {
				t.Errorf("bob code %d = %d, want %d", i, code, CPLReject)
			}
		}()
	}
	wg.Wait()
	if got := m.ExecCount(); got != 50 {
		t.Errorf("ExecCount = %d, want 50", got)
	}
}
