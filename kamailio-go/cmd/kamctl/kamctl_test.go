// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - kamctl CLI tests.
 *
 * These tests exercise the in-process helper functions that back each
 * kamctl subcommand (subscriber CRUD against an in-memory SQLite DB).
 * They do not fork the binary; instead they call the run() entrypoint
 * directly so failures are surfaced with line numbers.
 */

package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// runWithCapture invokes run(args) while redirecting stdout and stderr
// to buffers so tests can inspect the output. It returns the exit code
// and the captured stdout/stderr text.
func runWithCapture(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	oldStdout := stdout
	oldStderr := stderr
	var outBuf, errBuf bytes.Buffer
	stdout = &outBuf
	stderr = &errBuf
	t.Cleanup(func() {
		stdout = oldStdout
		stderr = oldStderr
	})
	code := run(args)
	return code, outBuf.String(), errBuf.String()
}

// computeExpectedHA1 mirrors the package-level ha1() helper so the
// tests stay independent of the production code path.
func computeExpectedHA1(user, realm, pass string) string {
	h := md5.Sum([]byte(user + ":" + realm + ":" + pass))
	return hex.EncodeToString(h[:])
}

func TestRun_Version(t *testing.T) {
	code, out, _ := runWithCapture(t, []string{"version"})
	if code != 0 {
		t.Fatalf("version exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "kamctl version") {
		t.Errorf("version output = %q", out)
	}
}

func TestRun_Help(t *testing.T) {
	code, out, _ := runWithCapture(t, []string{"help"})
	if code != 0 {
		t.Fatalf("help exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "kamctl - operator control CLI") {
		t.Errorf("help output missing banner: %q", out)
	}
	if !strings.Contains(out, "sub add") {
		t.Errorf("help output missing subcommands: %q", out)
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	code, _, errOut := runWithCapture(t, []string{"bogus-command"})
	if code != 2 {
		t.Fatalf("unknown command exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestSub_AddListPasswdRm(t *testing.T) {
	// Each :memory: SQLite opens a fresh in-memory DB, so we use a
	// temp file path that persists across runWithCapture calls within
	// this test. t.TempDir() is cleaned up automatically.
	dbPath := filepath.Join(t.TempDir(), "kamctl_test.db")
	dbURL := "sqlite:" + dbPath
	const realm = "example.org"

	// sub add alice secretpass alice@example.org
	code, out, errOut := runWithCapture(t, []string{
		"-db", dbURL, "-realm", realm, "sub", "add", "alice", "secretpass", "alice@example.org",
	})
	if code != 0 {
		t.Fatalf("sub add: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "added subscriber alice@example.org") {
		t.Errorf("sub add stdout = %q", out)
	}

	// sub list should now show alice.
	code, out, _ = runWithCapture(t, []string{
		"-db", dbURL, "-realm", realm, "sub", "list",
	})
	if code != 0 {
		t.Fatalf("sub list: code=%d", code)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("sub list stdout = %q", out)
	}

	// sub passwd alice newpass
	code, out, errOut = runWithCapture(t, []string{
		"-db", dbURL, "-realm", realm, "sub", "passwd", "alice", "newpass",
	})
	if code != 0 {
		t.Fatalf("sub passwd: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "updated password for alice@example.org") {
		t.Errorf("sub passwd stdout = %q", out)
	}

	// Verify the stored HA1 matches what we expect after passwd.
	// We re-open the DB via the production openDB() helper.
	conn, err := openDB(dbURL)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer conn.Close()
	rows, err := conn.Query("subscriber",
		[]db.DBKey{
			{Name: "username", Type: db.DBValString},
			{Name: "ha1", Type: db.DBValString},
			{Name: "ha1b", Type: db.DBValString},
		},
		[]db.DBCondition{{Key: "username", Op: "=", Value: db.NewStringValue("alice")}},
		"", 0, 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if rows.RowCount() != 1 {
		t.Fatalf("RowCount = %d, want 1", rows.RowCount())
	}
	gotHA1 := rows.Row(0).GetString("ha1")
	wantHA1 := computeExpectedHA1("alice", realm, "newpass")
	if gotHA1 != wantHA1 {
		t.Errorf("stored ha1 = %q, want %q", gotHA1, wantHA1)
	}
	gotHA1B := rows.Row(0).GetString("ha1b")
	wantHA1B := computeExpectedHA1("alice@"+realm, realm, "newpass")
	if gotHA1B != wantHA1B {
		t.Errorf("stored ha1b = %q, want %q", gotHA1B, wantHA1B)
	}

	// sub rm alice
	code, out, _ = runWithCapture(t, []string{
		"-db", dbURL, "-realm", realm, "sub", "rm", "alice",
	})
	if code != 0 {
		t.Fatalf("sub rm: code=%d", code)
	}
	if !strings.Contains(out, "removed 1 subscriber") {
		t.Errorf("sub rm stdout = %q", out)
	}

	// sub list should now be empty.
	code, out, _ = runWithCapture(t, []string{
		"-db", dbURL, "-realm", realm, "sub", "list",
	})
	if code != 0 {
		t.Fatalf("sub list after rm: code=%d", code)
	}
	if !strings.Contains(out, "no subscribers") {
		t.Errorf("sub list after rm stdout = %q", out)
	}
}

func TestSub_AddRequiresDBFlag(t *testing.T) {
	// Without -db, sub add must fail with a clear error.
	code, _, errOut := runWithCapture(t, []string{
		"sub", "add", "alice", "pass",
	})
	if code == 0 {
		t.Fatal("sub add without -db should fail")
	}
	if !strings.Contains(errOut, "-db URL is required") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestSub_RmNoSuchUser(t *testing.T) {
	code, _, errOut := runWithCapture(t, []string{
		"-db", "sqlite::memory:", "-realm", "example.org",
		"sub", "rm", "nobody",
	})
	if code == 0 {
		t.Fatal("sub rm of missing user should fail")
	}
	if !strings.Contains(errOut, "no such subscriber") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestHA1Helpers(t *testing.T) {
	// Sanity-check the HA1 / HA1B helpers against known vectors.
	if got := ha1("alice", "example.org", "secret"); got != computeExpectedHA1("alice", "example.org", "secret") {
		t.Errorf("ha1 mismatch: %q vs %q", got, computeExpectedHA1("alice", "example.org", "secret"))
	}
	// HA1B uses "user@realm" as the username portion.
	if got := ha1b("alice", "example.org", "secret"); got != computeExpectedHA1("alice@example.org", "example.org", "secret") {
		t.Errorf("ha1b mismatch: %q vs %q", got, computeExpectedHA1("alice@example.org", "example.org", "secret"))
	}
}

func TestParseDriverURL(t *testing.T) {
	cases := []struct {
		in       string
		driver   string
		rest     string
		wantErr  bool
	}{
		{"sqlite:./foo.db", "sqlite", "./foo.db", false},
		{"sqlite::memory:", "sqlite", ":memory:", false},
		{"postgres:host=localhost", "postgres", "host=localhost", false},
		{"malformed", "", "", true},
	}
	for _, c := range cases {
		d, r, err := parseDriverURL(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseDriverURL(%q) = (%q,%q,%v); want error", c.in, d, r, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDriverURL(%q) err: %v", c.in, err)
			continue
		}
		if d != c.driver || r != c.rest {
			t.Errorf("parseDriverURL(%q) = (%q,%q); want (%q,%q)", c.in, d, r, c.driver, c.rest)
		}
	}
}
