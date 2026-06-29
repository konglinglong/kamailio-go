// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * kamcmd - command line tool for the Kamailio SIP server.
 *
 * A Go port of utils/kamcmd/kamcmd.c. It connects to Kamailio's JSON-RPC
 * control endpoint and sends commands such as core.ping, stats.get_statistics,
 * dlg.list, htable.list, pike.status, ul.dump, etc. Both JSON-RPC over HTTP
 * and raw TCP (FIFO) transports are supported.
 *
 * Usage:
 *
 *	kamcmd [-s server:port] [-f json|text] [-h] command [params...]
 *
 * The default server is http://localhost:2048.
 */
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultServer is the JSON-RPC endpoint used when -s is not supplied.
const DefaultServer = "http://localhost:2048"

// DefaultTimeout is the deadline for a single JSON-RPC round trip.
const DefaultTimeout = 30 * time.Second

// Output is where PrintResult writes. It defaults to stdout and is
// overridable (e.g. by tests).
var Output io.Writer = os.Stdout

// Format selects how PrintResult renders results: "json" (pretty) or
// "text" (raw). Set by the -f flag.
var Format = "json"

// httpClient is the HTTP client used for JSON-RPC over HTTP.
var httpClient = &http.Client{Timeout: DefaultTimeout}

// requestID is a monotonically increasing JSON-RPC request identifier.
var requestID int64

// Request is a JSON-RPC 2.0 request object.
type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int         `json:"id"`
}

// rpcResponse is a JSON-RPC 2.0 response object. Result is kept as raw
// JSON so it can be decoded into a generic interface{}.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
	ID      int             `json:"id"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// BuildRequest constructs a JSON-RPC 2.0 request for command with the
// optional positional params. Each call yields a fresh, increasing id.
func BuildRequest(command string, params ...interface{}) *Request {
	r := &Request{
		JSONRPC: "2.0",
		Method:  command,
		ID:      int(atomic.AddInt64(&requestID, 1)),
	}
	if len(params) > 0 {
		r.Params = params
	}
	return r
}

// ParseServer normalises a server address. It accepts:
//   - "" (uses DefaultServer)
//   - bare "host:port" (prefixed with http://)
//   - "http://host:port" / "https://host:port"
//   - "tcp://host:port" or "tcp:host:port" (raw TCP/FIFO mode)
//   - "unix:/path" (unix datagram socket)
//
// Any other scheme is rejected with an error.
func ParseServer(server string) (string, error) {
	s := strings.TrimSpace(server)
	if s == "" {
		return DefaultServer, nil
	}
	switch {
	case strings.HasPrefix(s, "tcp://"):
		return s, nil
	case strings.HasPrefix(s, "tcp:"):
		return "tcp://" + strings.TrimPrefix(s, "tcp:"), nil
	case strings.HasPrefix(s, "http://"), strings.HasPrefix(s, "https://"):
		return s, nil
	case strings.HasPrefix(s, "unix:"):
		return s, nil
	case strings.Contains(s, "://"):
		return "", fmt.Errorf("kamcmd: unsupported server scheme %q", s)
	default:
		return "http://" + s, nil
	}
}

// Execute sends a JSON-RPC request to server and returns the decoded
// result. It transparently selects HTTP or raw TCP transport based on the
// server address scheme.
func Execute(server, command string, params ...interface{}) (interface{}, error) {
	addr, err := ParseServer(server)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(addr, "tcp://") {
		return executeTCP(addr, command, params...)
	}
	return executeHTTP(addr, command, params...)
}

// executeHTTP sends a JSON-RPC request over HTTP and decodes the response.
func executeHTTP(addr, command string, params ...interface{}) (interface{}, error) {
	body, err := json.Marshal(BuildRequest(command, params...))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, addr, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("kamcmd: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return decodeRPCResponse(data)
}

// executeTCP sends a JSON-RPC request over a raw TCP (FIFO) socket. The
// request is terminated by a newline and one line of response is read.
func executeTCP(addr, command string, params ...interface{}) (interface{}, error) {
	target := strings.TrimPrefix(addr, "tcp://")
	conn, err := net.DialTimeout("tcp", target, DefaultTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(DefaultTimeout))

	body, err := json.Marshal(BuildRequest(command, params...))
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(body, '\n')); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return nil, err
	}
	return decodeRPCResponse([]byte(line))
}

// decodeRPCResponse parses a JSON-RPC 2.0 response, returning either the
// result or the embedded error.
func decodeRPCResponse(data []byte) (interface{}, error) {
	var resp rpcResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("kamcmd: invalid JSON-RPC response: %w", err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var result interface{}
	if len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("kamcmd: cannot decode result: %w", err)
		}
	}
	return result, nil
}

// PrintResult pretty-prints result to Output according to Format. The
// "json" format (default) emits indented JSON; "text" prints the value
// directly.
func PrintResult(result interface{}) {
	switch Format {
	case "text":
		fmt.Fprintln(Output, result)
	default:
		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			// Fall back to a plain representation for non-JSON-encodable
			// values (e.g. channels, functions).
			fmt.Fprintln(Output, result)
			return
		}
		fmt.Fprintln(Output, string(b))
	}
}

// usage prints the command-line help.
func usage() {
	fmt.Fprintf(Output, `Usage: kamcmd [-s server:port] [-f json|text] [-h] command [params...]

Send a JSON-RPC command to a Kamailio SIP server.

Options:
    -s address   server address (host:port, http://..., tcp://..., unix:...)
                 default: %s
    -f format    output format: json (default) or text
    -h           show this help and exit

Commands (examples):
    core.ping, core.version, stats.get_statistics,
    dlg.list, htable.list, pike.status, ul.dump

Example:
    kamcmd -s localhost:2048 core.ping
    kamcmd -s tcp://localhost:2048 htable.list mytable
`, DefaultServer)
}

// run is the entry point, separated from main for testability.
func run(args []string) int {
	fs := flag.NewFlagSet("kamcmd", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	server := fs.String("s", DefaultServer, "server address")
	format := fs.String("f", "json", "output format: json|text")
	help := fs.Bool("h", false, "show help")
	if err := fs.Parse(args); err != nil {
		usage()
		return 2
	}
	Format = *format
	if *help {
		usage()
		return 0
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "kamcmd: missing command")
		usage()
		return 2
	}
	command := fs.Arg(0)
	var params []interface{}
	for _, a := range fs.Args()[1:] {
		params = append(params, a)
	}
	result, err := Execute(*server, command, params...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kamcmd: %v\n", err)
		return 1
	}
	PrintResult(result)
	return 0
}

func main() {
	os.Exit(run(os.Args[1:]))
}
