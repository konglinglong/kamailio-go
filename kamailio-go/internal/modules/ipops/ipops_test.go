// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IP operations (ipops) module.
 */

package ipops

import (
	"sync"
	"testing"
)

func TestIsIP(t *testing.T) {
	m := New()

	valid := []string{"192.0.2.1", "10.0.0.1", "::1", "2001:db8::1", "127.0.0.1"}
	for _, ip := range valid {
		if !m.IsIP(ip) {
			t.Errorf("IsIP(%q) = false, want true", ip)
		}
	}
	invalid := []string{"", "not-an-ip", "999.999.999.999", "192.0.2", "::g"}
	for _, ip := range invalid {
		if m.IsIP(ip) {
			t.Errorf("IsIP(%q) = true, want false", ip)
		}
	}
}

func TestIsIPv4AndIPv6(t *testing.T) {
	m := New()

	v4 := []string{"192.0.2.1", "10.0.0.1", "127.0.0.1", "0.0.0.0"}
	for _, ip := range v4 {
		if !m.IsIPv4(ip) {
			t.Errorf("IsIPv4(%q) = false, want true", ip)
		}
		if m.IsIPv6(ip) {
			t.Errorf("IsIPv6(%q) = true, want false", ip)
		}
	}
	v6 := []string{"::1", "2001:db8::1", "fe80::1"}
	for _, ip := range v6 {
		if !m.IsIPv6(ip) {
			t.Errorf("IsIPv6(%q) = false, want true", ip)
		}
		if m.IsIPv4(ip) {
			t.Errorf("IsIPv4(%q) = true, want false", ip)
		}
	}
	// Invalid -> both false.
	if m.IsIPv4("nope") {
		t.Errorf("IsIPv4(nope) = true, want false")
	}
	if m.IsIPv6("nope") {
		t.Errorf("IsIPv6(nope) = true, want false")
	}
}

func TestCompareIPs(t *testing.T) {
	m := New()

	if !m.CompareIPs("192.0.2.1", "192.0.2.1") {
		t.Errorf("CompareIPs(same) = false, want true")
	}
	if m.CompareIPs("192.0.2.1", "192.0.2.2") {
		t.Errorf("CompareIPs(diff) = true, want false")
	}
	// IPv4-mapped IPv6 equals the IPv4 form.
	if !m.CompareIPs("192.0.2.1", "::ffff:192.0.2.1") {
		t.Errorf("CompareIPs(v4-mapped) = false, want true")
	}
	// Invalid -> false.
	if m.CompareIPs("nope", "192.0.2.1") {
		t.Errorf("CompareIPs(invalid) = true, want false")
	}
}

func TestIsPrivateIP(t *testing.T) {
	m := New()

	private := []string{"10.0.0.1", "172.16.0.1", "172.31.255.1", "192.168.1.1", "127.0.0.1", "fc00::1", "fe80::1"}
	for _, ip := range private {
		if !m.IsPrivateIP(ip) {
			t.Errorf("IsPrivateIP(%q) = false, want true", ip)
		}
	}
	public := []string{"1.1.1.1", "8.8.8.8", "192.0.2.1", "2001:db8::1"}
	for _, ip := range public {
		if m.IsPrivateIP(ip) {
			t.Errorf("IsPrivateIP(%q) = true, want false", ip)
		}
	}
	if m.IsPrivateIP("nope") {
		t.Errorf("IsPrivateIP(invalid) = true, want false")
	}
}

func TestIsLocalhost(t *testing.T) {
	m := New()

	if !m.IsLocalhost("127.0.0.1") {
		t.Errorf("IsLocalhost(127.0.0.1) = false, want true")
	}
	if !m.IsLocalhost("127.1.2.3") {
		t.Errorf("IsLocalhost(127.1.2.3) = false, want true")
	}
	if !m.IsLocalhost("::1") {
		t.Errorf("IsLocalhost(::1) = false, want true")
	}
	if m.IsLocalhost("192.0.2.1") {
		t.Errorf("IsLocalhost(192.0.2.1) = true, want false")
	}
	if m.IsLocalhost("nope") {
		t.Errorf("IsLocalhost(invalid) = true, want false")
	}
}

func TestIP2IntAndInt2IP(t *testing.T) {
	m := New()

	cases := []struct {
		ip  string
		val uint32
	}{
		{"0.0.0.0", 0},
		{"192.0.2.1", 0xC0000201},
		{"10.0.0.1", 0x0A000001},
		{"255.255.255.255", 0xFFFFFFFF},
		{"127.0.0.1", 0x7F000001},
	}
	for _, tc := range cases {
		got, err := m.IP2Int(tc.ip)
		if err != nil {
			t.Errorf("IP2Int(%q) error = %v", tc.ip, err)
			continue
		}
		if got != tc.val {
			t.Errorf("IP2Int(%q) = 0x%X, want 0x%X", tc.ip, got, tc.val)
		}
		// Round trip back to the dotted-quad form.
		if back := m.Int2IP(tc.val); back != tc.ip {
			t.Errorf("Int2IP(0x%X) = %q, want %q", tc.val, back, tc.ip)
		}
	}

	// IPv6 -> error.
	if _, err := m.IP2Int("::1"); err == nil {
		t.Errorf("IP2Int(::1) should error")
	}
	// Invalid -> error.
	if _, err := m.IP2Int("nope"); err == nil {
		t.Errorf("IP2Int(invalid) should error")
	}
}

func TestIsInSubnet(t *testing.T) {
	m := New()

	if !m.IsInSubnet("192.0.2.10", "192.0.2.0/24") {
		t.Errorf("IsInSubnet(in) = false, want true")
	}
	if m.IsInSubnet("192.0.3.10", "192.0.2.0/24") {
		t.Errorf("IsInSubnet(out) = true, want false")
	}
	if !m.IsInSubnet("10.1.2.3", "10.0.0.0/8") {
		t.Errorf("IsInSubnet(10/8) = false, want true")
	}
	// IPv6 subnet.
	if !m.IsInSubnet("2001:db8::1", "2001:db8::/32") {
		t.Errorf("IsInSubnet(ipv6) = false, want true")
	}
	if m.IsInSubnet("2001:db9::1", "2001:db8::/32") {
		t.Errorf("IsInSubnet(ipv6 out) = true, want false")
	}
	// Bare IP (no mask) -> exact match.
	if !m.IsInSubnet("192.0.2.1", "192.0.2.1") {
		t.Errorf("IsInSubnet(exact) = false, want true")
	}
	if m.IsInSubnet("192.0.2.1", "192.0.2.2") {
		t.Errorf("IsInSubnet(exact diff) = true, want false")
	}
	// Invalid -> false.
	if m.IsInSubnet("nope", "192.0.2.0/24") {
		t.Errorf("IsInSubnet(invalid ip) = true, want false")
	}
	if m.IsInSubnet("192.0.2.1", "nope") {
		t.Errorf("IsInSubnet(invalid cidr) = true, want false")
	}
}

func TestResolveHost(t *testing.T) {
	m := New()

	// An IP literal is returned as-is.
	ips, err := m.ResolveHost("127.0.0.1")
	if err != nil {
		t.Fatalf("ResolveHost(ip) error = %v", err)
	}
	if len(ips) != 1 || ips[0] != "127.0.0.1" {
		t.Errorf("ResolveHost(ip) = %v, want [127.0.0.1]", ips)
	}

	// localhost should resolve to at least one loopback address.
	ips, err = m.ResolveHost("localhost")
	if err != nil {
		t.Fatalf("ResolveHost(localhost) error = %v", err)
	}
	if len(ips) == 0 {
		t.Fatalf("ResolveHost(localhost) returned no addresses")
	}
	foundLoopback := false
	for _, ip := range ips {
		if ip == "127.0.0.1" || ip == "::1" {
			foundLoopback = true
		}
	}
	if !foundLoopback {
		t.Errorf("ResolveHost(localhost) = %v, no loopback found", ips)
	}

	// Empty host -> error.
	if _, err := m.ResolveHost(""); err == nil {
		t.Errorf("ResolveHost(\"\") should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultIPOps()
	if d == nil {
		t.Fatalf("DefaultIPOps() returned nil")
	}
	if d != DefaultIPOps() {
		t.Fatalf("DefaultIPOps() returned different instances after Init()")
	}
	if !d.IsIP("1.2.3.4") {
		t.Errorf("default IsIP() = false, want true")
	}
}

func TestConcurrent(t *testing.T) {
	Init()
	shared := DefaultIPOps()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			shared.IsIP("192.0.2.1")
			shared.IsIPv4("10.0.0.1")
			shared.IsIPv6("::1")
			shared.CompareIPs("192.0.2.1", "192.0.2.1")
			shared.IsPrivateIP("10.0.0.1")
			shared.IsLocalhost("127.0.0.1")
			shared.IP2Int("192.0.2.1")
			shared.Int2IP(0xC0000201)
			shared.IsInSubnet("192.0.2.1", "192.0.2.0/24")
		}()
	}
	wg.Wait()
}
