// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * xmlrpc module - XML-RPC client and lightweight server helpers.
 * Port of the kamailio xmlrpc module (src/modules/xmlrpc).
 *
 * The original C module exposes Kamailio functions over the XML-RPC
 * protocol (it acts as a server) and can also issue XML-RPC calls. This
 * Go counterpart provides request/response encoding and decoding plus a
 * method registry, and a Call helper that issues XML-RPC requests over
 * HTTP.
 *
 * It is safe for concurrent use.
 */

package xmlrpc

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// XMLRPCRequest is an XML-RPC request.
type XMLRPCRequest struct {
	Method string
	Params []interface{}
}

// XMLRPCFault is an XML-RPC fault response.
type XMLRPCFault struct {
	Code    int
	Message string
}

// XMLRPCResponse is an XML-RPC response.
type XMLRPCResponse struct {
	Result interface{}
	Fault  *XMLRPCFault
}

// XMLRPCModule encodes, decodes and dispatches XML-RPC messages.
// It is the Go counterpart of the kamailio xmlrpc module.
type XMLRPCModule struct {
	mu       sync.RWMutex
	methods  map[string]func(params []interface{}) (interface{}, error)
	client   *http.Client
}

// New creates an XMLRPCModule.
func New() *XMLRPCModule {
	return &XMLRPCModule{methods: make(map[string]func(params []interface{}) (interface{}, error)), client: &http.Client{}}
}

// Call issues an XML-RPC request to url and returns the decoded response.
//
//	C: xmlrpc dispatch
func (m *XMLRPCModule) Call(url, method string, params []interface{}) (*XMLRPCResponse, error) {
	req := &XMLRPCRequest{Method: method, Params: params}
	body := m.Encode(req)
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Post(url, "text/xml", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("xmlrpc: %w", err)
	}
	defer resp.Body.Close()
	data, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, fmt.Errorf("xmlrpc: read body: %w", rerr)
	}
	return m.Decode(string(data))
}

// Encode serialises an XMLRPCRequest to an XML-RPC <methodCall> string.
//
//	C: build_methodCall()
func (m *XMLRPCModule) Encode(req *XMLRPCRequest) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>` + "\n")
	b.WriteString("<methodCall>\n")
	fmt.Fprintf(&b, "  <methodName>%s</methodName>\n", escapeXML(req.Method))
	if len(req.Params) == 0 {
		b.WriteString("</methodCall>")
		return b.String()
	}
	b.WriteString("  <params>\n")
	for _, p := range req.Params {
		b.WriteString("    <param><value>")
		b.WriteString(encodeValue(p))
		b.WriteString("</value></param>\n")
	}
	b.WriteString("  </params>\n")
	b.WriteString("</methodCall>")
	return b.String()
}

// Decode parses an XML-RPC <methodResponse> into an XMLRPCResponse.
//
//	C: parse_methodResponse()
func (m *XMLRPCModule) Decode(xmlStr string) (*XMLRPCResponse, error) {
	dec := xml.NewDecoder(strings.NewReader(xmlStr))
	resp := &XMLRPCResponse{}
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("xmlrpc: decode: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "fault" {
			val, _ := decodeValue(dec, se.Name.Local)
			if mp, ok := val.(map[string]interface{}); ok {
				resp.Fault = &XMLRPCFault{}
				if c, ok := mp["faultCode"]; ok {
					resp.Fault.Code = toInt(c)
				}
				if msg, ok := mp["faultString"]; ok {
					resp.Fault.Message = fmt.Sprintf("%v", msg)
				}
			}
			break
		}
		if se.Name.Local == "param" {
			val, _ := decodeValue(dec, se.Name.Local)
			resp.Result = val
		}
	}
	return resp, nil
}

// RegisterMethod registers a handler for a named method, enabling local
// dispatch of XML-RPC calls.
//
//	C: register_method()
func (m *XMLRPCModule) RegisterMethod(name string, handler func(params []interface{}) (interface{}, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.methods[name] = handler
}

// Dispatch invokes a registered method locally. Returns an error if the
// method is not registered.
func (m *XMLRPCModule) Dispatch(req *XMLRPCRequest) (*XMLRPCResponse, error) {
	m.mu.RLock()
	handler, ok := m.methods[req.Method]
	m.mu.RUnlock()
	if !ok {
		return &XMLRPCResponse{Fault: &XMLRPCFault{Code: -32601, Message: "method not found"}}, nil
	}
	result, err := handler(req.Params)
	if err != nil {
		return &XMLRPCResponse{Fault: &XMLRPCFault{Code: -32500, Message: err.Error()}}, nil
	}
	return &XMLRPCResponse{Result: result}, nil
}

// --- encoding helpers ---

// encodeValue renders a Go value as an XML-RPC <value> body.
func encodeValue(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "<string></string>"
	case string:
		return "<string>" + escapeXML(val) + "</string>"
	case int:
		return fmt.Sprintf("<int>%d</int>", val)
	case int64:
		return fmt.Sprintf("<int>%d</int>", val)
	case float64:
		return fmt.Sprintf("<double>%v</double>", val)
	case bool:
		if val {
			return "<boolean>1</boolean>"
		}
		return "<boolean>0</boolean>"
	case map[string]interface{}:
		var b strings.Builder
		b.WriteString("<struct>")
		for k, vv := range val {
			b.WriteString("<member>")
			fmt.Fprintf(&b, "<name>%s</name>", escapeXML(k))
			b.WriteString("<value>")
			b.WriteString(encodeValue(vv))
			b.WriteString("</value>")
			b.WriteString("</member>")
		}
		b.WriteString("</struct>")
		return b.String()
	case []interface{}:
		var b strings.Builder
		b.WriteString("<array><data>")
		for _, vv := range val {
			b.WriteString("<value>")
			b.WriteString(encodeValue(vv))
			b.WriteString("</value>")
		}
		b.WriteString("</data></array>")
		return b.String()
	default:
		return "<string>" + escapeXML(fmt.Sprintf("%v", val)) + "</string>"
	}
}

// decodeValue reads the inner content of a <value> or container element.
func decodeValue(dec *xml.Decoder, parent string) (interface{}, error) {
	var result interface{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return result, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "string":
				txt, _ := readText(dec)
				result = txt
			case "int", "i4":
				txt, _ := readText(dec)
				result = toInt(txt)
			case "double":
				txt, _ := readText(dec)
				f, _ := strconv.ParseFloat(strings.TrimSpace(txt), 64)
				result = f
			case "boolean":
				txt, _ := readText(dec)
				result = strings.TrimSpace(txt) == "1"
			case "struct":
				result = decodeStruct(dec)
			case "array":
				result = decodeArray(dec)
			case "value":
				v, _ := decodeValue(dec, "value")
				result = v
			default:
				// Unknown type: read text.
				txt, _ := readText(dec)
				result = txt
			}
		case xml.EndElement:
			if t.Name.Local == parent {
				return result, nil
			}
		}
	}
}

// decodeStruct reads <struct> members into a map.
func decodeStruct(dec *xml.Decoder) map[string]interface{} {
	out := make(map[string]interface{})
	var name string
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "name" {
				txt, _ := readText(dec)
				name = txt
			} else if t.Name.Local == "value" {
				v, _ := decodeValue(dec, "value")
				out[name] = v
			}
		case xml.EndElement:
			if t.Name.Local == "struct" {
				return out
			}
		}
	}
}

// decodeArray reads <array><data> values into a slice.
func decodeArray(dec *xml.Decoder) []interface{} {
	var out []interface{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "value" {
				v, _ := decodeValue(dec, "value")
				out = append(out, v)
			}
		case xml.EndElement:
			if t.Name.Local == "array" {
				return out
			}
		}
	}
}

// readText reads the character data until the closing tag.
func readText(dec *xml.Decoder) (string, error) {
	var b bytes.Buffer
	for {
		tok, err := dec.Token()
		if err != nil {
			return b.String(), err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			return b.String(), nil
		}
	}
}

// toInt converts a value to int, tolerating string/float64 inputs.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	}
	return 0
}

// escapeXML escapes the standard XML special characters.
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// --- package-level API ---

var defaultModule = New()

// DefaultXMLRPC returns the package-level default XMLRPCModule.
func DefaultXMLRPC() *XMLRPCModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	defaultModule = New()
}
