// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IP operations module - IP address inspection and manipulation.
 * Port of the kamailio ipops module (src/modules/ipops).
 *
 * The ipops module exposes helpers for inspecting IP addresses: validating
 * IPv4/IPv6 forms, comparing two addresses, detecting RFC 1918 private
 * and loopback addresses, converting between dotted-quad and integer
 * representations, subnet membership tests and forward DNS resolution.
 *
 * It is safe for concurrent use: the module holds no mutable state after
 * construction and the process-wide singleton is guarded by a mutex.
 */

package ipops

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
)

// IPOpsModule implements the ipops module functionality.
// C: struct module ipops
type IPOpsModule struct {
	mu sync.RWMutex
}

// New creates an IPOpsModule instance.
func New() *IPOpsModule {
	return &IPOpsModule{}
}

// IsIP reports whether ip is a valid IPv4 or IPv6 address.
//
//	C: is_ip()
func (m *IPOpsModule) IsIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// IsIPv4 reports whether ip is a valid IPv4 address.
//
//	C: is_ipv4()
func (m *IPOpsModule) IsIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.To4() != nil
}

// IsIPv6 reports whether ip is a valid IPv6 address.
//
//	C: is_ipv6()
func (m *IPOpsModule) IsIPv6(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.To4() == nil
}

// CompareIPs reports whether ip1 and ip2 represent the same IP address.
// Both textual forms are normalised via ParseIP, so "192.0.2.1" and an
// IPv4-mapped IPv6 form are considered equal when they denote the same
// address.
//
//	C: compare_ips()
func (m *IPOpsModule) CompareIPs(ip1, ip2 string) bool {
	p1 := net.ParseIP(ip1)
	p2 := net.ParseIP(ip2)
	if p1 == nil || p2 == nil {
		return false
	}
	return p1.Equal(p2)
}

// IsPrivateIP reports whether ip belongs to an RFC 1918 private range
// (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16), the loopback range, or the
// IPv6 unique-local (fc00::/7) / link-local (fe80::/10) ranges.
//
//	C: is_private_ip()
func (m *IPOpsModule) IsPrivateIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	if parsed.IsPrivate() {
		return true
	}
	// net.IP.IsPrivate covers RFC 1918 and ULA; also treat loopback and
	// link-local as private for the purposes of ipops.
	if parsed.IsLoopback() || parsed.IsLinkLocalUnicast() {
		return true
	}
	return false
}

// IsLocalhost reports whether ip is a loopback address (127.0.0.0/8 or
// ::1).
//
//	C: is_localhost()
func (m *IPOpsModule) IsLocalhost(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback()
}

// IP2Int converts an IPv4 address to its uint32 representation. Returns an
// error when ip is not a valid IPv4 address.
//
//	C: ip2int()
func (m *IPOpsModule) IP2Int(ip string) (uint32, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return 0, errors.New("ipops: invalid IP address")
	}
	v4 := parsed.To4()
	if v4 == nil {
		return 0, errors.New("ipops: not an IPv4 address")
	}
	return uint32(v4[0])<<24 | uint32(v4[1])<<16 | uint32(v4[2])<<8 | uint32(v4[3]), nil
}

// Int2IP converts a uint32 value to its dotted-quad IPv4 representation.
//
//	C: int2ip()
func (m *IPOpsModule) Int2IP(val uint32) string {
	b0 := byte(val >> 24)
	b1 := byte(val >> 16)
	b2 := byte(val >> 8)
	b3 := byte(val)
	return strconv.Itoa(int(b0)) + "." + strconv.Itoa(int(b1)) + "." +
		strconv.Itoa(int(b2)) + "." + strconv.Itoa(int(b3))
}

// IsInSubnet reports whether ip belongs to the supplied CIDR subnet (e.g.
// "192.0.2.0/24"). Returns false when either argument is invalid.
//
//	C: is_in_subnet()
func (m *IPOpsModule) IsInSubnet(ip string, cidr string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	// A bare IP without a mask is treated as a /32 (IPv4) or /128 (IPv6).
	if !strings.Contains(cidr, "/") {
		other := net.ParseIP(cidr)
		if other == nil {
			return false
		}
		return parsed.Equal(other)
	}
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return network.Contains(parsed)
}

// ResolveHost performs a forward DNS lookup of host and returns the list
// of resolved IP address strings. Both A and AAAA records are returned.
//
//	C: dns_query() / resolvehost()
func (m *IPOpsModule) ResolveHost(host string) ([]string, error) {
	if host == "" {
		return nil, errors.New("ipops: empty host")
	}
	// If host is already an IP, return it directly.
	if parsed := net.ParseIP(host); parsed != nil {
		return []string{parsed.String()}, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu    sync.RWMutex
	defaultIPOps *IPOpsModule
)

// DefaultIPOps returns the process-wide IPOpsModule, creating it on first
// use.
func DefaultIPOps() *IPOpsModule {
	defaultMu.RLock()
	m := defaultIPOps
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultIPOps == nil {
		defaultIPOps = New()
	}
	return defaultIPOps
}

// Init (re)initialises the process-wide IPOpsModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultIPOps = New()
}
