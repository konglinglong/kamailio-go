// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - pdt module tests.
 */

package pdt

import (
	"testing"
)

func TestTranslate(t *testing.T) {
	m := New()
	m.Add("1", "us.example.com")
	m.Add("12", "ny.us.example.com")
	m.Add("44", "uk.example.com")

	if domain, rest, ok := m.Translate("1212555"); !ok || domain != "ny.us.example.com" || rest != "12555" {
		t.Errorf("Translate(1212555) = %q,%q,%v, want ny.us.example.com,12555,true", domain, rest, ok)
	}
	if domain, rest, ok := m.Translate("15551234"); !ok || domain != "us.example.com" || rest != "5551234" {
		t.Errorf("Translate(15551234) = %q,%q,%v, want us.example.com,5551234,true", domain, rest, ok)
	}
	if domain, rest, ok := m.Translate("441234"); !ok || domain != "uk.example.com" || rest != "1234" {
		t.Errorf("Translate(441234) = %q,%q,%v, want uk.example.com,1234,true", domain, rest, ok)
	}
	if _, _, ok := m.Translate("99999"); ok {
		t.Error("Translate(99999) should not match")
	}
}

func TestRemoveAndList(t *testing.T) {
	m := New()
	m.Add("1", "us")
	m.Add("44", "uk")
	if !m.Remove("1") {
		t.Error("Remove(1) = false, want true")
	}
	if m.Remove("1") {
		t.Error("Remove(1) twice = true, want false")
	}
	lst := m.List()
	if len(lst) != 1 || lst["44"] != "uk" {
		t.Errorf("List = %v, want 44->uk", lst)
	}
}

func TestEmptyNumber(t *testing.T) {
	m := New()
	m.Add("1", "us")
	if _, _, ok := m.Translate(""); ok {
		t.Error("Translate(empty) should not match")
	}
}
