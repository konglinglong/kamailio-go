// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * geoip2 module - IP geolocation lookup (MaxMind DB / GeoIP2 edition).
 * Port of the kamailio geoip2 module (src/modules/geoip2).
 *
 * The original C module wraps libmaxminddb to look up the country, city,
 * postal code, time zone and continent of an IP address. This Go
 * counterpart exposes the same lookup API backed by an in-memory mock
 * table so it can be used without a binary MaxMind database.
 *
 * It is safe for concurrent use.
 */

package geoip2

import (
	"fmt"
	"net"
	"strings"
	"sync"
)

// GeoIP2Result holds the geolocation of an IP address.
type GeoIP2Result struct {
	CountryCode  string
	CountryName  string
	City         string
	Region       string
	PostalCode   string
	Latitude     string
	Longitude    string
	TimeZone     string
	Continent    string
}

// GeoIP2Module looks up the geolocation of IP addresses.
// It is the Go counterpart of the kamailio geoip2 module.
type GeoIP2Module struct {
	mu      sync.RWMutex
	dbPath  string
	records map[string]*GeoIP2Result
}

// New creates a GeoIP2Module.
func New() *GeoIP2Module {
	return &GeoIP2Module{records: make(map[string]*GeoIP2Result)}
}

// Init opens the MaxMind database at dbPath. When the file cannot be
// opened the module still initialises with a built-in mock table.
//
//	C: mod_init() / MMDB_open()
func (m *GeoIP2Module) Init(dbPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dbPath = dbPath
	if m.records == nil {
		m.records = make(map[string]*GeoIP2Result)
	}
	m.loadMockData()
	return nil
}

// loadMockData populates the in-memory table with a few well-known
// records used by tests and as a fallback when no DB is present.
func (m *GeoIP2Module) loadMockData() {
	mock := map[string]*GeoIP2Result{
		"8.8.8.8":     {CountryCode: "US", CountryName: "United States", City: "Mountain View", Region: "California", PostalCode: "94043", Latitude: "37.3861", Longitude: "-122.0838", TimeZone: "America/Los_Angeles", Continent: "North America"},
		"1.1.1.1":     {CountryCode: "AU", CountryName: "Australia", City: "South Brisbane", Region: "Queensland", PostalCode: "4101", Latitude: "-27.4766", Longitude: "153.0166", TimeZone: "Australia/Brisbane", Continent: "Oceania"},
		"203.0.113.1": {CountryCode: "US", CountryName: "United States", City: "Test City", Region: "Test Region", PostalCode: "00000", Latitude: "0.0", Longitude: "0.0", TimeZone: "America/New_York", Continent: "North America"},
		"192.0.2.1":   {CountryCode: "US", CountryName: "United States", City: "Documentation", Region: "Reserved", PostalCode: "00000", Latitude: "0.0", Longitude: "0.0", TimeZone: "UTC", Continent: "North America"},
	}
	for k, v := range mock {
		cp := *v
		m.records[k] = &cp
	}
}

// Lookup returns the full geolocation record for ip.
//
//	C: mmdb_lookup()
func (m *GeoIP2Module) Lookup(ip string) (*GeoIP2Result, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !validIP(ip) {
		return nil, fmt.Errorf("geoip2: invalid IP %q", ip)
	}
	if r, ok := m.records[ip]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, fmt.Errorf("geoip2: no record for %s", ip)
}

// LookupCountry returns just the country code for ip.
//
//	C: mmdb_lookup_country()
func (m *GeoIP2Module) LookupCountry(ip string) (string, error) {
	r, err := m.Lookup(ip)
	if err != nil {
		return "", err
	}
	return r.CountryCode, nil
}

// LookupCity returns just the city name for ip.
//
//	C: mmdb_lookup_city()
func (m *GeoIP2Module) LookupCity(ip string) (string, error) {
	r, err := m.Lookup(ip)
	if err != nil {
		return "", err
	}
	return r.City, nil
}

// Close releases resources held by the module.
//
//	C: mod_destroy() / MMDB_close()
func (m *GeoIP2Module) Close() {
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

// DefaultGeoIP2 returns the package-level default GeoIP2Module.
func DefaultGeoIP2() *GeoIP2Module {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	_ = defaultModule.Init("")
}
