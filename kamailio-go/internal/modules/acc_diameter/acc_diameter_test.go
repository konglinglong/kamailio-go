// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - acc_diameter module tests.
 */

package acc_diameter

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/acc"
)

func TestInitAndConnect(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("expected disconnected before Init")
	}
	if err := m.Init("diameter.example.com:3868"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsConnected() {
		t.Fatal("expected connected after Init")
	}
}

func TestWriteCDR(t *testing.T) {
	m := New()
	_ = m.Init("diameter.example.com:3868")
	if err := m.WriteCDR(&acc.CDR{CallID: "d1"}); err != nil {
		t.Fatalf("WriteCDR: %v", err)
	}
	if err := m.WriteCDR(&acc.CDR{CallID: "d2"}); err != nil {
		t.Fatalf("WriteCDR: %v", err)
	}
	if m.Count() != 2 {
		t.Errorf("Count = %d, want 2", m.Count())
	}
}

func TestWriteCDRNotConnected(t *testing.T) {
	m := New()
	_ = m.Init("")
	if m.IsConnected() {
		t.Fatal("expected disconnected with empty server")
	}
	if err := m.WriteCDR(&acc.CDR{CallID: "d1"}); err == nil {
		t.Fatal("expected error writing CDR while disconnected")
	}
	if err := m.WriteCDR(nil); err == nil {
		t.Fatal("expected error writing nil CDR")
	}
}
