// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - acc_json module tests.
 */

package acc_json

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/acc"
)

func sampleCDR() *acc.CDR {
	return &acc.CDR{
		CallID:     "call-1@host",
		FromUser:   "alice",
		ToUser:     "bob",
		Method:     "INVITE",
		StatusCode: 200,
		InviteTime: time.Now(),
	}
}

func TestWriteAndReadCDRs(t *testing.T) {
	m := New()
	if err := m.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.WriteCDR(sampleCDR()); err != nil {
		t.Fatalf("WriteCDR: %v", err)
	}
	if err := m.WriteCDR(sampleCDR()); err != nil {
		t.Fatalf("WriteCDR: %v", err)
	}
	got := m.ReadCDRs()
	if len(got) != 2 {
		t.Fatalf("expected 2 CDRs, got %d", len(got))
	}
	if m.Count() != 2 {
		t.Errorf("Count = %d, want 2", m.Count())
	}
}

func TestInitFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cdrs.jsonl")
	m := New()
	if err := m.Init(path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.WriteCDR(sampleCDR()); err != nil {
		t.Fatalf("WriteCDR: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("file is empty after WriteCDR")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("file size is 0")
	}
}

func TestNilSafety(t *testing.T) {
	var m *AccJSONModule
	if err := m.Init(""); err != nil {
		t.Errorf("nil Init: %v", err)
	}
	if err := m.WriteCDR(nil); err != nil {
		t.Errorf("nil WriteCDR: %v", err)
	}
	if got := m.ReadCDRs(); got != nil {
		t.Errorf("nil ReadCDRs = %v, want nil", got)
	}
	if m.Count() != 0 {
		t.Errorf("nil Count = %d, want 0", m.Count())
	}
}
