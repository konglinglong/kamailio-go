// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * DRouting module tests - prefix-based dynamic routing to gateways.
 */
package drouting

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

func TestAddGatewayAndCount(t *testing.T) {
	d := NewDRoutingModule()
	id1 := d.AddGateway(&Gateway{Address: "sip:gw1@example.com"})
	id2 := d.AddGateway(&Gateway{Address: "sip:gw2@example.com"})
	if id1 == id2 {
		t.Errorf("expected distinct gateway IDs, got %d == %d", id1, id2)
	}
	if got := d.CountGateways(); got != 2 {
		t.Errorf("CountGateways = %d, want 2", got)
	}
}

func TestAddCarrierAndCount(t *testing.T) {
	d := NewDRoutingModule()
	d.AddCarrier(&Carrier{Name: "carrier1"})
	d.AddCarrier(&Carrier{Name: "carrier2"})
	if got := d.CountCarriers(); got != 2 {
		t.Errorf("CountCarriers = %d, want 2", got)
	}
}

func TestRoutePrefixMatch(t *testing.T) {
	d := NewDRoutingModule()
	gwID := d.AddGateway(&Gateway{Address: "sip:gw1@example.com"})
	d.AddRule(&RouteRule{Group: 1, Prefix: "1", Priority: 1, GatewayID: gwID})

	msg := mustParseMsg(t, inviteBytes) // R-URI user = "1001"
	gw, err := d.Route(msg, 1)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if gw.Address != "sip:gw1@example.com" {
		t.Errorf("got gateway %s, want sip:gw1@example.com", gw.Address)
	}
}

func TestRouteLongestPrefix(t *testing.T) {
	d := NewDRoutingModule()
	gwShort := d.AddGateway(&Gateway{Address: "sip:short@example.com"})
	gwLong := d.AddGateway(&Gateway{Address: "sip:long@example.com"})
	d.AddRule(&RouteRule{Group: 1, Prefix: "1", Priority: 1, GatewayID: gwShort})
	d.AddRule(&RouteRule{Group: 1, Prefix: "1001", Priority: 1, GatewayID: gwLong})

	msg := mustParseMsg(t, inviteBytes) // user "1001"
	gw, err := d.Route(msg, 1)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if gw.Address != "sip:long@example.com" {
		t.Errorf("got %s, want sip:long@example.com (longest prefix)", gw.Address)
	}
}

func TestRoutePriority(t *testing.T) {
	d := NewDRoutingModule()
	gwLow := d.AddGateway(&Gateway{Address: "sip:low@example.com"})
	gwHigh := d.AddGateway(&Gateway{Address: "sip:high@example.com"})
	// Same prefix, different priorities: higher priority value wins.
	d.AddRule(&RouteRule{Group: 1, Prefix: "1", Priority: 10, GatewayID: gwLow})
	d.AddRule(&RouteRule{Group: 1, Prefix: "1", Priority: 20, GatewayID: gwHigh})

	msg := mustParseMsg(t, inviteBytes)
	gw, err := d.Route(msg, 1)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if gw.Address != "sip:high@example.com" {
		t.Errorf("got %s, want sip:high@example.com (higher priority)", gw.Address)
	}
}

func TestRouteNoMatch(t *testing.T) {
	d := NewDRoutingModule()
	gwID := d.AddGateway(&Gateway{Address: "sip:gw@example.com"})
	d.AddRule(&RouteRule{Group: 1, Prefix: "9999", Priority: 1, GatewayID: gwID})

	msg := mustParseMsg(t, inviteBytes) // user "1001"
	if _, err := d.Route(msg, 1); err == nil {
		t.Error("expected error for no matching rule")
	}
	// A different group must not match either.
	if _, err := d.Route(msg, 2); err == nil {
		t.Error("expected error for non-existent group")
	}
}

func TestRouteToGateway(t *testing.T) {
	d := NewDRoutingModule()
	gwID := d.AddGateway(&Gateway{Address: "sip:gw@example.com"})

	gw, err := d.RouteToGateway(gwID)
	if err != nil {
		t.Fatalf("RouteToGateway failed: %v", err)
	}
	if gw.Address != "sip:gw@example.com" {
		t.Errorf("got %s, want sip:gw@example.com", gw.Address)
	}
	if _, err := d.RouteToGateway(999); err == nil {
		t.Error("expected error for unknown gateway")
	}
}

func TestUseGateway(t *testing.T) {
	d := NewDRoutingModule()
	gw := &Gateway{Address: "sip:gw@example.com", Strip: 2, Prefix: "9"}
	d.AddGateway(gw)

	msg := mustParseMsg(t, inviteBytes) // sip:1001@example.com
	if err := d.UseGateway(msg, gw); err != nil {
		t.Fatalf("UseGateway failed: %v", err)
	}
	// Strip 2 from "1001" -> "01", prefix "9" -> "901".
	if got := msg.NewURI.String(); got != "sip:901@example.com" {
		t.Errorf("NewURI = %q, want sip:901@example.com", got)
	}
}

func TestMarkGateway(t *testing.T) {
	d := NewDRoutingModule()
	gwID := d.AddGateway(&Gateway{Address: "sip:gw@example.com"})
	d.AddRule(&RouteRule{Group: 1, Prefix: "1", Priority: 1, GatewayID: gwID})

	d.MarkGateway(gwID, false)
	msg := mustParseMsg(t, inviteBytes)
	if _, err := d.Route(msg, 1); err == nil {
		t.Error("expected error when routing to inactive gateway")
	}
	if _, err := d.RouteToGateway(gwID); err == nil {
		t.Error("expected error for RouteToGateway on inactive gateway")
	}

	d.MarkGateway(gwID, true)
	if _, err := d.Route(msg, 1); err != nil {
		t.Errorf("expected success after reactivating, got %v", err)
	}
}

func TestCountAndList(t *testing.T) {
	d := NewDRoutingModule()
	d.AddGateway(&Gateway{Address: "sip:gw1@example.com"})
	d.AddGateway(&Gateway{Address: "sip:gw2@example.com"})
	d.AddCarrier(&Carrier{Name: "carrier1"})

	if got := d.CountGateways(); got != 2 {
		t.Errorf("CountGateways = %d, want 2", got)
	}
	if got := d.CountCarriers(); got != 1 {
		t.Errorf("CountCarriers = %d, want 1", got)
	}
	if got := len(d.ListGateways()); got != 2 {
		t.Errorf("ListGateways len = %d, want 2", got)
	}
	if got := len(d.ListCarriers()); got != 1 {
		t.Errorf("ListCarriers len = %d, want 1", got)
	}
}

func TestConcurrentAccess(t *testing.T) {
	d := NewDRoutingModule()
	gwID := d.AddGateway(&Gateway{Address: "sip:gw@example.com"})
	d.AddRule(&RouteRule{Group: 1, Prefix: "1", Priority: 1, GatewayID: gwID})
	msg := mustParseMsg(t, inviteBytes)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Route(msg, 1)
			d.CountGateways()
			d.ListGateways()
		}()
	}
	wg.Wait()
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	dr := DefaultDRouting()
	if dr == nil {
		t.Fatal("expected non-nil default drouting")
	}
	dr.AddGateway(&Gateway{Address: "sip:gw@example.com"})
	if got := dr.CountGateways(); got != 1 {
		t.Errorf("CountGateways = %d, want 1", got)
	}
}
