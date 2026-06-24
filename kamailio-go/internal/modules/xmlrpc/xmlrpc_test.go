// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the xmlrpc module - XML-RPC encode/decode/dispatch.
 */
package xmlrpc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestEncode(t *testing.T) {
	m := New()
	req := &XMLRPCRequest{
		Method: "example.add",
		Params: []interface{}{1, 2, "three"},
	}
	out := m.Encode(req)
	if !strings.Contains(out, "<methodName>example.add</methodName>") {
		t.Errorf("Encode missing methodName: %s", out)
	}
	if !strings.Contains(out, "<int>1</int>") {
		t.Errorf("Encode missing int param: %s", out)
	}
	if !strings.Contains(out, "<string>three</string>") {
		t.Errorf("Encode missing string param: %s", out)
	}
}

func TestDecode(t *testing.T) {
	m := New()
	resp := `<?xml version="1.0"?>
<methodResponse>
  <params>
    <param><value><int>42</int></value></param>
  </params>
</methodResponse>`
	r, err := m.Decode(resp)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if r.Fault != nil {
		t.Fatalf("unexpected fault: %+v", r.Fault)
	}
	if r.Result != 42 {
		t.Errorf("Result = %v, want 42", r.Result)
	}
}

func TestDecodeFault(t *testing.T) {
	m := New()
	resp := `<?xml version="1.0"?>
<methodResponse>
  <fault>
    <value><struct>
      <member><name>faultCode</name><value><int>-32601</int></value></member>
      <member><name>faultString</name><value><string>no method</string></value></member>
    </struct></value>
  </fault>
</methodResponse>`
	r, err := m.Decode(resp)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if r.Fault == nil {
		t.Fatal("expected fault, got nil")
	}
	if r.Fault.Code != -32601 {
		t.Errorf("faultCode = %d", r.Fault.Code)
	}
	if r.Fault.Message != "no method" {
		t.Errorf("faultString = %q", r.Fault.Message)
	}
}

func TestDecodeStructAndArray(t *testing.T) {
	m := New()
	resp := `<?xml version="1.0"?>
<methodResponse>
  <params>
    <param><value><struct>
      <member><name>name</name><value><string>alice</string></value></member>
      <member><name>count</name><value><int>5</int></value></member>
    </struct></value></param>
  </params>
</methodResponse>`
	r, err := m.Decode(resp)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	mp, ok := r.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Result = %T", r.Result)
	}
	if mp["name"] != "alice" {
		t.Errorf("name = %v", mp["name"])
	}
	if mp["count"] != 5 {
		t.Errorf("count = %v", mp["count"])
	}
}

func TestRegisterAndDispatch(t *testing.T) {
	m := New()
	m.RegisterMethod("add", func(params []interface{}) (interface{}, error) {
		sum := 0
		for _, p := range params {
			sum += toInt(p)
		}
		return sum, nil
	})
	resp, err := m.Dispatch(&XMLRPCRequest{Method: "add", Params: []interface{}{1, 2, 3}})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if resp.Fault != nil {
		t.Fatalf("unexpected fault: %+v", resp.Fault)
	}
	if resp.Result != 6 {
		t.Errorf("Result = %v, want 6", resp.Result)
	}
	// Unknown method -> fault.
	resp2, _ := m.Dispatch(&XMLRPCRequest{Method: "missing"})
	if resp2.Fault == nil {
		t.Error("missing method should yield fault")
	}
}

func TestCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<methodResponse><params><param><value><string>ok</string></value></param></params></methodResponse>`))
	}))
	defer srv.Close()
	m := New()
	resp, err := m.Call(srv.URL, "ping", []interface{}{"hello"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Result != "ok" {
		t.Errorf("Result = %v, want ok", resp.Result)
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultXMLRPC()
	d2 := DefaultXMLRPC()
	if d1 != d2 {
		t.Error("DefaultXMLRPC should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	m.RegisterMethod("add", func(params []interface{}) (interface{}, error) {
		return toInt(params[0]) + toInt(params[1]), nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Encode(&XMLRPCRequest{Method: "add", Params: []interface{}{1, 2}})
			_, _ = m.Dispatch(&XMLRPCRequest{Method: "add", Params: []interface{}{1, 2}})
		}()
	}
	wg.Wait()
}
