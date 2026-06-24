// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Built-in fixup functions for command and parameter arguments -
 * matching Kamailio core mod_fix.c.
 *
 * Each fixup function converts a raw value (typically a string from the
 * configuration file) into its runtime representation. These are the Go
 * counterparts of C's fixup_str(), fixup_int(), fixup_bool(), fixup_regex()
 * and fixup_pvar() functions. They are registered with the default
 * FixupRegistry at package initialisation time.
 */

package srmodule

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// registerDefaults registers the built-in fixup functions on the given
// FixupRegistry. It is called both from DefaultFixupRegistry() (on first
// use) and from resetFixupRegistry() (after Init()).
func registerDefaults(fr *FixupRegistry) {
	fr.RegisterFixup("str", FixupStr)
	fr.RegisterFixup("int", FixupInt)
	fr.RegisterFixup("bool", FixupBool)
	fr.RegisterFixup("regex", FixupRegex)
	fr.RegisterFixup("pvar", FixupPVar)
}

// FixupStr converts a raw value to a string. It accepts strings, byte
// slices, integers and any value implementing fmt.Stringer. It is the
// Go counterpart of C's fixup_str().
func FixupStr(param interface{}) (interface{}, error) {
	if param == nil {
		return nil, fmt.Errorf("nil parameter")
	}
	switch v := param.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case bool:
		return strconv.FormatBool(v), nil
	case fmt.Stringer:
		return v.String(), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to string", param)
	}
}

// FixupInt converts a raw value to an int. It accepts integers of any
// width and strings (including hex with 0x prefix, octal with 0 prefix,
// and binary with 0b prefix). It is the Go counterpart of C's
// fixup_int() / fixup_ig().
func FixupInt(param interface{}) (interface{}, error) {
	if param == nil {
		return nil, fmt.Errorf("nil parameter")
	}
	switch v := param.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		return int(v), nil
	case string:
		n, err := strconv.ParseInt(v, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as int: %w", v, err)
		}
		return int(n), nil
	case []byte:
		n, err := strconv.ParseInt(string(v), 0, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as int: %w", string(v), err)
		}
		return int(n), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to int", param)
	}
}

// FixupBool converts a raw value to a bool. It accepts booleans, integers
// (non-zero is true) and strings (true/false, yes/no, on/off, 1/0,
// case-insensitive). It is the Go counterpart of C's fixup_bool().
func FixupBool(param interface{}) (interface{}, error) {
	if param == nil {
		return nil, fmt.Errorf("nil parameter")
	}
	switch v := param.(type) {
	case bool:
		return v, nil
	case int:
		return v != 0, nil
	case int64:
		return v != 0, nil
	case uint:
		return v != 0, nil
	case uint64:
		return v != 0, nil
	case string:
		b, err := parseBool(v)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as bool: %w", v, err)
		}
		return b, nil
	case []byte:
		b, err := parseBool(string(v))
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as bool: %w", string(v), err)
		}
		return b, nil
	default:
		return nil, fmt.Errorf("cannot convert %T to bool", param)
	}
}

// FixupRegex compiles a raw value into a *regexp.Regexp. The input must
// be a string or byte slice containing a valid Go regular expression.
// It is the Go counterpart of C's fixup_regex() which compiles a
// PCRE pattern.
func FixupRegex(param interface{}) (interface{}, error) {
	if param == nil {
		return nil, fmt.Errorf("nil parameter")
	}
	var pattern string
	switch v := param.(type) {
	case string:
		pattern = v
	case []byte:
		pattern = string(v)
	default:
		return nil, fmt.Errorf("cannot convert %T to regex", param)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex %q: %w", pattern, err)
	}
	return re, nil
}

// FixupPVar validates and normalises a pseudo-variable reference. The
// input must be a string starting with "$" (e.g. "$avp(name)",
// "$var(x)"). The returned value is the variable name with surrounding
// whitespace trimmed. It is the Go counterpart of C's fixup_pvar()
// which calls pv_parse_spec() to parse the PV reference.
func FixupPVar(param interface{}) (interface{}, error) {
	if param == nil {
		return nil, fmt.Errorf("nil parameter")
	}
	var s string
	switch v := param.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return nil, fmt.Errorf("cannot convert %T to pseudo-variable", param)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty pseudo-variable name")
	}
	if !strings.HasPrefix(s, "$") {
		return nil, fmt.Errorf("pseudo-variable %q must start with '$'", s)
	}
	// In a full implementation this would call pv_parse_spec() to
	// parse the name into a structured PV spec. For now we return
	// the normalised name string.
	return s, nil
}

// FixupFree releases any resources held by a fixed-up value. In Go the
// garbage collector handles memory deallocation, so this function is
// effectively a no-op. It exists for API compatibility with C's
// fixup_free_*() functions and may be extended in the future if a fixup
// allocates resources that require explicit cleanup.
func FixupFree(param interface{}) {
	// No-op: Go's garbage collector handles all memory reclamation.
	// This function is provided for API compatibility with C's
	// fixup_free_*() functions.
	_ = param
}
