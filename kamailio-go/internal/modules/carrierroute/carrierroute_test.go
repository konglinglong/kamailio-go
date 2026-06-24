// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - CarrierRoute module tests.
 */
package carrierroute

import (
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

// TestAddCarrier verifies carrier creation and counting.
func TestAddCarrier(t *testing.T) {
	m := NewCarrierRouteModule()
	c1 := m.AddCarrier("carrier1")
	c2 := m.AddCarrier("carrier2")
	if c1 == nil || c2 == nil {
		t.Fatal("expected non-nil carriers")
	}
	if c1.ID == c2.ID {
		t.Errorf("expected distinct IDs, got %d == %d", c1.ID, c2.ID)
	}
	if c1.Name != "carrier1" {
		t.Errorf("Name = %q, want carrier1", c1.Name)
	}
	if got := m.CountCarriers(); got != 2 {
		t.Errorf("CountCarriers = %d, want 2", got)
	}
	if m.AddCarrier("") != nil {
		t.Error("expected nil for empty name")
	}
}

// TestAddRouteAndCount verifies route insertion and counting.
func TestAddRouteAndCount(t *testing.T) {
	m := NewCarrierRouteModule()
	c := m.AddCarrier("carrier1")
	id1 := m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "1", Host: "gw1.example.com", Port: 5060})
	id2 := m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "10", Host: "gw2.example.com", Port: 5060})
	if id1 == id2 {
		t.Errorf("expected distinct route IDs, got %d == %d", id1, id2)
	}
	if got := m.CountRoutes(); got != 2 {
		t.Errorf("CountRoutes = %d, want 2", got)
	}
	if m.AddRoute(nil) != -1 {
		t.Error("expected -1 for nil route")
	}
}

// TestRoute verifies prefix-based routing falls back to the R-URI user.
func TestRoute(t *testing.T) {
	m := NewCarrierRouteModule()
	c := m.AddCarrier("carrier1")
	m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "1", Host: "gw1.example.com", Port: 5060, Priority: 1})
	// R-URI user is "1001"; an empty prefix falls back to it.
	msg := mustParseMsg(t, inviteBytes)
	r, err := m.Route(msg, "pstn", "")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if r.Host != "gw1.example.com" {
		t.Errorf("Host = %q, want gw1.example.com", r.Host)
	}
	// Explicit prefix also works.
	r, err = m.Route(nil, "pstn", "1234")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if r.Host != "gw1.example.com" {
		t.Errorf("Host = %q, want gw1.example.com", r.Host)
	}
}

// TestRouteLongestPrefix verifies the longest ScanPrefix wins.
func TestRouteLongestPrefix(t *testing.T) {
	m := NewCarrierRouteModule()
	c := m.AddCarrier("carrier1")
	m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "1", Host: "short.example.com", Port: 5060})
	m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "1001", Host: "long.example.com", Port: 5060})
	r, err := m.Route(nil, "pstn", "1001")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if r.Host != "long.example.com" {
		t.Errorf("Host = %q, want long.example.com (longest prefix)", r.Host)
	}
}

// TestRoutePriority verifies priority breaks ties.
func TestRoutePriority(t *testing.T) {
	m := NewCarrierRouteModule()
	c := m.AddCarrier("carrier1")
	m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "1", Host: "low.example.com", Port: 5060, Priority: 5})
	m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "1", Host: "high.example.com", Port: 5060, Priority: 10})
	r, err := m.Route(nil, "pstn", "1234")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if r.Host != "high.example.com" {
		t.Errorf("Host = %q, want high.example.com (higher priority)", r.Host)
	}
}

// TestRouteByCarrier verifies routing restricted to a carrier.
func TestRouteByCarrier(t *testing.T) {
	m := NewCarrierRouteModule()
	c1 := m.AddCarrier("carrier1")
	c2 := m.AddCarrier("carrier2")
	m.AddRoute(&RouteEntry{CarrierID: c1.ID, Domain: "pstn", ScanPrefix: "1", Host: "c1.example.com", Port: 5060})
	m.AddRoute(&RouteEntry{CarrierID: c2.ID, Domain: "pstn", ScanPrefix: "1", Host: "c2.example.com", Port: 5060})
	r, err := m.RouteByCarrier(c2.ID, "pstn", "1234")
	if err != nil {
		t.Fatalf("RouteByCarrier failed: %v", err)
	}
	if r.Host != "c2.example.com" {
		t.Errorf("Host = %q, want c2.example.com", r.Host)
	}
	// Unknown carrier errors.
	if _, err := m.RouteByCarrier(999, "pstn", "1234"); err == nil {
		t.Error("expected error for unknown carrier")
	}
}

// TestMarkRoute verifies toggling route active state.
func TestMarkRoute(t *testing.T) {
	m := NewCarrierRouteModule()
	c := m.AddCarrier("carrier1")
	id := m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "1", Host: "gw.example.com", Port: 5060})
	m.MarkRoute(id, false)
	if _, err := m.Route(nil, "pstn", "1234"); err == nil {
		t.Error("expected error when route inactive")
	}
	m.MarkRoute(id, true)
	r, err := m.Route(nil, "pstn", "1234")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if r.Host != "gw.example.com" {
		t.Errorf("Host = %q, want gw.example.com", r.Host)
	}
}

// TestRouteNoMatch verifies an error when nothing matches.
func TestRouteNoMatch(t *testing.T) {
	m := NewCarrierRouteModule()
	c := m.AddCarrier("carrier1")
	m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "9999", Host: "gw.example.com", Port: 5060})
	if _, err := m.Route(nil, "pstn", "1234"); err == nil {
		t.Error("expected error for no matching route")
	}
	// Wrong domain does not match.
	if _, err := m.Route(nil, "other", "9999"); err == nil {
		t.Error("expected error for non-matching domain")
	}
}

// TestListAndCount verifies listing and counting.
func TestListAndCount(t *testing.T) {
	m := NewCarrierRouteModule()
	c := m.AddCarrier("carrier1")
	m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "1", Host: "gw1.example.com", Port: 5060})
	m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "2", Host: "gw2.example.com", Port: 5060})
	if got := m.CountCarriers(); got != 1 {
		t.Errorf("CountCarriers = %d, want 1", got)
	}
	if got := m.CountRoutes(); got != 2 {
		t.Errorf("CountRoutes = %d, want 2", got)
	}
	if got := len(m.ListCarriers()); got != 1 {
		t.Errorf("ListCarriers len = %d, want 1", got)
	}
	routes := m.ListRoutes()
	if len(routes) != 2 {
		t.Fatalf("ListRoutes len = %d, want 2", len(routes))
	}
	// Insertion order preserved.
	if routes[0].Host != "gw1.example.com" {
		t.Errorf("first route Host = %q, want gw1.example.com", routes[0].Host)
	}
	// Mutating a returned copy must not affect the module.
	routes[0].Host = "mutated"
	if m.ListRoutes()[0].Host == "mutated" {
		t.Fatal("expected isolation from ListRoutes copy")
	}
}

// TestGlobalFunctions exercises the package-level API.
func TestGlobalFunctions(t *testing.T) {
	Init()
	cr := DefaultCarrierRoute()
	if cr == nil {
		t.Fatal("expected non-nil default carrierroute")
	}
	c := cr.AddCarrier("global")
	cr.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "1", Host: "gw.example.com", Port: 5060})
	if cr.CountCarriers() != 1 {
		t.Errorf("CountCarriers = %d, want 1", cr.CountCarriers())
	}
}

// TestConcurrentAccess exercises the module under the race detector.
func TestConcurrentAccess(t *testing.T) {
	m := NewCarrierRouteModule()
	c := m.AddCarrier("carrier1")
	id := m.AddRoute(&RouteEntry{CarrierID: c.ID, Domain: "pstn", ScanPrefix: "1", Host: "gw.example.com", Port: 5060})
	msg := mustParseMsg(t, inviteBytes)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Route(msg, "pstn", "")
			_, _ = m.RouteByCarrier(c.ID, "pstn", "1234")
			m.MarkRoute(id, false)
			m.MarkRoute(id, true)
			_ = m.ListCarriers()
			_ = m.ListRoutes()
			_ = m.CountCarriers()
			_ = m.CountRoutes()
		}()
	}
	wg.Wait()
}
