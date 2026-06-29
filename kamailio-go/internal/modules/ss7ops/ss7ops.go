// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * ss7ops module - SS7 (SCCP/ISUP) parsing and building helpers.
 * Port of the kamailio ss7ops module (src/modules/ss7ops).
 *
 * The original C module parses and builds SCCP addresses and ISUP
 * messages and extracts the CIC (Circuit Identification Code). This Go
 * counterpart exposes the same operations using compact binary formats:
 *
 * SCCP:  [0] address-type indicator, [1] digit count, [2..] digits
 * ISUP:  [0:2] CIC (big-endian uint16), [2] message type,
 *        [3] called length, called..., [next] calling length, calling...
 *
 * It is safe for concurrent use.
 */

package ss7ops

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
)

// SS7Module parses and builds SCCP and ISUP messages.
// It is the Go counterpart of the kamailio ss7ops module.
type SS7Module struct {
	mu sync.RWMutex
}

// New creates an SS7Module.
func New() *SS7Module {
	return &SS7Module{}
}

// ParseSCCP parses an SCCP address byte slice into a dialled-number
// string.
//
//	C: ss7ops_parse_sccp()
func (m *SS7Module) ParseSCCP(sccp []byte) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(sccp) < 2 {
		return "", fmt.Errorf("ss7ops: sccp too short")
	}
	digitCount := int(sccp[1])
	if 2+digitCount > len(sccp) {
		return "", fmt.Errorf("ss7ops: truncated sccp address")
	}
	return string(sccp[2 : 2+digitCount]), nil
}

// BuildSCCP builds an SCCP address byte slice from a number string.
//
//	C: ss7ops_build_sccp()
func (m *SS7Module) BuildSCCP(address string) []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	digits := digitsOnly(address)
	out := make([]byte, 0, 2+len(digits))
	out = append(out, 0x02) // address-type: national significant number
	out = append(out, byte(len(digits)))
	out = append(out, digits...)
	return out
}

// ParseISUP parses an ISUP byte slice into a parameter map.
//
//	C: ss7ops_parse_isup()
func (m *SS7Module) ParseISUP(data []byte) (map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(data) < 3 {
		return nil, fmt.Errorf("ss7ops: isup too short")
	}
	out := map[string]interface{}{
		"cic":  int(binary.BigEndian.Uint16(data[0:2])),
		"type": int(data[2]),
	}
	pos := 3
	if pos >= len(data) {
		return out, nil
	}
	calledLen := int(data[pos])
	pos++
	if pos+calledLen <= len(data) {
		out["called"] = string(data[pos : pos+calledLen])
		pos += calledLen
	}
	if pos >= len(data) {
		return out, nil
	}
	callingLen := int(data[pos])
	pos++
	if pos+callingLen <= len(data) {
		out["calling"] = string(data[pos : pos+callingLen])
	}
	return out, nil
}

// BuildISUP builds an ISUP byte slice from a parameter map. The map
// must contain "cic" (int); "type", "called" and "calling" are optional.
//
//	C: ss7ops_build_isup()
func (m *SS7Module) BuildISUP(params map[string]interface{}) []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cic := toInt(params["cic"])
	msgType := toInt(params["type"])
	if msgType == 0 {
		msgType = 0x01 // default IAM
	}
	called := toString(params["called"])
	calling := toString(params["calling"])
	out := make([]byte, 0, 3+1+len(called)+1+len(calling))
	var cicBytes [2]byte
	binary.BigEndian.PutUint16(cicBytes[:], uint16(cic))
	out = append(out, cicBytes[:]...)
	out = append(out, byte(msgType))
	out = append(out, byte(len(called)))
	out = append(out, called...)
	out = append(out, byte(len(calling)))
	out = append(out, calling...)
	return out
}

// GetCIC extracts the CIC from an ISUP byte slice.
//
//	C: ss7ops_get_cic()
func (m *SS7Module) GetCIC(data []byte) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(data) < 2 {
		return 0, fmt.Errorf("ss7ops: isup too short for CIC")
	}
	return int(binary.BigEndian.Uint16(data[0:2])), nil
}

// --- helpers ---

// digitsOnly strips everything except digits from s.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// toInt converts an interface{} to int.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i := 0
		for _, r := range n {
			if r < '0' || r > '9' {
				return 0
			}
			i = i*10 + int(r-'0')
		}
		return i
	}
	return 0
}

// toString converts an interface{} to string.
func toString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// --- package-level API ---

var defaultModule = New()

// DefaultSS7 returns the package-level default SS7Module.
func DefaultSS7() *SS7Module {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	defaultModule = New()
}
