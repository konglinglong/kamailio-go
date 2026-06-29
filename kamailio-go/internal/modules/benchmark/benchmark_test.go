// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - benchmark module tests.
 */

package benchmark

import (
	"testing"
	"time"
)

func TestStartStop(t *testing.T) {
	m := New()
	m.Start("op")
	time.Sleep(2 * time.Millisecond)
	d := m.Stop("op")
	if d <= 0 {
		t.Fatalf("Stop returned non-positive duration %v", d)
	}
	s := m.GetStats("op")
	if s == nil {
		t.Fatal("GetStats returned nil")
	}
	if s.Count != 1 {
		t.Errorf("Count = %d, want 1", s.Count)
	}
	if s.Min <= 0 {
		t.Errorf("Min = %v, want > 0", s.Min)
	}
	if s.Max < s.Min {
		t.Errorf("Max %v < Min %v", s.Max, s.Min)
	}
}

func TestStopWithoutStart(t *testing.T) {
	m := New()
	if d := m.Stop("nope"); d != 0 {
		t.Errorf("Stop without start = %v, want 0", d)
	}
	if s := m.GetStats("nope"); s != nil {
		t.Errorf("GetStats(unknown) = %v, want nil", s)
	}
	if l := m.List(); len(l) != 0 {
		t.Errorf("List = %v, want empty", l)
	}
}

func TestMultipleSamplesAndList(t *testing.T) {
	m := New()
	for i := 0; i < 5; i++ {
		m.Start("loop")
		m.Stop("loop")
	}
	m.Start("other")
	m.Stop("other")
	s := m.GetStats("loop")
	if s.Count != 5 {
		t.Errorf("Count = %d, want 5", s.Count)
	}
	if s.Avg() <= 0 {
		t.Error("Avg should be positive")
	}
	names := m.List()
	if len(names) != 2 {
		t.Fatalf("List len = %d, want 2", len(names))
	}
	if names[0] != "loop" || names[1] != "other" {
		t.Errorf("List = %v, want [loop other]", names)
	}
}
