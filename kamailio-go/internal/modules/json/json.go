// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * JSON module - JSON value access and manipulation.
 * Port of the kamailio json module (src/modules/json).
 *
 * The original C module uses libjson-c to fetch a single field from a
 * JSON document by name and assign it to a pseudo-variable. This Go
 * counterpart exposes the same idea through a small JSONPath-style API
 * built on top of encoding/json: dot-notation paths with numeric
 * indices for arrays (e.g. "user.name", "items.0.id").
 *
 * It is safe for concurrent use.
 */

package json

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// JSONModule provides JSON parsing, querying and mutation helpers.
// It is the Go counterpart of the kamailio json module.
type JSONModule struct {
	mu sync.Mutex
}

// New creates a JSONModule.
func New() *JSONModule {
	return &JSONModule{}
}

// Parse decodes a JSON string into a generic Go value
// (map[string]interface{}, []interface{}, float64, string, bool or nil).
//
//	C: json_tokener_parse()
func (m *JSONModule) Parse(jsonStr string) (interface{}, error) {
	if jsonStr == "" {
		return nil, errors.New("json: empty input")
	}
	var v interface{}
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return nil, fmt.Errorf("json: parse error: %w", err)
	}
	return v, nil
}

// Stringify serialises a Go value to a JSON string.
//
//	C: json_object_to_json_string()
func (m *JSONModule) Stringify(val interface{}) (string, error) {
	b, err := json.Marshal(val)
	if err != nil {
		return "", fmt.Errorf("json: stringify error: %w", err)
	}
	return string(b), nil
}

// Get retrieves the value at the given JSON path and returns its string
// representation. Objects are addressed by key, arrays by numeric index.
// Returns an error if the path does not exist or the document is invalid.
//
//	C: _json_get_field() / json_get_field()
func (m *JSONModule) Get(jsonStr string, path string) (string, error) {
	v, err := m.Parse(jsonStr)
	if err != nil {
		return "", err
	}
	cur, err := lookupPath(v, path)
	if err != nil {
		return "", err
	}
	return valueToString(cur), nil
}

// Set sets the value at the given JSON path. The value string is parsed
// as JSON when possible (so "123" becomes a number and "true" a bool);
// otherwise it is treated as a JSON string. The modified document is
// returned as a JSON string.
//
//	C: json_object_object_add() (extended to support nested paths)
func (m *JSONModule) Set(jsonStr string, path string, value string) (string, error) {
	v, err := m.Parse(jsonStr)
	if err != nil {
		return "", err
	}
	parsedVal, perr := parseValueLoose(value)
	if perr != nil {
		return "", perr
	}
	if err := setPath(v, path, parsedVal); err != nil {
		return "", err
	}
	return m.Stringify(v)
}

// ArrayLength returns the number of elements in the array at the given
// JSON path. Returns an error if the path does not exist or does not
// reference an array.
func (m *JSONModule) ArrayLength(jsonStr string, path string) (int, error) {
	v, err := m.Parse(jsonStr)
	if err != nil {
		return 0, err
	}
	cur, err := lookupPath(v, path)
	if err != nil {
		return 0, err
	}
	arr, ok := cur.([]interface{})
	if !ok {
		return 0, fmt.Errorf("json: value at %q is not an array", path)
	}
	return len(arr), nil
}

// Exists reports whether the given JSON path resolves to a value.
func (m *JSONModule) Exists(jsonStr string, path string) bool {
	v, err := m.Parse(jsonStr)
	if err != nil {
		return false
	}
	_, err = lookupPath(v, path)
	return err == nil
}

// --- path helpers ---

// splitPath splits a dot-notation path into segments. Numeric segments
// address array indices.
func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

// lookupPath resolves a dot-notation path against a decoded JSON value.
func lookupPath(root interface{}, path string) (interface{}, error) {
	cur := root
	for _, seg := range splitPath(path) {
		if seg == "" {
			return nil, fmt.Errorf("json: empty path segment in %q", path)
		}
		switch v := cur.(type) {
		case map[string]interface{}:
			next, ok := v[seg]
			if !ok {
				return nil, fmt.Errorf("json: key %q not found", seg)
			}
			cur = next
		case []interface{}:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("json: invalid array index %q", seg)
			}
			cur = v[idx]
		default:
			return nil, fmt.Errorf("json: cannot index %T with %q", cur, seg)
		}
	}
	return cur, nil
}

// setPath mutates the decoded JSON value, setting the leaf of path to val.
// It auto-creates intermediate objects/arrays as needed.
func setPath(root interface{}, path string, val interface{}) error {
	segs := splitPath(path)
	if len(segs) == 0 {
		return errors.New("json: empty path")
	}
	cur := root
	for i, seg := range segs {
		last := i == len(segs)-1
		if last {
			switch v := cur.(type) {
			case map[string]interface{}:
				v[seg] = val
				return nil
			case []interface{}:
				idx, err := strconv.Atoi(seg)
				if err != nil || idx < 0 || idx >= len(v) {
					return fmt.Errorf("json: invalid array index %q", seg)
				}
				v[idx] = val
				return nil
			default:
				return fmt.Errorf("json: cannot set on %T", cur)
			}
		}
		// descend, creating containers when missing
		switch v := cur.(type) {
		case map[string]interface{}:
			next, ok := v[seg]
			if !ok {
				next = make(map[string]interface{})
				v[seg] = next
			}
			cur = next
		case []interface{}:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(v) {
				return fmt.Errorf("json: invalid array index %q", seg)
			}
			cur = v[idx]
		default:
			return fmt.Errorf("json: cannot descend into %T", cur)
		}
	}
	return nil
}

// valueToString renders a decoded JSON value as a string. Strings are
// returned without quotes; other types use their JSON encoding.
func valueToString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return ""
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

// parseValueLoose parses a value string as JSON; on failure it is
// treated as a plain JSON string.
func parseValueLoose(s string) (interface{}, error) {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v, nil
	}
	// fall back to a JSON string literal
	return s, nil
}

// --- package-level API ---

var defaultModule = New()

// DefaultJSON returns the package-level default JSONModule.
func DefaultJSON() *JSONModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	defaultModule = New()
}
