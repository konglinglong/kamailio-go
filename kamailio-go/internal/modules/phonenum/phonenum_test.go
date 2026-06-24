// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the phonenum module - phone number parsing/validation.
 */
package phonenum

import (
	"sync"
	"testing"
)

func TestParseInternational(t *testing.T) {
	m := New()
	info, err := m.Parse("+12025551234", "US")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.CountryCode != "1" {
		t.Errorf("CountryCode = %q, want 1", info.CountryCode)
	}
	if info.NationalNumber != "2025551234" {
		t.Errorf("NationalNumber = %q", info.NationalNumber)
	}
	if info.IsValid != "true" {
		t.Errorf("IsValid = %q", info.IsValid)
	}
	if info.Number != "+12025551234" {
		t.Errorf("Number = %q", info.Number)
	}
}

func TestParseNational(t *testing.T) {
	m := New()
	info, err := m.Parse("2025551234", "US")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.CountryCode != "1" {
		t.Errorf("CountryCode = %q", info.CountryCode)
	}
	if info.NationalNumber != "2025551234" {
		t.Errorf("NationalNumber = %q", info.NationalNumber)
	}
}

func TestParseWithFormatting(t *testing.T) {
	m := New()
	info, err := m.Parse("+1 (202) 555-1234", "US")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.NationalNumber != "2025551234" {
		t.Errorf("NationalNumber = %q", info.NationalNumber)
	}
}

func TestIsValid(t *testing.T) {
	m := New()
	if !m.IsValid("+12025551234", "US") {
		t.Error("valid US number should be valid")
	}
	if m.IsValid("+1234", "US") {
		t.Error("short number should be invalid")
	}
	if m.IsValid("not-a-number", "US") {
		t.Error("non-number should be invalid")
	}
}

func TestGetCountryAndType(t *testing.T) {
	m := New()
	cc, err := m.GetCountry("+447911123456")
	if err != nil {
		t.Fatalf("GetCountry: %v", err)
	}
	if cc != "44" {
		t.Errorf("GetCountry = %q, want 44", cc)
	}
	typ, err := m.GetType("+447911123456")
	if err != nil {
		t.Fatalf("GetType: %v", err)
	}
	if typ != "mobile" {
		t.Errorf("GetType = %q, want mobile", typ)
	}
}

func TestFormat(t *testing.T) {
	m := New()
	e164, err := m.Format("+12025551234", "e164")
	if err != nil {
		t.Fatalf("Format e164: %v", err)
	}
	if e164 != "+12025551234" {
		t.Errorf("e164 = %q", e164)
	}
	nat, _ := m.Format("+12025551234", "national")
	if nat != "2025551234" {
		t.Errorf("national = %q", nat)
	}
	intl, _ := m.Format("+12025551234", "international")
	if intl != "+1 2025551234" {
		t.Errorf("international = %q", intl)
	}
	if _, err := m.Format("+12025551234", "bogus"); err == nil {
		t.Error("Format with unknown format should error")
	}
}

func TestParseErrors(t *testing.T) {
	m := New()
	if _, err := m.Parse("", "US"); err == nil {
		t.Error("Parse(empty) should error")
	}
	if _, err := m.Parse("2025551234", "ZZ"); err == nil {
		t.Error("Parse with unknown region should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultPhoneNum()
	d2 := DefaultPhoneNum()
	if d1 != d2 {
		t.Error("DefaultPhoneNum should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Parse("+12025551234", "US")
			_ = m.IsValid("+447911123456", "GB")
			_, _ = m.Format("+12025551234", "e164")
		}()
	}
	wg.Wait()
}
