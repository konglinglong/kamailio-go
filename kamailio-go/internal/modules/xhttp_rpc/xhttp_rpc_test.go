// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - xhttp_rpc module tests.
 */

package xhttp_rpc

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestInit(t *testing.T) {
	m := New()
	if err := m.Init(&XHTTPRPCConfig{
		ListenAddr: "127.0.0.1:9090",
		Path:       "/RPC",
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if m.cfg == nil {
		t.Fatalf("cfg not set after Init()")
	}
	if m.cfg.ListenAddr != "127.0.0.1:9090" {
		t.Errorf("ListenAddr = %q", m.cfg.ListenAddr)
	}
	if m.cfg.Path != "/RPC" {
		t.Errorf("Path = %q", m.cfg.Path)
	}
	// nil config is accepted.
	if err := (&XHTTPRPCModule{}).Init(nil); err != nil {
		t.Errorf("Init(nil) error = %v", err)
	}
}

func TestRegisterAndHandle(t *testing.T) {
	m := New()
	m.RegisterMethod("subtract", func(params interface{}) (interface{}, error) {
		arr, ok := params.([]interface{})
		if !ok || len(arr) != 2 {
			return nil, nil
		}
		a, _ := arr[0].(float64)
		b, _ := arr[1].(float64)
		return a - b, nil
	})

	req := `{"jsonrpc":"2.0","method":"subtract","params":[42,23],"id":1}`
	out, err := m.HandleRequest([]byte(req))
	if err != nil {
		t.Fatalf("HandleRequest() error = %v", err)
	}
	var resp struct {
		Result float64 `json:"result"`
		ID     int     `json:"id"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal error: %v\nbody: %s", err, out)
	}
	if resp.Result != 19 {
		t.Errorf("result = %v, want 19", resp.Result)
	}
	if resp.ID != 1 {
		t.Errorf("id = %v, want 1", resp.ID)
	}
}

func TestMethodNotFound(t *testing.T) {
	m := New()
	req := `{"jsonrpc":"2.0","method":"missing","params":null,"id":2}`
	out, err := m.HandleRequest([]byte(req))
	if err != nil {
		t.Fatalf("HandleRequest() error = %v", err)
	}
	var resp struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		ID int `json:"id"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal error: %v\nbody: %s", err, out)
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
	if resp.ID != 2 {
		t.Errorf("id = %v, want 2", resp.ID)
	}
}

func TestHandlerError(t *testing.T) {
	m := New()
	m.RegisterMethod("boom", func(params interface{}) (interface{}, error) {
		return nil, errSentinel
	})
	req := `{"jsonrpc":"2.0","method":"boom","id":3}`
	out, _ := m.HandleRequest([]byte(req))
	var resp struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(out, &resp)
	if resp.Error.Code != -32000 {
		t.Errorf("error code = %d, want -32000", resp.Error.Code)
	}
}

func TestInvalidJSON(t *testing.T) {
	m := New()
	out, err := m.HandleRequest([]byte("not json"))
	if err != nil {
		t.Fatalf("HandleRequest() error = %v", err)
	}
	var resp struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal error: %v\nbody: %s", err, out)
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700 (parse error)", resp.Error.Code)
	}
}

func TestNotification(t *testing.T) {
	m := New()
	called := false
	m.RegisterMethod("ping", func(params interface{}) (interface{}, error) {
		called = true
		return "pong", nil
	})
	// No id -> notification; response should be empty.
	req := `{"jsonrpc":"2.0","method":"ping"}`
	out, err := m.HandleRequest([]byte(req))
	if err != nil {
		t.Fatalf("HandleRequest() error = %v", err)
	}
	if !called {
		t.Errorf("notification handler not called")
	}
	if len(out) != 0 {
		t.Errorf("notification response should be empty, got %s", out)
	}
}

func TestUnregisterAndListMethods(t *testing.T) {
	m := New()
	m.RegisterMethod("a", func(p interface{}) (interface{}, error) { return nil, nil })
	m.RegisterMethod("b", func(p interface{}) (interface{}, error) { return nil, nil })
	m.RegisterMethod("c", func(p interface{}) (interface{}, error) { return nil, nil })

	methods := m.ListMethods()
	if len(methods) != 3 {
		t.Fatalf("ListMethods() = %v, want 3", methods)
	}
	// Sorted.
	if methods[0] != "a" || methods[2] != "c" {
		t.Errorf("ListMethods() not sorted: %v", methods)
	}

	m.UnregisterMethod("b")
	methods = m.ListMethods()
	if len(methods) != 2 {
		t.Errorf("ListMethods() after unregister = %v, want 2", methods)
	}
	// Unregistering unknown is a no-op.
	m.UnregisterMethod("nope")
}

func TestStartStopLifecycle(t *testing.T) {
	m := New()
	if err := m.Init(&XHTTPRPCConfig{ListenAddr: "127.0.0.1:0", Path: "/RPC"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	m.RegisterMethod("echo", func(p interface{}) (interface{}, error) {
		return p, nil
	})
	if err := m.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	addr := m.listener.Addr().String()

	body := strings.NewReader(`{"jsonrpc":"2.0","method":"echo","params":"hi","id":7}`)
	resp, err := http.Post("http://"+addr+"/RPC", "application/json", body)
	if err != nil {
		t.Fatalf("http.Post error: %v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	var r struct {
		Result string `json:"result"`
		ID     int    `json:"id"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("unmarshal error: %v\nbody: %s", err, out)
	}
	if r.Result != "hi" {
		t.Errorf("result = %q, want hi", r.Result)
	}
	if r.ID != 7 {
		t.Errorf("id = %d, want 7", r.ID)
	}

	m.Stop()
	// After Stop, new requests must fail (listener closed).
	if _, err := http.Post("http://"+addr+"/RPC", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","method":"echo","id":8}`)); err == nil {
		t.Errorf("expected request to fail after Stop")
	}
	m.Stop()
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultXHTTPRPC() == nil {
		t.Fatalf("DefaultXHTTPRPC() nil")
	}
	Init()
	d := DefaultXHTTPRPC()
	if d == nil {
		t.Fatalf("DefaultXHTTPRPC() nil after Init")
	}
	if d != DefaultXHTTPRPC() {
		t.Fatalf("DefaultXHTTPRPC() returned different instances")
	}
}

func TestConcurrentMethods(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "m" + itoa(i)
			m.RegisterMethod(name, func(p interface{}) (interface{}, error) { return i, nil })
			req := `{"jsonrpc":"2.0","method":"` + name + `","id":` + itoa(i) + `}`
			out, err := m.HandleRequest([]byte(req))
			if err != nil {
				t.Errorf("HandleRequest(%s) error: %v", name, err)
				return
			}
			var resp struct {
				Result float64 `json:"result"`
			}
			_ = json.Unmarshal(out, &resp)
			if resp.Result != float64(i) {
				t.Errorf("result = %v, want %d", resp.Result, i)
			}
			m.UnregisterMethod(name)
		}(i)
	}
	wg.Wait()
}

var errSentinel = sentinelErr{}

type sentinelErr struct{}

func (sentinelErr) Error() string { return "boom" }

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
