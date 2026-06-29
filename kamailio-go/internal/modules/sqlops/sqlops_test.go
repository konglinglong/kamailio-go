// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the SQL operations (sqlops) module.
 */

package sqlops

import (
	"errors"
	"sync"
	"testing"
)

// mockExecutor returns a canned result with the given columns and rows.
func mockExecutor(columns []string, rows [][]string, affected int64) QueryExecutor {
	return func(query string, params []interface{}) (*SQLResult, error) {
		return &SQLResult{
			Columns:  columns,
			Rows:     rows,
			Affected: affected,
		}, nil
	}
}

func TestRegisterQuery(t *testing.T) {
	m := New()

	if ret := m.RegisterQuery("get_user", "SELECT * FROM users WHERE id=?"); ret != 0 {
		t.Fatalf("RegisterQuery() = %d, want 0", ret)
	}
	if m.Count() != 1 {
		t.Errorf("Count() = %d, want 1", m.Count())
	}
	list := m.ListQueries()
	if len(list) != 1 || list[0] != "get_user" {
		t.Errorf("ListQueries() = %v, want [get_user]", list)
	}

	// Duplicate name -> -1.
	if ret := m.RegisterQuery("get_user", "SELECT 1"); ret != -1 {
		t.Errorf("RegisterQuery(duplicate) = %d, want -1", ret)
	}
	// Empty name -> -1.
	if ret := m.RegisterQuery("", "SELECT 1"); ret != -1 {
		t.Errorf("RegisterQuery(empty name) = %d, want -1", ret)
	}
	// Empty query -> -1.
	if ret := m.RegisterQuery("q", ""); ret != -1 {
		t.Errorf("RegisterQuery(empty query) = %d, want -1", ret)
	}
}

func TestExecute(t *testing.T) {
	m := New()
	m.SetExecutor(mockExecutor(
		[]string{"id", "name"},
		[][]string{{"1", "alice"}, {"2", "bob"}},
		2,
	))
	m.RegisterQuery("get_users", "SELECT id, name FROM users")

	res, err := m.Execute("get_users", 1)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if res == nil {
		t.Fatalf("Execute() returned nil result")
	}
	if len(res.Columns) != 2 {
		t.Errorf("Columns = %v, want 2 entries", res.Columns)
	}
	if len(res.Rows) != 2 {
		t.Errorf("Rows = %d, want 2", len(res.Rows))
	}
	if res.Affected != 2 {
		t.Errorf("Affected = %d, want 2", res.Affected)
	}

	// Unknown query -> error.
	if _, err := m.Execute("nope"); err == nil {
		t.Errorf("Execute(unknown) should error")
	}
}

func TestExecuteRaw(t *testing.T) {
	m := New()
	m.SetExecutor(mockExecutor(
		[]string{"count"},
		[][]string{{"42"}},
		1,
	))

	res, err := m.ExecuteRaw("SELECT COUNT(*) FROM users")
	if err != nil {
		t.Fatalf("ExecuteRaw() error = %v", err)
	}
	if res.Rows[0][0] != "42" {
		t.Errorf("ExecuteRaw() value = %q, want 42", res.Rows[0][0])
	}
	// The result is cached under the raw query string.
	if got := m.GetResult("SELECT COUNT(*) FROM users"); got == nil {
		t.Errorf("GetResult(raw) returned nil")
	}

	// Empty query -> error.
	if _, err := m.ExecuteRaw(""); err == nil {
		t.Errorf("ExecuteRaw(empty) should error")
	}
}

func TestExecuteError(t *testing.T) {
	m := New()
	m.SetExecutor(func(query string, params []interface{}) (*SQLResult, error) {
		return nil, errors.New("db down")
	})
	m.RegisterQuery("q", "SELECT 1")

	if _, err := m.Execute("q"); err == nil {
		t.Errorf("Execute() with failing executor should error")
	}
}

func TestGetResultAndRowCount(t *testing.T) {
	m := New()
	m.SetExecutor(mockExecutor(
		[]string{"id"},
		[][]string{{"1"}, {"2"}, {"3"}},
		3,
	))
	m.RegisterQuery("q", "SELECT id FROM t")

	m.Execute("q")

	if got := m.RowCount("q"); got != 3 {
		t.Errorf("RowCount() = %d, want 3", got)
	}
	if got := m.RowCount("nope"); got != 0 {
		t.Errorf("RowCount(unknown) = %d, want 0", got)
	}
	if m.GetResult("nope") != nil {
		t.Errorf("GetResult(unknown) should return nil")
	}
}

func TestFieldValue(t *testing.T) {
	m := New()
	m.SetExecutor(mockExecutor(
		[]string{"id", "name"},
		[][]string{{"1", "alice"}, {"2", "bob"}},
		2,
	))
	m.RegisterQuery("q", "SELECT id, name FROM users")
	m.Execute("q")

	if v, ok := m.FieldValue("q", 0, 1); !ok || v != "alice" {
		t.Errorf("FieldValue(0,1) = %q ok=%v, want alice true", v, ok)
	}
	if v, ok := m.FieldValue("q", 1, 0); !ok || v != "2" {
		t.Errorf("FieldValue(1,0) = %q ok=%v, want 2 true", v, ok)
	}
	// Out of range -> false.
	if _, ok := m.FieldValue("q", 5, 0); ok {
		t.Errorf("FieldValue(row OOR) = true, want false")
	}
	if _, ok := m.FieldValue("q", 0, 5); ok {
		t.Errorf("FieldValue(col OOR) = true, want false")
	}
	if _, ok := m.FieldValue("nope", 0, 0); ok {
		t.Errorf("FieldValue(unknown) = true, want false")
	}
}

func TestDefaultExecutor(t *testing.T) {
	// Without SetExecutor the default executor returns an empty result.
	m := New()
	m.RegisterQuery("q", "SELECT 1")
	res, err := m.Execute("q")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if res == nil {
		t.Fatalf("Execute() returned nil")
	}
	if len(res.Rows) != 0 {
		t.Errorf("default executor Rows = %d, want 0", len(res.Rows))
	}

	// SetExecutor(nil) restores the default.
	m.SetExecutor(mockExecutor(nil, [][]string{{"x"}}, 1))
	m.Execute("q")
	if m.RowCount("q") != 1 {
		t.Fatalf("RowCount() after mock = %d, want 1", m.RowCount("q"))
	}
	m.SetExecutor(nil)
	m.Execute("q")
	if m.RowCount("q") != 0 {
		t.Errorf("RowCount() after restore = %d, want 0", m.RowCount("q"))
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultSQLOps()
	if d == nil {
		t.Fatalf("DefaultSQLOps() returned nil")
	}
	if d != DefaultSQLOps() {
		t.Fatalf("DefaultSQLOps() returned different instances after Init()")
	}

	// Re-init resets state.
	d.RegisterQuery("q", "SELECT 1")
	if got := d.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}
	Init()
	if got := DefaultSQLOps().Count(); got != 0 {
		t.Errorf("Count() after re-Init = %d, want 0", got)
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultSQLOps()
	shared.SetExecutor(mockExecutor(
		[]string{"id"},
		[][]string{{"1"}, {"2"}},
		2,
	))
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			name := "q" + itoa(i)
			shared.RegisterQuery(name, "SELECT id FROM t WHERE n=?")
			shared.Execute(name, i)
			shared.GetResult(name)
			shared.RowCount(name)
			shared.FieldValue(name, 0, 0)
			shared.ListQueries()
			shared.Count()
		}(i)
	}
	wg.Wait()
	if got := shared.Count(); got != goroutines {
		t.Errorf("Count() after concurrent = %d, want %d", got, goroutines)
	}
}

// itoa is a tiny local int->string helper to avoid pulling strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
