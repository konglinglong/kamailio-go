// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - jsonrpcs module tests.
 *
 * These tests exercise the JSON-RPC 2.0 dispatch (single + batch,
 * notifications, error mapping) without any network transport.
 */

package jsonrpcs

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func echoHandler(params interface{}) (interface{}, error) {
	return map[string]interface{}{"echo": params}, nil
}

func TestRegisterAndListMethods(t *testing.T) {
	m := New()
	m.RegisterMethod("echo", echoHandler)
	m.RegisterMethod("ping", func(p interface{}) (interface{}, error) {
		return "pong", nil
	})
	methods := m.ListMethods()
	if len(methods) != 2 {
		t.Fatalf("ListMethods = %v, want 2 entries", methods)
	}
	if methods[0] != "echo" || methods[1] != "ping" {
		t.Errorf("ListMethods = %v, want [echo ping]", methods)
	}
}

func TestUnregisterMethod(t *testing.T) {
	m := New()
	m.RegisterMethod("echo", echoHandler)
	m.UnregisterMethod("echo")
	if len(m.ListMethods()) != 0 {
		t.Errorf("UnregisterMethod left methods: %v", m.ListMethods())
	}
}

func TestHandleRequestSingle(t *testing.T) {
	m := New()
	m.RegisterMethod("echo", echoHandler)
	req := []byte(`{"jsonrpc":"2.0","method":"echo","params":{"x":1},"id":42}`)
	out, err := m.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", resp.JSONRPC)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	echo, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result = %T, want map", resp.Result)
	}
	if echo["echo"] == nil {
		t.Errorf("echo result missing params")
	}
}

func TestHandleRequestNotification(t *testing.T) {
	m := New()
	called := false
	m.RegisterMethod("notify", func(p interface{}) (interface{}, error) {
		called = true
		return nil, nil
	})
	// No "id" -> notification -> no response.
	req := []byte(`{"jsonrpc":"2.0","method":"notify","params":null}`)
	out, err := m.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if out != nil {
		t.Errorf("notification produced response: %s", out)
	}
	if !called {
		t.Error("notification handler not called")
	}
}

func TestHandleRequestBatch(t *testing.T) {
	m := New()
	m.RegisterMethod("echo", echoHandler)
	m.RegisterMethod("ping", func(p interface{}) (interface{}, error) { return "pong", nil })
	req := []byte(`[
		{"jsonrpc":"2.0","method":"echo","params":"a","id":1},
		{"jsonrpc":"2.0","method":"ping","id":2},
		{"jsonrpc":"2.0","method":"notify","params":null}
	]`)
	out, err := m.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	var resps []rpcResponse
	if err := json.Unmarshal(out, &resps); err != nil {
		t.Fatalf("Unmarshal batch: %v (raw=%s)", err, out)
	}
	if len(resps) != 2 {
		t.Fatalf("batch responses = %d, want 2 (notification omitted)", len(resps))
	}
	if resps[0].ID != float64(1) {
		t.Errorf("resp[0].id = %v, want 1", resps[0].ID)
	}
	if resps[1].ID != float64(2) {
		t.Errorf("resp[1].id = %v, want 2", resps[1].ID)
	}
}

func TestHandleRequestBatchAllNotifications(t *testing.T) {
	m := New()
	m.RegisterMethod("notify", func(p interface{}) (interface{}, error) { return nil, nil })
	req := []byte(`[
		{"jsonrpc":"2.0","method":"notify","params":null},
		{"jsonrpc":"2.0","method":"notify","params":null}
	]`)
	out, err := m.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if out != nil {
		t.Errorf("all-notification batch produced response: %s", out)
	}
}

func TestHandleRequestMethodNotFound(t *testing.T) {
	m := New()
	req := []byte(`{"jsonrpc":"2.0","method":"nope","id":"a"}`)
	out, err := m.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrMethod {
		t.Errorf("error = %v, want code %d", resp.Error, ErrMethod)
	}
}

func TestHandleRequestHandlerError(t *testing.T) {
	m := New()
	m.RegisterMethod("boom", func(p interface{}) (interface{}, error) {
		return nil, &RPCError{Code: -32099, Message: "kaboom"}
	})
	req := []byte(`{"jsonrpc":"2.0","method":"boom","id":7}`)
	out, err := m.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != -32099 || resp.Error.Message != "kaboom" {
		t.Errorf("error = %v, want code -32099 kaboom", resp.Error)
	}
}

func TestHandleRequestGenericError(t *testing.T) {
	m := New()
	m.RegisterMethod("fail", func(p interface{}) (interface{}, error) {
		return nil, errors.New("plain failure")
	})
	req := []byte(`{"jsonrpc":"2.0","method":"fail","id":1}`)
	out, err := m.HandleRequest(req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrInternal {
		t.Errorf("error = %v, want code %d", resp.Error, ErrInternal)
	}
}

func TestHandleRequestParseError(t *testing.T) {
	m := New()
	out, err := m.HandleRequest([]byte(`{not json`))
	if err != nil {
		t.Fatalf("HandleRequest returned err: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrParse {
		t.Errorf("error = %v, want code %d", resp.Error, ErrParse)
	}
}

func TestHandleRequestEmpty(t *testing.T) {
	m := New()
	if _, err := m.HandleRequest(nil); err == nil {
		t.Error("HandleRequest(nil) expected error")
	}
	if _, err := m.HandleRequest([]byte("   ")); err == nil {
		t.Error("HandleRequest(whitespace) expected error")
	}
}

func TestConfigValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Errorf("default config: %v", err)
	}
	if err := (&Config{Transport: 99}).Validate(); err == nil {
		t.Error("invalid transport expected error")
	}
}

func TestInitWithConfig(t *testing.T) {
	m := New()
	cfg := Config{Transport: TransDgram, SockName: "/tmp/x.sock"}
	if err := m.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.Config().Transport != TransDgram {
		t.Errorf("transport = %d, want %d", m.Config().Transport, TransDgram)
	}
	m.RegisterMethod("a", echoHandler)
	if err := m.Init(cfg); err != nil {
		t.Fatalf("Init reset: %v", err)
	}
	if len(m.ListMethods()) != 0 {
		t.Errorf("Init did not reset handlers: %v", m.ListMethods())
	}
}

func TestStartStop(t *testing.T) {
	m := New()
	if m.IsStarted() {
		t.Error("IsStarted true before Start")
	}
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !m.IsStarted() {
		t.Error("IsStarted false after Start")
	}
	if err := m.Start(); err == nil {
		t.Error("double Start expected error")
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if m.IsStarted() {
		t.Error("IsStarted true after Stop")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init(*DefaultConfig())
	d := DefaultJSONRPCS()
	if d == nil {
		t.Fatal("DefaultJSONRPCS nil")
	}
	RegisterMethod("pkg.echo", echoHandler)
	if len(ListMethods()) != 1 {
		t.Errorf("ListMethods = %v, want 1", ListMethods())
	}
	out, err := HandleRequest([]byte(`{"jsonrpc":"2.0","method":"pkg.echo","params":null,"id":1}`))
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if out == nil {
		t.Fatal("HandleRequest returned nil response")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.RegisterMethod("echo", echoHandler)
	req := []byte(`{"jsonrpc":"2.0","method":"echo","params":{"x":1},"id":1}`)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := m.HandleRequest(req)
			if err != nil {
				t.Errorf("HandleRequest: %v", err)
				return
			}
			if out == nil {
				t.Errorf("nil response")
			}
		}()
	}
	wg.Wait()
	if got := m.CallCount(); got != 50 {
		t.Errorf("CallCount = %d, want 50", got)
	}
}
