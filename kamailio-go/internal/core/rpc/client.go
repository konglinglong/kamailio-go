// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * JSON-RPC 2.0 client - shared by kamcmd and kamctl.
 *
 * This file factors the JSON-RPC client previously duplicated in
 * cmd/kamcmd into the rpc package so that kamctl (and any future
 * tooling) can reuse the same transport, request building, and result
 * printing logic.
 *
 * Both HTTP and raw TCP (FIFO-style) transports are supported, selected
 * by the server URL scheme:
 *
 *	http://host:port, https://host:port   -> HTTP POST
 *	tcp://host:port, tcp:host:port        -> raw TCP, newline-framed
 *	unix:/path                            -> (reserved, not yet wired)
 *	bare host:port                        -> prefixed with http://
 */

package rpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Client is a JSON-RPC 2.0 client. The zero value is not usable;
// construct one with NewClient. A Client is safe for concurrent use.
type Client struct {
	server  string
	timeout time.Duration
	http    *http.Client
	idSeq   int64
}

// NewClient returns a Client that talks to server (any scheme accepted
// by NormalizeServer). A timeout of zero means "no timeout"; prefer a
// positive value in production.
func NewClient(server string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		server:  server,
		timeout: timeout,
		http:    &http.Client{Timeout: timeout},
	}
}

// ClientRequest is a JSON-RPC 2.0 request object.
type ClientRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int64       `json:"id"`
}

// clientResponse is a JSON-RPC 2.0 response object. Result is kept as
// raw JSON so it can be decoded into a generic interface{}.
type clientResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
	ID      int64           `json:"id"`
}

// NormalizeServer normalises a server address. It accepts:
//   - "" (returns DefaultClientServer)
//   - bare "host:port" (prefixed with http://)
//   - "http://host:port" / "https://host:port"
//   - "tcp://host:port" or "tcp:host:port" (raw TCP mode)
//   - "unix:/path" (reserved)
//
// Any other scheme is rejected with an error.
func NormalizeServer(server string) (string, error) {
	s := strings.TrimSpace(server)
	if s == "" {
		return DefaultClientServer, nil
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
		return "", fmt.Errorf("rpc: unsupported server scheme %q", s)
	default:
		return "http://" + s, nil
	}
}

// DefaultClientServer is the JSON-RPC endpoint used when no -s flag is
// supplied to kamcmd / kamctl.
const DefaultClientServer = "http://localhost:2048"

// Call sends a JSON-RPC 2.0 request for method with the given params
// (positional) and returns the decoded result. It transparently
// selects HTTP or raw TCP transport based on the server URL scheme.
func (c *Client) Call(method string, params ...interface{}) (interface{}, error) {
	addr, err := NormalizeServer(c.server)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(addr, "tcp://") {
		return c.callTCP(addr, method, params...)
	}
	return c.callHTTP(addr, method, params...)
}

// BuildRequest constructs a JSON-RPC 2.0 request for method with the
// optional positional params. Each call yields a fresh, increasing id.
func (c *Client) BuildRequest(method string, params ...interface{}) *ClientRequest {
	r := &ClientRequest{
		JSONRPC: "2.0",
		Method:  method,
		ID:      atomic.AddInt64(&c.idSeq, 1),
	}
	if len(params) > 0 {
		r.Params = params
	}
	return r
}

// callHTTP sends a JSON-RPC request over HTTP and decodes the response.
func (c *Client) callHTTP(addr, method string, params ...interface{}) (interface{}, error) {
	body, err := json.Marshal(c.BuildRequest(method, params...))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, addr, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("rpc: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return decodeClientResponse(data)
}

// callTCP sends a JSON-RPC request over a raw TCP (FIFO) socket. The
// request is terminated by a newline and one line of response is read.
func (c *Client) callTCP(addr, method string, params ...interface{}) (interface{}, error) {
	target := strings.TrimPrefix(addr, "tcp://")
	conn, err := net.DialTimeout("tcp", target, c.timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(c.timeout))

	body, err := json.Marshal(c.BuildRequest(method, params...))
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
	return decodeClientResponse([]byte(line))
}

// decodeClientResponse parses a JSON-RPC 2.0 response, returning either
// the result or the embedded error.
func decodeClientResponse(data []byte) (interface{}, error) {
	var resp clientResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("rpc: invalid JSON-RPC response: %w", err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var result interface{}
	if len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("rpc: cannot decode result: %w", err)
		}
	}
	return result, nil
}

// PrintResult writes result to w in the requested format. "json" emits
// indented JSON; "text" prints the value directly via fmt. Unknown
// formats fall back to "json".
func PrintResult(w io.Writer, result interface{}, format string) {
	switch format {
	case "text":
		fmt.Fprintln(w, result)
	default:
		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintln(w, result)
			return
		}
		fmt.Fprintln(w, string(b))
	}
}
