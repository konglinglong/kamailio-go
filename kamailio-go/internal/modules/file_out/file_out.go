// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * FileOut module - file-based output store.
 * Port of the kamailio file_out module analogy.
 *
 * The file_out module writes arbitrary blobs to files under a
 * configured directory and reads them back. It is used to persist
 * auxiliary data (e.g. captured messages, logs) to disk.
 *
 * It is safe for concurrent use: the directory path is guarded by a
 * read/write lock and mutating file operations are serialised.
 */

package file_out

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// FileOutModule reads and writes blobs in a configured directory.
type FileOutModule struct {
	mu   sync.RWMutex
	path string
}

// New creates a FileOutModule with no configured directory.
func New() *FileOutModule {
	return &FileOutModule{}
}

// Init configures the output directory, creating it when missing. The path
// is stored regardless of whether creation succeeds; a later Write will
// surface any error.
//
//	C: file_out_init()
func (m *FileOutModule) Init(path string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.path = path
	if path != "" {
		_ = os.MkdirAll(path, 0o755)
	}
}

// Write stores data under name in the configured directory. Returns an
// error when no directory is configured or the write fails.
func (m *FileOutModule) Write(name string, data []byte) error {
	if m == nil {
		return errors.New("file_out: nil module")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.path == "" {
		return errors.New("file_out: not initialised")
	}
	if name == "" {
		return errors.New("file_out: empty name")
	}
	return os.WriteFile(filepath.Join(m.path, name), data, 0o644)
}

// Read returns the contents of name from the configured directory.
func (m *FileOutModule) Read(name string) ([]byte, error) {
	if m == nil {
		return nil, errors.New("file_out: nil module")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.path == "" {
		return nil, errors.New("file_out: not initialised")
	}
	return os.ReadFile(filepath.Join(m.path, name))
}

// Delete removes name from the configured directory. Returns true when a file
// was removed.
func (m *FileOutModule) Delete(name string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.path == "" || name == "" {
		return false
	}
	if err := os.Remove(filepath.Join(m.path, name)); err != nil {
		return false
	}
	return true
}

// List returns the names of every regular file in the configured directory,
// sorted. Returns nil when no directory is configured.
func (m *FileOutModule) List() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	path := m.path
	m.mu.RUnlock()
	if path == "" {
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.RWMutex
	defaultFileOut *FileOutModule
)

// DefaultFileOut returns the process-wide FileOutModule, creating it on first
// use.
func DefaultFileOut() *FileOutModule {
	defaultMu.RLock()
	m := defaultFileOut
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultFileOut == nil {
		defaultFileOut = New()
	}
	return defaultFileOut
}

// Init (re)initialises the process-wide FileOutModule to a fresh,
// unconfigured state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultFileOut = New()
}
