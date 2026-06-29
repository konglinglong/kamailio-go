// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the ss7ops module - SCCP/ISUP parsing and building.
 */
package ss7ops

import (
	"sync"
	"testing"
)

func TestSCCPRoundTrip(t *testing.T) {
	m := New()
	addr := "12025551234"
	sccp := m.BuildSCCP(addr)
	if sccp[0] != 0x02 {
		t.Errorf("address type = 0x%02x", sccp[0])
	}
	parsed, err := m.ParseSCCP(sccp)
	if err != nil {
		t.Fatalf("ParseSCCP: %v", err)
	}
	if parsed != addr {
		t.Errorf("ParseSCCP = %q, want %q", parsed, addr)
	}
}

func TestParseSCPCErrors(t *testing.T) {
	m := New()
	if _, err := m.ParseSCCP(nil); err == nil {
		t.Error("ParseSCCP(nil) should error")
	}
	if _, err := m.ParseSCCP([]byte{0x02}); err == nil {
		t.Error("ParseSCCP short should error")
	}
}

func TestParseAndBuildISUP(t *testing.T) {
	m := New()
	params := map[string]interface{}{
		"cic":     42,
		"type":    1,
		"called":  "12025551234",
		"calling": "16175559999",
	}
	data := m.BuildISUP(params)
	parsed, err := m.ParseISUP(data)
	if err != nil {
		t.Fatalf("ParseISUP: %v", err)
	}
	if parsed["cic"] != 42 {
		t.Errorf("cic = %v, want 42", parsed["cic"])
	}
	if parsed["type"] != 1 {
		t.Errorf("type = %v", parsed["type"])
	}
	if parsed["called"] != "12025551234" {
		t.Errorf("called = %v", parsed["called"])
	}
	if parsed["calling"] != "16175559999" {
		t.Errorf("calling = %v", parsed["calling"])
	}
}

func TestGetCIC(t *testing.T) {
	m := New()
	data := m.BuildISUP(map[string]interface{}{"cic": 1000})
	cic, err := m.GetCIC(data)
	if err != nil {
		t.Fatalf("GetCIC: %v", err)
	}
	if cic != 1000 {
		t.Errorf("CIC = %d, want 1000", cic)
	}
}

func TestGetCICErrors(t *testing.T) {
	m := New()
	if _, err := m.GetCIC(nil); err == nil {
		t.Error("GetCIC(nil) should error")
	}
	if _, err := m.GetCIC([]byte{0x01}); err == nil {
		t.Error("GetCIC short should error")
	}
}

func TestParseISUPMinimal(t *testing.T) {
	m := New()
	// Minimal ISUP: just CIC and type.
	data := m.BuildISUP(map[string]interface{}{"cic": 5, "type": 6})
	parsed, err := m.ParseISUP(data)
	if err != nil {
		t.Fatalf("ParseISUP: %v", err)
	}
	if parsed["cic"] != 5 {
		t.Errorf("cic = %v", parsed["cic"])
	}
	if parsed["type"] != 6 {
		t.Errorf("type = %v", parsed["type"])
	}
}

func TestParseISUPErrors(t *testing.T) {
	m := New()
	if _, err := m.ParseISUP(nil); err == nil {
		t.Error("ParseISUP(nil) should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultSS7()
	d2 := DefaultSS7()
	if d1 != d2 {
		t.Error("DefaultSS7 should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sccp := m.BuildSCCP("12025551234")
			_, _ = m.ParseSCCP(sccp)
			data := m.BuildISUP(map[string]interface{}{"cic": 1, "called": "x"})
			_, _ = m.ParseISUP(data)
			_, _ = m.GetCIC(data)
		}()
	}
	wg.Wait()
}
