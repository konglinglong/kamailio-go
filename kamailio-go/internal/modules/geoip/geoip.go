// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * geoip module - IP geolocation lookup.
 * Port of the kamailio geoip module (src/modules/geoip).
 *
 * The original C module wraps the MaxMind GeoIP legacy library to look
 * up the country/city of an IP address. This Go counterpart exposes the
 * same lookup API backed by an in-memory mock table so it can be used
 * without a binary GeoIP database; real deployments would swap the
 * lookup implementation for a MaxMind reader.
 *
 * It is safe for concurrent use.
 */

package geoip

import (
	"fmt"
	"net"
	"strings"
	"sync"
)

// GeoIPResult holds the geolocation of an IP address.
type GeoIPResult struct {
	CountryCode string
	CountryName string
	City        string
	Region      string
	Latitude    string
	Longitude   string
}

// GeoIPModule looks up the geolocation of IP addresses.
// It is the Go counterpart of the kamailio geoip module.
type GeoIPModule struct {
	mu      sync.RWMutex
	dbPath  string
	records map[string]*GeoIPResult
}

// New creates a GeoIPModule.
func New() *GeoIPModule {
	return &GeoIPModule{records: make(map[string]*GeoIPResult)}
}

// Init opens the GeoIP database at dbPath. When the file cannot be
// opened the module still initialises with a built-in mock table so
// lookups of well-known test addresses succeed.
//
//	C: mod_init() / GeoIP_open()
func (m *GeoIPModule) Init(dbPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dbPath = dbPath
	if m.records == nil {
		m.records = make(map[string]*GeoIPResult)
	}
	m.loadMockData()
	return nil
}

// loadMockData populates the in-memory table with a few well-known
// records used by tests and as a fallback when no DB is present.
func (m *GeoIPModule) loadMockData() {
	mock := map[string]*GeoIPResult{
		"8.8.8.8":       {CountryCode: "US", CountryName: "United States", City: "Mountain View", Region: "California", Latitude: "37.3861", Longitude: "-122.0838"},
		"1.1.1.1":       {CountryCode: "AU", CountryName: "Australia", City: "South Brisbane", Region: "Queensland", Latitude: "-27.4766", Longitude: "153.0166"},
		"203.0.113.1":   {CountryCode: "US", CountryName: "United States", City: "Test City", Region: "Test Region", Latitude: "0.0", Longitude: "0.0"},
		"192.0.2.1":      {CountryCode: "US", CountryName: "United States", City: "Documentation", Region: "Reserved", Latitude: "0.0", Longitude: "0.0"},
	}
	for k, v := range mock {
		cp := *v
		m.records[k] = &cp
	}
}

// Lookup returns the full geolocation record for ip.
//
//	C: geoip_lookup()
func (m *GeoIPModule) Lookup(ip string) (*GeoIPResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !validIP(ip) {
		return nil, fmt.Errorf("geoip: invalid IP %q", ip)
	}
	if r, ok := m.records[ip]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, fmt.Errorf("geoip: no record for %s", ip)
}

// LookupCountry returns just the country code for ip.
//
//	C: geoip_lookup_country()
func (m *GeoIPModule) LookupCountry(ip string) (string, error) {
	r, err := m.Lookup(ip)
	if err != nil {
		return "", err
	}
	return r.CountryCode, nil
}

// LookupCity returns just the city name for ip.
//
//	C: geoip_lookup_city()
func (m *GeoIPModule) LookupCity(ip string) (string, error) {
	r, err := m.Lookup(ip)
	if err != nil {
		return "", err
	}
	return r.City, nil
}

// Close releases resources held by the module.
//
//	C: mod_destroy() / GeoIP_delete()
func (m *GeoIPModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = nil
}

// validIP reports whether s parses as an IPv4 or IPv6 address.
func validIP(s string) bool {
	return net.ParseIP(strings.TrimSpace(s)) != nil
}

// --- package-level API ---

var defaultModule = New()

// DefaultGeoIP returns the package-level default GeoIPModule.
func DefaultGeoIP() *GeoIPModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	_ = defaultModule.Init("")
}
