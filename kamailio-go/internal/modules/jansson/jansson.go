// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Jansson module - JSON parsing and manipulation.
 * Port of the kamailio jansson module (src/modules/jansson).
 *
 * The module wraps encoding/json to provide parse/stringify operations
 * plus dot-path get/set helpers. It is safe for concurrent use.
 */

package jansson

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

// JanssonModule provides JSON helpers.
type JanssonModule struct {
	mu sync.RWMutex
}

// New creates a JanssonModule.
func New() *JanssonModule {
	return &JanssonModule{}
}

// Parse decodes a JSON document into a generic Go value.
//
//	C: jansson_parse()
func (m *JanssonModule) Parse(j string) (interface{}, error) {
	if strings.TrimSpace(j) == "" {
		return nil, errors.New("jansson: empty input")
	}
	var v interface{}
	if err := json.Unmarshal([]byte(j), &v); err != nil {
		return nil, err
	}
	return v, nil
}

// Stringify encodes a Go value into a JSON string.
//
//	C: jansson_stringify()
func (m *JanssonModule) Stringify(val interface{}) (string, error) {
	out, err := json.Marshal(val)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Get navigates the dot-separated path into the JSON document and
// returns the value found there. String values are returned without
// quotes; other types are JSON-encoded. It returns an error when the
// path does not exist.
//
//	C: jansson_get()
func (m *JanssonModule) Get(j, path string) (string, error) {
	v, err := m.Parse(j)
	if err != nil {
		return "", err
	}
	cur := v
	for _, key := range splitPath(path) {
		obj, ok := cur.(map[string]interface{})
		if !ok {
			return "", errors.New("jansson: path not found: " + path)
		}
		cur, ok = obj[key]
		if !ok {
			return "", errors.New("jansson: path not found: " + path)
		}
	}
	switch val := cur.(type) {
	case string:
		return val, nil
	default:
		out, err := json.Marshal(val)
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
}

// Set navigates the dot-separated path into the JSON document, assigns
// the value there (parsing it as JSON when possible, otherwise treating
// it as a plain string), and returns the modified document as a string.
//
//	C: jansson_set()
func (m *JanssonModule) Set(j, path, value string) (string, error) {
	v, err := m.Parse(j)
	if err != nil {
		return "", err
	}
	keys := splitPath(path)
	if len(keys) == 0 {
		return "", errors.New("jansson: empty path")
	}
	// Ensure the root is an object.
	root, ok := v.(map[string]interface{})
	if !ok {
		return "", errors.New("jansson: root is not an object")
	}
	cur := root
	for i, key := range keys {
		if i == len(keys)-1 {
			cur[key] = parseValue(value)
			break
		}
		next, ok := cur[key].(map[string]interface{})
		if !ok {
			next = make(map[string]interface{})
			cur[key] = next
		}
		cur = next
	}
	out, err := json.Marshal(root)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// splitPath splits a dot-separated path into keys, ignoring empty parts.
func splitPath(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseValue attempts to parse value as JSON, falling back to a plain
// string.
func parseValue(value string) interface{} {
	var v interface{}
	if err := json.Unmarshal([]byte(value), &v); err == nil {
		return v
	}
	return value
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *JanssonModule
)

// DefaultJansson returns the process-wide JanssonModule.
func DefaultJansson() *JanssonModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide JanssonModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Parse is the package-level wrapper around DefaultJansson().Parse.
func Parse(j string) (interface{}, error) { return DefaultJansson().Parse(j) }

// Stringify is the package-level wrapper around DefaultJansson().Stringify.
func Stringify(val interface{}) (string, error) { return DefaultJansson().Stringify(val) }

// Get is the package-level wrapper around DefaultJansson().Get.
func Get(j, path string) (string, error) { return DefaultJansson().Get(j, path) }

// Set is the package-level wrapper around DefaultJansson().Set.
func Set(j, path, value string) (string, error) { return DefaultJansson().Set(j, path, value) }
