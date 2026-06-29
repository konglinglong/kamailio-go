// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the pdb module - prefix database carrier lookup.
 */
package pdb

import (
	"sync"
	"testing"
)

func TestInit(t *testing.T) {
	m := New()
	if err := m.Init("/tmp/nonexistent.pdb"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.dbPath != "/tmp/nonexistent.pdb" {
		t.Errorf("dbPath = %q", m.dbPath)
	}
	if len(m.entries) == 0 {
		t.Error("Init should load mock data")
	}
}

func TestLookupLongestPrefix(t *testing.T) {
	m := New()
	_ = m.Init("")
	// "1202555" is a longer prefix than "1202" and should win.
	e, err := m.Lookup("+12025551234")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if e.Number != "1202555" {
		t.Errorf("Number = %q, want 1202555 (longest prefix)", e.Number)
	}
	if e.Carrier != "Verizon" {
		t.Errorf("Carrier = %q", e.Carrier)
	}
	if e.Description != "Washington DC local" {
		t.Errorf("Description = %q", e.Description)
	}
}

func TestLookupShorterPrefix(t *testing.T) {
	m := New()
	_ = m.Init("")
	e, err := m.Lookup("12125559999")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if e.Number != "1212" {
		t.Errorf("Number = %q, want 1212", e.Number)
	}
	if e.Carrier != "AT&T" {
		t.Errorf("Carrier = %q", e.Carrier)
	}
}

func TestLookupCarrier(t *testing.T) {
	m := New()
	_ = m.Init("")
	c, err := m.LookupCarrier("+447911123456")
	if err != nil {
		t.Fatalf("LookupCarrier: %v", err)
	}
	if c != "Vodafone" {
		t.Errorf("LookupCarrier = %q, want Vodafone", c)
	}
}

func TestLookupErrors(t *testing.T) {
	m := New()
	_ = m.Init("")
	if _, err := m.Lookup(""); err == nil {
		t.Error("Lookup(empty) should error")
	}
	if _, err := m.Lookup("0000"); err == nil {
		t.Error("Lookup(unknown) should error")
	}
}

func TestClose(t *testing.T) {
	m := New()
	_ = m.Init("")
	m.Close()
	if _, err := m.Lookup("1202"); err == nil {
		t.Error("Lookup after Close should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultPDB()
	d2 := DefaultPDB()
	if d1 != d2 {
		t.Error("DefaultPDB should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	_ = m.Init("")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Lookup("+12025551234")
			_, _ = m.LookupCarrier("+447911123456")
		}()
	}
	wg.Wait()
}
