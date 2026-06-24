// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the lost module - LoST service mapping.
 */
package lost

import (
	"sync"
	"testing"
)

func TestInit(t *testing.T) {
	m := New()
	if err := m.Init(&LoSTConfig{Server: "lost.example.org", Domain: "example.org"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.server != "lost.example.org" {
		t.Errorf("server = %q", m.server)
	}
	if m.domain != "example.org" {
		t.Errorf("domain = %q", m.domain)
	}
}

func TestFindService(t *testing.T) {
	m := New()
	_ = m.Init(nil)
	r, err := m.FindService("civic:US", "urn:service:sos")
	if err != nil {
		t.Fatalf("FindService: %v", err)
	}
	if r.URI != "sip:sos@example.org" {
		t.Errorf("URI = %q", r.URI)
	}
	if r.Service != "urn:service:sos" {
		t.Errorf("Service = %q", r.Service)
	}
	if r.Display != "Emergency Services" {
		t.Errorf("Display = %q", r.Display)
	}
	// Unknown service errors.
	if _, err := m.FindService("civic:US", "urn:service:unknown"); err == nil {
		t.Error("FindService unknown should error")
	}
}

func TestMapService(t *testing.T) {
	m := New()
	_ = m.Init(nil)
	cases := map[string]string{
		"911":  "urn:service:sos",
		"112":  "urn:service:sos",
		"999":  "urn:service:sos.police",
		"119":  "urn:service:sos.fire",
		"120":  "urn:service:sos.medical",
		"1234": "",
	}
	for in, want := range cases {
		if got := m.MapService(in); got != want {
			t.Errorf("MapService(%q) = %q, want %q", in, got, want)
		}
	}
	// Handles formatted input.
	if got := m.MapService("+1-911"); got != "urn:service:sos" {
		t.Errorf("MapService(+1-911) = %q", got)
	}
}

func TestFindServiceAllDefaults(t *testing.T) {
	m := New()
	_ = m.Init(nil)
	services := []string{
		"urn:service:sos",
		"urn:service:sos.fire",
		"urn:service:sos.police",
		"urn:service:sos.medical",
	}
	for _, s := range services {
		r, err := m.FindService("loc", s)
		if err != nil {
			t.Errorf("FindService(%q): %v", s, err)
		}
		if r.URI == "" {
			t.Errorf("FindService(%q) empty URI", s)
		}
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultLoST()
	d2 := DefaultLoST()
	if d1 != d2 {
		t.Error("DefaultLoST should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	_ = m.Init(nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.FindService("loc", "urn:service:sos")
			_ = m.MapService("911")
		}()
	}
	wg.Wait()
}
