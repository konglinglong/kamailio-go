// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * DBText database backend - matching C db_text (db_text.c / dbt_lib.c)
 *
 * Stores tables as plain CSV-like text files. The first line of each file is
 * the header (column names); subsequent lines are data rows. Fields are
 * separated by a configurable delimiter (default ",").
 *
 * C equivalent: db_text.so - registers itself via dbt_bind_api and exposes
 * connection / query primitives through the srdb1 abstraction.
 */

package db_text

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// DefaultDelimiter is the default field delimiter.
const DefaultDelimiter = ","

// defaultDelimiter is the delimiter used by the package-level file helpers.
var defaultDelimiter = DefaultDelimiter

// TextConfig holds DBText connection parameters.
//
// C equivalent: db_path / dbt_delim module parameters.
type TextConfig struct {
	DBPath    string
	Delimiter string
}

// DefaultTextConfig returns a config with sensible defaults.
func DefaultTextConfig() *TextConfig {
	return &TextConfig{
		DBPath:    "",
		Delimiter: DefaultDelimiter,
	}
}

// Validate checks required config fields.
func (c *TextConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("db_text config: nil")
	}
	if c.Delimiter == "" {
		return fmt.Errorf("db_text config: delimiter is required")
	}
	return nil
}

// TextTable represents a single text table loaded into memory.
type TextTable struct {
	Name    string
	Headers []string
	Rows    [][]string
}

// ---------------------------------------------------------------------------
// Package-level file helpers (table argument is a file path)
// ---------------------------------------------------------------------------

// LoadTable loads a CSV-like text file into a TextTable. The first line is
// treated as the header row.
func LoadTable(path string) (*TextTable, error) {
	return loadTableDelim(path, defaultDelimiter)
}

// SaveTable writes a TextTable to a file path.
func SaveTable(table *TextTable, path string) error {
	return saveTableDelim(table, path, defaultDelimiter)
}

// Query returns the data rows of the table at path matching the given
// conditions. A row matches when, for every column/value pair in conditions,
// the row's value at that column equals the requested value.
func Query(table string, conditions map[string]string) ([][]string, error) {
	return queryDelim(table, defaultDelimiter, conditions)
}

// InsertRow appends a data row to the table file at path.
func InsertRow(table string, row []string) error {
	return insertRowDelim(table, defaultDelimiter, row)
}

// DeleteRow removes rows matching the conditions from the table file at path
// and returns the number of deleted rows.
func DeleteRow(table string, conditions map[string]string) (int, error) {
	return deleteRowDelim(table, defaultDelimiter, conditions)
}

// ---------------------------------------------------------------------------
// Internal helpers parameterised by delimiter
// ---------------------------------------------------------------------------

func loadTableDelim(path, delim string) (*TextTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("db_text load %q: %w", path, err)
	}
	t := &TextTable{Name: tableName(path)}
	lines := splitLines(string(data))
	if len(lines) == 0 {
		return t, nil
	}
	t.Headers = splitFields(lines[0], delim)
	for _, line := range lines[1:] {
		t.Rows = append(t.Rows, splitFields(line, delim))
	}
	return t, nil
}

func saveTableDelim(table *TextTable, path, delim string) error {
	if table == nil {
		return fmt.Errorf("db_text save: nil table")
	}
	var sb strings.Builder
	if len(table.Headers) > 0 {
		sb.WriteString(strings.Join(table.Headers, delim))
		sb.WriteByte('\n')
	}
	for _, row := range table.Rows {
		sb.WriteString(strings.Join(row, delim))
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("db_text save %q: %w", path, err)
	}
	return nil
}

func queryDelim(path, delim string, conditions map[string]string) ([][]string, error) {
	t, err := loadTableDelim(path, delim)
	if err != nil {
		return nil, err
	}
	return filterRows(t, conditions), nil
}

func insertRowDelim(path, delim string, row []string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("db_text insert %q: %w", path, err)
	}
	defer f.Close()
	_, err = f.WriteString(strings.Join(row, delim) + "\n")
	return err
}

func deleteRowDelim(path, delim string, conditions map[string]string) (int, error) {
	t, err := loadTableDelim(path, delim)
	if err != nil {
		return 0, err
	}
	kept := make([][]string, 0, len(t.Rows))
	var n int
	for _, row := range t.Rows {
		if rowMatchesText(t.Headers, row, conditions) {
			n++
			continue
		}
		kept = append(kept, row)
	}
	t.Rows = kept
	if err := saveTableDelim(t, path, delim); err != nil {
		return 0, err
	}
	return n, nil
}

// filterRows returns the data rows matching the conditions.
func filterRows(t *TextTable, conditions map[string]string) [][]string {
	var out [][]string
	for _, row := range t.Rows {
		if rowMatchesText(t.Headers, row, conditions) {
			out = append(out, row)
		}
	}
	return out
}

// rowMatchesText reports whether a row matches all conditions by column name.
func rowMatchesText(headers []string, row []string, conditions map[string]string) bool {
	for col, want := range conditions {
		idx := indexOf(headers, col)
		if idx < 0 || idx >= len(row) {
			return false
		}
		if row[idx] != want {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// TextConn - implements db.DBConn against text files
// ---------------------------------------------------------------------------

// TextConn implements db.DBConn backed by text files in DBPath.
//
// C equivalent: struct dbt_con which holds the cached table handle.
type TextConn struct {
	mu  sync.Mutex
	cfg *TextConfig
}

// Compile-time interface check.
var _ db.DBConn = (*TextConn)(nil)

// Query selects rows from a text table matching the where clause.
func (c *TextConn) Query(table string, keys []db.DBKey, where []db.DBCondition, orderBy string, limit, offset int) (*db.DBResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil db_text conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.tablePath(table)
	t, err := loadTableDelim(path, c.delim())
	if err != nil {
		return nil, err
	}
	conds := whereToMap(where)
	rows := filterRows(t, conds)
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
	return textRowsToResult(t.Headers, rows), nil
}

// Insert appends a row to a text table, creating the file with a header
// line derived from keys when it does not yet exist.
func (c *TextConn) Insert(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil {
		return fmt.Errorf("nil db_text conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("insert: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.tablePath(table)
	row := valuesToStrings(values)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		headers := make([]string, len(keys))
		for i, k := range keys {
			headers[i] = k.Name
		}
		t := &TextTable{Name: table, Headers: headers, Rows: [][]string{row}}
		return saveTableDelim(t, path, c.delim())
	}
	return insertRowDelim(path, c.delim(), row)
}

// Update updates rows matching the where clause.
func (c *TextConn) Update(table string, keys []db.DBKey, values []db.DBValue, where []db.DBCondition) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("nil db_text conn")
	}
	if len(keys) != len(values) {
		return 0, fmt.Errorf("update: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.tablePath(table)
	t, err := loadTableDelim(path, c.delim())
	if err != nil {
		return 0, err
	}
	conds := whereToMap(where)
	colIdx := make(map[string]int, len(keys))
	for _, k := range keys {
		colIdx[k.Name] = indexOf(t.Headers, k.Name)
	}
	var n int64
	for _, row := range t.Rows {
		if !rowMatchesText(t.Headers, row, conds) {
			continue
		}
		for i, k := range keys {
			idx := colIdx[k.Name]
			if idx >= 0 && idx < len(row) {
				row[idx] = values[i].String()
			}
		}
		n++
	}
	if err := saveTableDelim(t, path, c.delim()); err != nil {
		return 0, err
	}
	return n, nil
}

// Delete removes rows matching the where clause.
func (c *TextConn) Delete(table string, where []db.DBCondition) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("nil db_text conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.tablePath(table)
	n, err := deleteRowDelim(path, c.delim(), whereToMap(where))
	if err != nil {
		return 0, err
	}
	return int64(n), nil
}

// Replace inserts or updates a row, matching on the first key.
func (c *TextConn) Replace(table string, keys []db.DBKey, values []db.DBValue) error {
	if c == nil {
		return fmt.Errorf("nil db_text conn")
	}
	if len(keys) != len(values) {
		return fmt.Errorf("replace: key/value length mismatch (%d vs %d)", len(keys), len(values))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.tablePath(table)
	t, err := loadTableDelim(path, c.delim())
	if err != nil {
		// Table does not exist: create it with headers and the row.
		headers := make([]string, len(keys))
		for i, k := range keys {
			headers[i] = k.Name
		}
		t = &TextTable{Name: table, Headers: headers}
	}
	pkIdx := indexOf(t.Headers, keys[0].Name)
	pv := values[0].String()
	for _, row := range t.Rows {
		if pkIdx >= 0 && pkIdx < len(row) && row[pkIdx] == pv {
			for i, k := range keys {
				idx := indexOf(t.Headers, k.Name)
				if idx >= 0 && idx < len(row) {
					row[idx] = values[i].String()
				}
			}
			return saveTableDelim(t, path, c.delim())
		}
	}
	t.Rows = append(t.Rows, valuesToStrings(values))
	return saveTableDelim(t, path, c.delim())
}

// Raw executes a raw query. This mock loads the table named by the query.
func (c *TextConn) Raw(query string, args ...interface{}) (*db.DBResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil db_text conn")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.tablePath(query)
	t, err := loadTableDelim(path, c.delim())
	if err != nil {
		return &db.DBResult{}, nil
	}
	return textRowsToResult(t.Headers, t.Rows), nil
}

// Close closes the connection (no-op for text files).
func (c *TextConn) Close() error {
	return nil
}

// Ping checks the connection is alive.
func (c *TextConn) Ping() error {
	if c == nil {
		return fmt.Errorf("nil db_text conn")
	}
	return nil
}

func (c *TextConn) tablePath(table string) string {
	if c.cfg == nil || c.cfg.DBPath == "" {
		return table
	}
	return filepath.Join(c.cfg.DBPath, table)
}

func (c *TextConn) delim() string {
	if c.cfg == nil || c.cfg.Delimiter == "" {
		return DefaultDelimiter
	}
	return c.cfg.Delimiter
}

// ---------------------------------------------------------------------------
// TextDriver - implements db.DBDriver
// ---------------------------------------------------------------------------

// TextDriver implements db.DBDriver for DBText.
//
// C equivalent: dbt_bind_api / the module export table.
type TextDriver struct{}

// Compile-time interface check.
var _ db.DBDriver = (*TextDriver)(nil)

// NewTextDriver creates a new TextDriver.
func NewTextDriver() *TextDriver {
	return &TextDriver{}
}

// Name returns the driver name used for registration.
func (d *TextDriver) Name() string {
	return "text"
}

// Open parses a text:// URL (or treats the URL as a DBPath) and returns a
// db.DBConn.
func (d *TextDriver) Open(url string) (db.DBConn, error) {
	cfg := DefaultTextConfig()
	cfg.DBPath = parseTextURL(url)
	return &TextConn{cfg: cfg}, nil
}

// parseTextURL extracts the DBPath from a text:// URL.
func parseTextURL(rawurl string) string {
	if rawurl == "" {
		return ""
	}
	return strings.TrimPrefix(rawurl, "text://")
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// whereToMap converts a slice of DBCondition (equality only) to a map.
func whereToMap(where []db.DBCondition) map[string]string {
	m := make(map[string]string, len(where))
	for _, cond := range where {
		m[cond.Key] = cond.Value.String()
	}
	return m
}

// valuesToStrings converts a slice of DBValue to strings.
func valuesToStrings(values []db.DBValue) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = v.String()
	}
	return out
}

// textRowsToResult builds a DBResult from text headers and rows.
func textRowsToResult(headers []string, rows [][]string) *db.DBResult {
	keys := make([]db.DBKey, len(headers))
	for i, h := range headers {
		keys[i] = db.DBKey{Name: h, Type: db.DBValString}
	}
	res := &db.DBResult{Keys: keys}
	for _, row := range rows {
		vals := make([]db.DBValue, len(keys))
		for i := range keys {
			v := ""
			if i < len(row) {
				v = row[i]
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

// splitFields splits a line into fields by delimiter.
func splitFields(line, delim string) []string {
	return strings.Split(line, delim)
}

// indexOf returns the index of s in slice, or -1.
func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}

// tableName derives a table name from a file path.
func tableName(path string) string {
	base := filepath.Base(path)
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		return base[:idx]
	}
	return base
}

// init registers the DBText driver with the global db registry.
func init() {
	_ = db.RegisterDriver(NewTextDriver())
}
