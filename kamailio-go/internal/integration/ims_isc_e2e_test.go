// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * IMS ISC (IMS Service Control) End-to-End tests - 3GPP TS 23.218
 *
 * Covers Initial Filter Criteria (iFC) evaluation on the ISC interface:
 *   - Basic iFC evaluation
 *   - Originating / Terminating session-case triggers
 *   - Priority ordering
 *   - Default handling (continue / terminate)
 *   - No-match behaviour
 *   - Multiple matching routes
 *   - Route removal
 *   - Concurrent evaluation (ISCModule is thread-safe)
 *   - Originating_Unregistered session case
 */

package integration

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/modules/ims_isc"
)

// buildISCInvite builds an INVITE suitable for iFC evaluation.
func buildISCInvite(callID string) []byte {
	return buildINVITE("sip:alice@ims.example.com", "sip:bob@ims.example.com", "isc-tag", callID, 1)
}

// ---------------------------------------------------------------------------
// Test: Basic iFC evaluation - TS 23.218
// ---------------------------------------------------------------------------

func TestE2E_IMS_ISC_BasicIFCEvaluation(t *testing.T) {
	// 3GPP TS 23.218 - Basic Initial Filter Criteria evaluation.
	m := ims_isc.NewISCModule()

	// 1. Add an originating iFC route (trigger: method INVITE, AS: sip:as1@example.com)
	routeID := m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as1@example.com",
		ASName:       "as1",
		Priority:     10,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})
	if routeID <= 0 {
		t.Fatalf("AddRoute returned invalid id %d", routeID)
	}

	// 2. Build INVITE message
	raw := buildISCInvite("isc-basic-001")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}

	// 3. Evaluate routes
	route, err := m.Evaluate(msg, ims_isc.SessionCaseOriginating)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// 4. Verify matched AS
	if route.ASAddress != "sip:as1@example.com" {
		t.Fatalf("ASAddress = %q, want sip:as1@example.com", route.ASAddress)
	}
	if route.ID != routeID {
		t.Fatalf("route ID = %d, want %d", route.ID, routeID)
	}
}

// ---------------------------------------------------------------------------
// Test: Originating trigger - TS 23.218
// ---------------------------------------------------------------------------

func TestE2E_IMS_ISC_OriginatingTrigger(t *testing.T) {
	// 3GPP TS 23.218 - Originating session case (SessionCase=0) trigger.
	m := ims_isc.NewISCModule()
	m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:orig-as@example.com",
		ASName:       "orig-as",
		Priority:     5,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})

	// 2. Send INVITE
	raw := buildISCInvite("isc-orig-001")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}

	// 3. Verify matching originating route
	route, err := m.Evaluate(msg, ims_isc.SessionCaseOriginating)
	if err != nil {
		t.Fatalf("Evaluate originating: %v", err)
	}
	if route.SessionCase != ims_isc.SessionCaseOriginating {
		t.Fatalf("SessionCase = %d, want %d", route.SessionCase, ims_isc.SessionCaseOriginating)
	}
	if route.ASAddress != "sip:orig-as@example.com" {
		t.Fatalf("ASAddress = %q, want sip:orig-as@example.com", route.ASAddress)
	}
}

// ---------------------------------------------------------------------------
// Test: Terminating trigger - TS 23.218
// ---------------------------------------------------------------------------

func TestE2E_IMS_ISC_TerminatingTrigger(t *testing.T) {
	// 3GPP TS 23.218 - Terminating session case (SessionCase=1) trigger.
	m := ims_isc.NewISCModule()
	m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:term-as@example.com",
		ASName:       "term-as",
		Priority:     5,
		SessionCase:  ims_isc.SessionCaseTerminating,
	})

	// 2. Send INVITE
	raw := buildISCInvite("isc-term-001")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}

	// 3. Verify matching terminating route
	route, err := m.Evaluate(msg, ims_isc.SessionCaseTerminating)
	if err != nil {
		t.Fatalf("Evaluate terminating: %v", err)
	}
	if route.SessionCase != ims_isc.SessionCaseTerminating {
		t.Fatalf("SessionCase = %d, want %d", route.SessionCase, ims_isc.SessionCaseTerminating)
	}
	if route.ASAddress != "sip:term-as@example.com" {
		t.Fatalf("ASAddress = %q, want sip:term-as@example.com", route.ASAddress)
	}
}

// ---------------------------------------------------------------------------
// Test: Priority ordering - TS 23.218
// ---------------------------------------------------------------------------

func TestE2E_IMS_ISC_PriorityOrdering(t *testing.T) {
	// 3GPP TS 23.218 - iFC priority ordering (lower value = higher precedence).
	m := ims_isc.NewISCModule()

	// 1. Add 3 iFC with priorities 10, 5, 1
	m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as-p10@example.com",
		Priority:     10,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})
	m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as-p5@example.com",
		Priority:     5,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})
	idP1 := m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as-p1@example.com",
		Priority:     1,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})

	// 2. Verify ListRoutes returns by priority ascending
	listed := m.ListRoutes()
	if len(listed) != 3 {
		t.Fatalf("expected 3 routes, got %d", len(listed))
	}
	if listed[0].Priority != 1 || listed[1].Priority != 5 || listed[2].Priority != 10 {
		t.Fatalf("priorities not ascending: %d %d %d",
			listed[0].Priority, listed[1].Priority, listed[2].Priority)
	}

	// 3. Verify priority=1 route matches first via Evaluate
	raw := buildISCInvite("isc-prio-001")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}
	route, err := m.Evaluate(msg, ims_isc.SessionCaseOriginating)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if route.Priority != 1 {
		t.Fatalf("expected priority 1, got %d", route.Priority)
	}
	if route.ID != idP1 {
		t.Fatalf("expected route ID %d, got %d", idP1, route.ID)
	}
	if route.ASAddress != "sip:as-p1@example.com" {
		t.Fatalf("ASAddress = %q, want sip:as-p1@example.com", route.ASAddress)
	}
}

// ---------------------------------------------------------------------------
// Test: Default handling - TS 23.218
// ---------------------------------------------------------------------------

func TestE2E_IMS_ISC_DefaultHandling(t *testing.T) {
	// 3GPP TS 23.218 - Default handling (continue / terminate on AS failure).
	m := ims_isc.NewISCModule()

	// 1. Add iFC with DefaultHandling=Continue
	idCont := m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint:    "method:INVITE",
		ASAddress:       "sip:as-cont@example.com",
		Priority:        1,
		SessionCase:     ims_isc.SessionCaseOriginating,
		DefaultHandling: ims_isc.DefaultHandlingContinue,
	})
	// 2. Add iFC with DefaultHandling=Terminate
	idTerm := m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint:    "method:INVITE",
		ASAddress:       "sip:as-term@example.com",
		Priority:        2,
		SessionCase:     ims_isc.SessionCaseOriginating,
		DefaultHandling: ims_isc.DefaultHandlingTerminate,
	})

	// 3. Verify route attributes correct
	listed := m.ListRoutes()
	if len(listed) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(listed))
	}

	var contRoute, termRoute *ims_isc.ISCRoute
	for _, r := range listed {
		switch r.ID {
		case idCont:
			contRoute = r
		case idTerm:
			termRoute = r
		}
	}
	if contRoute == nil {
		t.Fatal("continue route not found")
	}
	if contRoute.DefaultHandling != ims_isc.DefaultHandlingContinue {
		t.Fatalf("continue route DefaultHandling = %d, want %d",
			contRoute.DefaultHandling, ims_isc.DefaultHandlingContinue)
	}
	if termRoute == nil {
		t.Fatal("terminate route not found")
	}
	if termRoute.DefaultHandling != ims_isc.DefaultHandlingTerminate {
		t.Fatalf("terminate route DefaultHandling = %d, want %d",
			termRoute.DefaultHandling, ims_isc.DefaultHandlingTerminate)
	}
}

// ---------------------------------------------------------------------------
// Test: No matching iFC - TS 23.218
// ---------------------------------------------------------------------------

func TestE2E_IMS_ISC_NoMatch(t *testing.T) {
	// 3GPP TS 23.218 - No matching iFC for the given message.
	m := ims_isc.NewISCModule()
	m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as1@example.com",
		Priority:     1,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})

	// 2. Send REGISTER (not INVITE) - should not match method:INVITE trigger
	raw := buildREGISTER("sip:alice@ims.example.com", "sip:alice@192.168.1.100", "")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("parse REGISTER: %v", err)
	}

	// 3. Verify no iFC matches
	_, err = m.Evaluate(msg, ims_isc.SessionCaseOriginating)
	if err == nil {
		t.Fatal("expected error for non-matching message, got nil")
	}
}

// ---------------------------------------------------------------------------
// Test: Multiple matching routes - TS 23.218
// ---------------------------------------------------------------------------

func TestE2E_IMS_ISC_MultipleRoutes(t *testing.T) {
	// 3GPP TS 23.218 - Multiple matching iFC routes, returned by priority.
	m := ims_isc.NewISCModule()

	// 1. Add multiple matching iFC with different priorities
	m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as-a@example.com",
		Priority:     30,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})
	m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as-b@example.com",
		Priority:     20,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})
	m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as-c@example.com",
		Priority:     10,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})

	// 2. Verify all returned by ListRoutes, sorted by priority ascending
	listed := m.ListRoutes()
	if len(listed) != 3 {
		t.Fatalf("expected 3 routes, got %d", len(listed))
	}
	for i := 1; i < len(listed); i++ {
		if listed[i-1].Priority > listed[i].Priority {
			t.Fatalf("routes not sorted by priority: %d > %d",
				listed[i-1].Priority, listed[i].Priority)
		}
	}
	if listed[0].ASAddress != "sip:as-c@example.com" {
		t.Fatalf("first route ASAddress = %q, want sip:as-c@example.com", listed[0].ASAddress)
	}

	// Verify Evaluate returns the highest-priority (lowest value) route
	raw := buildISCInvite("isc-multi-001")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}
	route, err := m.Evaluate(msg, ims_isc.SessionCaseOriginating)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if route.Priority != 10 {
		t.Fatalf("expected priority 10, got %d", route.Priority)
	}
	if route.ASAddress != "sip:as-c@example.com" {
		t.Fatalf("ASAddress = %q, want sip:as-c@example.com", route.ASAddress)
	}
}

// ---------------------------------------------------------------------------
// Test: Route removal - TS 23.218
// ---------------------------------------------------------------------------

func TestE2E_IMS_ISC_RemoveRoute(t *testing.T) {
	// 3GPP TS 23.218 - iFC route removal.
	m := ims_isc.NewISCModule()
	id := m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as1@example.com",
		Priority:     1,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})
	if m.Count() != 1 {
		t.Fatalf("expected 1 route, got %d", m.Count())
	}

	// 2. Remove the route
	if !m.RemoveRoute(id) {
		t.Fatal("RemoveRoute returned false for existing route")
	}
	if m.Count() != 0 {
		t.Fatalf("expected 0 routes after removal, got %d", m.Count())
	}

	// 3. Verify no longer matches
	raw := buildISCInvite("isc-remove-001")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}
	_, err = m.Evaluate(msg, ims_isc.SessionCaseOriginating)
	if err == nil {
		t.Fatal("expected error after route removal, got nil")
	}

	// Verify removing non-existent route returns false
	if m.RemoveRoute(id) {
		t.Fatal("RemoveRoute for non-existent id should return false")
	}
}

// ---------------------------------------------------------------------------
// Test: Concurrent iFC evaluation - TS 23.218
// ---------------------------------------------------------------------------

func TestE2E_IMS_ISC_Concurrent(t *testing.T) {
	// 3GPP TS 23.218 - Concurrent iFC evaluation.
	// ISCModule is thread-safe (sync.RWMutex), so 50 goroutines can evaluate
	// concurrently on the same module.
	m := ims_isc.NewISCModule()
	m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as1@example.com",
		Priority:     1,
		SessionCase:  ims_isc.SessionCaseOriginating,
	})

	raw := buildISCInvite("isc-conc-001")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}

	// 2. 50 goroutines simultaneously evaluate
	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			route, err := m.Evaluate(msg, ims_isc.SessionCaseOriginating)
			if err != nil {
				errs <- err
				return
			}
			if route.ASAddress != "sip:as1@example.com" {
				errs <- fmt.Errorf("ASAddress = %q, want sip:as1@example.com", route.ASAddress)
			}
		}()
	}
	wg.Wait()
	close(errs)

	// 3. Verify results consistent, no panic
	for err := range errs {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Test: Originating_Unregistered session case - TS 23.218
// ---------------------------------------------------------------------------

func TestE2E_IMS_ISC_SessionCaseOriginatingUnregistered(t *testing.T) {
	// 3GPP TS 23.218 - Originating_Unregistered session case (SessionCase=3).
	m := ims_isc.NewISCModule()
	m.AddRoute(&ims_isc.ISCRoute{
		TriggerPoint: "method:INVITE",
		ASAddress:    "sip:as-unreg@example.com",
		ASName:       "as-unreg",
		Priority:     1,
		SessionCase:  ims_isc.SessionCaseOriginatingUnregistered,
	})

	// 2. Verify evaluation result for the matching session case
	raw := buildISCInvite("isc-unreg-001")
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}

	route, err := m.Evaluate(msg, ims_isc.SessionCaseOriginatingUnregistered)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if route.SessionCase != ims_isc.SessionCaseOriginatingUnregistered {
		t.Fatalf("SessionCase = %d, want %d",
			route.SessionCase, ims_isc.SessionCaseOriginatingUnregistered)
	}
	if route.ASAddress != "sip:as-unreg@example.com" {
		t.Fatalf("ASAddress = %q, want sip:as-unreg@example.com", route.ASAddress)
	}

	// Verify it does NOT match a different session case
	_, err = m.Evaluate(msg, ims_isc.SessionCaseOriginating)
	if err == nil {
		t.Fatal("expected no match for originating session case")
	}
}
