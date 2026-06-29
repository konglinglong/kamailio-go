// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * LCR module tests - least-cost routing with gateway fallback.
 */
package lcr

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
	d := NewLCRModule()
	id1 := d.AddGateway(&LCRGateway{Name: "gw1", URI: "sip:gw1@example.com"})
	id2 := d.AddGateway(&LCRGateway{Name: "gw2", URI: "sip:gw2@example.com"})
	if id1 == id2 {
		t.Errorf("expected distinct gateway IDs, got %d == %d", id1, id2)
	}
	if got := d.CountGateways(); got != 2 {
		t.Errorf("CountGateways = %d, want 2", got)
	}
}

func TestAddRuleAndCount(t *testing.T) {
	d := NewLCRModule()
	gwID := d.AddGateway(&LCRGateway{Name: "gw1", URI: "sip:gw1@example.com"})
	d.AddRule(&LCRRule{Prefix: "1", GatewayID: gwID, Priority: 1})
	d.AddRule(&LCRRule{Prefix: "2", GatewayID: gwID, Priority: 1})
	if got := d.CountRules(); got != 2 {
		t.Errorf("CountRules = %d, want 2", got)
	}
}

func TestLoadRulesAndGateways(t *testing.T) {
	d := NewLCRModule()
	d.LoadGateways([]*LCRGateway{
		{Name: "gw1", URI: "sip:gw1@example.com", Priority: 1},
		{Name: "gw2", URI: "sip:gw2@example.com", Priority: 2},
	})
	d.LoadRules([]*LCRRule{
		{Prefix: "1", GatewayID: 1, Priority: 1},
		{Prefix: "2", GatewayID: 2, Priority: 1},
	})
	if got := d.CountGateways(); got != 2 {
		t.Errorf("CountGateways = %d, want 2", got)
	}
	if got := d.CountRules(); got != 2 {
		t.Errorf("CountRules = %d, want 2", got)
	}
}

func TestSelectGatewayLowestCost(t *testing.T) {
	d := NewLCRModule()
	// Lower priority value = lower cost = selected first.
	gwHigh := d.AddGateway(&LCRGateway{Name: "gwHigh", URI: "sip:gw1@example.com", Priority: 1})
	gwLow := d.AddGateway(&LCRGateway{Name: "gwLow", URI: "sip:gw2@example.com", Priority: 10})
	d.AddRule(&LCRRule{Prefix: "1", GatewayID: gwHigh, Priority: 1})
	d.AddRule(&LCRRule{Prefix: "1", GatewayID: gwLow, Priority: 1})

	msg := mustParseMsg(t, inviteBytes) // user "1001"
	gw, err := d.SelectGateway(msg)
	if err != nil {
		t.Fatalf("SelectGateway failed: %v", err)
	}
	if gw.Name != "gwHigh" {
		t.Errorf("got %s, want gwHigh (lowest cost)", gw.Name)
	}
}

func TestSelectGatewayPrefixMatch(t *testing.T) {
	d := NewLCRModule()
	gwMatch := d.AddGateway(&LCRGateway{Name: "match", URI: "sip:gw1@example.com", Priority: 1})
	gwNoMatch := d.AddGateway(&LCRGateway{Name: "nomatch", URI: "sip:gw2@example.com", Priority: 1})
	d.AddRule(&LCRRule{Prefix: "1", GatewayID: gwMatch, Priority: 1})
	d.AddRule(&LCRRule{Prefix: "9999", GatewayID: gwNoMatch, Priority: 1})

	msg := mustParseMsg(t, inviteBytes) // user "1001"
	gw, err := d.SelectGateway(msg)
	if err != nil {
		t.Fatalf("SelectGateway failed: %v", err)
	}
	if gw.Name != "match" {
		t.Errorf("got %s, want match (prefix match)", gw.Name)
	}
}

func TestSelectGatewayFromURI(t *testing.T) {
	d := NewLCRModule()
	gwMatch := d.AddGateway(&LCRGateway{Name: "match", URI: "sip:gw1@example.com", Priority: 1})
	gwNoMatch := d.AddGateway(&LCRGateway{Name: "nomatch", URI: "sip:gw2@example.com", Priority: 1})
	// From header URI is sip:alice@example.com.
	d.AddRule(&LCRRule{Prefix: "1", FromURI: "sip:alice@example.com", GatewayID: gwMatch, Priority: 1})
	d.AddRule(&LCRRule{Prefix: "1", FromURI: "sip:bob@example.com", GatewayID: gwNoMatch, Priority: 1})

	msg := mustParseMsg(t, inviteBytes)
	gw, err := d.SelectGateway(msg)
	if err != nil {
		t.Fatalf("SelectGateway failed: %v", err)
	}
	if gw.Name != "match" {
		t.Errorf("got %s, want match (FromURI match)", gw.Name)
	}
}

func TestNextGateway(t *testing.T) {
	d := NewLCRModule()
	gwHigh := d.AddGateway(&LCRGateway{Name: "gwHigh", URI: "sip:gw1@example.com", Priority: 1})
	gwLow := d.AddGateway(&LCRGateway{Name: "gwLow", URI: "sip:gw2@example.com", Priority: 10})
	d.AddRule(&LCRRule{Prefix: "1", GatewayID: gwHigh, Priority: 1})
	d.AddRule(&LCRRule{Prefix: "1", GatewayID: gwLow, Priority: 1})

	msg := mustParseMsg(t, inviteBytes)
	first, err := d.SelectGateway(msg)
	if err != nil {
		t.Fatalf("SelectGateway failed: %v", err)
	}
	second, err := d.NextGateway(msg)
	if err != nil {
		t.Fatalf("NextGateway failed: %v", err)
	}
	if first.Name == second.Name {
		t.Errorf("expected distinct gateways, got %s twice", first.Name)
	}
	if first.Name != "gwHigh" {
		t.Errorf("first = %s, want gwHigh", first.Name)
	}
	if second.Name != "gwLow" {
		t.Errorf("second = %s, want gwLow", second.Name)
	}
	// A third call must report exhaustion.
	if _, err := d.NextGateway(msg); err == nil {
		t.Error("expected error when no more gateways")
	}
}

func TestNextGatewayWithoutSelect(t *testing.T) {
	d := NewLCRModule()
	gwID := d.AddGateway(&LCRGateway{Name: "only", URI: "sip:gw@example.com", Priority: 1})
	d.AddRule(&LCRRule{Prefix: "1", GatewayID: gwID, Priority: 1})
	msg := mustParseMsg(t, inviteBytes)

	// Calling NextGateway before SelectGateway is an error.
	if _, err := d.NextGateway(msg); err == nil {
		t.Error("expected error when NextGateway called before SelectGateway")
	}
}

func TestMarkGateway(t *testing.T) {
	d := NewLCRModule()
	// The inactive gateway has the lower priority value (would be picked first
	// if it were active); it must be skipped.
	gwActive := d.AddGateway(&LCRGateway{Name: "active", URI: "sip:gw1@example.com", Priority: 10})
	gwInactive := d.AddGateway(&LCRGateway{Name: "inactive", URI: "sip:gw2@example.com", Priority: 1})
	d.AddRule(&LCRRule{Prefix: "1", GatewayID: gwActive, Priority: 1})
	d.AddRule(&LCRRule{Prefix: "1", GatewayID: gwInactive, Priority: 1})
	d.MarkGateway(gwInactive, false)

	msg := mustParseMsg(t, inviteBytes)
	gw, err := d.SelectGateway(msg)
	if err != nil {
		t.Fatalf("SelectGateway failed: %v", err)
	}
	if gw.Name != "active" {
		t.Errorf("got %s, want active (inactive skipped)", gw.Name)
	}
	// Only one candidate remains; NextGateway must report exhaustion.
	if _, err := d.NextGateway(msg); err == nil {
		t.Error("expected exhaustion when only one active gateway")
	}
}

func TestListGateways(t *testing.T) {
	d := NewLCRModule()
	d.AddGateway(&LCRGateway{Name: "gw1", URI: "sip:gw1@example.com"})
	d.AddGateway(&LCRGateway{Name: "gw2", URI: "sip:gw2@example.com"})
	if got := len(d.ListGateways()); got != 2 {
		t.Errorf("ListGateways len = %d, want 2", got)
	}
}

func TestSelectGatewayNoMatch(t *testing.T) {
	d := NewLCRModule()
	gwID := d.AddGateway(&LCRGateway{Name: "gw", URI: "sip:gw@example.com", Priority: 1})
	d.AddRule(&LCRRule{Prefix: "9999", GatewayID: gwID, Priority: 1})
	msg := mustParseMsg(t, inviteBytes) // user "1001"
	if _, err := d.SelectGateway(msg); err == nil {
		t.Error("expected error for no matching rule")
	}
}

func TestConcurrentAccess(t *testing.T) {
	d := NewLCRModule()
	gwID := d.AddGateway(&LCRGateway{Name: "gw", URI: "sip:gw@example.com", Priority: 1})
	d.AddRule(&LCRRule{Prefix: "1", GatewayID: gwID, Priority: 1})
	msg := mustParseMsg(t, inviteBytes)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.SelectGateway(msg)
			d.CountGateways()
			d.ListGateways()
		}()
	}
	wg.Wait()
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	l := DefaultLCR()
	if l == nil {
		t.Fatal("expected non-nil default lcr")
	}
	l.AddGateway(&LCRGateway{Name: "gw", URI: "sip:gw@example.com"})
	if got := l.CountGateways(); got != 1 {
		t.Errorf("CountGateways = %d, want 1", got)
	}
}
