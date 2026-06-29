// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - rtjson module tests.
 */

package rtjson

import (
	"testing"
)

func TestParse(t *testing.T) {
	m := New()
	raw := `{"destinations":["sip:a@example.com","sip:b@example.com"],"mode":"serial","flags":7}`
	r, err := m.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r.Mode != "serial" {
		t.Errorf("Mode = %q, want serial", r.Mode)
	}
	if r.Flags != 7 {
		t.Errorf("Flags = %d, want 7", r.Flags)
	}
	if len(r.Destinations) != 2 || r.Destinations[0] != "sip:a@example.com" {
		t.Errorf("Destinations = %v", r.Destinations)
	}
}

func TestBuild(t *testing.T) {
	m := New()
	r := &RTJSONRoute{
		Destinations: []string{"sip:a@example.com", "sip:b@example.com"},
		Mode:         "parallel",
		Flags:        3,
	}
	out := m.Build(r)
	if out == "" {
		t.Fatal("Build returned empty string")
	}
	// Round-trip back.
	parsed, err := m.Parse(out)
	if err != nil {
		t.Fatalf("Parse(Build) error: %v", err)
	}
	if parsed.Mode != "parallel" {
		t.Errorf("Mode = %q, want parallel", parsed.Mode)
	}
	if parsed.Flags != 3 {
		t.Errorf("Flags = %d, want 3", parsed.Flags)
	}
	if len(parsed.Destinations) != 2 {
		t.Errorf("Destinations len = %d, want 2", len(parsed.Destinations))
	}
}

func TestParseErrors(t *testing.T) {
	m := New()
	if _, err := m.Parse(`{invalid json`); err == nil {
		t.Error("Parse(invalid) expected error")
	}
	if m.Build(nil) != "" {
		t.Error("Build(nil) should return empty")
	}
	// Empty destinations round-trips fine.
	r, err := m.Parse(`{"destinations":null,"mode":"","flags":0}`)
	if err != nil {
		t.Fatalf("Parse(empty) error: %v", err)
	}
	if r.Mode != "" {
		t.Errorf("Mode = %q, want empty", r.Mode)
	}
}
