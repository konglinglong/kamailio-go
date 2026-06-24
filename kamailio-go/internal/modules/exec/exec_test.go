// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - exec module tests.
 */

package exec

import (
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

func TestExec(t *testing.T) {
	m := New()
	out, code, err := m.Exec("echo", "hello")
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if out != "hello" {
		t.Errorf("out = %q, want %q", out, "hello")
	}
}

func TestExecExitCode(t *testing.T) {
	m := New()
	// sh -c 'exit 7' produces a non-zero exit code.
	out, code, err := m.Exec("sh", "-c", "exit 7")
	if err == nil {
		t.Fatalf("expected error for non-zero exit")
	}
	if code != 7 {
		t.Errorf("code = %d, want 7", code)
	}
	if out != "" {
		t.Errorf("out = %q, want empty", out)
	}
}

func TestExecNotFound(t *testing.T) {
	m := New()
	_, code, err := m.Exec("this-command-does-not-exist-xyz")
	if err == nil {
		t.Fatalf("expected error for missing command")
	}
	if code == 0 {
		t.Errorf("code = 0, want non-zero")
	}
}

func TestExecAsync(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	wg.Add(1)
	var gotOut string
	var gotCode int
	var gotErr error
	m.ExecAsync("echo", func(out string, code int, err error) {
		gotOut, gotCode, gotErr = out, code, err
		wg.Done()
	}, "async")
	wg.Wait()
	if gotErr != nil {
		t.Fatalf("ExecAsync error: %v", gotErr)
	}
	if gotCode != 0 {
		t.Errorf("code = %d, want 0", gotCode)
	}
	if gotOut != "async" {
		t.Errorf("out = %q, want async", gotOut)
	}
}

func TestExecTimeout(t *testing.T) {
	m := New()
	m.SetTimeout(50 * time.Millisecond)
	// sleep longer than the timeout.
	_, _, err := m.Exec("sleep", "2")
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}

func TestSetWorkDir(t *testing.T) {
	m := New()
	m.SetWorkDir("/tmp")
	out, _, err := m.Exec("pwd")
	if err != nil {
		t.Fatalf("pwd error: %v", err)
	}
	if out != "/tmp" {
		t.Errorf("pwd = %q, want /tmp", out)
	}
}

func TestExecURI(t *testing.T) {
	m := New()
	msgRaw := []byte("INVITE sip:alice@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776\r\n" +
		"From: <sip:a@b>;tag=1\r\n" +
		"To: <sip:b@c>\r\n" +
		"Call-ID: 1\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	msg, err := parser.ParseMsg(msgRaw)
	if err != nil {
		t.Fatalf("ParseMsg error: %v", err)
	}
	// echo the request URI back.
	out, err := m.ExecURI(msg, "echo")
	if err != nil {
		t.Fatalf("ExecURI error: %v", err)
	}
	if out != "sip:alice@example.com" {
		t.Errorf("ExecURI out = %q, want sip:alice@example.com", out)
	}
}

func TestExecURINilMsg(t *testing.T) {
	m := New()
	// With a nil message the command runs without the URI argument.
	out, err := m.ExecURI(nil, "echo")
	if err != nil {
		t.Fatalf("ExecURI error: %v", err)
	}
	if out != "" {
		t.Errorf("ExecURI out = %q, want empty", out)
	}
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultExec() == nil {
		t.Fatalf("DefaultExec() nil")
	}
	Init()
	if DefaultExec() == nil {
		t.Fatalf("DefaultExec() nil after Init")
	}
	out, _, err := DefaultExec().Exec("echo", "default")
	if err != nil {
		t.Fatalf("default Exec error: %v", err)
	}
	if out != "default" {
		t.Errorf("default out = %q, want default", out)
	}
}
