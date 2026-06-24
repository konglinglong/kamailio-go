// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Path module.
 */

package path

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// addHeader appends a header to msg and wires up the matching quick-access
// pointer, mirroring what the full parser does.
func addHeader(msg *parser.SIPMsg, name, value string) *parser.HdrField {
	h := msg.AddHeader(name, value)
	switch h.Type {
	case parser.HdrPath:
		if msg.Path == nil {
			msg.Path = h
		}
	case parser.HdrVia:
		if msg.HdrVia1 == nil {
			msg.HdrVia1 = h
		}
	}
	return h
}

func TestAddPathHeader(t *testing.T) {
	m := New()

	msg := &parser.SIPMsg{}
	if ret := m.AddPathHeader(msg, "sip:proxy.example.com:5060;lr"); ret != 0 {
		t.Fatalf("AddPathHeader() = %d, want 0", ret)
	}

	paths := msg.GetAllHeadersByType(parser.HdrPath)
	if len(paths) != 1 {
		t.Fatalf("expected 1 Path header, got %d", len(paths))
	}
	// Bare URI gets wrapped in angle brackets.
	want := "<sip:proxy.example.com:5060;lr>"
	if got := paths[0].Body.String(); got != want {
		t.Errorf("Path body = %q, want %q", got, want)
	}
	if msg.Path == nil || msg.Path != paths[0] {
		t.Errorf("msg.Path quick ref not wired to the new header")
	}

	// Already bracketed URI is left untouched.
	msg2 := &parser.SIPMsg{}
	if ret := m.AddPathHeader(msg2, "<sip:other.example.com;lr>"); ret != 0 {
		t.Fatalf("AddPathHeader(bracketed) = %d, want 0", ret)
	}
	if got := msg2.Path.Body.String(); got != "<sip:other.example.com;lr>" {
		t.Errorf("bracketed Path body = %q, want unchanged", got)
	}

	// Empty URI -> -1.
	if ret := m.AddPathHeader(&parser.SIPMsg{}, ""); ret != -1 {
		t.Errorf("AddPathHeader(empty) = %d, want -1", ret)
	}
	// nil message -> -1.
	if ret := m.AddPathHeader(nil, "sip:x"); ret != -1 {
		t.Errorf("AddPathHeader(nil) = %d, want -1", ret)
	}
}

func TestAddPathReceived(t *testing.T) {
	m := New()

	// Message with a Via carrying a received parameter.
	msg := &parser.SIPMsg{}
	addHeader(msg, "Via", "SIP/2.0/UDP 192.0.2.1;received=198.51.100.1")

	if ret := m.AddPathReceived(msg); ret != 0 {
		t.Fatalf("AddPathReceived() = %d, want 0", ret)
	}
	paths := msg.GetAllHeadersByType(parser.HdrPath)
	if len(paths) != 1 {
		t.Fatalf("expected 1 Path header, got %d", len(paths))
	}
	body := paths[0].Body.String()
	if body == "" {
		t.Fatalf("Path body is empty")
	}
	// The received parameter must be present on the Path header.
	if !containsParam(body, "received") {
		t.Errorf("Path body %q does not contain a received parameter", body)
	}
	// The received value from the Via must be carried through.
	if !containsReceivedValue(body, "198.51.100.1") {
		t.Errorf("Path body %q does not carry received=198.51.100.1", body)
	}

	// No Via at all -> still adds a Path header (received empty).
	msg2 := &parser.SIPMsg{}
	if ret := m.AddPathReceived(msg2); ret != 0 {
		t.Fatalf("AddPathReceived() without Via = %d, want 0", ret)
	}
	if msg2.Path == nil {
		t.Errorf("Path header should be added even without Via")
	}

	// nil message -> -1.
	if ret := m.AddPathReceived(nil); ret != -1 {
		t.Errorf("AddPathReceived(nil) = %d, want -1", ret)
	}
}

func TestProcessPath(t *testing.T) {
	m := New()

	msg := &parser.SIPMsg{}
	addHeader(msg, "Path", "<sip:proxy1.example.com;lr>")
	addHeader(msg, "Path", "<sip:proxy2.example.com:5060;lr>")

	uris := m.ProcessPath(msg)
	if len(uris) != 2 {
		t.Fatalf("ProcessPath() returned %d URIs, want 2", len(uris))
	}
	if uris[0] != "<sip:proxy1.example.com;lr>" {
		t.Errorf("ProcessPath()[0] = %q, want %q", uris[0], "<sip:proxy1.example.com;lr>")
	}
	if uris[1] != "<sip:proxy2.example.com:5060;lr>" {
		t.Errorf("ProcessPath()[1] = %q, want %q", uris[1], "<sip:proxy2.example.com:5060;lr>")
	}

	// No Path headers -> empty slice (not nil) is acceptable.
	empty := m.ProcessPath(&parser.SIPMsg{})
	if len(empty) != 0 {
		t.Errorf("ProcessPath() on empty msg = %v, want empty", empty)
	}

	// nil message -> empty.
	if got := m.ProcessPath(nil); len(got) != 0 {
		t.Errorf("ProcessPath(nil) = %v, want empty", got)
	}
}

func TestHandlePath(t *testing.T) {
	m := New()

	msg := &parser.SIPMsg{}
	addHeader(msg, "Path", "<sip:proxy.example.com;lr>")
	if !m.HandlePath(msg) {
		t.Errorf("HandlePath() = false, want true (Path present)")
	}

	empty := &parser.SIPMsg{}
	if m.HandlePath(empty) {
		t.Errorf("HandlePath() on empty msg = true, want false")
	}

	if m.HandlePath(nil) {
		t.Errorf("HandlePath(nil) = true, want false")
	}
}

func TestSetPathParams(t *testing.T) {
	m := New()

	msg := &parser.SIPMsg{}
	addHeader(msg, "Path", "<sip:proxy.example.com;lr>")

	if ret := m.SetPathParams(msg, "ftag=abc;r2=on"); ret != 0 {
		t.Fatalf("SetPathParams() = %d, want 0", ret)
	}
	paths := msg.GetAllHeadersByType(parser.HdrPath)
	if len(paths) != 1 {
		t.Fatalf("expected 1 Path header, got %d", len(paths))
	}
	want := "<sip:proxy.example.com;lr>;ftag=abc;r2=on"
	if got := paths[0].Body.String(); got != want {
		t.Errorf("after SetPathParams body = %q, want %q", got, want)
	}

	// No Path header -> -1.
	if ret := m.SetPathParams(&parser.SIPMsg{}, "x=y"); ret != -1 {
		t.Errorf("SetPathParams() without Path = %d, want -1", ret)
	}
	// Empty params -> -1.
	if ret := m.SetPathParams(msg, ""); ret != -1 {
		t.Errorf("SetPathParams(empty) = %d, want -1", ret)
	}
	// nil message -> -1.
	if ret := m.SetPathParams(nil, "x=y"); ret != -1 {
		t.Errorf("SetPathParams(nil) = %d, want -1", ret)
	}
}

func TestDefaultAndInit(t *testing.T) {
	// DefaultPath returns a configured module.
	Init()
	d := DefaultPath()
	if d == nil {
		t.Fatalf("DefaultPath() returned nil")
	}
	if d != DefaultPath() {
		t.Fatalf("DefaultPath() returned different instances after Init()")
	}

	// DefaultPath() exposes the default received-name parameter.
	if d.ReceivedName != "received" {
		t.Errorf("DefaultPath().ReceivedName = %q, want %q", d.ReceivedName, "received")
	}

	// Package-level wrappers delegate to the default module.
	Init()
	msg := &parser.SIPMsg{}
	if ret := AddPathHeader(msg, "sip:g.example.com;lr"); ret != 0 {
		t.Fatalf("package AddPathHeader() = %d, want 0", ret)
	}
	if !HandlePath(msg) {
		t.Errorf("package HandlePath() = false, want true")
	}
}

func TestConcurrent(t *testing.T) {
	// Exercise the module under concurrency to validate -race safety.
	Init()
	shared := DefaultPath()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			m := &parser.SIPMsg{}
			shared.AddPathHeader(m, fmt.Sprintf("sip:p%d.example.com;lr", i))
			shared.HandlePath(m)
			shared.ProcessPath(m)
		}(i)
	}
	wg.Wait()
}

// containsParam reports whether a Path header body contains the named
// parameter (case-insensitive).
func containsParam(body, name string) bool {
	for i := 0; i+len(name) <= len(body); i++ {
		if equalFoldAt(body, i, name) {
			// must be preceded by ';' or '<' boundary and followed by
			// '=', ';' or end.
			return true
		}
	}
	return false
}

// containsReceivedValue reports whether body contains received=<value>.
func containsReceivedValue(body, value string) bool {
	idx := indexFold(body, "received=")
	if idx < 0 {
		return false
	}
	rest := body[idx+len("received="):]
	if len(rest) < len(value) {
		return false
	}
	return equalFoldAt(rest, 0, value)
}

func equalFoldAt(s string, i int, sub string) bool {
	if i+len(sub) > len(s) {
		return false
	}
	for j := 0; j < len(sub); j++ {
		a := s[i+j]
		b := sub[j]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFoldAt(s, i, sub) {
			return i
		}
	}
	return -1
}
