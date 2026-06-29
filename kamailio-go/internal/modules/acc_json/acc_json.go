// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * acc_json - JSON accounting backend.
 *
 * Writes Call Detail Records as one JSON object per line (JSON Lines / NDJSON)
 * to an in-memory buffer and optionally to a file. Mirrors the kamailio
 * acc_json module which appends CDRs as JSON documents.
 */

package acc_json

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/acc"
)

// AccJSONModule is the JSON accounting backend.
type AccJSONModule struct {
	mu      sync.Mutex
	path    string
	records []string
}

// New returns a new AccJSONModule.
func New() *AccJSONModule {
	return &AccJSONModule{}
}

// Init configures the output file path. An empty path keeps records in
// memory only. If the path is set the file is created/truncated.
func (m *AccJSONModule) Init(path string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.path = path
	m.records = nil
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		_ = f.Close()
	}
	return nil
}

// WriteCDR serialises a CDR to JSON and stores it. Returns an error if the
// CDR cannot be marshalled.
func (m *AccJSONModule) WriteCDR(cdr *acc.CDR) error {
	if m == nil || cdr == nil {
		return nil
	}
	data, err := json.Marshal(cdr)
	if err != nil {
		return err
	}
	line := string(data)
	m.mu.Lock()
	m.records = append(m.records, line)
	path := m.path
	m.mu.Unlock()
	if path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		_, err = f.WriteString(line + "\n")
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ReadCDRs returns a snapshot of the stored JSON CDR lines.
func (m *AccJSONModule) ReadCDRs() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.records))
	copy(out, m.records)
	return out
}

// Count returns the number of stored CDRs.
func (m *AccJSONModule) Count() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}
