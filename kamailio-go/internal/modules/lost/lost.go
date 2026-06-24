// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * lost module - LoST (Location-to-Service Translation) client.
 * Port of the kamailio lost module (src/modules/lost).
 *
 * The original C module queries a LoST server (RFC 5222) to map a
 * civic or geodetic location to a service URI, typically for emergency
 * call routing (E.164 to PSAP mapping). This Go counterpart exposes
 * the same FindService API backed by an in-memory mapping table.
 *
 * It is safe for concurrent use.
 */

package lost

import (
	"fmt"
	"strings"
	"sync"
)

// LoSTConfig configures a LoSTModule.
type LoSTConfig struct {
	Server string
	Domain string
}

// LoSTResult holds a LoST mapping result.
type LoSTResult struct {
	URI     string
	Service string
	Display string
}

// LoSTModule maps locations and phone numbers to service URIs.
// It is the Go counterpart of the kamailio lost module.
type LoSTModule struct {
	mu       sync.RWMutex
	server   string
	domain   string
	services map[string]string // service urn -> default URI
}

// New creates a LoSTModule.
func New() *LoSTModule {
	return &LoSTModule{services: make(map[string]string)}
}

// Init (re)configures the module from cfg. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *LoSTModule) Init(cfg *LoSTConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &LoSTConfig{}
	}
	m.server = cfg.Server
	m.domain = cfg.Domain
	if m.services == nil {
		m.services = make(map[string]string)
	}
	m.loadDefaults()
	return nil
}

// loadDefaults populates the default service-to-URI mapping.
func (m *LoSTModule) loadDefaults() {
	m.services["urn:service:sos"] = "sip:sos@example.org"
	m.services["urn:service:sos.fire"] = "sip:fire@example.org"
	m.services["urn:service:sos.police"] = "sip:police@example.org"
	m.services["urn:service:sos.medical"] = "sip:medical@example.org"
}

// FindService maps a location and service URN to a LoSTResult. The
// location is currently informational; the service URN selects the URI.
//
//	C: lost_find_service()
func (m *LoSTModule) FindService(location, service string) (*LoSTResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	uri, ok := m.services[service]
	if !ok {
		return nil, fmt.Errorf("lost: no mapping for service %q", service)
	}
	return &LoSTResult{
		URI:     uri,
		Service: service,
		Display: m.displayName(service),
	}, nil
}

// MapService maps a phone number (emergency short code) to a service
// URN. Returns an empty string when no mapping is known. Numbers that
// carry a country-code prefix (e.g. "+1-911") are matched by their
// trailing emergency digits.
//
//	C: lost_map_service()
func (m *LoSTModule) MapService(number string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	digits := digitsOnly(number)
	switch digits {
	case "911", "112":
		return "urn:service:sos"
	case "999":
		return "urn:service:sos.police"
	case "119":
		return "urn:service:sos.fire"
	case "120", "110":
		return "urn:service:sos.medical"
	}
	// Fall back to a trailing 3-digit emergency code for numbers that
	// include a country-code prefix (e.g. "+1-911" -> "1911").
	if suffix := trailingCode(digits, 3); suffix != "" {
		switch suffix {
		case "911", "112":
			return "urn:service:sos"
		case "999":
			return "urn:service:sos.police"
		case "119":
			return "urn:service:sos.fire"
		}
	}
	return ""
}

// trailingCode returns the last n digits of s, or "" when s has fewer
// than n digits.
func trailingCode(s string, n int) string {
	if len(s) < n {
		return ""
	}
	return s[len(s)-n:]
}

// displayName derives a human-readable name from a service URN.
func (m *LoSTModule) displayName(service string) string {
	switch service {
	case "urn:service:sos":
		return "Emergency Services"
	case "urn:service:sos.fire":
		return "Fire Department"
	case "urn:service:sos.police":
		return "Police"
	case "urn:service:sos.medical":
		return "Medical Emergency"
	default:
		return service
	}
}

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

// --- package-level API ---

var defaultModule = New()

// DefaultLoST returns the package-level default LoSTModule.
func DefaultLoST() *LoSTModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	_ = defaultModule.Init(&LoSTConfig{})
}
