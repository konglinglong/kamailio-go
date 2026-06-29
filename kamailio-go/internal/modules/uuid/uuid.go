// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * UUID module - RFC 4122 UUID generation and inspection.
 * Port of the kamailio uuid module (src/modules/uuid).
 *
 * The uuid module generates RFC 4122 version 4 (random) UUIDs and
 * inspects existing ones (validity and version).
 *
 * The methods are stateless and therefore safe for concurrent use.
 */

package uuid

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strconv"
)

// uuidRe matches the canonical 8-4-4-4-12 hex form of a UUID.
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// UUIDModule generates and inspects UUIDs.
// C: struct module uuid
type UUIDModule struct{}

// New creates a UUIDModule.
func New() *UUIDModule {
	return &UUIDModule{}
}

// Generate returns a fresh RFC 4122 version 4 (random) UUID.
//
//	C: uuid_generate()
func (m *UUIDModule) Generate() string {
	if m == nil {
		return ""
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read should not fail; fall back to a zeroed buffer rather
		// than panicking.
		return "00000000-0000-4000-8000-000000000000"
	}
	// RFC 4122: set version (4) and variant (10xx).
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Validate reports whether uuid is a well-formed canonical UUID.
//
//	C: uuid_validate()
func (m *UUIDModule) Validate(uuid string) bool {
	if m == nil || uuid == "" {
		return false
	}
	return uuidRe.MatchString(uuid)
}

// Version returns the RFC 4122 version digit of uuid (1..5), or 0 when the
// uuid is not well-formed.
//
//	C: uuid_version()
func (m *UUIDModule) Version(uuid string) int {
	if m == nil || !m.Validate(uuid) {
		return 0
	}
	// The version is the first hex digit of the third group (index 14).
	n, err := strconv.ParseInt(string(uuid[14]), 16, 0)
	if err != nil {
		return 0
	}
	return int(n)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

// DefaultUUID returns the process-wide UUIDModule.
func DefaultUUID() *UUIDModule {
	return New()
}

// Init (re)initialises the process-wide UUIDModule. Stateless, so a no-op
// apart from returning the default instance.
func Init() *UUIDModule {
	return New()
}
