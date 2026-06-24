// SPDX-License-Identifier-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * LogSystemd module - systemd-style structured logging.
 * Port of the kamailio log_systemd module (src/modules/log_systemd).
 *
 * This module mirrors the behaviour of writing log lines to the systemd
 * journal: each Log call records a structured entry tagged with a
 * severity level. Entries are buffered in memory so tests can inspect
 * them. The minimum level can be raised with SetLevel so that noisier
 * levels are dropped.
 *
 * The module is safe for concurrent use.
 */

package log_systemd

import (
	"strings"
	"sync"
	"time"
)

// LogLevel is the severity of a journal entry.
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelNotice
	LevelWarning
	LevelError
	LevelCritical
)

// levelNames maps a level name to its LogLevel.
var levelNames = map[string]LogLevel{
	"debug":    LevelDebug,
	"info":     LevelInfo,
	"notice":   LevelNotice,
	"warning":  LevelWarning,
	"warn":     LevelWarning,
	"error":    LevelError,
	"err":      LevelError,
	"critical": LevelCritical,
	"crit":     LevelCritical,
}

// Entry is a single buffered log record.
type Entry struct {
	Time    time.Time
	Level   LogLevel
	Message string
}

// LogSystemdModule buffers structured log entries.
type LogSystemdModule struct {
	mu       sync.Mutex
	entries  []Entry
	minLevel LogLevel
	closed   bool
}

// New creates a LogSystemdModule with the default minimum level Info.
func New() *LogSystemdModule {
	return &LogSystemdModule{minLevel: LevelInfo}
}

// Init (re)initialises the module: clears buffered entries and resets the
// minimum level to Info. It mirrors Kamailio's mod_init.
func (m *LogSystemdModule) Init() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = nil
	m.minLevel = LevelInfo
	m.closed = false
}

// SetLevel sets the minimum level that Log will accept. Unknown levels
// are ignored (the current level is left unchanged).
func (m *LogSystemdModule) SetLevel(level string) {
	lvl, ok := levelNames[strings.ToLower(strings.TrimSpace(level))]
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.minLevel = lvl
}

// Log records a message at the given level unless the level is below the
// configured minimum or the module is closed.
func (m *LogSystemdModule) Log(level string, msg string) {
	lvl, ok := levelNames[strings.ToLower(strings.TrimSpace(level))]
	if !ok {
		lvl = LevelInfo
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || lvl < m.minLevel {
		return
	}
	m.entries = append(m.entries, Entry{
		Time:    time.Now(),
		Level:   lvl,
		Message: msg,
	})
}

// Close flushes and disables the module. Subsequent Log calls are no-ops.
func (m *LogSystemdModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

// Entries returns a copy of the buffered log entries.
func (m *LogSystemdModule) Entries() []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Entry, len(m.entries))
	copy(out, m.entries)
	return out
}

// Count returns the number of buffered entries.
func (m *LogSystemdModule) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// IsClosed reports whether the module has been closed.
func (m *LogSystemdModule) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *LogSystemdModule
)

// DefaultLogSystemd returns the process-wide module, creating it on first use.
func DefaultLogSystemd() *LogSystemdModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init is the package-level (re)initialiser.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
	defaultM.Init()
}

// Log is the package-level wrapper.
func Log(level string, msg string) { DefaultLogSystemd().Log(level, msg) }

// SetLevel is the package-level wrapper.
func SetLevel(level string) { DefaultLogSystemd().SetLevel(level) }

// Close is the package-level wrapper.
func Close() { DefaultLogSystemd().Close() }
