// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - cnxcc module tests.
 */

package cnxcc

import (
	"sync"
	"testing"
)

func TestSetAndGetBalance(t *testing.T) {
	m := New()
	m.SetCredit("alice", 1000)
	if got := m.GetBalance("alice"); got != 1000 {
		t.Errorf("GetBalance = %d, want 1000", got)
	}
	if got := m.GetBalance("unknown"); got != 0 {
		t.Errorf("GetBalance(unknown) = %d, want 0", got)
	}
}

func TestCheckAndDeductCredit(t *testing.T) {
	m := New()
	m.SetCredit("bob", 500)
	if !m.CheckCredit("bob", 500) {
		t.Error("CheckCredit(500) = false, want true")
	}
	if m.CheckCredit("bob", 501) {
		t.Error("CheckCredit(501) = true, want false")
	}
	if got := m.DeductCredit("bob", 200); got != 300 {
		t.Errorf("DeductCredit = %d, want 300", got)
	}
	if got := m.DeductCredit("bob", 1000); got != 0 {
		t.Errorf("DeductCredit below zero = %d, want 0", got)
	}
}

func TestConcurrentDeduct(t *testing.T) {
	m := New()
	m.SetCredit("carol", 1000)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.DeductCredit("carol", 1)
		}()
	}
	wg.Wait()
	if got := m.GetBalance("carol"); got != 900 {
		t.Errorf("balance = %d, want 900", got)
	}
}
