// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Flatstore database backend - matching C db_flatstore (db_flatstore.c)
 *
 * Flatstore is an append-only log store: every write appends a delimited
 * row to a per-table flat file. It does not support updates or deletes. This
 * implementation keeps no real buffering (writes are flushed immediately),
 * so Flush is a no-op and Rotate renames the current log files.
 *
 * C equivalent: db_flatstore.so - registers itself via flat_bind_api and
 * exposes connection / query primitives through the srdb1 abstraction.
 */

package db_flatstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// DefaultFlatDelimiter is the default field delimiter.
const DefaultFlatDelimiter = "|"

// DefaultFlatSuffix is the default file suffix.
const DefaultFlatSuffix = ".log"

// FlatConfig holds flatstore connection parameters.
//
// C equivalent: flat_delimiter / flat_suffix / flat_pid module parameters.
type FlatConfig struct {
	Path      string
	Delimiter string
	Suffix    string
}

// DefaultFlatConfig returns a config with sensible defaults.
func DefaultFlatConfig() *FlatConfig {
	return &FlatConfig{
		Path:      "",
		Delimiter: DefaultFlatDelimiter,
		Suffix:    DefaultFlatSuffix,
	}
}

// Validate checks required config fields.
func (c *FlatConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("flatstore config: nil")
	}
	if c.Delimiter == "" {
		return fmt.Errorf("flatstore config: delimiter is required")
	}
	if c.Suffix == "" {
		return fmt.Errorf("flatstore config: suffix is required")
	}
	return nil
}

// ---------------------------------------------------------------------------
// FlatConn - implements db.DBConn against flat log files
// ---------------------------------------------------------------------------

// FlatConn implements db.DBConn backed by append-only flat files.
//
// C equivalent: struct flat_con which holds the FILE* handle per process.
type FlatConn struct {
	mu     sync.Mutex
	cfg    *FlatConfig
	files  map[string]bool // tables that have been written to
	closed bool
}

// Compile-time interface check.
var _ db.DBConn = (*FlatConn)(nil)

// WriteRow appends a row to the flat file for the given table.
func (c *FlatConn) WriteRow(table string, row []string) error {
	if c == nil {
		return fmt.Errorf("nil flatstore conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("flatstore conn closed")
	}
	path := c.tablePath(table)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("flatstore write %q: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(row, c.delim()) + "\n"); err != nil {
		return fmt.Errorf("flatstore write %q: %w", path, err)
	}
	c.files[table] = true
	return nil
}

// ReadTable reads all rows from the flat file for the given table. A
// non-existent table returns an empty slice and no error.
func (c *FlatConn) ReadTable(table string) ([][]string, error) {
	if c == nil {
		return nil, fmt.Errorf("nil flatstore conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.tablePath(table)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return [][]string{}, nil
		}
		return nil, fmt.Errorf("flatstore read %q: %w", path, err)
	}
	var rows [][]string
	for _, line := range splitLines(string(data)) {
		rows = append(rows, strings.Split(line, c.delim()))
	}
	return rows, nil
}

// Flush flushes any buffered writes. Since this implementation writes
// directly to disk, Flush is a no-op.
func (c *FlatConn) Flush() error {
	if c == nil {
		return fmt.Errorf("nil flatstore conn")
	}
	return nil
}

// Rotate renames the current flat files (one per written table) by
// appending a timestamp suffix, so subsequent writes start new files.
func (c *FlatConn) Rotate() error {
	if c == nil {
		return fmt.Errorf("nil flatstore conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("flatstore conn closed")
	}
	stamp := fmt.Sprintf(".%d", time.Now().UnixNano())
	for table := range c.files {
		path := c.tablePath(table)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		rotated := path + stamp
		if err := os.Rename(path, rotated); err != nil {
			return fmt.Errorf("flatstore rotate %q: %w", path, err)
		}
	}
	// Future writes start fresh files.
	c.files = make(map[string]bool)
	return nil
}

// Query returns all rows from a flat table. The where clause is ignored as
// flatstore rows have no column headers.
func (c *FlatConn) Query(table string, keys []db.DBKey, where []db.DBCondition, orderBy string, limit, offset int) (*db.DBResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil flatstore conn")
	}
	rows, err := c.ReadTable(table)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if offset >= len(rows) {
			rows = nil
		} else {
			rows = rows[offset:]
		}
	}
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	return flatRowsToResult(rows), nil
}

// Insert appends a row to a flat table (same as WriteRow).
func (c *FlatConn) Insert(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil {
		return fmt.Errorf("nil flatstore conn")
	}
	row := make([]string, len(values))
	for i, v := range values {
		row[i] = v.String()
	}
	return c.WriteRow(table, row)
}

// Update is a no-op: flatstore is append-only.
func (c *FlatConn) Update(table string, keys []db.DBKey, values []db.DBValue, where []db.DBCondition) (int64, error) {
	return 0, nil
}

// Delete is a no-op: flatstore is append-only.
func (c *FlatConn) Delete(table string, where []db.DBCondition) (int64, error) {
	return 0, nil
}

// Replace appends a row (upsert is not meaningful for an append-only store).
func (c *FlatConn) Replace(table string, keys []db.DBKey, values []db.DBValue) error {
	return c.Insert(table, keys, values)
}

// Raw returns all rows from the table named by the query string.
func (c *FlatConn) Raw(query string, args ...interface{}) (*db.DBResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil flatstore conn")
	}
	rows, err := c.ReadTable(query)
	if err != nil {
		return nil, err
	}
	return flatRowsToResult(rows), nil
}

// Close closes the connection.
func (c *FlatConn) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// Ping checks the connection is alive.
func (c *FlatConn) Ping() error {
	if c == nil {
		return fmt.Errorf("nil flatstore conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("flatstore conn closed")
	}
	return nil
}

func (c *FlatConn) tablePath(table string) string {
	if c.cfg == nil || c.cfg.Path == "" {
		return table + c.suffix()
	}
	return filepath.Join(c.cfg.Path, table+c.suffix())
}

func (c *FlatConn) delim() string {
	if c.cfg == nil || c.cfg.Delimiter == "" {
		return DefaultFlatDelimiter
	}
	return c.cfg.Delimiter
}

func (c *FlatConn) suffix() string {
	if c.cfg == nil || c.cfg.Suffix == "" {
		return DefaultFlatSuffix
	}
	return c.cfg.Suffix
}

// ---------------------------------------------------------------------------
// FlatDriver - implements db.DBDriver
// ---------------------------------------------------------------------------

// FlatDriver implements db.DBDriver for flatstore.
//
// C equivalent: flat_bind_api / the module export table.
type FlatDriver struct{}

// Compile-time interface check.
var _ db.DBDriver = (*FlatDriver)(nil)

// NewFlatDriver creates a new FlatDriver.
func NewFlatDriver() *FlatDriver {
	return &FlatDriver{}
}

// Name returns the driver name used for registration.
func (d *FlatDriver) Name() string {
	return "flatstore"
}

// Open parses a flatstore:// URL (or treats the URL as a Path) and returns a
// db.DBConn.
func (d *FlatDriver) Open(url string) (db.DBConn, error) {
	cfg := DefaultFlatConfig()
	cfg.Path = parseFlatURL(url)
	return &FlatConn{cfg: cfg, files: make(map[string]bool)}, nil
}

// parseFlatURL extracts the Path from a flatstore:// URL.
func parseFlatURL(rawurl string) string {
	if rawurl == "" {
		return ""
	}
	return strings.TrimPrefix(rawurl, "flatstore://")
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// flatRowsToResult builds a DBResult from flat rows using generic column
// names (c0, c1, ...) derived from the widest row.
func flatRowsToResult(rows [][]string) *db.DBResult {
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	keys := make([]db.DBKey, width)
	for i := 0; i < width; i++ {
		keys[i] = db.DBKey{Name: fmt.Sprintf("c%d", i), Type: db.DBValString}
	}
	res := &db.DBResult{Keys: keys}
	for _, r := range rows {
		vals := make([]db.DBValue, width)
		for i := 0; i < width; i++ {
			v := ""
			if i < len(r) {
				v = r[i]
			}
			vals[i] = db.DBValue{Type: db.DBValString, StrVal: v}
		}
		res.Rows = append(res.Rows, &db.DBRow{Keys: keys, Values: vals})
	}
	return res
}

// splitLines splits file content into non-empty trimmed lines.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// init registers the flatstore driver with the global db registry.
func init() {
	_ = db.RegisterDriver(NewFlatDriver())
}
