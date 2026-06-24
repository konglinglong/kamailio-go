// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the jansson module.
 */

package jansson

import (
	"strings"
	"sync"
	"testing"
)

func TestParseAndStringify(t *testing.T) {
	m := New()

	v, err := m.Parse(`{"name":"alice","age":30}`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("Parse did not return an object")
	}
	if obj["name"] != "alice" {
		t.Errorf("Parse()[name] = %v, want alice", obj["name"])
	}

	s, err := m.Stringify(map[string]interface{}{"k": "v"})
	if err != nil {
		t.Fatalf("Stringify error: %v", err)
	}
	if s != `{"k":"v"}` {
		t.Errorf("Stringify = %q, want %q", s, `{"k":"v"}`)
	}

	if _, err := m.Parse(""); err == nil {
		t.Errorf("Parse(\"\") should error")
	}
	if _, err := m.Parse("{bad"); err == nil {
		t.Errorf("Parse({bad) should error")
	}
}

func TestGet(t *testing.T) {
	m := New()
	j := `{"user":{"name":"alice","tags":["a","b"]},"count":3}`

	v, err := m.Get(j, "user.name")
	if err != nil {
		t.Fatalf("Get(user.name) error: %v", err)
	}
	if v != "alice" {
		t.Errorf("Get(user.name) = %q, want %q", v, "alice")
	}

	v, _ = m.Get(j, "count")
	if v != "3" {
		t.Errorf("Get(count) = %q, want %q", v, "3")
	}

	v, _ = m.Get(j, "user.tags")
	if v != `["a","b"]` {
		t.Errorf("Get(user.tags) = %q, want %q", v, `["a","b"]`)
	}

	if _, err := m.Get(j, "user.missing"); err == nil {
		t.Errorf("Get(user.missing) should error")
	}
}

func TestSet(t *testing.T) {
	m := New()
	j := `{"user":{"name":"alice"}}`

	out, err := m.Set(j, "user.name", "bob")
	if err != nil {
		t.Fatalf("Set error: %v", err)
	}
	if !strings.Contains(out, `"name":"bob"`) {
		t.Errorf("Set output = %q, want name:bob", out)
	}

	// Set a new nested key.
	out, err = m.Set(j, "user.age", "25")
	if err != nil {
		t.Fatalf("Set(new key) error: %v", err)
	}
	if !strings.Contains(out, `"age":25`) {
		t.Errorf("Set output = %q, want age:25", out)
	}

	// Set with a JSON object value.
	out, _ = m.Set(j, "user.meta", `{"x":1}`)
	if !strings.Contains(out, `"meta":{"x":1}`) {
		t.Errorf("Set output = %q, want meta object", out)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultJansson()
	if d == nil {
		t.Fatalf("DefaultJansson() returned nil")
	}
	v, err := Parse(`{"a":1}`)
	if err != nil || v == nil {
		t.Errorf("package Parse = %v,%v", v, err)
	}
	s, _ := Get(`{"a":{"b":"c"}}`, "a.b")
	if s != "c" {
		t.Errorf("package Get(a.b) = %q, want %q", s, "c")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultJansson()
	j := `{"a":{"b":"1"}}`
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			shared.Get(j, "a.b")
			shared.Set(j, "a.b", itoa(i))
			shared.Parse(j)
			shared.Stringify(map[string]interface{}{"i": i})
		}(i)
	}
	wg.Wait()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
