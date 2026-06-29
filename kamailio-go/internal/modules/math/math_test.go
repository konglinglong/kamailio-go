// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - math module tests.
 */

package math

import (
	"math"
	"testing"
)

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestEval(t *testing.T) {
	m := New()
	cases := []struct {
		expr string
		want float64
	}{
		{"1 + 2", 3},
		{"2 * 3 + 4", 10},
		{"(2 + 3) * 4", 20},
		{"10 / 4", 2.5},
		{"-5 + 3", -2},
		{"2 * (3 + (4 - 1))", 12},
		{"3.5 * 2", 7},
	}
	for _, c := range cases {
		got, err := m.Eval(c.expr)
		if err != nil {
			t.Errorf("Eval(%q) error: %v", c.expr, err)
			continue
		}
		if !approxEqual(got, c.want) {
			t.Errorf("Eval(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestEvalErrors(t *testing.T) {
	m := New()
	if _, err := m.Eval("1 / 0"); err == nil {
		t.Error("Eval(1/0) expected error")
	}
	if _, err := m.Eval("1 + "); err == nil {
		t.Error("Eval(1 + ) expected error")
	}
	if _, err := m.Eval("(1 + 2"); err == nil {
		t.Error("Eval((1 + 2) expected error")
	}
	if _, err := m.Eval("1 2"); err == nil {
		t.Error("Eval(1 2) expected error")
	}
}

func TestRoundFloorCeilSqrtPow(t *testing.T) {
	m := New()
	if !approxEqual(m.Round(3.14159, 2), 3.14) {
		t.Errorf("Round = %v, want 3.14", m.Round(3.14159, 2))
	}
	if !approxEqual(m.Round(2.5, 0), 3) {
		t.Errorf("Round(2.5,0) = %v, want 3", m.Round(2.5, 0))
	}
	if !approxEqual(m.Floor(3.9), 3) {
		t.Errorf("Floor = %v, want 3", m.Floor(3.9))
	}
	if !approxEqual(m.Ceil(3.1), 4) {
		t.Errorf("Ceil = %v, want 4", m.Ceil(3.1))
	}
	if !approxEqual(m.Sqrt(16), 4) {
		t.Errorf("Sqrt = %v, want 4", m.Sqrt(16))
	}
	if !approxEqual(m.Pow(2, 10), 1024) {
		t.Errorf("Pow = %v, want 1024", m.Pow(2, 10))
	}
}
