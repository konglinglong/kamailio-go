// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the xmlops module - XML parse/query/build/validate.
 */
package xmlops

import (
	"strings"
	"sync"
	"testing"
)

const sampleXML = `<?xml version="1.0"?>
<presence entity="pres:alice@example.com">
  <tuple id="t1">
    <status>
      <basic>open</basic>
    </status>
  </tuple>
</presence>`

func TestParse(t *testing.T) {
	m := New()
	v, err := m.Parse(sampleXML)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	mp, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("Parse returned %T", v)
	}
	if _, ok := mp["presence"]; !ok {
		t.Errorf("Parse missing presence key: %+v", mp)
	}
}

func TestGet(t *testing.T) {
	m := New()
	got, err := m.Get(sampleXML, "presence.tuple.status.basic")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "open" {
		t.Errorf("Get = %q, want open", got)
	}
	// Missing path errors.
	if _, err := m.Get(sampleXML, "presence.tuple.status.missing"); err == nil {
		t.Error("Get on missing path should error")
	}
}

func TestSet(t *testing.T) {
	m := New()
	out, err := m.Set(sampleXML, "presence.tuple.status.basic", "closed")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !strings.Contains(out, "closed") {
		t.Errorf("Set output missing 'closed': %s", out)
	}
	// Re-query the modified XML.
	got, err := m.Get(out, "presence.tuple.status.basic")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if got != "closed" {
		t.Errorf("Get after Set = %q, want closed", got)
	}
}

func TestSetCreatePath(t *testing.T) {
	m := New()
	out, err := m.Set(sampleXML, "presence.tuple.note", "away")
	if err != nil {
		t.Fatalf("Set create: %v", err)
	}
	got, err := m.Get(out, "presence.tuple.note")
	if err != nil {
		t.Fatalf("Get created: %v", err)
	}
	if got != "away" {
		t.Errorf("Get created = %q, want away", got)
	}
}

func TestBuild(t *testing.T) {
	m := New()
	data := map[string]interface{}{
		"presence": map[string]interface{}{
			"_attrs": map[string]string{"entity": "pres:bob@example.com"},
			"tuple": map[string]interface{}{
				"_attrs": map[string]string{"id": "t1"},
				"status": map[string]interface{}{
					"basic": map[string]interface{}{"_text": "open"},
				},
			},
		},
	}
	out, err := m.Build(data)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(out, "pres:bob@example.com") {
		t.Errorf("Build missing entity: %s", out)
	}
	if !strings.Contains(out, "<basic>open</basic>") {
		t.Errorf("Build missing basic: %s", out)
	}
	// Built XML should be valid.
	if !m.Validate(out, "") {
		t.Error("Build output should validate")
	}
}

func TestValidate(t *testing.T) {
	m := New()
	if !m.Validate(sampleXML, "") {
		t.Error("valid XML should validate")
	}
	if m.Validate("<<not xml>>", "") {
		t.Error("invalid XML should not validate")
	}
	if m.Validate("", "") {
		t.Error("empty XML should not validate")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultXMLOps()
	d2 := DefaultXMLOps()
	if d1 != d2 {
		t.Error("DefaultXMLOps should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Parse(sampleXML)
			_, _ = m.Get(sampleXML, "presence.tuple.status.basic")
			_, _ = m.Set(sampleXML, "presence.tuple.status.basic", "closed")
			_ = m.Validate(sampleXML, "")
		}()
	}
	wg.Wait()
}
