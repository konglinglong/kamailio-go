// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the sipt module - ISUP/SIP translation.
 */
package sipt

import (
	"strings"
	"sync"
	"testing"
)

func TestBuildIAMAndGetNumbers(t *testing.T) {
	m := New()
	isup, err := m.BuildIAM("12025551234", "16175559999")
	if err != nil {
		t.Fatalf("BuildIAM: %v", err)
	}
	if isup[0] != MsgIAM {
		t.Errorf("message type = 0x%02x, want IAM", isup[0])
	}
	called, err := m.GetIAMCalledNumber(isup)
	if err != nil {
		t.Fatalf("GetIAMCalledNumber: %v", err)
	}
	if called != "12025551234" {
		t.Errorf("called = %q", called)
	}
	calling, err := m.GetIAMCallingNumber(isup)
	if err != nil {
		t.Fatalf("GetIAMCallingNumber: %v", err)
	}
	if calling != "16175559999" {
		t.Errorf("calling = %q", calling)
	}
}

func TestIsupToSip(t *testing.T) {
	m := New()
	isup, _ := m.BuildIAM("12025551234", "16175559999")
	sip, err := m.IsupToSip(isup)
	if err != nil {
		t.Fatalf("IsupToSip: %v", err)
	}
	if !strings.Contains(sip, "12025551234") {
		t.Errorf("sip missing called: %s", sip)
	}
	if !strings.Contains(sip, "16175559999") {
		t.Errorf("sip missing calling: %s", sip)
	}
	if !strings.Contains(sip, "IAM") {
		t.Errorf("sip missing ISUP marker: %s", sip)
	}
}

func TestSipToIsupRoundTrip(t *testing.T) {
	m := New()
	isup, _ := m.BuildIAM("12025551234", "16175559999")
	sip, _ := m.IsupToSip(isup)
	isup2, err := m.SipToIsup(sip)
	if err != nil {
		t.Fatalf("SipToIsup: %v", err)
	}
	called, _ := m.GetIAMCalledNumber(isup2)
	calling, _ := m.GetIAMCallingNumber(isup2)
	if called != "12025551234" {
		t.Errorf("round-trip called = %q", called)
	}
	if calling != "16175559999" {
		t.Errorf("round-trip calling = %q", calling)
	}
}

func TestParseIAMErrors(t *testing.T) {
	m := New()
	if _, err := m.GetIAMCalledNumber(nil); err == nil {
		t.Error("GetIAMCalledNumber(nil) should error")
	}
	if _, err := m.GetIAMCalledNumber([]byte{0x06}); err == nil {
		t.Error("non-IAM should error")
	}
	if _, err := m.IsupToSip([]byte{0x06}); err == nil {
		t.Error("IsupToSip non-IAM should error")
	}
}

func TestBuildIAMEmpty(t *testing.T) {
	m := New()
	if _, err := m.BuildIAM("", "16175559999"); err == nil {
		t.Error("BuildIAM with empty called should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultSIPT()
	d2 := DefaultSIPT()
	if d1 != d2 {
		t.Error("DefaultSIPT should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			isup, _ := m.BuildIAM("12025551234", "16175559999")
			_, _ = m.IsupToSip(isup)
			_, _ = m.SipToIsup("From: <16175559999>\r\nTo: <12025551234>\r\n")
		}()
	}
	wg.Wait()
}
