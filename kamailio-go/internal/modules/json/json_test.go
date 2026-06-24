// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - JSON module tests.
 */

package json

import (
	"strings"
	"sync"
	"testing"
)

const sampleJSON = `{"user":{"name":"alice","age":30},"tags":["a","b","c"],"active":true}`

func TestParse(t *testing.T) {
	m := New()
	v, err := m.Parse(sampleJSON)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", v)
	}
	if _, ok := obj["user"]; !ok {
		t.Errorf("expected user key")
	}
}

func TestParseEmpty(t *testing.T) {
	m := New()
	if _, err := m.Parse(""); err == nil {
		t.Errorf("expected error for empty input")
	}
}

func TestGet(t *testing.T) {
	m := New()
	got, err := m.Get(sampleJSON, "user.name")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got != "alice" {
		t.Errorf("user.name = %q, want %q", got, "alice")
	}
	// numeric value comes back as JSON-encoded number
	if got, err := m.Get(sampleJSON, "user.age"); err != nil {
		t.Fatalf("Get user.age error: %v", err)
	} else if got != "30" {
		t.Errorf("user.age = %q, want %q", got, "30")
	}
	// array element
	if got, err := m.Get(sampleJSON, "tags.1"); err != nil {
		t.Fatalf("Get tags.1 error: %v", err)
	} else if got != "b" {
		t.Errorf("tags.1 = %q, want %q", got, "b")
	}
}

func TestGetMissing(t *testing.T) {
	m := New()
	if _, err := m.Get(sampleJSON, "user.missing"); err == nil {
		t.Errorf("expected error for missing path")
	}
	if _, err := m.Get(sampleJSON, "tags.99"); err == nil {
		t.Errorf("expected error for out-of-range index")
	}
}

func TestSet(t *testing.T) {
	m := New()
	out, err := m.Set(sampleJSON, "user.name", "bob")
	if err != nil {
		t.Fatalf("Set error: %v", err)
	}
	got, err := m.Get(out, "user.name")
	if err != nil {
		t.Fatalf("Get after Set error: %v", err)
	}
	if got != "bob" {
		t.Errorf("after Set user.name = %q, want %q", got, "bob")
	}
	// Setting a numeric-looking value parses it as a number.
	out, err = m.Set(sampleJSON, "user.age", "31")
	if err != nil {
		t.Fatalf("Set age error: %v", err)
	}
	got, err = m.Get(out, "user.age")
	if err != nil {
		t.Fatalf("Get age error: %v", err)
	}
	if got != "31" {
		t.Errorf("after Set user.age = %q, want %q", got, "31")
	}
}

func TestStringify(t *testing.T) {
	m := New()
	in := map[string]interface{}{"k": "v", "n": float64(1)}
	out, err := m.Stringify(in)
	if err != nil {
		t.Fatalf("Stringify error: %v", err)
	}
	if !strings.Contains(out, `"k":"v"`) {
		t.Errorf("Stringify output missing k:v: %s", out)
	}
	if !strings.Contains(out, `"n":1`) {
		t.Errorf("Stringify output missing n:1: %s", out)
	}
}

func TestArrayLength(t *testing.T) {
	m := New()
	n, err := m.ArrayLength(sampleJSON, "tags")
	if err != nil {
		t.Fatalf("ArrayLength error: %v", err)
	}
	if n != 3 {
		t.Errorf("tags length = %d, want 3", n)
	}
	if _, err := m.ArrayLength(sampleJSON, "user"); err == nil {
		t.Errorf("expected error for non-array path")
	}
}

func TestExists(t *testing.T) {
	m := New()
	if !m.Exists(sampleJSON, "user.name") {
		t.Errorf("user.name should exist")
	}
	if m.Exists(sampleJSON, "user.missing") {
		t.Errorf("user.missing should not exist")
	}
	if m.Exists(sampleJSON, "tags.99") {
		t.Errorf("tags.99 should not exist")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Get(sampleJSON, "user.name"); err != nil {
				t.Errorf("concurrent Get error: %v", err)
			}
			if _, err := m.Set(sampleJSON, "user.age", "40"); err != nil {
				t.Errorf("concurrent Set error: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultJSON() == nil {
		t.Fatalf("DefaultJSON() returned nil")
	}
	Init()
	if DefaultJSON() == nil {
		t.Fatalf("DefaultJSON() nil after Init")
	}
	if got, err := DefaultJSON().Get(sampleJSON, "active"); err != nil {
		t.Fatalf("default Get error: %v", err)
	} else if got != "true" {
		t.Errorf("default active = %q, want true", got)
	}
}
