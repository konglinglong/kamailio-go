// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Dispatcher module tests - load balancing across destination sets.
 */
package dispatcher

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

func TestAddSetAndGetSet(t *testing.T) {
	d := NewDispatcherModule()
	s := d.AddSet(1, "set1")
	if s == nil {
		t.Fatal("expected non-nil set")
	}
	if s.ID != 1 || s.Name != "set1" {
		t.Errorf("set = {ID:%d Name:%q}, want {1, set1}", s.ID, s.Name)
	}
	if got := d.GetSet(1); got != s {
		t.Errorf("GetSet(1) returned %p, want %p", got, s)
	}
	if got := d.GetSet(999); got != nil {
		t.Errorf("GetSet(999) = %v, want nil", got)
	}
	// Adding a set with an existing ID replaces it.
	s2 := d.AddSet(1, "replaced")
	if d.GetSet(1) != s2 {
		t.Errorf("expected GetSet(1) to return the replaced set")
	}
}

func TestAddDestinationAndCount(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")

	id1 := d.AddDestination(1, &Destination{URI: "sip:a@example.com", Weight: 1})
	id2 := d.AddDestination(1, &Destination{URI: "sip:b@example.com", Weight: 1})
	if id1 == id2 {
		t.Errorf("expected distinct destination IDs, got %d == %d", id1, id2)
	}
	if got := d.Count(1); got != 2 {
		t.Errorf("Count(1) = %d, want 2", got)
	}
	if got := d.Count(999); got != 0 {
		t.Errorf("Count(999) = %d, want 0", got)
	}
}

func TestRemoveDestination(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	id := d.AddDestination(1, &Destination{URI: "sip:a@example.com"})
	if !d.RemoveDestination(1, id) {
		t.Errorf("RemoveDestination returned false for existing dest")
	}
	if got := d.Count(1); got != 0 {
		t.Errorf("Count after remove = %d, want 0", got)
	}
	if d.RemoveDestination(1, id) {
		t.Errorf("RemoveDestination returned true for non-existent dest")
	}
	if d.RemoveDestination(999, 1) {
		t.Errorf("RemoveDestination returned true for non-existent set")
	}
}

func TestRoundRobin(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	d.AddDestination(1, &Destination{URI: "sip:a@example.com"})
	d.AddDestination(1, &Destination{URI: "sip:b@example.com"})
	d.AddDestination(1, &Destination{URI: "sip:c@example.com"})

	first := d.SelectDestination(1, "round-robin")
	second := d.SelectDestination(1, "round-robin")
	third := d.SelectDestination(1, "round-robin")
	fourth := d.SelectDestination(1, "round-robin")

	if first == nil || second == nil || third == nil || fourth == nil {
		t.Fatalf("expected non-nil selections")
	}
	if first.URI == second.URI || second.URI == third.URI {
		t.Errorf("round-robin returned same dest consecutively: %s %s %s",
			first.URI, second.URI, third.URI)
	}
	if fourth.URI != first.URI {
		t.Errorf("round-robin did not wrap: first=%s fourth=%s", first.URI, fourth.URI)
	}
}

func TestSelectDestinationSkipsInactive(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	idA := d.AddDestination(1, &Destination{URI: "sip:a@example.com"})
	d.AddDestination(1, &Destination{URI: "sip:b@example.com"})

	d.MarkDestination(1, idA, false)
	if d.IsActive(1, idA) {
		t.Errorf("IsActive should be false after MarkDestination(false)")
	}

	for i := 0; i < 6; i++ {
		sel := d.SelectDestination(1, "round-robin")
		if sel == nil {
			t.Fatal("expected non-nil selection")
		}
		if sel.URI == "sip:a@example.com" {
			t.Errorf("round-robin selected inactive destination")
		}
	}
}

func TestSelectDestinationPriority(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	// Lower priority value = higher precedence (Kamailio convention).
	d.AddDestination(1, &Destination{URI: "sip:low@example.com", Priority: 10})
	d.AddDestination(1, &Destination{URI: "sip:high@example.com", Priority: 1})
	d.AddDestination(1, &Destination{URI: "sip:mid@example.com", Priority: 5})

	sel := d.SelectDestination(1, "priority")
	if sel == nil {
		t.Fatal("expected non-nil selection")
	}
	if sel.URI != "sip:high@example.com" {
		t.Errorf("priority selected %s, want sip:high@example.com", sel.URI)
	}
}

func TestSelectDestinationWeight(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	d.AddDestination(1, &Destination{URI: "sip:heavy@example.com", Weight: 100})
	d.AddDestination(1, &Destination{URI: "sip:light@example.com", Weight: 1})

	// With a 100:1 weighting, the heavy destination should dominate.
	heavy := 0
	const n = 500
	for i := 0; i < n; i++ {
		sel := d.SelectDestination(1, "weight")
		if sel == nil {
			t.Fatal("expected non-nil selection")
		}
		if sel.URI == "sip:heavy@example.com" {
			heavy++
		}
	}
	if heavy < n*9/10 {
		t.Errorf("weight algorithm selected heavy only %d/%d times, want >= %d",
			heavy, n, n*9/10)
	}
}

func TestSelectDestinationsMultiple(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	d.AddDestination(1, &Destination{URI: "sip:a@example.com"})
	d.AddDestination(1, &Destination{URI: "sip:b@example.com"})
	d.AddDestination(1, &Destination{URI: "sip:c@example.com"})

	sels := d.SelectDestinations(1, 2, "round-robin")
	if len(sels) != 2 {
		t.Fatalf("expected 2 destinations, got %d", len(sels))
	}
	if sels[0].URI == sels[1].URI {
		t.Errorf("expected distinct destinations, got %s twice", sels[0].URI)
	}
	// Asking for more than available returns all available.
	sels = d.SelectDestinations(1, 10, "round-robin")
	if len(sels) != 3 {
		t.Errorf("expected 3 destinations, got %d", len(sels))
	}
}

func TestSelectDestinationHashStable(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	d.AddDestination(1, &Destination{URI: "sip:a@example.com"})
	d.AddDestination(1, &Destination{URI: "sip:b@example.com"})
	d.AddDestination(1, &Destination{URI: "sip:c@example.com"})

	msg := mustParseMsg(t, inviteBytes)
	sel1 := d.SelectDestinationWithMsg(1, "hash", msg)
	sel2 := d.SelectDestinationWithMsg(1, "hash", msg)
	if sel1 == nil || sel2 == nil {
		t.Fatal("expected non-nil selections")
	}
	if sel1.URI != sel2.URI {
		t.Errorf("hash not stable for same message: %s vs %s", sel1.URI, sel2.URI)
	}
}

func TestSelectDestinationUnknownAlgorithm(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	d.AddDestination(1, &Destination{URI: "sip:a@example.com"})
	if sel := d.SelectDestination(1, "bogus"); sel != nil {
		t.Errorf("unknown algorithm should return nil, got %v", sel)
	}
}

func TestListSets(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	d.AddSet(2, "set2")
	sets := d.ListSets()
	if len(sets) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(sets))
	}
}

func TestLoadFromCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dispatch.csv")
	content := []byte("# comment line\n" +
		"1,sip:gw1@example.com,0,5,1,first gateway\n" +
		"1,sip:gw2@example.com,0,1,1,second gateway\n" +
		"2,sip:gw3@example.com,0,1,1,third gateway\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	d := NewDispatcherModule()
	if err := d.LoadFromCSV(path); err != nil {
		t.Fatalf("LoadFromCSV failed: %v", err)
	}
	if got := d.Count(1); got != 2 {
		t.Errorf("Count(1) = %d, want 2", got)
	}
	if got := d.Count(2); got != 1 {
		t.Errorf("Count(2) = %d, want 1", got)
	}
}

func TestStats(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	idA := d.AddDestination(1, &Destination{URI: "sip:a@example.com"})
	idB := d.AddDestination(1, &Destination{URI: "sip:b@example.com"})
	// Newly added destinations are active by default (Kamailio semantics).
	d.MarkDestination(1, idB, false)

	stats := d.Stats()
	if got := stats.ActiveDestinations.Load(); got != 1 {
		t.Errorf("ActiveDestinations = %d, want 1", got)
	}
	if got := stats.InactiveDestinations.Load(); got != 1 {
		t.Errorf("InactiveDestinations = %d, want 1", got)
	}

	d.SelectDestination(1, "round-robin")
	if got := stats.TotalSelected.Load(); got != 1 {
		t.Errorf("TotalSelected = %d, want 1", got)
	}

	// Marking toggles the counts.
	d.MarkDestination(1, idA, false)
	stats = d.Stats()
	if got := stats.ActiveDestinations.Load(); got != 0 {
		t.Errorf("ActiveDestinations after mark = %d, want 0", got)
	}
	if got := stats.InactiveDestinations.Load(); got != 2 {
		t.Errorf("InactiveDestinations after mark = %d, want 2", got)
	}
}

func TestConcurrentAccess(t *testing.T) {
	d := NewDispatcherModule()
	d.AddSet(1, "set1")
	for i := 0; i < 10; i++ {
		d.AddDestination(1, &Destination{URI: "sip:gw@example.com"})
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.SelectDestination(1, "round-robin")
			d.SelectDestination(1, "random")
			d.Count(1)
			d.ListSets()
		}()
	}
	wg.Wait()
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	dp := DefaultDispatcher()
	if dp == nil {
		t.Fatal("expected non-nil default dispatcher")
	}
	dp.AddSet(100, "global")
	if got := dp.Count(100); got != 0 {
		t.Errorf("Count(100) = %d, want 0", got)
	}
}
