// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * XLog module - extended logging from the config script, matching Kamailio
 * modules/xlog (xlog.c / xl_lib.c).
 *
 * The C xlog module lets the config script emit log lines at any of the
 * Kamailio log levels (xlog/xdbg/xinfo/xnotice/xwarn/xerr/...) with
 * pseudo-variable expansion. The active log level acts as a threshold:
 * only messages at or above the threshold severity are emitted.
 *
 * Here we keep an in-memory ring of recent entries (useful for tests and
 * diagnostics) and optionally mirror each emitted line to an io.Writer. The
 * level numbering follows Kamailio's dprint.h: lower numbers are more
 * severe, so a message at level L is emitted when L <= threshold.
 */

package xlog

import (
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// LogLevel mirrors Kamailio's L_* levels (core/dprint.h). Lower values are
// more severe; a message at level L is emitted when L <= the configured
// threshold.
// C: L_ALERT, L_CRIT, L_ERR, L_WARN, L_NOTICE, L_INFO, L_DBG.
type LogLevel int

const (
	// LevelAlert matches C L_ALERT (-3).
	LevelAlert LogLevel = -3
	// LevelCrit matches C L_CRIT (-2).
	LevelCrit LogLevel = -2
	// LevelError matches C L_ERR (-1).
	LevelError LogLevel = -1
	// LevelWarn matches C L_WARN (0).
	LevelWarn LogLevel = 0
	// LevelNotice matches C L_NOTICE (1).
	LevelNotice LogLevel = 1
	// LevelInfo matches C L_INFO (2).
	LevelInfo LogLevel = 2
	// LevelDebug matches C L_DBG (3).
	LevelDebug LogLevel = 3
)

// maxEntries caps the in-memory entry ring so a long-running process does
// not grow the slice without bound.
const maxEntries = 1024

// LogEntry is a single captured log line.
type LogEntry struct {
	Level     LogLevel
	LevelName string
	Message   string
	Time      time.Time
}

// XLogModule implements the xlog module functionality.
// C: struct module xlog (xlog.c) + cfg_group_xlog.
type XLogModule struct {
	mu      sync.Mutex
	level   LogLevel
	entries []LogEntry
	out     io.Writer
	prefix  string
}

// NewXLogModule creates a new XLogModule with the default info level and a
// "<script>: " prefix, mirroring the C _xlog_prefix default.
func NewXLogModule() *XLogModule {
	return &XLogModule{
		level:  LevelInfo,
		out:    os.Stderr,
		prefix: "<script>: ",
	}
}

// SetOutput redirects emitted log lines to w. Pass nil to disable mirroring
// to an external writer (entries are still captured in memory).
func (m *XLogModule) SetOutput(w io.Writer) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.out = w
}

// SetPrefix sets the prefix prepended to every emitted line, mirroring the
// C _xlog_prefix module parameter.
func (m *XLogModule) SetPrefix(prefix string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prefix = prefix
}

// ---------------------------------------------------------------------------
// Level handling (C: cfg_group_xlog + set log level)
// ---------------------------------------------------------------------------

// parseLevel maps a level name to a LogLevel. Recognised names (case
// insensitive): alert, crit/critical, err/error, warn/warning, notice,
// info, dbg/debug. Unknown names default to LevelInfo.
// C: log_level_name() / dprint level parsing.
func parseLevel(name string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "alert":
		return LevelAlert
	case "crit", "critical":
		return LevelCrit
	case "err", "error":
		return LevelError
	case "warn", "warning":
		return LevelWarn
	case "notice":
		return LevelNotice
	case "info":
		return LevelInfo
	case "dbg", "debug":
		return LevelDebug
	default:
		return LevelInfo
	}
}

// levelName returns the canonical Kamailio name for a LogLevel.
func levelName(l LogLevel) string {
	switch l {
	case LevelAlert:
		return "alert"
	case LevelCrit:
		return "crit"
	case LevelError:
		return "err"
	case LevelWarn:
		return "warn"
	case LevelNotice:
		return "notice"
	case LevelInfo:
		return "info"
	case LevelDebug:
		return "dbg"
	default:
		return "info"
	}
}

// SetLogLevel sets the log level threshold from a level name.
// C: $xlog_level assignment / cfg_set.
func (m *XLogModule) SetLogLevel(level string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.level = parseLevel(level)
}

// SetLogLevelValue sets the log level threshold directly from a LogLevel.
func (m *XLogModule) SetLogLevelValue(level LogLevel) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.level = level
}

// GetLogLevel returns the current log level threshold as its canonical name.
// C: $xlog_level.
func (m *XLogModule) GetLogLevel() string {
	if m == nil {
		return "info"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return levelName(m.level)
}

// GetLogLevelValue returns the current log level threshold as a LogLevel.
func (m *XLogModule) GetLogLevelValue() LogLevel {
	if m == nil {
		return LevelInfo
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.level
}

// IsEnabled returns true if a message at level would be emitted given the
// current threshold. A message is emitted when level <= threshold.
func (m *XLogModule) IsEnabled(level LogLevel) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return level <= m.level
}

// ---------------------------------------------------------------------------
// Logging (C: xlog / xdbg / xinfo / xnotice / xwarn / xerr)
// ---------------------------------------------------------------------------

// Log emits msg at the given level name. Returns true if the message was
// emitted (i.e. the level was enabled), false otherwise.
// C: xlog(level, msg).
func (m *XLogModule) Log(level string, msg string) bool {
	return m.logAt(parseLevel(level), msg)
}

// XLog emits msg at the default info level.
// C: xlog(msg) / xlog_1().
func (m *XLogModule) XLog(msg string) bool {
	return m.logAt(LevelInfo, msg)
}

// XDBG emits msg at debug level.
// C: xdbg().
func (m *XLogModule) XDBG(msg string) bool {
	return m.logAt(LevelDebug, msg)
}

// XERR emits msg at error level.
// C: xerr().
func (m *XLogModule) XERR(msg string) bool {
	return m.logAt(LevelError, msg)
}

// XINFO emits msg at info level.
// C: xinfo().
func (m *XLogModule) XINFO(msg string) bool {
	return m.logAt(LevelInfo, msg)
}

// XWARN emits msg at warn level.
// C: xwarn().
func (m *XLogModule) XWARN(msg string) bool {
	return m.logAt(LevelWarn, msg)
}

// XNOTICE emits msg at notice level.
// C: xnotice().
func (m *XLogModule) XNOTICE(msg string) bool {
	return m.logAt(LevelNotice, msg)
}

// XCRIT emits msg at critical level.
// C: xcrit().
func (m *XLogModule) XCRIT(msg string) bool {
	return m.logAt(LevelCrit, msg)
}

// XALERT emits msg at alert level.
// C: xalert().
func (m *XLogModule) XALERT(msg string) bool {
	return m.logAt(LevelAlert, msg)
}

// logAt is the shared emit path. It records the entry in the in-memory ring
// and mirrors it to the configured writer when the level is enabled.
func (m *XLogModule) logAt(level LogLevel, msg string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	enabled := level <= m.level
	if enabled {
		entry := LogEntry{
			Level:     level,
			LevelName: levelName(level),
			Message:   msg,
			Time:      time.Now(),
		}
		// Ring-buffer style: drop the oldest once we hit the cap.
		if len(m.entries) >= maxEntries {
			m.entries = m.entries[1:]
		}
		m.entries = append(m.entries, entry)
		if m.out != nil {
			// Mirror to the writer; ignore write errors (logging must not
			// disrupt the caller).
			_, _ = io.WriteString(m.out, m.prefix+entry.LevelName+": "+msg+"\n")
		}
	}
	m.mu.Unlock()
	return enabled
}

// ---------------------------------------------------------------------------
// Entry inspection (test/diagnostic helpers)
// ---------------------------------------------------------------------------

// Entries returns a snapshot of the captured log entries.
func (m *XLogModule) Entries() []LogEntry {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]LogEntry, len(m.entries))
	copy(out, m.entries)
	return out
}

// LastEntry returns the most recently emitted entry, or nil if none.
func (m *XLogModule) LastEntry() *LogEntry {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) == 0 {
		return nil
	}
	e := m.entries[len(m.entries)-1]
	return &e
}

// Reset clears the captured log entries.
func (m *XLogModule) Reset() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = nil
}

// ---------------------------------------------------------------------------
// Package-level default instance and global functions
// ---------------------------------------------------------------------------

var (
	defaultMu    sync.Mutex
	defaultXLog  *XLogModule
	defaultOnce  sync.Once
)

// DefaultXLog returns the package-level default XLogModule instance.
func DefaultXLog() *XLogModule {
	defaultOnce.Do(func() {
		defaultXLog = NewXLogModule()
	})
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultXLog == nil {
		defaultXLog = NewXLogModule()
	}
	return defaultXLog
}

// Init initialises the xlog module (resets the default instance).
// Mirrors C mod_init().
func Init() error {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultXLog = NewXLogModule()
	return nil
}

// Log is the package-level wrapper around the default instance.
func Log(level string, msg string) bool {
	return DefaultXLog().Log(level, msg)
}

// XLog is the package-level wrapper around the default instance.
func XLog(msg string) bool {
	return DefaultXLog().XLog(msg)
}

// XDBG is the package-level wrapper around the default instance.
func XDBG(msg string) bool {
	return DefaultXLog().XDBG(msg)
}

// XERR is the package-level wrapper around the default instance.
func XERR(msg string) bool {
	return DefaultXLog().XERR(msg)
}
