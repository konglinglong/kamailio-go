// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * LogCustom module - custom file logging.
 * Port of a kamailio custom-logging module analogy.
 *
 * The log_custom module writes log lines to a configurable file with a
 * level threshold (alert, crit, err, warn, notice, info, debug). A
 * message is emitted when its level is at or below the configured
 * threshold severity (lower numbers are more severe, matching
 * Kamailio's L_* levels).
 *
 * It is safe for concurrent use.
 */

package log_custom

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// logLevel mirrors Kamailio's L_* levels. Lower values are more severe.
type logLevel int

const (
	levelAlert  logLevel = -3
	levelCrit   logLevel = -2
	levelErr    logLevel = -1
	levelWarn   logLevel = 0
	levelNotice logLevel = 1
	levelInfo   logLevel = 2
	levelDebug  logLevel = 3
)

// LogCustomModule writes level-filtered log lines to a file.
type LogCustomModule struct {
	mu    sync.Mutex
	level logLevel
	file  *os.File
}

// New creates a LogCustomModule with the default info level and no file.
func New() *LogCustomModule {
	return &LogCustomModule{level: levelInfo}
}

// parseLevel maps a level name to a logLevel. Unknown names default to info.
func parseLevel(name string) logLevel {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "alert":
		return levelAlert
	case "crit", "critical":
		return levelCrit
	case "err", "error":
		return levelErr
	case "warn", "warning":
		return levelWarn
	case "notice":
		return levelNotice
	case "info":
		return levelInfo
	case "dbg", "debug":
		return levelDebug
	default:
		return levelInfo
	}
}

// levelName returns the canonical name for a logLevel.
func levelName(l logLevel) string {
	switch l {
	case levelAlert:
		return "alert"
	case levelCrit:
		return "crit"
	case levelErr:
		return "err"
	case levelWarn:
		return "warn"
	case levelNotice:
		return "notice"
	case levelInfo:
		return "info"
	case levelDebug:
		return "debug"
	default:
		return "info"
	}
}

// Init opens the log file at path for appending (creating it when missing)
// and resets the level to info. If the file cannot be opened the module
// remains usable but Log is a no-op.
//
//	C: log_custom_init()
func (m *LogCustomModule) Init(path string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.file != nil {
		_ = m.file.Close()
		m.file = nil
	}
	if path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			m.file = f
		}
	}
	m.level = levelInfo
}

// Log writes msg at the given level when that level is at or below the
// configured threshold. The line is prefixed with a timestamp and the
// level name.
//
//	C: log_custom_log()
func (m *LogCustomModule) Log(level, msg string) {
	if m == nil {
		return
	}
	lv := parseLevel(level)
	m.mu.Lock()
	defer m.mu.Unlock()
	if lv > m.level || m.file == nil {
		return
	}
	line := fmt.Sprintf("%s %s: %s\n", time.Now().Format("2006-01-02 15:04:05"), levelName(lv), msg)
	_, _ = m.file.WriteString(line)
}

// SetLevel sets the level threshold from a level name.
//
//	C: log_custom_set_level()
func (m *LogCustomModule) SetLevel(level string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.level = parseLevel(level)
}

// GetLevel returns the current level threshold as its canonical name.
func (m *LogCustomModule) GetLevel() string {
	if m == nil {
		return "info"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return levelName(m.level)
}

// Close closes the underlying log file. Subsequent Log calls are no-ops.
func (m *LogCustomModule) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.file != nil {
		_ = m.file.Close()
		m.file = nil
	}
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultLogCustom *LogCustomModule
)

// DefaultLogCustom returns the process-wide LogCustomModule, creating it on
// first use.
func DefaultLogCustom() *LogCustomModule {
	defaultMu.RLock()
	m := defaultLogCustom
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultLogCustom == nil {
		defaultLogCustom = New()
	}
	return defaultLogCustom
}

// Init (re)initialises the process-wide LogCustomModule to a fresh,
// unconfigured state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultLogCustom = New()
}
