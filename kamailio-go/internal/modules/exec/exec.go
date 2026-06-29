// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * exec module - external command execution.
 * Port of the kamailio exec module (src/modules/exec).
 *
 * The original C module uses popen() to run shell commands from the
 * Kamailio script, optionally feeding the SIP message to the command's
 * stdin (exec_msg) or passing the request URI (exec_uri). This Go
 * counterpart uses os/exec with a configurable timeout and working
 * directory, exposing synchronous and asynchronous execution.
 *
 * It is safe for concurrent use.
 */

package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// DefaultTimeout is the default command execution timeout.
const DefaultTimeout = 30 * time.Second

// ExecConfig configures an ExecModule.
type ExecConfig struct {
	Timeout time.Duration
	WorkDir string
}

// ExecModule runs external commands.
// It is the Go counterpart of the kamailio exec module.
type ExecModule struct {
	mu      sync.RWMutex
	timeout time.Duration
	workDir string
}

// New creates an ExecModule with default settings.
func New() *ExecModule {
	return &ExecModule{timeout: DefaultTimeout}
}

// Init (re)configures the module from cfg. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *ExecModule) Init(cfg *ExecConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		m.timeout = DefaultTimeout
		m.workDir = ""
		return
	}
	if cfg.Timeout > 0 {
		m.timeout = cfg.Timeout
	} else {
		m.timeout = DefaultTimeout
	}
	m.workDir = cfg.WorkDir
}

// SetTimeout updates the execution timeout for subsequent commands.
func (m *ExecModule) SetTimeout(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if timeout > 0 {
		m.timeout = timeout
	}
}

// SetWorkDir updates the working directory for subsequent commands.
func (m *ExecModule) SetWorkDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workDir = dir
}

// Exec runs command with args synchronously and returns the trimmed
// stdout, the process exit code, and any error. A non-zero exit code is
// reported both via the returned code and a non-nil error.
//
//	C: exec_str() / popen()
func (m *ExecModule) Exec(command string, args ...string) (string, int, error) {
	return m.run(command, args)
}

// ExecAsync runs command with args in a goroutine and invokes callback
// with the stdout, exit code and error when it completes.
//
// NOTE: Go forbids a parameter after a variadic one, so the callback
// precedes the variadic args. This is the only valid form that keeps
// args variadic.
func (m *ExecModule) ExecAsync(command string, callback func(stdout string, exitcode int, err error), args ...string) {
	go func() {
		stdout, code, err := m.run(command, args)
		if callback != nil {
			callback(stdout, code, err)
		}
	}()
}

// ExecURI runs command, passing the request URI of msg as an argument.
// This mirrors the kamailio exec_uri() behaviour, where the request URI
// is made available to the executed command. If msg is nil or has no
// request URI the command is run without the extra argument.
//
//	C: exec_uri()
func (m *ExecModule) ExecURI(msg *parser.SIPMsg, uri string) (string, error) {
	args := []string{}
	if msg != nil && msg.FirstLine != nil && msg.FirstLine.Req != nil {
		if ruri := msg.FirstLine.Req.URI.String(); ruri != "" {
			args = append(args, ruri)
		}
	}
	stdout, _, err := m.run(uri, args)
	return stdout, err
}

// run executes the command with the configured timeout and work dir.
func (m *ExecModule) run(command string, args []string) (string, int, error) {
	m.mu.RLock()
	timeout := m.timeout
	workDir := m.workDir
	m.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		code := exitCode(err)
		if ctx.Err() == context.DeadlineExceeded {
			return strings.TrimSpace(stdout.String()), code,
				fmt.Errorf("exec: timeout after %s: %w", timeout, err)
		}
		return strings.TrimSpace(stdout.String()), code,
			fmt.Errorf("exec: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), 0, nil
}

// exitCode extracts the process exit code from an exec error, or
// returns 1 when it cannot be determined.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

// --- package-level API ---

var defaultModule = New()

// DefaultExec returns the package-level default ExecModule.
func DefaultExec() *ExecModule {
	return defaultModule
}

// Init (re)initialises the package-level default module with defaults.
func Init() {
	defaultModule = New()
}
