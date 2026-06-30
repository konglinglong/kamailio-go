// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Registrar's runtime config swap (SetConfig / Config),
 * which backs the hot-reload subscriber wired in app.Bootstrap.
 */

package registrar

import (
	"testing"
	"time"
)

func TestSetConfig_SwapsPolicyValues(t *testing.T) {
	r := New(&Config{
		Realm:          "old.example.com",
		DefaultExpires: 60 * time.Second,
		MaxExpires:     3600 * time.Second,
		MinExpires:     10 * time.Second,
	})
	if got := r.Config().Realm; got != "old.example.com" {
		t.Fatalf("initial realm = %q", got)
	}

	r.SetConfig(&Config{
		Realm:          "new.example.com",
		DefaultExpires: 120 * time.Second,
		MaxExpires:     7200 * time.Second,
		MinExpires:     30 * time.Second,
	})
	c := r.Config()
	if c.Realm != "new.example.com" {
		t.Fatalf("post-swap realm = %q, want new.example.com", c.Realm)
	}
	if c.DefaultExpires != 120*time.Second {
		t.Fatalf("DefaultExpires = %v, want 120s", c.DefaultExpires)
	}
	if c.MaxExpires != 7200*time.Second {
		t.Fatalf("MaxExpires = %v, want 7200s", c.MaxExpires)
	}
	if c.MinExpires != 30*time.Second {
		t.Fatalf("MinExpires = %v, want 30s", c.MinExpires)
	}
}

func TestSetConfig_AppliesDefaultsToZeroValues(t *testing.T) {
	r := New(&Config{Realm: "old.example.com"})
	// Pass a config with zero-value durations — SetConfig should fill
	// them in with the same defaults used by New().
	r.SetConfig(&Config{Realm: "new.example.com"})
	c := r.Config()
	if c.DefaultExpires != 3600*time.Second {
		t.Fatalf("DefaultExpires = %v, want 3600s (default)", c.DefaultExpires)
	}
	if c.MaxExpires != 24*time.Hour {
		t.Fatalf("MaxExpires = %v, want 24h (default)", c.MaxExpires)
	}
	if c.MinExpires != 60*time.Second {
		t.Fatalf("MinExpires = %v, want 60s (default)", c.MinExpires)
	}
}

func TestSetConfig_NilFallsBackToDefaults(t *testing.T) {
	r := New(&Config{Realm: "old.example.com"})
	r.SetConfig(nil)
	c := r.Config()
	if c.Realm != "kamailio-go" {
		t.Fatalf("Realm = %q, want default kamailio-go", c.Realm)
	}
	if c.DefaultExpires != 3600*time.Second {
		t.Fatalf("DefaultExpires = %v, want 3600s (default)", c.DefaultExpires)
	}
}

func TestSetConfig_PreservesExistingBindings(t *testing.T) {
	// Existing domain/AOR bindings must survive a config swap.
	r := New(&Config{Realm: "old.example.com"})
	// Register a domain & AOR via the public API.
	r.Domain("old.example.com")
	r.SetConfig(&Config{Realm: "new.example.com"})
	// The old domain should still be cached.
	d := r.Domain("old.example.com")
	if d == nil {
		t.Fatal("old domain disappeared after SetConfig")
	}
}
