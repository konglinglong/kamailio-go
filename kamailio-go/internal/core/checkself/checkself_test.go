// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the checkself package.
 */

package checkself

import (
	"net"
	"sync"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if aliases := r.Aliases(); len(aliases) != 0 {
		t.Errorf("new registry should have no aliases, got %v", aliases)
	}
}

func TestAddAlias(t *testing.T) {
	r := NewRegistry()
	r.AddAlias("example.com", 5060)

	if !r.CheckSelf("example.com", 5060, "udp") {
		t.Error("expected CheckSelf to match alias with port")
	}
	if !r.CheckSelf("example.com", 0, "udp") {
		t.Error("expected CheckSelf to match alias without port")
	}
	if !r.CheckSelf("EXAMPLE.com", 5060, "udp") {
		t.Error("expected CheckSelf to be case-insensitive")
	}
}

func TestAddAlias_EmptyHost(t *testing.T) {
	r := NewRegistry()
	r.AddAlias("   ", 0)
	if aliases := r.Aliases(); len(aliases) != 0 {
		t.Errorf("empty host should not register aliases, got %v", aliases)
	}
}

func TestAddAlias_NoPort(t *testing.T) {
	r := NewRegistry()
	r.AddAlias("proxy.example.com", 0)

	if !r.CheckSelf("proxy.example.com", 0, "tcp") {
		t.Error("expected CheckSelf to match alias with no port for any port")
	}
	if !r.CheckSelf("proxy.example.com", 9999, "tcp") {
		t.Error("expected CheckSelf to match alias (no port) regardless of queried port")
	}
}

func TestRemoveAlias(t *testing.T) {
	r := NewRegistry()
	r.AddAlias("gone.example.com", 5060)
	if !r.CheckSelf("gone.example.com", 5060, "udp") {
		t.Fatal("alias should be registered")
	}

	r.RemoveAlias("gone.example.com", 5060)
	if r.CheckSelf("gone.example.com", 5060, "udp") {
		t.Error("alias should be removed")
	}
}

func TestRemoveAlias_CaseInsensitive(t *testing.T) {
	r := NewRegistry()
	r.AddAlias("Host.Example.COM", 5060)
	r.RemoveAlias("host.example.com", 5060)
	if r.CheckSelf("host.example.com", 5060, "udp") {
		t.Error("alias should be removed despite case difference")
	}
}

func TestAliases_Snapshot(t *testing.T) {
	r := NewRegistry()
	r.AddAlias("a.com", 0)
	r.AddAlias("b.com", 5060)

	got := r.Aliases()
	if len(got) != 3 { // a.com, b.com, b.com:5060
		t.Errorf("expected 3 alias entries, got %d: %v", len(got), got)
	}

	// Mutating the snapshot must not affect the registry.
	got[0] = "mutated"
	if r.CheckSelf("a.com", 0, "udp") {
		// still works
	} else {
		t.Error("mutating snapshot should not affect registry")
	}
}

func TestCheckSelf_EmptyHost(t *testing.T) {
	r := NewRegistry()
	if r.CheckSelf("", 5060, "udp") {
		t.Error("empty host should not match")
	}
	if r.CheckSelf("   ", 5060, "udp") {
		t.Error("whitespace host should not match")
	}
}

func TestCheckSelf_HostPortPriority(t *testing.T) {
	r := NewRegistry()
	// Register host:port alias only.
	r.AddAlias("srv.example.com", 5060)
	// host:port match
	if !r.CheckSelf("srv.example.com", 5060, "udp") {
		t.Error("expected host:port match")
	}
	// bare host should also match because AddAlias registers both forms
	if !r.CheckSelf("srv.example.com", 0, "udp") {
		t.Error("expected bare host match (AddAlias registers both forms)")
	}
}

func TestCheckSelf_Miss(t *testing.T) {
	r := NewRegistry()
	r.AddAlias("known.example.com", 0)
	if r.CheckSelf("unknown.example.com", 5060, "udp") {
		t.Error("unknown host should not match")
	}
}

func TestRegisterCallback(t *testing.T) {
	r := NewRegistry()
	called := false
	r.RegisterCallback(func(host string, port int, proto string) bool {
		if host == "callback.example.com" {
			called = true
			return true
		}
		return false
	})

	if !r.CheckSelf("callback.example.com", 0, "udp") {
		t.Error("expected callback to make CheckSelf return true")
	}
	if !called {
		t.Error("callback was not invoked")
	}
}

func TestRegisterCallback_FirstTrueWins(t *testing.T) {
	r := NewRegistry()
	secondCalled := false
	r.RegisterCallback(func(host string, port int, proto string) bool {
		return true // always true
	})
	r.RegisterCallback(func(host string, port int, proto string) bool {
		secondCalled = true
		return false
	})

	if !r.CheckSelf("any.host", 0, "udp") {
		t.Error("first callback returned true, CheckSelf should be true")
	}
	if secondCalled {
		t.Error("second callback should not be invoked after first returned true")
	}
}

func TestRegisterCallback_NoMatch(t *testing.T) {
	r := NewRegistry()
	r.RegisterCallback(func(host string, port int, proto string) bool {
		return false
	})
	if r.CheckSelf("nope.example.com", 0, "udp") {
		t.Error("callback returning false should not make CheckSelf true")
	}
}

func TestIsMyself(t *testing.T) {
	r := NewRegistry()
	r.AddAlias("myself.example.com", 0)
	if !r.IsMyself("myself.example.com", 0) {
		t.Error("IsMyself should match registered alias")
	}
	if r.IsMyself("other.example.com", 0) {
		t.Error("IsMyself should not match unregistered host")
	}
}

func TestCheckSelfIP_Loopback(t *testing.T) {
	r := NewRegistry()
	if !r.CheckSelfIP(net.ParseIP("127.0.0.1"), 0, "udp") {
		t.Error("127.0.0.1 should be considered local")
	}
	if !r.CheckSelfIP(net.ParseIP("::1"), 0, "udp") {
		t.Error("::1 should be considered local")
	}
}

func TestCheckSelfIP_LinkLocal(t *testing.T) {
	r := NewRegistry()
	if !r.CheckSelfIP(net.ParseIP("169.254.1.1"), 0, "udp") {
		t.Error("169.254.1.1 link-local should be considered local")
	}
	if !r.CheckSelfIP(net.ParseIP("fe80::1"), 0, "udp") {
		t.Error("fe80::1 link-local should be considered local")
	}
}

func TestCheckSelfIP_Registered(t *testing.T) {
	r := NewRegistry()
	r.AddAlias("10.0.0.1", 5060)
	if !r.CheckSelfIP(net.ParseIP("10.0.0.1"), 5060, "udp") {
		t.Error("registered IP should be considered local")
	}
}

func TestCheckSelfIP_Nil(t *testing.T) {
	r := NewRegistry()
	if r.CheckSelfIP(nil, 0, "udp") {
		t.Error("nil IP should not be considered local")
	}
}

func TestCheckSelfIP_Unregistered(t *testing.T) {
	r := NewRegistry()
	if r.CheckSelfIP(net.ParseIP("203.0.113.1"), 0, "udp") {
		t.Error("unregistered public IP should not be considered local")
	}
}

func TestReset(t *testing.T) {
	r := NewRegistry()
	r.AddAlias("reset.example.com", 0)
	r.RegisterCallback(func(host string, port int, proto string) bool { return true })

	r.Reset()

	if aliases := r.Aliases(); len(aliases) != 0 {
		t.Errorf("after Reset, aliases should be empty, got %v", aliases)
	}
	if r.CheckSelf("reset.example.com", 0, "udp") {
		t.Error("after Reset, no callback should match")
	}
}

func TestItoa(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{5060, "5060"},
		{1, "1"},
		{65535, "65535"},
	}
	for _, c := range cases {
		if got := itoa(c.in); got != c.want {
			t.Errorf("itoa(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDefault_Initialized(t *testing.T) {
	d := Default()
	if d == nil {
		t.Fatal("Default returned nil")
	}
	// Default registry should include localhost and loopback.
	if !d.CheckSelf("localhost", 0, "udp") {
		t.Error("default registry should consider localhost as self")
	}
	if !d.CheckSelf("127.0.0.1", 0, "udp") {
		t.Error("default registry should consider 127.0.0.1 as self")
	}
	if !d.CheckSelf("::1", 0, "udp") {
		t.Error("default registry should consider ::1 as self")
	}
}

func TestDefault_SameInstance(t *testing.T) {
	if Default() != Default() {
		t.Error("Default should return the same instance on subsequent calls")
	}
}

func TestInit_ResetDefault(t *testing.T) {
	// Snapshot the current default, then re-init.
	original := Default()
	original.AddAlias("pre-init.example.com", 0)

	Init()

	// After Init, the pre-init alias should be gone.
	if Default().CheckSelf("pre-init.example.com", 0, "udp") {
		t.Error("Init should reset the default registry and drop pre-Init aliases")
	}
	// But the standard defaults should still be present.
	if !Default().CheckSelf("localhost", 0, "udp") {
		t.Error("Init should restore localhost alias")
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.AddAlias("host", 5000+n)
		}(i)
	}
	// Concurrent readers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.CheckSelf("host", 5000, "udp")
			_ = r.Aliases()
		}()
	}
	// Concurrent callback registrations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.RegisterCallback(func(host string, port int, proto string) bool {
				return false
			})
		}()
	}
	wg.Wait()
}

// hostnameHook allows tests to override the hostname function.
func TestDefault_HostnameAlias(t *testing.T) {
	original := hostname
	defer func() { hostname = original }()

	hostname = func() (string, error) {
		return "testhost.example.com", nil
	}
	Init()
	if !Default().CheckSelf("testhost.example.com", 0, "udp") {
		t.Error("hostname-derived alias should be considered self")
	}
	if !Default().CheckSelf("TESTHOST.EXAMPLE.COM", 0, "udp") {
		t.Error("hostname alias should be matched case-insensitively")
	}
}

func TestDefault_HostnameError(t *testing.T) {
	original := hostname
	defer func() { hostname = original }()

	hostname = func() (string, error) {
		return "", errHostnameUnavailable
	}
	Init()
	// Should not panic even if hostname lookup fails.
	if !Default().CheckSelf("localhost", 0, "udp") {
		t.Error("localhost should still be considered self even if hostname lookup fails")
	}
}

var errHostnameUnavailable = &hostnameErr{"hostname unavailable"}

type hostnameErr struct{ msg string }

func (e *hostnameErr) Error() string { return e.msg }
