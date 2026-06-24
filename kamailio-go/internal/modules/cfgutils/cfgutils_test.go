// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the cfgutils module.
 */

package cfgutils

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// inviteMsg is a minimal INVITE used only to exercise the msg-scoped helpers.
const inviteMsg = "INVITE sip:bob@biloxi.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776\r\n" +
	"To: Bob <sip:bob@biloxi.com>\r\n" +
	"From: Alice <sip:alice@atlanta.com>;tag=1928301774\r\n" +
	"Call-ID: a84b4c76e66710@pc33.atlanta.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

func mustParse(t *testing.T, raw string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMsg failed: %v", err)
	}
	return msg
}

func TestRouteRegisterAndExists(t *testing.T) {
	c := NewCfgUtilsModule()
	msg := mustParse(t, inviteMsg)

	// No routes registered -> CheckRouteRegister is false.
	if c.CheckRouteRegister(msg) {
		t.Error("CheckRouteRegister = true on fresh module, want false")
	}
	if c.RouteExists("RELAY") {
		t.Error("RouteExists(RELAY) = true, want false")
	}

	c.RegisterRoute("RELAY")
	c.RegisterRoute("WITHINDLG")
	if !c.CheckRouteRegister(msg) {
		t.Error("CheckRouteRegister = false after registering routes, want true")
	}
	if !c.RouteExists("RELAY") {
		t.Error("RouteExists(RELAY) = false, want true")
	}
	if c.RouteExists("MISSING") {
		t.Error("RouteExists(MISSING) = true, want false")
	}

	// Empty name is a no-op.
	c.RegisterRoute("")
	if c.RouteExists("") {
		t.Error("RouteExists(empty) = true, want false")
	}

	c.UnregisterRoute("RELAY")
	if c.RouteExists("RELAY") {
		t.Error("RouteExists(RELAY) after unregister = true, want false")
	}
	// WITHINDLG still registered.
	if !c.CheckRouteRegister(msg) {
		t.Error("CheckRouteRegister = false with WITHINDLG still registered")
	}
}

func TestCounters(t *testing.T) {
	c := NewCfgUtilsModule()

	// Fresh counter reads 0.
	if got := c.GetCount("invites"); got != 0 {
		t.Errorf("GetCount(invites) = %d, want 0", got)
	}

	c.SetCount("invites", 5)
	if got := c.GetCount("invites"); got != 5 {
		t.Errorf("GetCount(invites) = %d, want 5", got)
	}

	c.SetCount("invites", 10)
	if got := c.GetCount("invites"); got != 10 {
		t.Errorf("GetCount(invites) after set = %d, want 10", got)
	}

	c.ResetCount("invites")
	if got := c.GetCount("invites"); got != 0 {
		t.Errorf("GetCount(invites) after reset = %d, want 0", got)
	}

	// IncCount creates and increments atomically.
	if got := c.IncCount("byes", 3); got != 3 {
		t.Errorf("IncCount(byes, 3) = %d, want 3", got)
	}
	if got := c.IncCount("byes", 2); got != 5 {
		t.Errorf("IncCount(byes, 2) = %d, want 5", got)
	}
	if got := c.GetCount("byes"); got != 5 {
		t.Errorf("GetCount(byes) = %d, want 5", got)
	}

	// Empty name is a no-op.
	c.SetCount("", 99)
	if got := c.GetCount(""); got != 0 {
		t.Errorf("GetCount(empty) = %d, want 0", got)
	}
}

func TestVars(t *testing.T) {
	c := NewCfgUtilsModule()

	if c.VarExists("domain") {
		t.Error("VarExists(domain) = true, want false")
	}
	if got := c.GetVar("domain"); got != "" {
		t.Errorf("GetVar(domain) = %q, want empty", got)
	}

	c.SetVar("domain", "biloxi.com")
	if !c.VarExists("domain") {
		t.Error("VarExists(domain) = false after set, want true")
	}
	if got := c.GetVar("domain"); got != "biloxi.com" {
		t.Errorf("GetVar(domain) = %q, want biloxi.com", got)
	}

	c.SetVar("domain", "atlanta.com")
	if got := c.GetVar("domain"); got != "atlanta.com" {
		t.Errorf("GetVar(domain) after overwrite = %q, want atlanta.com", got)
	}

	// Empty name is a no-op.
	c.SetVar("", "value")
	if c.VarExists("") {
		t.Error("VarExists(empty) = true, want false")
	}
}

func TestLockUnlockTryLock(t *testing.T) {
	c := NewCfgUtilsModule()

	// TryLock on a free lock succeeds.
	if !c.TryLock("k1") {
		t.Error("TryLock(k1) = false, want true")
	}
	// A second TryLock on the held lock fails.
	if c.TryLock("k1") {
		t.Error("TryLock(k1) held = true, want false")
	}
	// Unlock releases it.
	c.Unlock("k1")
	if !c.TryLock("k1") {
		t.Error("TryLock(k1) after unlock = false, want true")
	}
	c.Unlock("k1")

	// Unlock of an unknown key is a safe no-op.
	c.Unlock("never-locked")

	// Empty key is a no-op.
	if c.TryLock("") {
		t.Error("TryLock(empty) = true, want false")
	}
}

func TestLockBlocksUntilReleased(t *testing.T) {
	c := NewCfgUtilsModule()
	c.Lock("blocker")

	// Acquired in a goroutine; should not complete until we unlock.
	done := make(chan struct{})
	go func() {
		c.Lock("blocker")
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Lock returned while still held by the test goroutine")
	case <-time.After(50 * time.Millisecond):
		// expected: still blocked
	}

	c.Unlock("blocker")
	select {
	case <-done:
		// expected: Lock acquired after release
	case <-time.After(time.Second):
		t.Fatal("Lock did not return after Unlock within 1s")
	}
	c.Unlock("blocker")
}

func TestSleep(t *testing.T) {
	c := NewCfgUtilsModule()

	// Sleep should block for at least the requested duration.
	start := time.Now()
	c.Sleep(40)
	elapsed := time.Since(start)
	if elapsed < 30*time.Millisecond {
		t.Errorf("Sleep(40ms) returned after %v, want >= 30ms", elapsed)
	}

	// Non-positive values return immediately.
	start = time.Now()
	c.Sleep(0)
	c.Sleep(-5)
	if d := time.Since(start); d > 20*time.Millisecond {
		t.Errorf("Sleep(0/-5) took %v, want near-zero", d)
	}
}

func TestDefaultAndInit(t *testing.T) {
	d := DefaultCfgUtils()
	if d == nil {
		t.Fatal("DefaultCfgUtils() = nil")
	}
	d.SetVar("temp", "1")
	if got := d.GetVar("temp"); got != "1" {
		t.Errorf("GetVar(temp) = %q, want 1", got)
	}

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	d2 := DefaultCfgUtils()
	if d2.VarExists("temp") {
		t.Error("Init() did not reset the default instance")
	}

	// Package-level wrappers work on the default instance.
	SetCount("c", 7)
	if got := GetCount("c"); got != 7 {
		t.Errorf("GetCount(c) = %d, want 7", got)
	}
	SetVar("v", "hello")
	if got := GetVar("v"); got != "hello" {
		t.Errorf("GetVar(v) = %q, want hello", got)
	}
}

func TestConcurrentCounters(t *testing.T) {
	c := NewCfgUtilsModule()
	var wg sync.WaitGroup
	var fails atomic.Int32
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.IncCount("shared", 1)
			// TryLock contention path.
			if c.TryLock("L") {
				c.Unlock("L")
			} else {
				fails.Add(1)
			}
			_ = c.GetCount("shared")
			_ = c.GetVar("v")
		}()
	}
	wg.Wait()
	if got := c.GetCount("shared"); got != 50 {
		t.Errorf("GetCount(shared) = %d, want 50", got)
	}
}
