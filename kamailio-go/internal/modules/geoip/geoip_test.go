// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the geoip module - IP geolocation lookups.
 */
package geoip

import (
	"sync"
	"testing"
)

func TestInit(t *testing.T) {
	m := New()
	if err := m.Init("/tmp/nonexistent.mmdb"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.dbPath != "/tmp/nonexistent.mmdb" {
		t.Errorf("dbPath = %q", m.dbPath)
	}
}

func TestLookup(t *testing.T) {
	m := New()
	_ = m.Init("")
	r, err := m.Lookup("8.8.8.8")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if r.CountryCode != "US" {
		t.Errorf("CountryCode = %q, want US", r.CountryCode)
	}
	if r.City != "Mountain View" {
		t.Errorf("City = %q", r.City)
	}
	if r.Latitude == "" {
		t.Error("Latitude empty")
	}
	// Mutating result must not affect the store.
	r.CountryCode = "ZZ"
	r2, _ := m.Lookup("8.8.8.8")
	if r2.CountryCode != "US" {
		t.Errorf("store mutated by caller: %q", r2.CountryCode)
	}
}

func TestLookupCountryAndCity(t *testing.T) {
	m := New()
	_ = m.Init("")
	c, err := m.LookupCountry("1.1.1.1")
	if err != nil {
		t.Fatalf("LookupCountry: %v", err)
	}
	if c != "AU" {
		t.Errorf("LookupCountry = %q, want AU", c)
	}
	city, err := m.LookupCity("1.1.1.1")
	if err != nil {
		t.Fatalf("LookupCity: %v", err)
	}
	if city != "South Brisbane" {
		t.Errorf("LookupCity = %q", city)
	}
}

func TestLookupErrors(t *testing.T) {
	m := New()
	_ = m.Init("")
	if _, err := m.Lookup("not-an-ip"); err == nil {
		t.Error("Lookup(invalid) should error")
	}
	if _, err := m.Lookup("10.0.0.1"); err == nil {
		t.Error("Lookup(unknown) should error")
	}
	if _, err := m.LookupCountry("bad"); err == nil {
		t.Error("LookupCountry(invalid) should error")
	}
}

func TestClose(t *testing.T) {
	m := New()
	_ = m.Init("")
	m.Close()
	// After close, records is nil; Lookup should error.
	if _, err := m.Lookup("8.8.8.8"); err == nil {
		t.Error("Lookup after Close should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultGeoIP()
	d2 := DefaultGeoIP()
	if d1 != d2 {
		t.Error("DefaultGeoIP should return same instance")
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
			_, _ = m.Lookup("8.8.8.8")
			_, _ = m.LookupCountry("1.1.1.1")
			_, _ = m.LookupCity("8.8.8.8")
		}()
	}
	wg.Wait()
}
