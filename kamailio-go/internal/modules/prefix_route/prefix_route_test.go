// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - PrefixRoute module tests.
 */
package prefix_route

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

var inviteBytes = []byte("INVITE sip:1001@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776asdhds\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: call-1234@10.0.0.1\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@10.0.0.1>\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

// TestAddRouteAndCount verifies insertion, ID assignment and counting.
func TestAddRouteAndCount(t *testing.T) {
	m := NewPrefixRouteModule()
	id1 := m.AddRoute(&PrefixRoute{Prefix: "1", Route: "route1", Enabled: true})
	id2 := m.AddRoute(&PrefixRoute{Prefix: "10", Route: "route2", Enabled: true})
	if id1 == id2 {
		t.Errorf("expected distinct IDs, got %d == %d", id1, id2)
	}
	if got := m.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	if got := len(m.ListRoutes()); got != 2 {
		t.Errorf("ListRoutes len = %d, want 2", got)
	}
	if m.AddRoute(nil) != -1 {
		t.Error("expected -1 for nil route")
	}
}

// TestMatch verifies matching by the R-URI user part.
func TestMatch(t *testing.T) {
	m := NewPrefixRouteModule()
	m.AddRoute(&PrefixRoute{Prefix: "1", Route: "route1", Enabled: true})
	msg := mustParseMsg(t, inviteBytes) // R-URI user "1001"
	r, err := m.Match(msg)
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if r.Route != "route1" {
		t.Errorf("Route = %q, want route1", r.Route)
	}
	// Nil message errors.
	if _, err := m.Match(nil); err == nil {
		t.Error("expected error for nil message")
	}
}

// TestMatchPrefixLongest verifies the longest prefix wins.
func TestMatchPrefixLongest(t *testing.T) {
	m := NewPrefixRouteModule()
	m.AddRoute(&PrefixRoute{Prefix: "1", Route: "short", Enabled: true})
	m.AddRoute(&PrefixRoute{Prefix: "1001", Route: "exact", Enabled: true})
	r, err := m.MatchPrefix("1001")
	if err != nil {
		t.Fatalf("MatchPrefix failed: %v", err)
	}
	if r.Route != "exact" {
		t.Errorf("Route = %q, want exact (longest prefix)", r.Route)
	}
}

// TestMatchPrefixPriority verifies priority breaks ties on equal-length
// prefixes.
func TestMatchPrefixPriority(t *testing.T) {
	m := NewPrefixRouteModule()
	m.AddRoute(&PrefixRoute{Prefix: "1", Route: "low", Priority: 5, Enabled: true})
	m.AddRoute(&PrefixRoute{Prefix: "1", Route: "high", Priority: 10, Enabled: true})
	r, err := m.MatchPrefix("1234")
	if err != nil {
		t.Fatalf("MatchPrefix failed: %v", err)
	}
	if r.Route != "high" {
		t.Errorf("Route = %q, want high (higher priority)", r.Route)
	}
}

// TestMatchPrefixNoMatch verifies an error when nothing matches.
func TestMatchPrefixNoMatch(t *testing.T) {
	m := NewPrefixRouteModule()
	m.AddRoute(&PrefixRoute{Prefix: "9999", Route: "route1", Enabled: true})
	if _, err := m.MatchPrefix("1234"); err == nil {
		t.Error("expected error for no matching route")
	}
}

// TestRemoveRoute verifies removal by prefix.
func TestRemoveRoute(t *testing.T) {
	m := NewPrefixRouteModule()
	m.AddRoute(&PrefixRoute{Prefix: "1", Route: "route1", Enabled: true})
	if !m.RemoveRoute("1") {
		t.Fatal("expected RemoveRoute true")
	}
	if m.Count() != 0 {
		t.Fatalf("expected count 0, got %d", m.Count())
	}
	if m.RemoveRoute("1") {
		t.Error("expected RemoveRoute false for already removed")
	}
}

// TestEnableDisable verifies enabling/disabling routes by prefix.
func TestEnableDisable(t *testing.T) {
	m := NewPrefixRouteModule()
	m.AddRoute(&PrefixRoute{Prefix: "1", Route: "route1", Enabled: true})
	if !m.DisableRoute("1") {
		t.Fatal("expected DisableRoute true")
	}
	if _, err := m.MatchPrefix("1234"); err == nil {
		t.Error("expected error when route disabled")
	}
	if !m.EnableRoute("1") {
		t.Fatal("expected EnableRoute true")
	}
	r, err := m.MatchPrefix("1234")
	if err != nil {
		t.Fatalf("MatchPrefix failed: %v", err)
	}
	if r.Route != "route1" {
		t.Errorf("Route = %q, want route1", r.Route)
	}
	if m.EnableRoute("missing") {
		t.Error("expected EnableRoute false for unknown prefix")
	}
}

// TestMatchReturnsCopy verifies the returned route is a copy.
func TestMatchReturnsCopy(t *testing.T) {
	m := NewPrefixRouteModule()
	m.AddRoute(&PrefixRoute{Prefix: "1", Route: "route1", Description: "orig", Enabled: true})
	r, err := m.MatchPrefix("1234")
	if err != nil {
		t.Fatalf("MatchPrefix failed: %v", err)
	}
	r.Description = "mutated"
	if m.ListRoutes()[0].Description == "mutated" {
		t.Fatal("expected isolation from Match copy")
	}
}

// TestLoadFromCSV verifies CSV loading.
func TestLoadFromCSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "routes.csv")
	content := "prefix,route,description,priority,enabled\n" +
		"1,route1,short,5,1\n" +
		"1001,route2,exact,10,1\n" +
		"9999,route3,disabled,0,0\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := NewPrefixRouteModule()
	if err := m.LoadFromCSV(p); err != nil {
		t.Fatalf("LoadFromCSV: %v", err)
	}
	if m.Count() != 3 {
		t.Fatalf("expected 3 routes, got %d", m.Count())
	}
	// Longest prefix wins.
	r, err := m.MatchPrefix("1001")
	if err != nil {
		t.Fatalf("MatchPrefix failed: %v", err)
	}
	if r.Route != "route2" {
		t.Errorf("Route = %q, want route2", r.Route)
	}
	// The disabled route (9999) does not match.
	if _, err := m.MatchPrefix("9999"); err == nil {
		t.Error("expected error for disabled route")
	}
}

// TestLoadFromCSV_Errors verifies error handling.
func TestLoadFromCSV_Errors(t *testing.T) {
	m := NewPrefixRouteModule()
	if err := m.LoadFromCSV(""); err == nil {
		t.Fatal("expected error for empty path")
	}
	if err := m.LoadFromCSV("/nonexistent/path/file.csv"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	Init()
	p := DefaultPrefixRoute()
	if p == nil {
		t.Fatal("expected non-nil default prefix_route")
	}
	p.AddRoute(&PrefixRoute{Prefix: "1", Route: "route1", Enabled: true})
	if p.Count() != 1 {
		t.Errorf("Count = %d, want 1", p.Count())
	}
}

// TestConcurrentAccess exercises the module under the race detector.
func TestConcurrentAccess(t *testing.T) {
	m := NewPrefixRouteModule()
	m.AddRoute(&PrefixRoute{Prefix: "1", Route: "route1", Enabled: true})
	msg := mustParseMsg(t, inviteBytes)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Match(msg)
			_, _ = m.MatchPrefix("1234")
			_ = m.ListRoutes()
			_ = m.Count()
			m.DisableRoute("1")
			m.EnableRoute("1")
		}()
	}
	wg.Wait()
}
