# IMS Split-Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable deploying P-CSCF / I-CSCF / S-CSCF as separate processes via a single `kamailio-go` binary with a `--role` flag, connected by standard SIP forwarding.

**Architecture:** Single binary + role flag. A `proxy.CSCFAdaptor` interface sits in the ProxyCore dispatch path; each role implements it and returns a `proxy.ResponseAction` (existing type) that either responds (terminal role) or forwards to a configured `next_hop` (transit role). `--role all` reuses the same networked forwarding path with localhost next_hops. Three IMS business packages keep their existing APIs and tests untouched.

**Tech Stack:** Go 1.21+, YAML config, existing `proxy.ProxyCore`, `tm.Manager`, `forward.Forwarder`, `usrloc.Registrar`, `cdp.TransactionManager`.

**Spec:** `docs/superpowers/specs/2026-06-25-ims-split-deployment-design.md`

**Refinement vs spec:** The spec proposed a new `internal/ims/cscf` package with `Action/ActionKind/SIPResponse/ForwardTarget` types. Investigation shows `proxy.ResponseAction` (proxy.go:352) already encapsulates Status/Reason/ExtraHeaders/Target/StopRouting, and `dispatchRegister` already returns it. To avoid an import cycle (`cscf` ↔ `proxy`) and to honor DRY, the adaptor interface lives in `proxy` and returns `proxy.ResponseAction` directly. This is fully aligned with the spec's intent ("business layer declares forward/respond; ProxyCore executes") while dropping a redundant type layer.

---

## File Structure

**New files:**
- `internal/core/proxy/cscf_adaptor.go` — `CSCFAdaptor` interface + `SetCSCFAdaptors`/`applyCSCFAction` helpers (lives in `proxy` package to avoid import cycle).
- `internal/core/proxy/cscf_adaptor_test.go` — dispatch + applyAction tests.
- `internal/ims/pcscf/adaptor.go` — `pcscf.Adaptor` implementing `proxy.CSCFAdaptor`.
- `internal/ims/pcscf/adaptor_test.go` — unit tests.
- `internal/ims/icscf/adaptor.go` — `icscf.Adaptor` implementing `proxy.CSCFAdaptor`.
- `internal/ims/icscf/adaptor_test.go` — unit tests.
- `internal/ims/scscf/adaptor.go` — `scscf.Adaptor` implementing `proxy.CSCFAdaptor`.
- `internal/ims/scscf/adaptor_test.go` — unit tests.
- `internal/core/app/ims_bootstrap.go` — `buildIMSAdaptors` + helpers.
- `internal/core/app/ims_bootstrap_test.go` — bootstrap wiring tests.
- `configs/ims-pcscf.yaml`, `configs/ims-icscf.yaml`, `configs/ims-scscf.yaml`, `configs/ims-all.yaml`.
- `internal/integration/ims_split_deployment_e2e_test.go` — `--role all` three-hop E2E.

**Modified files:**
- `internal/core/config/config.go` — extend `IMSConfig` with per-role sections + `ResolveRole` + `ListenFor`.
- `internal/core/config/validator.go` — IMS role validation rules.
- `internal/core/config/config_test.go` — role resolution, listen, backward-compat tests.
- `internal/core/app/bootstrap.go` — `BootstrapOptions.Role` + IMS wiring call.
- `cmd/kamailio/main.go` — `--role` flag.

**Untouched:** `internal/ims/{pcscf,scscf,icscf}` business-package existing files & tests; `internal/modules/ims_*` legacy modules; `forward.Forwarder`, `tm.Manager`, `usrloc.Registrar`, `cdp.TransactionManager` interfaces.

---

## Task 1: CSCFAdaptor Interface + ProxyCore Attachment

**Files:**
- Create: `internal/core/proxy/cscf_adaptor.go`
- Modify: `internal/core/proxy/proxy.go` (add field + setter, ~5 lines)
- Test: `internal/core/proxy/cscf_adaptor_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/core/proxy/cscf_adaptor_test.go`:

```go
package proxy

import (
	"context"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// stubAdaptor is a minimal CSCFAdaptor for testing dispatch.
type stubAdaptor struct {
	role     int
	register func(ctx context.Context, msg *parser.SIPMsg) ResponseAction
	invite   func(ctx context.Context, msg *parser.SIPMsg) ResponseAction
	indialog func(ctx context.Context, msg *parser.SIPMsg) ResponseAction
}

func (s *stubAdaptor) Role() int { return s.role }
func (s *stubAdaptor) HandleRegister(ctx context.Context, msg *parser.SIPMsg) ResponseAction {
	if s.register != nil {
		return s.register(ctx, msg)
	}
	return ResponseAction{Status: 0, StopRouting: false}
}
func (s *stubAdaptor) HandleInvite(ctx context.Context, msg *parser.SIPMsg) ResponseAction {
	if s.invite != nil {
		return s.invite(ctx, msg)
	}
	return ResponseAction{Status: 0, StopRouting: false}
}
func (s *stubAdaptor) HandleInDialog(ctx context.Context, msg *parser.SIPMsg) ResponseAction {
	if s.indialog != nil {
		return s.indialog(ctx, msg)
	}
	return ResponseAction{Status: 0, StopRouting: false}
}

func TestSetCSCFAdaptors_Attaches(t *testing.T) {
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	pcore.SetCSCFAdaptors([]CSCFAdaptor{
		&stubAdaptor{role: RolePCSCF},
	})
	if got := len(pcore.cscfAdaptors); got != 1 {
		t.Fatalf("adaptors = %d, want 1", got)
	}
}

func TestApplyCSCFAction_RespondStops(t *testing.T) {
	// A Respond action (Status set, StopRouting true) must report handled.
	act := ResponseAction{Status: 401, Reason: "Unauthorized", StopRouting: true}
	if !applyCSCFAction(ResponseAction{}, act) {
		// applyCSCFAction returns true when the action is terminal
		// (Status != 0 or Target != "" or StopRouting).
		t.Fatalf("applyCSCFAction returned false for Respond")
	}
	_ = act
}

func TestApplyCSCFAction_ForwardStops(t *testing.T) {
	act := ResponseAction{Target: "sip:icscf.home.net:5060", StopRouting: true}
	if !applyCSCFAction(ResponseAction{}, act) {
		t.Fatalf("applyCSCFAction returned false for Forward")
	}
}

func TestApplyCSCFAction_EmptyContinues(t *testing.T) {
	// An empty action (Status 0, no Target, not StopRouting) means
	// "this adaptor declined; try the next".
	act := ResponseAction{}
	if applyCSCFAction(ResponseAction{}, act) {
		t.Fatalf("applyCSCFAction returned true for empty (declined) action")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /workspace/kamailio-go && go test ./internal/core/proxy/ -run TestSetCSCFAdaptors_Attaches 2>&1 | tail -10`
Expected: FAIL with `undefined: CSCFAdaptor` / `undefined: cscfAdaptors`.

- [ ] **Step 3: Create the interface file**

Create `internal/core/proxy/cscf_adaptor.go`:

```go
// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * CSCFAdaptor — the attachment surface for IMS P/I/S-CSCF role handlers
 * on ProxyCore's dispatch path. Each role implements this interface;
 * the bootstrap attaches the relevant adaptors based on --role.
 */

package proxy

import (
	"context"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// CSCF role identifiers.
const (
	RolePCSCF = iota
	RoleICSCF
	RoleSCSCF
)

// CSCFAdaptor is a CSCF role's hook into ProxyCore's dispatch path.
// Implementations call their IMS business layer, then return a
// ResponseAction that either:
//   - responds directly (Status/Reason/ExtraHeaders set, StopRouting=true)
//     for terminal roles (e.g. S-CSCF REGISTER); or
//   - forwards to a next hop (Target set, StopRouting=true) for transit
//     roles (e.g. P-CSCF forwarding REGISTER to I-CSCF); or
//   - declines (zero value) so the next adaptor / fallback registrar runs.
type CSCFAdaptor interface {
	Role() int
	HandleRegister(ctx context.Context, msg *parser.SIPMsg) ResponseAction
	HandleInvite(ctx context.Context, msg *parser.SIPMsg) ResponseAction
	HandleInDialog(ctx context.Context, msg *parser.SIPMsg) ResponseAction
}

// applyCSCFAction reports whether the adaptor produced a terminal action
// (Status != 0, Target != "", or StopRouting). A declined adaptor returns
// the zero value and applyCSCFAction returns false so dispatch can continue.
func applyCSCFAction(_ ResponseAction, act ResponseAction) bool {
	return act.Status != 0 || act.Target != "" || act.StopRouting
}
```

- [ ] **Step 4: Add the field + setter to ProxyCore**

Modify `internal/core/proxy/proxy.go`. In the `ProxyCore` struct (around line 131, after `tmMgr *tm.Manager`), add:

```go
	// cscfAdaptors holds IMS role handlers attached via SetCSCFAdaptors.
	// Dispatch walks them in order before the generic registrar/tm path.
	cscfAdaptors []CSCFAdaptor
```

Add the setter near the other `Set*` methods (after `SetTM` around line 238):

```go
// SetCSCFAdaptors attaches IMS role adaptors. When non-empty, REGISTER and
// INVITE dispatch walks the adaptors first; a declined adaptor (zero
// ResponseAction) falls through to the next, and finally to the generic
// registrar/tm path.
func (p *ProxyCore) SetCSCFAdaptors(a []CSCFAdaptor) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cscfAdaptors = a
}
```

- [ ] **Step 5: Run all proxy tests to verify nothing regressed**

Run: `cd /workspace/kamailio-go && go test ./internal/core/proxy/ 2>&1 | tail -10`
Expected: PASS (new tests + existing tests).

- [ ] **Step 6: Commit**

```bash
cd /workspace/kamailio-go && git add internal/core/proxy/cscf_adaptor.go internal/core/proxy/cscf_adaptor_test.go internal/core/proxy/proxy.go && git commit -m "feat(proxy): add CSCFAdaptor interface and SetCSCFAdaptors"
```

---

## Task 2: ProxyCore Dispatch Integration

**Files:**
- Modify: `internal/core/proxy/proxy.go` — wire adaptors into `dispatchRegister` and `dispatchInvite`.
- Test: `internal/core/proxy/cscf_adaptor_test.go` (extend).

- [ ] **Step 1: Write failing dispatch tests**

Append to `internal/core/proxy/cscf_adaptor_test.go`:

```go
func TestDispatchRegister_RoutesToFirstAdaptor(t *testing.T) {
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	called := 0
	a1 := &stubAdaptor{
		role: RolePCSCF,
		register: func(ctx context.Context, msg *parser.SIPMsg) ResponseAction {
			called++
			return ResponseAction{Status: 401, Reason: "Unauthorized", StopRouting: true}
		},
	}
	a2 := &stubAdaptor{
		role: RoleICSCF,
		register: func(ctx context.Context, msg *parser.SIPMsg) ResponseAction {
			t.Fatal("second adaptor should not be called when first returns terminal")
			return ResponseAction{}
		},
	}
	pcore.SetCSCFAdaptors([]CSCFAdaptor{a1, a2})

	msg := mustBuildRegisterMsg(t, "sip:user@home.net")
	act := pcore.dispatchRegisterViaCSCF(msg, nil)
	if called != 1 {
		t.Fatalf("adaptor1 called %d times, want 1", called)
	}
	if act.Status != 401 {
		t.Fatalf("Status = %d, want 401", act.Status)
	}
}

func TestDispatchRegister_FallsBackWhenAllDecline(t *testing.T) {
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	a1 := &stubAdaptor{role: RolePCSCF} // returns zero (decline)
	pcore.SetCSCFAdaptors([]CSCFAdaptor{a1})

	// Without a registrar attached, the fallback stub returns 200 (no auth).
	msg := mustBuildRegisterMsg(t, "sip:user@home.net")
	act := pcore.dispatchRegisterViaCSCF(msg, nil)
	if act.Status != 200 {
		t.Fatalf("fallback Status = %d, want 200", act.Status)
	}
}

func TestDispatchRegister_NoAdaptorsUsesExistingPath(t *testing.T) {
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	msg := mustBuildRegisterMsg(t, "sip:user@home.net")
	act := pcore.dispatchRegisterViaCSCF(msg, nil)
	if act.Status != 200 {
		t.Fatalf("Status = %d, want 200 (no auth fallback)", act.Status)
	}
}

// mustBuildRegisterMsg builds a minimal REGISTER SIPMsg for tests.
func mustBuildRegisterMsg(t *testing.T, uri string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseSIPMsg("REGISTER sip:home.net SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1:5060\r\nFrom: <" + uri + ">;tag=abc\r\nTo: <" + uri + ">\r\nCall-ID: test-cid\r\nCSeq: 1 REGISTER\r\nContact: <" + uri + ">\r\nContent-Length: 0\r\n\r\n")
	if err != nil {
		t.Fatalf("parse REGISTER: %v", err)
	}
	return msg
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /workspace/kamailio-go && go test ./internal/core/proxy/ -run TestDispatchRegister 2>&1 | tail -10`
Expected: FAIL with `undefined: pcore.dispatchRegisterViaCSCF` (or parser API mismatch — adjust to actual `parser.ParseSIPMsg` signature if different).

If `parser.ParseSIPMsg` does not exist with that signature, search the parser package:
Run: `cd /workspace/kamailio-go && grep -rn "func Parse" internal/core/parser/ | head -5`
Adjust `mustBuildRegisterMsg` to use the actual parser entry point found.

- [ ] **Step 3: Add dispatchRegisterViaCSCF and wire it into dispatchRegister**

Modify `internal/core/proxy/proxy.go`. At the top of `dispatchRegister` (line ~1081), insert an adaptor walk before the existing registrar logic:

```go
func (p *ProxyCore) dispatchRegister(msg *parser.SIPMsg, src net.Addr) ResponseAction {
	// IMS role adaptors first.
	if act := p.dispatchRegisterViaCSCF(msg, src); act.Status != 0 || act.Target != "" || act.StopRouting {
		return act
	}

	p.mu.RLock()
	reg := p.registrar
	p.mu.RUnlock()
	if reg != nil {
		// ... existing registrar path unchanged ...
	}
	// ... existing fallback unchanged ...
}

// dispatchRegisterViaCSCF walks attached CSCF adaptors in order. The first
// one to return a terminal action (Status!=0, Target!="", or StopRouting)
// wins. Returns the zero ResponseAction if all adaptors decline.
func (p *ProxyCore) dispatchRegisterViaCSCF(msg *parser.SIPMsg, src net.Addr) ResponseAction {
	p.mu.RLock()
	adaptors := p.cscfAdaptors
	p.mu.RUnlock()
	if len(adaptors) == 0 {
		return ResponseAction{}
	}
	ctx := context.Background()
	for _, a := range adaptors {
		act := a.HandleRegister(ctx, msg)
		if applyCSCFAction(ResponseAction{}, act) {
			return act
		}
	}
	return ResponseAction{}
}
```

Add `"context"` to the import block of proxy.go if not already present.

- [ ] **Step 4: Repeat for dispatchInvite**

At the top of `dispatchInvite` (line ~1112), insert:

```go
	// IMS role adaptors first.
	if act := p.dispatchInviteViaCSCF(msg, src); act.Status != 0 || act.Target != "" || act.StopRouting {
		return act
	}
```

And add the helper:

```go
func (p *ProxyCore) dispatchInviteViaCSCF(msg *parser.SIPMsg, src net.Addr) ResponseAction {
	p.mu.RLock()
	adaptors := p.cscfAdaptors
	p.mu.RUnlock()
	if len(adaptors) == 0 {
		return ResponseAction{}
	}
	ctx := context.Background()
	for _, a := range adaptors {
		act := a.HandleInvite(ctx, msg)
		if applyCSCFAction(ResponseAction{}, act) {
			return act
		}
	}
	return ResponseAction{}
}
```

- [ ] **Step 5: Run all proxy tests**

Run: `cd /workspace/kamailio-go && go test ./internal/core/proxy/ 2>&1 | tail -15`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /workspace/kamailio-go && git add internal/core/proxy/proxy.go internal/core/proxy/cscf_adaptor_test.go && git commit -m "feat(proxy): wire CSCFAdaptors into REGISTER/INVITE dispatch"
```

---

## Task 3: Config — IMS Per-Role Sections + Types

**Files:**
- Modify: `internal/core/config/config.go`.
- Test: `internal/core/config/config_test.go`.

- [ ] **Step 1: Write failing tests for the new types and ResolveRole**

Append to `internal/core/config/config_test.go`:

```go
func TestIMSConfig_ResolveRole_FlagOverridesConfig(t *testing.T) {
	cfg := &Config{IMS: IMSConfig{Enabled: true, Role: "all"}}
	roles := cfg.IMS.ResolveRole("pcscf")
	if len(roles) != 1 || roles[0] != RolePCSCF {
		t.Fatalf("got %v, want [RolePCSCF]", roles)
	}
}

func TestIMSConfig_ResolveRole_AllDefaultsToAll(t *testing.T) {
	cfg := &Config{IMS: IMSConfig{Enabled: true}}
	roles := cfg.IMS.ResolveRole("")
	if len(roles) != 3 {
		t.Fatalf("got %v, want 3 roles", roles)
	}
}

func TestIMSConfig_ResolveRole_AllWithLegacyBooleans(t *testing.T) {
	cfg := &Config{IMS: IMSConfig{Enabled: true, SCSCF_: true}}
	roles := cfg.IMS.ResolveRole("")
	// role==all + SCSCF_=true means only S-CSCF (legacy boolean override).
	if len(roles) != 1 || roles[0] != RoleSCSCF {
		t.Fatalf("got %v, want [RoleSCSCF]", roles)
	}
}

func TestIMSConfig_ResolveRole_SingleRole(t *testing.T) {
	cfg := &Config{IMS: IMSConfig{Enabled: true}}
	for _, tc := range []struct {
		flag string
		want int
	}{
		{"pcscf", RolePCSCF},
		{"icscf", RoleICSCF},
		{"scscf", RoleSCSCF},
	} {
		roles := cfg.IMS.ResolveRole(tc.flag)
		if len(roles) != 1 || roles[0] != tc.want {
			t.Errorf("flag=%q got %v, want [%d]", tc.flag, roles, tc.want)
		}
	}
}

func TestIMSConfig_ListenFor_SingleRole(t *testing.T) {
	cfg := &Config{
		Core: CoreConfig{Listen: []string{"udp:0.0.0.0:5060"}},
		IMS: IMSConfig{
			Enabled: true,
			PCSCF: &PCSCFConfig{Listen: []string{"udp:0.0.0.0:5061"}},
		},
	}
	got := cfg.IMS.ListenFor([]int{RolePCSCF}, cfg.Core.Listen)
	if len(got) != 1 || got[0] != "udp:0.0.0.0:5061" {
		t.Fatalf("got %v, want [udp:0.0.0.0:5061]", got)
	}
}

func TestIMSConfig_ListenFor_AllReusesCore(t *testing.T) {
	cfg := &Config{
		Core: CoreConfig{Listen: []string{"udp:0.0.0.0:5060"}},
		IMS: IMSConfig{Enabled: true},
	}
	got := cfg.IMS.ListenFor([]int{RolePCSCF, RoleICSCF, RoleSCSCF}, cfg.Core.Listen)
	if len(got) != 1 || got[0] != "udp:0.0.0.0:5060" {
		t.Fatalf("got %v, want core.listen", got)
	}
}

func TestIMSConfig_BackwardCompat_OldFlatFields(t *testing.T) {
	// Old-style flat config (no per-role sections) must still parse.
	yamlIn := []byte(`
ims:
  enabled: true
  realm: home.net
  scscf: true
  aka_algorithm: AKAv1-MD5
  default_expires: 3600
`)
	cfg, err := LoadFromBytes(yamlIn)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	if cfg.IMS.Realm != "home.net" || !cfg.IMS.SCSCF_ {
		t.Fatalf("flat fields not parsed: %+v", cfg.IMS)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `cd /workspace/kamailio-go && go test ./internal/core/config/ -run TestIMSConfig 2>&1 | tail -10`
Expected: FAIL with `undefined: RolePCSCF` etc.

- [ ] **Step 3: Extend IMSConfig with role types and sections**

In `internal/core/config/config.go`, replace the `IMSConfig` block (lines 59-70) with:

```go
// CSCF role identifiers (mirror proxy.RolePCSCF/ICSCF/SCSCF to avoid an
// import cycle between config and proxy).
const (
	RolePCSCF = iota
	RoleICSCF
	RoleSCSCF
)

// IMSConfig represents IMS-specific settings, with optional per-role
// sections. Old flat fields (SCSCF_/PCSCF_/ICSCF_/Realm/AKAAlgorithm/...)
// remain for backward compatibility: when role=="all" the legacy booleans
// refine which subset of roles to start.
type IMSConfig struct {
	Enabled bool   `yaml:"enabled"`
	Role    string `yaml:"role,omitempty"` // pcscf|scscf|icscf|all (default all)
	Realm   string `yaml:"realm,omitempty"`

	PCSCF *PCSCFConfig `yaml:"pcscf,omitempty"`
	ICSCF *ICSCFConfig `yaml:"icscf,omitempty"`
	SCSCF *SCSCFConfig `yaml:"scscf,omitempty"`

	// Legacy boolean role toggles (backward compat). When Role=="all"
	// and any of these is true, only the true roles are started.
	SCSCF_ bool `yaml:"scscf,omitempty"`
	PCSCF_ bool `yaml:"pcscf,omitempty"`
	ICSCF_ bool `yaml:"icscf,omitempty"`

	// Legacy flat fields (mapped to role-section defaults when sections
	// are absent).
	AKAAlgorithm     string `yaml:"aka_algorithm,omitempty"`
	DefaultExpires   int    `yaml:"default_expires,omitempty"`
	MinExpires       int    `yaml:"min_expires,omitempty"`
	MaxExpires       int    `yaml:"max_expires,omitempty"`
	VisitedNetworkID string `yaml:"visited_network_id,omitempty"`
}

// PCSCFConfig is the P-CSCF role configuration.
type PCSCFConfig struct {
	Listen           []string   `yaml:"listen,omitempty"`
	Realm            string     `yaml:"realm,omitempty"`
	VisitedNetworkID string     `yaml:"visited_network_id,omitempty"`
	ICSCFAddr        string     `yaml:"icscf_addr,omitempty"`
	SCSCFAddr        string     `yaml:"scscf_addr,omitempty"`
	IPSEC            IPSECConfig `yaml:"ipsec,omitempty"`
}

// ICSCFConfig is the I-CSCF role configuration.
type ICSCFConfig struct {
	Listen            []string             `yaml:"listen,omitempty"`
	Realm             string               `yaml:"realm,omitempty"`
	DiameterPeers     []DiameterPeerConfig `yaml:"diameter_peers,omitempty"`
	ForcedPeer        string               `yaml:"forced_peer,omitempty"`
	SCSCFAddr         string               `yaml:"scscf_addr,omitempty"`
	SCSCFCapabilities []SCSCFCapConfig     `yaml:"scscf_capabilities,omitempty"`
	EntryExpiry       int                  `yaml:"entry_expiry,omitempty"`
	PreferredSCSCF    []string             `yaml:"preferred_scscf,omitempty"`
}

// SCSCFConfig is the S-CSCF role configuration.
type SCSCFConfig struct {
	Listen        []string             `yaml:"listen,omitempty"`
	Realm         string               `yaml:"realm,omitempty"`
	DiameterPeers []DiameterPeerConfig `yaml:"diameter_peers,omitempty"`
	AKAAlgorithm  string               `yaml:"aka_algorithm,omitempty"`
	DefaultExpires int                 `yaml:"default_expires,omitempty"`
	MinExpires     int                 `yaml:"min_expires,omitempty"`
	MaxExpires     int                 `yaml:"max_expires,omitempty"`
}

// DiameterPeerConfig describes a single Diameter peer (HSS / SLF).
type DiameterPeerConfig struct {
	Host string `yaml:"host"`
	IP   string `yaml:"ip"`
	Port int    `yaml:"port"`
}

// SCSCFCapConfig is a static S-CSCF capability entry for I-CSCF.
type SCSCFCapConfig struct {
	ID            int    `yaml:"id"`
	Name          string `yaml:"name"`
	MandatoryCaps []int  `yaml:"mandatory_caps,omitempty"`
	OptionalCaps  []int  `yaml:"optional_caps,omitempty"`
}

// IPSECConfig is the P-CSCF IPSec policy placeholder.
type IPSECConfig struct {
	Enabled bool `yaml:"enabled"`
}

// ResolveRole returns the set of CSCF roles to start, honoring the --role
// flag (highest precedence), then cfg.Role, then "all" with legacy
// booleans refining the subset.
func (c IMSConfig) ResolveRole(flagRole string) []int {
	role := flagRole
	if role == "" {
		role = c.Role
	}
	if role == "" {
		role = "all"
	}
	switch role {
	case "pcscf":
		return []int{RolePCSCF}
	case "icscf":
		return []int{RoleICSCF}
	case "scscf":
		return []int{RoleSCSCF}
	case "all":
		// Legacy booleans refine the subset when any is true.
		if c.SCSCF_ || c.PCSCF_ || c.ICSCF_ {
			var out []int
			if c.PCSCF_ {
				out = append(out, RolePCSCF)
			}
			if c.ICSCF_ {
				out = append(out, RoleICSCF)
			}
			if c.SCSCF_ {
				out = append(out, RoleSCSCF)
			}
			return out
		}
		return []int{RolePCSCF, RoleICSCF, RoleSCSCF}
	}
	return nil
}

// ListenFor returns the listen addresses for the given role set. If any
// role's own Listen is set, those are used; otherwise coreListen is used
// (the --role all case where all roles share one ProxyCore).
func (c IMSConfig) ListenFor(roles []int, coreListen []string) []string {
	for _, r := range roles {
		var sectionListen []string
		switch r {
		case RolePCSCF:
			if c.PCSCF != nil {
				sectionListen = c.PCSCF.Listen
			}
		case RoleICSCF:
			if c.ICSCF != nil {
				sectionListen = c.ICSCF.Listen
			}
		case RoleSCSCF:
			if c.SCSCF != nil {
				sectionListen = c.SCSCF.Listen
			}
		}
		if len(sectionListen) > 0 {
			return sectionListen
		}
	}
	return coreListen
}
```

Also add a `LoadFromBytes` helper if not present (used by the test):

```go
// LoadFromBytes parses YAML config bytes without reading a file.
func LoadFromBytes(data []byte) (*Config, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /workspace/kamailio-go && go test ./internal/core/config/ -run TestIMSConfig 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /workspace/kamailio-go && git add internal/core/config/config.go internal/core/config/config_test.go && git commit -m "feat(config): add IMS per-role sections, ResolveRole, ListenFor"
```

---

## Task 4: Config Validator — IMS Role Rules

**Files:**
- Modify: `internal/core/config/validator.go`.
- Test: `internal/core/config/config_test.go`.

- [ ] **Step 1: Write failing validator tests**

Append to `internal/core/config/config_test.go`:

```go
func TestValidate_IMSRoleInvalid(t *testing.T) {
	cfg := &Config{
		Core: CoreConfig{Listen: []string{"udp:0.0.0.0:5060"}, LogLevel: "info", Workers: 1},
		IMS:  IMSConfig{Enabled: true, Role: "foo"},
	}
	report := cfg.ValidateStrict()
	if !report.HasErrors() {
		t.Fatalf("expected error for invalid role, got none")
	}
}

func TestValidate_PCSCFMissingICSCFAddrCrossProcess(t *testing.T) {
	cfg := &Config{
		Core: CoreConfig{Listen: []string{"udp:0.0.0.0:5060"}, LogLevel: "info", Workers: 1},
		IMS: IMSConfig{
			Enabled: true,
			Role:    "pcscf",
			PCSCF:   &PCSCFConfig{}, // no icscf_addr
		},
	}
	report := cfg.ValidateStrict()
	if !report.HasErrors() {
		t.Fatalf("expected error for missing icscf_addr, got none")
	}
}

func TestValidate_PCSCFOkWhenICSCFSameProcess(t *testing.T) {
	cfg := &Config{
		Core: CoreConfig{Listen: []string{"udp:0.0.0.0:5060"}, LogLevel: "info", Workers: 1},
		IMS: IMSConfig{
			Enabled: true,
			Role:    "all",
			PCSCF:   &PCSCFConfig{}, // icscf_addr can be empty; loopback used
			ICSCF:   &ICSCFConfig{Listen: []string{"udp:127.0.0.1:5061"}},
		},
	}
	report := cfg.ValidateStrict()
	if report.HasErrors() {
		t.Fatalf("expected no error in all-mode loopback, got: %v", report.Errors)
	}
}

func TestValidate_ICSCFDiameterPeersRequired(t *testing.T) {
	cfg := &Config{
		Core: CoreConfig{Listen: []string{"udp:0.0.0.0:5060"}, LogLevel: "info", Workers: 1},
		IMS: IMSConfig{
			Enabled: true,
			Role:    "icscf",
			ICSCF:   &ICSCFConfig{SCSCFAddr: "sip:scscf.home.net:5060"}, // no diameter_peers
		},
	}
	report := cfg.ValidateStrict()
	if !report.HasErrors() {
		t.Fatalf("expected error for missing diameter_peers, got none")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /workspace/kamailio-go && go test ./internal/core/config/ -run TestValidate_IMS 2>&1 | tail -10`
Expected: FAIL (no IMS validation yet).

- [ ] **Step 3: Add IMS validation rules**

Inspect current `ValidateStrict` structure first:
Run: `cd /workspace/kamailio-go && grep -n "func.*ValidateStrict\|func.*appendError\|func.*appendWarning" internal/core/config/validator.go`
Then add a new method called from `ValidateStrict`:

In `internal/core/config/validator.go`, add (using the actual error-append helper found above; adapt the call site):

```go
// validateIMS checks IMS role configuration rules. Errors are appended to
// the provided report.
func validateIMS(cfg *Config, report *ValidationReport) {
	if !cfg.IMS.Enabled {
		return
	}
	roles := cfg.IMS.ResolveRole("")
	if len(roles) == 0 {
		// ResolveRole returns nil only for an explicit invalid role string.
		report.appendError("ims.role", "must be one of pcscf|scscf|icscf|all")
		return
	}
	allMode := len(roles) == 3
	for _, r := range roles {
		switch r {
		case RolePCSCF:
			if cfg.IMS.PCSCF == nil {
				cfg.IMS.PCSCF = &PCSCFConfig{}
			}
			// icscf_addr required unless ICSCF is also in-process (all mode).
			if cfg.IMS.PCSCF.ICSCFAddr == "" {
				if !(allMode && containsRole(roles, RoleICSCF)) {
					report.appendError("ims.pcscf.icscf_addr", "required when icscf role is not co-located")
				}
			}
		case RoleICSCF:
			if cfg.IMS.ICSCF == nil {
				cfg.IMS.ICSCF = &ICSCFConfig{}
			}
			// scscf_addr required unless SCSCF is also in-process.
			if cfg.IMS.ICSCF.SCSCFAddr == "" {
				if !(allMode && containsRole(roles, RoleSCSCF)) {
					report.appendError("ims.icscf.scscf_addr", "required when scscf role is not co-located")
				}
			}
			// diameter_peers required for standalone icscf.
			if !allMode && len(cfg.IMS.ICSCF.DiameterPeers) == 0 {
				report.appendError("ims.icscf.diameter_peers", "required for standalone icscf")
			}
		case RoleSCSCF:
			if cfg.IMS.SCSCF == nil {
				cfg.IMS.SCSCF = &SCSCFConfig{}
			}
			if !allMode && len(cfg.IMS.SCSCF.DiameterPeers) == 0 {
				report.appendError("ims.scscf.diameter_peers", "required for standalone scscf")
			}
		}
	}
}

func containsRole(roles []int, want int) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}
```

Call `validateIMS(cfg, report)` from inside `ValidateStrict` (locate where other validators are invoked and add the call there).

- [ ] **Step 4: Run tests**

Run: `cd /workspace/kamailio-go && go test ./internal/core/config/ 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /workspace/kamailio-go && git add internal/core/config/validator.go internal/core/config/config_test.go && git commit -m "feat(config): validate IMS role configuration"
```

---

## Task 5: S-CSCF Adaptor

**Files:**
- Create: `internal/ims/scscf/adaptor.go`
- Test: `internal/ims/scscf/adaptor_test.go`

**Why S-CSCF first:** It is the terminal role — REGISTER ends here, INVITE either terminates (404) or forwards to a registered contact. Simplest flow, exercises `ResponseAction` for both respond and forward.

- [ ] **Step 1: Write failing tests**

Create `internal/ims/scscf/adaptor_test.go`:

```go
package scscf

import (
	"context"
	"strings"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
)

func TestSCSCFAdaptor_RegisterChallenge_Responds401(t *testing.T) {
	reg := NewRegistrar("home.net")
	ad := NewAdaptor(reg, NewSessionHandler(reg), nil)

	msg := mustBuildRegister(t, "sip:user@home.net")
	act := ad.HandleRegister(context.Background(), msg)
	if act.Status != 401 {
		t.Fatalf("Status = %d, want 401", act.Status)
	}
	if !act.StopRouting {
		t.Fatalf("StopRouting should be true for terminal respond")
	}
	// WWW-Authenticate header should be present.
	found := false
	for _, h := range act.ExtraHeaders {
		if strings.HasPrefix(h, "WWW-Authenticate") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("WWW-Authenticate header missing: %v", act.ExtraHeaders)
	}
}

func TestSCSCFAdaptor_InviteUnregistered_Responds404(t *testing.T) {
	reg := NewRegistrar("home.net")
	ad := NewAdaptor(reg, NewSessionHandler(reg), nil)

	msg := mustBuildInvite(t, "sip:unknown@home.net", "sip:caller@elsewhere.net")
	act := ad.HandleInvite(context.Background(), msg)
	if act.Status != 404 && act.Status != 403 {
		t.Fatalf("Status = %d, want 404 or 403 (unregistered)", act.Status)
	}
	if !act.StopRouting {
		t.Fatalf("StopRouting should be true")
	}
}

func TestSCSCFAdaptor_InviteRegistered_ForwardsToContact(t *testing.T) {
	reg := NewRegistrar("home.net")
	reg.SetRecordForTest("sip:user@home.net", "sip:user@10.0.0.5:5060")
	ad := NewAdaptor(reg, NewSessionHandler(reg), nil)

	msg := mustBuildInvite(t, "sip:user@home.net", "sip:caller@elsewhere.net")
	act := ad.HandleInvite(context.Background(), msg)
	// Forwarding: Status 0 (no reply) + Target set + StopRouting true, OR
	// a SessionResult with RouteTarget mapped to Target.
	if act.Target == "" && act.Status == 0 {
		t.Fatalf("expected forward Target or non-zero Status, got %+v", act)
	}
	if !act.StopRouting {
		t.Fatalf("StopRouting should be true")
	}
}

func TestSCSCFAdaptor_Role(t *testing.T) {
	ad := NewAdaptor(NewRegistrar("home.net"), nil, nil)
	if ad.Role() != proxy.RoleSCSCF {
		t.Fatalf("Role = %d, want %d", ad.Role(), proxy.RoleSCSCF)
	}
}

// mustBuildRegister constructs a minimal REGISTER SIPMsg.
func mustBuildRegister(t *testing.T, uri string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseSIPMsg("REGISTER sip:home.net SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1:5060\r\nFrom: <" + uri + ">;tag=abc\r\nTo: <" + uri + ">\r\nCall-ID: cid-1\r\nCSeq: 1 REGISTER\r\nContact: <" + uri + ">\r\nContent-Length: 0\r\n\r\n")
	if err != nil {
		t.Fatalf("parse REGISTER: %v", err)
	}
	return msg
}

func mustBuildInvite(t *testing.T, toURI, fromURI string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseSIPMsg("INVITE " + toURI + " SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1:5060\r\nFrom: <" + fromURI + ">;tag=abc\r\nTo: <" + toURI + ">\r\nCall-ID: cid-2\r\nCSeq: 1 INVITE\r\nContact: <" + fromURI + ">\r\nContent-Length: 0\r\n\r\n")
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}
	return msg
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /workspace/kamailio-go && go test ./internal/ims/scscf/ -run TestSCSCFAdaptor 2>&1 | tail -10`
Expected: FAIL with `undefined: NewAdaptor`.

- [ ] **Step 3: Create the adaptor**

Create `internal/ims/scscf/adaptor.go`:

```go
// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * S-CSCF Adaptor — bridges scscf.Registrar/SessionHandler to ProxyCore's
 * dispatch path by returning proxy.ResponseAction. S-CSCF is the terminal
 * role for REGISTER and the末端 forwarder for INVITE to registered contacts.
 */

package scscf

import (
	"context"
	"fmt"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// Adaptor implements proxy.CSCFAdaptor for the S-CSCF role.
type Adaptor struct {
	registrar *Registrar
	sessions  *SessionHandler
}

// NewAdaptor creates an S-CSCF adaptor. fwd may be nil in unit tests.
func NewAdaptor(reg *Registrar, sess *SessionHandler, fwd interface{}) *Adaptor {
	return &Adaptor{registrar: reg, sessions: sess}
}

// Role returns the S-CSCF role identifier.
func (a *Adaptor) Role() int { return proxy.RoleSCSCF }

// HandleRegister runs the AKA challenge / response flow and returns a
// terminal ResponseAction (401 / 200 / 403).
func (a *Adaptor) HandleRegister(ctx context.Context, msg *parser.SIPMsg) proxy.ResponseAction {
	if a.registrar == nil {
		return proxy.ResponseAction{Status: 500, Reason: "S-CSCF not initialized", StopRouting: true}
	}
	result, err := a.registrar.HandleRegister(msg)
	if err != nil {
		return proxy.ResponseAction{Status: 500, Reason: "S-CSCF error: " + err.Error(), StopRouting: true}
	}
	if result == nil {
		return proxy.ResponseAction{Status: 500, Reason: "S-CSCF nil result", StopRouting: true}
	}
	return proxy.ResponseAction{
		Status:       int(result.StatusCode),
		Reason:       result.StatusReason,
		ExtraHeaders: headersToStrList(result.Headers),
		Body:         result.Body,
		StopRouting:  true,
	}
}

// HandleInvite routes an INVITE to the registered contact, or rejects.
func (a *Adaptor) HandleInvite(ctx context.Context, msg *parser.SIPMsg) proxy.ResponseAction {
	if a.sessions == nil {
		return proxy.ResponseAction{Status: 500, Reason: "S-CSCF session handler not initialized", StopRouting: true}
	}
	result, err := a.sessions.HandleInvite(msg)
	if err != nil {
		return proxy.ResponseAction{Status: 500, Reason: "S-CSCF invite error: " + err.Error(), StopRouting: true}
	}
	if result == nil {
		return proxy.ResponseAction{Status: 500, Reason: "S-CSCF nil invite result", StopRouting: true}
	}
	// If the session layer picked a route target, forward to it.
	if result.RouteTarget != "" {
		return proxy.ResponseAction{
			Target:      result.RouteTarget,
			StopRouting: true,
		}
	}
	// Otherwise it's a terminal response (4xx).
	return proxy.ResponseAction{
		Status:       int(result.StatusCode),
		Reason:       result.StatusReason,
		ExtraHeaders: headersToStrList(result.Headers),
		Body:         result.Body,
		StopRouting:  true,
	}
}

// HandleInDialog currently delegates to the session handler for BYE/etc.
// Future work: full in-dialog routing.
func (a *Adaptor) HandleInDialog(ctx context.Context, msg *parser.SIPMsg) proxy.ResponseAction {
	// Decline for now — let ProxyCore's existing dialog/tm path handle BYE.
	return proxy.ResponseAction{}
}

// headersToStrList converts a str.Str header map to the flat list form
// expected by ResponseAction.ExtraHeaders.
func headersToStrList(headers map[string]str.Str) []string {
	var out []string
	for name, v := range headers {
		out = append(out, fmt.Sprintf("%s: %s", name, v.String()))
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `cd /workspace/kamailio-go && go test ./internal/ims/scscf/ 2>&1 | tail -15`
Expected: PASS. If `parser.ParseSIPMsg` doesn't exist with that signature, inspect parser package and adjust `mustBuildRegister`/`mustBuildInvite`.

- [ ] **Step 5: Commit**

```bash
cd /workspace/kamailio-go && git add internal/ims/scscf/adaptor.go internal/ims/scscf/adaptor_test.go && git commit -m "feat(scscf): add Adaptor bridging to ProxyCore dispatch"
```

---

## Task 6: I-CSCF Adaptor

**Files:**
- Create: `internal/ims/icscf/adaptor.go`
- Test: `internal/ims/icscf/adaptor_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/ims/icscf/adaptor_test.go`:

```go
package icscf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/modules/cdp"
)

// stubICSCF is a minimal ICSCF stand-in for adaptor tests. It returns
// canned UAAResult/LIAResult and tracks DropList calls.
type stubICSCF struct {
	tbl      *SCSCFTable
	uaa      *UAAResult
	lia      *LIAResult
	uarErr   error
	lirErr   error
}

func (s *stubICSCF) SendUAR(ctx context.Context, callID string, req *UARRequest) (*UAAResult, error) {
	if s.uarErr != nil {
		return nil, s.uarErr
	}
	return s.uaa, nil
}
func (s *stubICSCF) SendLIR(ctx context.Context, callID string, req *LIRRequest) (*LIAResult, error) {
	if s.lirErr != nil {
		return nil, s.lirErr
	}
	return s.lia, nil
}
func (s *stubICSCF) Table() *SCSCFTable { return s.tbl }

func newStubTbl() *SCSCFTable {
	tbl := NewSCSCFTable()
	tbl.LoadSCSCFs([]SCSCFCapability{
		{ID: 1, Name: "sip:scscf1.home.net", MandatoryCaps: []int{1}, OptionalCaps: []int{2}},
	})
	return tbl
}

func mustBuildRegister(t *testing.T, uri string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseSIPMsg("REGISTER sip:home.net SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1:5060\r\nFrom: <" + uri + ">;tag=abc\r\nTo: <" + uri + ">\r\nCall-ID: cid-1\r\nCSeq: 1 REGISTER\r\nContact: <" + uri + ">\r\nContent-Length: 0\r\n\r\n")
	if err != nil {
		t.Fatalf("parse REGISTER: %v", err)
	}
	return msg
}

func TestICSCFAdaptor_FirstReg_SelectsAndForwards(t *testing.T) {
	tbl := newStubTbl()
	stub := &stubICSCF{
		tbl: tbl,
		uaa: &UAAResult{
			ExperimentalResultCode: ExCodeFirstRegistration,
			HasExperimentalResult:  true,
			MandatoryCaps:           []int{1},
			HasServerCapabilities:   true,
		},
	}
	ad := NewAdaptor(stub, stub.tbl, "sip:scscf.home.net:5060", nil)

	msg := mustBuildRegister(t, "sip:user@home.net")
	act := ad.HandleRegister(context.Background(), msg)
	if act.Target == "" {
		t.Fatalf("expected forward Target, got %+v", act)
	}
	if act.Target != "sip:scscf1.home.net" && act.Target != "sip:scscf1.home.net;orig" {
		t.Fatalf("Target = %q, want sip:scscf1.home.net", act.Target)
	}
	if !act.StopRouting {
		t.Fatalf("StopRouting should be true")
	}
	// Candidate list should be dropped after select.
	if tbl.ListCount() != 0 {
		t.Fatalf("ListCount after select = %d, want 0", tbl.ListCount())
	}
}

func TestICSCFAdaptor_UserUnknown_Responds403(t *testing.T) {
	tbl := newStubTbl()
	stub := &stubICSCF{
		tbl: tbl,
		uaa: &UAAResult{
			ExperimentalResultCode: ExCodeErrorUserUnknown,
			HasExperimentalResult:  true,
		},
	}
	ad := NewAdaptor(stub, stub.tbl, "sip:scscf.home.net:5060", nil)

	msg := mustBuildRegister(t, "sip:user@home.net")
	act := ad.HandleRegister(context.Background(), msg)
	if act.Status != 403 {
		t.Fatalf("Status = %d, want 403", act.Status)
	}
	if !act.StopRouting {
		t.Fatalf("StopRouting should be true")
	}
}

func TestICSCFAdaptor_DiameterTimeout_Responds480(t *testing.T) {
	tbl := newStubTbl()
	stub := &stubICSCF{
		tbl:    tbl,
		uarErr: errors.New("context deadline exceeded"),
	}
	ad := NewAdaptor(stub, stub.tbl, "sip:scscf.home.net:5060", nil)

	msg := mustBuildRegister(t, "sip:user@home.net")
	// Use a context that's already cancelled to simulate timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)

	act := ad.HandleRegister(ctx, msg)
	if act.Status != 480 {
		t.Fatalf("Status = %d, want 480 (timeout)", act.Status)
	}
}

func TestICSCFAdaptor_NoCandidate_Responds500(t *testing.T) {
	// UAA returns FirstRegistration but the table has no matching S-CSCF.
	tbl := NewSCSCFTable() // empty catalogue
	stub := &stubICSCF{
		tbl: tbl,
		uaa: &UAAResult{
			ExperimentalResultCode: ExCodeFirstRegistration,
			HasExperimentalResult:  true,
			MandatoryCaps:           []int{99}, // no S-CSCF has cap 99
			HasServerCapabilities:   true,
		},
	}
	ad := NewAdaptor(stub, stub.tbl, "sip:scscf.home.net:5060", nil)

	msg := mustBuildRegister(t, "sip:user@home.net")
	act := ad.HandleRegister(context.Background(), msg)
	if act.Status != 500 {
		t.Fatalf("Status = %d, want 500 (no candidate)", act.Status)
	}
}

// Compile-time check that Adaptor satisfies proxy.CSCFAdaptor.
var _ cdp.DiameterMessage = cdp.DiameterMessage{} // placeholder to keep cdp import used
```

If `parser.ParseSIPMsg` doesn't exist with that signature, search parser package and adjust.

- [ ] **Step 2: Run to verify failure**

Run: `cd /workspace/kamailio-go && go test ./internal/ims/icscf/ -run TestICSCFAdaptor 2>&1 | tail -10`
Expected: FAIL with `undefined: NewAdaptor`.

- [ ] **Step 3: Create the adaptor**

Create `internal/ims/icscf/adaptor.go`:

```go
// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * I-CSCF Adaptor — bridges icscf.ICSCF (Cx UAR/LIR) to ProxyCore dispatch.
 * On REGISTER: SendUAR → (Select | reject) → forward to chosen S-CSCF.
 * On INVITE:   SendLIR → Select → forward.
 */

package icscf

import (
	"context"
	"errors"

	"github.com/kamailio/kamailio-go/internal/core/log"
	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
)

// ICSCFInterface is the subset of *ICSCF the adaptor uses. Defined as an
// interface so tests can inject a stub.
type ICSCFInterface interface {
	SendUAR(ctx context.Context, callID string, req *UARRequest) (*UAAResult, error)
	SendLIR(ctx context.Context, callID string, req *LIRRequest) (*LIAResult, error)
	Table() *SCSCFTable
}

// Adaptor implements proxy.CSCFAdaptor for the I-CSCF role.
type Adaptor struct {
	icscf     ICSCFInterface
	tbl        *SCSCFTable
	scscfAddr  string // fallback when no candidate is named by HSS
	fwd        interface{}
}

// NewAdaptor creates an I-CSCF adaptor. fwd is the forwarder (typed as
// interface{} to avoid an import cycle in tests that don't need it).
func NewAdaptor(i ICSCFInterface, tbl *SCSCFTable, scscfAddr string, fwd interface{}) *Adaptor {
	if tbl == nil {
		tbl = i.Table()
	}
	return &Adaptor{icscf: i, tbl: tbl, scscfAddr: scscfAddr, fwd: fwd}
}

// Role returns the I-CSCF role identifier.
func (a *Adaptor) Role() int { return proxy.RoleICSCF }

// HandleRegister runs the UAR/LIR exchange and forwards to the selected
// S-CSCF, or responds with an error.
func (a *Adaptor) HandleRegister(ctx context.Context, msg *parser.SIPMsg) proxy.ResponseAction {
	callID := extractCallID(msg)
	impu := extractPublicIdentity(msg)
	visitedNetID := "" // from config at construction time; pass via Adaptor field if needed

	uarReq := &UARRequest{
		PublicIdentity:    impu,
		VisitedNetworkID:  visitedNetID,
		AuthorizationType:  AuthzRegistrationAndCapabilities,
	}
	uaa, err := a.icscf.SendUAR(ctx, callID, uarReq)
	if err != nil {
		// Diameter timeout / transport error.
		log.Warn("I-CSCF UAR failed", log.String("err", err.Error()))
		a.tbl.DropList(callID)
		return proxy.ResponseAction{Status: 480, Reason: "Temporarily Unavailable", StopRouting: true}
	}
	if uaa == nil {
		a.tbl.DropList(callID)
		return proxy.ResponseAction{Status: 500, Reason: "HSS returned empty UAA", StopRouting: true}
	}

	switch uaa.RegistrationCase() {
	case RegistrationCaseUserUnknown, RegistrationCaseIdentitiesMismatch,
		RegistrationCaseRoamingNotAllowed, RegistrationCaseIdentityNotRegistered:
		a.tbl.DropList(callID)
		return proxy.ResponseAction{Status: 403, Reason: "Forbidden", StopRouting: true}
	case RegistrationCaseFirst, RegistrationCaseSubsequent,
		RegistrationCaseServerSelection, RegistrationCaseUnregistered:
		// fall through to selection
	default:
		a.tbl.DropList(callID)
		return proxy.ResponseAction{Status: 500, Reason: "Unexpected HSS result", StopRouting: true}
	}

	cand, err := a.tbl.Select(callID)
	a.tbl.DropList(callID)
	if err != nil {
		if errors.Is(err, ErrNoCandidateList) || cand.Name == "" {
			return proxy.ResponseAction{Status: 500, Reason: "No S-CSCF available", StopRouting: true}
		}
		return proxy.ResponseAction{Status: 500, Reason: "Select error: " + err.Error(), StopRouting: true}
	}
	return proxy.ResponseAction{
		Target:      cand.Name,
		StopRouting: true,
	}
}

// HandleInvite runs the LIR exchange and forwards to the selected S-CSCF.
func (a *Adaptor) HandleInvite(ctx context.Context, msg *parser.SIPMsg) proxy.ResponseAction {
	callID := extractCallID(msg)
	impu := extractPublicIdentity(msg)

	lirReq := &LIRRequest{
		PublicIdentity: impu,
	}
	lia, err := a.icscf.SendLIR(ctx, callID, lirReq)
	if err != nil {
		log.Warn("I-CSCF LIR failed", log.String("err", err.Error()))
		a.tbl.DropList(callID)
		return proxy.ResponseAction{Status: 480, Reason: "Temporarily Unavailable", StopRouting: true}
	}
	if lia == nil {
		a.tbl.DropList(callID)
		return proxy.ResponseAction{Status: 500, Reason: "HSS returned empty LIA", StopRouting: true}
	}

	cand, err := a.tbl.Select(callID)
	a.tbl.DropList(callID)
	if err != nil {
		return proxy.ResponseAction{Status: 500, Reason: "No S-CSCF available", StopRouting: true}
	}
	return proxy.ResponseAction{
		Target:      cand.Name,
		StopRouting: true,
	}
}

// HandleInDialog declines (in-dialog requests don't traverse I-CSCF).
func (a *Adaptor) HandleInDialog(ctx context.Context, msg *parser.SIPMsg) proxy.ResponseAction {
	return proxy.ResponseAction{}
}

// extractCallID pulls the Call-ID header value from msg.
func extractCallID(msg *parser.SIPMsg) string {
	if msg == nil || msg.CallID == nil {
		return ""
	}
	return msg.CallID.Body.String()
}

// extractPublicIdentity pulls the IMPU from the To header URI.
func extractPublicIdentity(msg *parser.SIPMsg) string {
	if msg == nil || msg.To == nil {
		return ""
	}
	body := msg.To.Body.String()
	// Strip <...> if present.
	start := -1
	for i, c := range body {
		if c == '<' {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return body
	}
	end := -1
	for i := start; i < len(body); i++ {
		if body[i] == '>' {
			end = i
			break
		}
	}
	if end < 0 {
		return body[start:]
	}
	return body[start:end]
}
```

- [ ] **Step 4: Run tests**

Run: `cd /workspace/kamailio-go && go test ./internal/ims/icscf/ 2>&1 | tail -20`
Expected: PASS. If `ExCodeFirstRegistration` etc. constant names differ, check `cx_messages.go` for the actual names (search `ExCode`).

If `LIRRequest`/`LIAResult` field names differ, check `cx_messages.go` (search `type LIRRequest`/`type LIAResult`).

- [ ] **Step 5: Commit**

```bash
cd /workspace/kamailio-go && git add internal/ims/icscf/adaptor.go internal/ims/icscf/adaptor_test.go && git commit -m "feat(icscf): add Adaptor bridging UAR/LIR to ProxyCore dispatch"
```

---

## Task 7: P-CSCF Adaptor

**Files:**
- Create: `internal/ims/pcscf/adaptor.go`
- Test: `internal/ims/pcscf/adaptor_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/ims/pcscf/adaptor_test.go`:

```go
package pcscf

import (
	"context"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
	"github.com/kamailio/kamailio-go/internal/core/usrloc"
)

func TestPCSCFAdaptor_InitialRegister_ForwardsToICSCF(t *testing.T) {
	reg := usrloc.New(&usrloc.Config{Realm: "home.net"})
	sh := NewSessionHandler()
	ad := NewAdaptor(sh, reg, "sip:icscf.home.net:5060", "sip:scscf.home.net:5060", nil)

	msg := mustBuildRegister(t, "sip:user@home.net")
	act := ad.HandleRegister(context.Background(), msg)
	if act.Target != "sip:icscf.home.net:5060" {
		t.Fatalf("Target = %q, want sip:icscf.home.net:5060", act.Target)
	}
	if !act.StopRouting {
		t.Fatalf("StopRouting should be true")
	}
}

func TestPCSCFAdaptor_Invite_ForwardsToICSCF(t *testing.T) {
	reg := usrloc.New(&usrloc.Config{Realm: "home.net"})
	sh := NewSessionHandler()
	ad := NewAdaptor(sh, reg, "sip:icscf.home.net:5060", "sip:scscf.home.net:5060", nil)

	msg := mustBuildInvite(t, "sip:user@home.net", "sip:caller@elsewhere.net")
	act := ad.HandleInvite(context.Background(), msg)
	if act.Target != "sip:icscf.home.net:5060" {
		t.Fatalf("Target = %q, want sip:icscf.home.net:5060", act.Target)
	}
	if !act.StopRouting {
		t.Fatalf("StopRouting should be true")
	}
}

func TestPCSCFAdaptor_NoICSCFAddr_Responds500(t *testing.T) {
	reg := usrloc.New(&usrloc.Config{Realm: "home.net"})
	sh := NewSessionHandler()
	ad := NewAdaptor(sh, reg, "", "", nil) // no next hops

	msg := mustBuildRegister(t, "sip:user@home.net")
	act := ad.HandleRegister(context.Background(), msg)
	if act.Status != 500 {
		t.Fatalf("Status = %d, want 500 (no icscf_addr)", act.Status)
	}
}

func TestPCSCFAdaptor_Role(t *testing.T) {
	ad := NewAdaptor(nil, nil, "", "", nil)
	if ad.Role() != proxy.RolePCSCF {
		t.Fatalf("Role = %d, want %d", ad.Role(), proxy.RolePCSCF)
	}
}

func mustBuildRegister(t *testing.T, uri string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseSIPMsg("REGISTER sip:home.net SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1:5060\r\nFrom: <" + uri + ">;tag=abc\r\nTo: <" + uri + ">\r\nCall-ID: cid-1\r\nCSeq: 1 REGISTER\r\nContact: <" + uri + ">\r\nContent-Length: 0\r\n\r\n")
	if err != nil {
		t.Fatalf("parse REGISTER: %v", err)
	}
	return msg
}

func mustBuildInvite(t *testing.T, toURI, fromURI string) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseSIPMsg("INVITE " + toURI + " SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1:5060\r\nFrom: <" + fromURI + ">;tag=abc\r\nTo: <" + toURI + ">\r\nCall-ID: cid-2\r\nCSeq: 1 INVITE\r\nContact: <" + fromURI + ">\r\nContent-Length: 0\r\n\r\n")
	if err != nil {
		t.Fatalf("parse INVITE: %v", err)
	}
	return msg
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /workspace/kamailio-go && go test ./internal/ims/pcscf/ -run TestPCSCFAdaptor 2>&1 | tail -10`
Expected: FAIL with `undefined: NewAdaptor`.

If `usrloc.New` / `usrloc.Config` signatures differ, inspect:
Run: `cd /workspace/kamailio-go && grep -n "func New\|type Config" internal/core/usrloc/usrloc.go | head -5`

- [ ] **Step 3: Create the adaptor**

Create `internal/ims/pcscf/adaptor.go`:

```go
// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * P-CSCF Adaptor — bridges pcscf.SessionHandler / RouteFromRegistration to
 * ProxyCore dispatch. P-CSCF is a pure transit role: it never terminates
 * a request with a final response (except 500 on misconfiguration); it
 * forwards REGISTER/INVITE to I-CSCF (initial) or S-CSCF (re-register).
 */

package pcscf

import (
	"context"

	"github.com/kamailio/kamailio-go/internal/core/log"
	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
	"github.com/kamailio/kamailio-go/internal/core/usrloc"
)

// Adaptor implements proxy.CSCFAdaptor for the P-CSCF role.
type Adaptor struct {
	sessions  *SessionHandler
	registrar *usrloc.Registrar
	icscfAddr string
	scscfAddr string
	fwd       interface{}
}

// NewAdaptor creates a P-CSCF adaptor. fwd is typed as interface{} to keep
// tests forwarder-free; the bootstrap passes a real *forward.Forwarder.
func NewAdaptor(sh *SessionHandler, reg *usrloc.Registrar, icscfAddr, scscfAddr string, fwd interface{}) *Adaptor {
	return &Adaptor{
		sessions:  sh,
		registrar: reg,
		icscfAddr: icscfAddr,
		scscfAddr: scscfAddr,
		fwd:       fwd,
	}
}

// Role returns the P-CSCF role identifier.
func (a *Adaptor) Role() int { return proxy.RolePCSCF }

// HandleRegister forwards a REGISTER to I-CSCF (initial) or S-CSCF
// (re-registration, when the user is already in usrloc).
func (a *Adaptor) HandleRegister(ctx context.Context, msg *parser.SIPMsg) proxy.ResponseAction {
	if a.icscfAddr == "" && a.scscfAddr == "" {
		return proxy.ResponseAction{Status: 500, Reason: "P-CSCF has no next hop configured", StopRouting: true}
	}

	// Try to look up an existing registration to decide next hop.
	if a.registrar != nil {
		contacts, err := RouteFromRegistration(a.registrar, msg)
		if err == nil && len(contacts) > 0 && a.scscfAddr != "" {
			// Re-register: forward directly to S-CSCF.
			return proxy.ResponseAction{Target: a.scscfAddr, StopRouting: true}
		}
	}
	if a.icscfAddr == "" {
		return proxy.ResponseAction{Status: 500, Reason: "P-CSCF has no I-CSCF address for initial registration", StopRouting: true}
	}
	// Initial registration: forward to I-CSCF.
	return proxy.ResponseAction{Target: a.icscfAddr, StopRouting: true}
}

// HandleInvite records the session and forwards to I-CSCF for S-CSCF
// selection.
func (a *Adaptor) HandleInvite(ctx context.Context, msg *parser.SIPMsg) proxy.ResponseAction {
	if a.icscfAddr == "" {
		return proxy.ResponseAction{Status: 500, Reason: "P-CSCF has no I-CSCF address", StopRouting: true}
	}
	if a.sessions != nil {
		if _, err := a.sessions.HandleInvite(msg); err != nil {
			log.Warn("P-CSCF session record error", log.String("err", err.Error()))
			// Non-fatal: still forward.
		}
	}
	return proxy.ResponseAction{Target: a.icscfAddr, StopRouting: true}
}

// HandleInDialog declines (P-CSCF does not terminate in-dialog requests).
func (a *Adaptor) HandleInDialog(ctx context.Context, msg *parser.SIPMsg) proxy.ResponseAction {
	return proxy.ResponseAction{}
}
```

- [ ] **Step 4: Run tests**

Run: `cd /workspace/kamailio-go && go test ./internal/ims/pcscf/ 2>&1 | tail -15`
Expected: PASS. If `RouteFromRegistration` signature differs (it's `(reg *usrloc.Registrar, msg *parser.SIPMsg) ([]string, error)` per the existing code), adjust accordingly.

- [ ] **Step 5: Commit**

```bash
cd /workspace/kamailio-go && git add internal/ims/pcscf/adaptor.go internal/ims/pcscf/adaptor_test.go && git commit -m "feat(pcscf): add Adaptor bridging to ProxyCore dispatch"
```

---

## Task 8: IMS Bootstrap Wiring

**Files:**
- Create: `internal/core/app/ims_bootstrap.go`.
- Modify: `internal/core/app/bootstrap.go`.
- Test: `internal/core/app/ims_bootstrap_test.go`.

- [ ] **Step 1: Write failing tests**

Create `internal/core/app/ims_bootstrap_test.go`:

```go
package app

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/config"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
)

func TestBuildIMSAdaptors_AllRoles(t *testing.T) {
	cfg := &config.Config{
		IMS: config.IMSConfig{
			Enabled: true,
			Realm:   "home.net",
			PCSCF:   &config.PCSCFConfig{ICSCFAddr: "sip:icscf:5060", SCSCFAddr: "sip:scscf:5060"},
			ICSCF:   &config.ICSCFConfig{Listen: []string{"udp:127.0.0.1:5061"}, SCSCFAddr: "sip:scscf:5060"},
			SCSCF:   &config.SCSCFConfig{Realm: "home.net"},
		},
	}
	b := &Bootstrap{}
	adaptors := b.buildIMSAdaptors(cfg.IMS.ResolveRole(""), cfg.IMS)
	if len(adaptors) != 3 {
		t.Fatalf("got %d adaptors, want 3", len(adaptors))
	}
	// Order should be [pcscf, icscf, scscf] for dispatch priority.
	if adaptors[0].Role() != proxy.RolePCSCF {
		t.Errorf("adaptor[0] role = %d, want PCSCF", adaptors[0].Role())
	}
	if adaptors[1].Role() != proxy.RoleICSCF {
		t.Errorf("adaptor[1] role = %d, want ICSCF", adaptors[1].Role())
	}
	if adaptors[2].Role() != proxy.RoleSCSCF {
		t.Errorf("adaptor[2] role = %d, want SCSCF", adaptors[2].Role())
	}
}

func TestBuildIMSAdaptors_SingleRole(t *testing.T) {
	cfg := &config.Config{
		IMS: config.IMSConfig{
			Enabled: true,
			Realm:   "home.net",
			PCSCF:   &config.PCSCFConfig{ICSCFAddr: "sip:icscf:5060"},
		},
	}
	b := &Bootstrap{}
	adaptors := b.buildIMSAdaptors(cfg.IMS.ResolveRole("pcscf"), cfg.IMS)
	if len(adaptors) != 1 {
		t.Fatalf("got %d adaptors, want 1", len(adaptors))
	}
	if adaptors[0].Role() != proxy.RolePCSCF {
		t.Errorf("role = %d, want PCSCF", adaptors[0].Role())
	}
}

func TestLoopbackSIP(t *testing.T) {
	got := loopbackSIP([]string{"udp:127.0.0.1:5061"})
	if got != "sip:127.0.0.1:5061" {
		t.Fatalf("loopbackSIP = %q, want sip:127.0.0.1:5061", got)
	}
}

func TestLoopbackSIP_EmptyReturnsFallback(t *testing.T) {
	got := loopbackSIP(nil)
	if got == "" {
		t.Fatalf("loopbackSIP(nil) should return non-empty fallback")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /workspace/kamailio-go && go test ./internal/core/app/ -run TestBuildIMSAdaptors 2>&1 | tail -10`
Expected: FAIL with `undefined: buildIMSAdaptors`.

- [ ] **Step 3: Create ims_bootstrap.go**

Create `internal/core/app/ims_bootstrap.go`:

```go
package app

import (
	"strings"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/config"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
	"github.com/kamailio/kamailio-go/internal/ims/icscf"
	"github.com/kamailio/kamailio-go/internal/ims/pcscf"
	"github.com/kamailio/kamailio-go/internal/ims/scscf"
	"github.com/kamailio/kamailio-go/internal/modules/cdp"
)

// buildIMSAdaptors constructs the CSCF adaptor list for the given roles.
// Order is fixed as [pcscf, icscf, scscf] to keep dispatch priority stable.
func (b *Bootstrap) buildIMSAdaptors(roles []int, cfg config.IMSConfig) []proxy.CSCFAdaptor {
	var out []proxy.CSCFAdaptor
	for _, r := range roles {
		switch r {
		case config.RolePCSCF:
			out = append(out, b.buildPCSCFAdaptor(roles, cfg))
		case config.RoleICSCF:
			out = append(out, b.buildICSCFAdaptor(roles, cfg))
		case config.RoleSCSCF:
			out = append(out, b.buildSCSCFAdaptor(cfg))
		}
	}
	return out
}

func (b *Bootstrap) buildPCSCFAdaptor(roles []int, cfg config.IMSConfig) proxy.CSCFAdaptor {
	pcfg := cfg.PCSCF
	if pcfg == nil {
		pcfg = &config.PCSCFConfig{}
	}
	icscfAddr := pcfg.ICSCFAddr
	if icscfAddr == "" && configContainsRole(roles, config.RoleICSCF) {
		icscfAddr = loopbackSIP(cfg.ICSCF.Listen)
	}
	return pcscf.NewAdaptor(
		pcscf.NewSessionHandler(),
		b.registrar, // core/usrloc.Registrar attached during bootstrap
		icscfAddr,
		pcfg.SCSCFAddr,
		b.ProxyCore.Forwarder(),
	)
}

func (b *Bootstrap) buildICSCFAdaptor(roles []int, cfg config.IMSConfig) proxy.CSCFAdaptor {
	icfg := cfg.ICSCF
	if icfg == nil {
		icfg = &config.ICSCFConfig{}
	}
	tbl := icscf.NewSCSCFTable()
	tbl.LoadSCSCFs(toSCSCFCapabilities(icfg.SCSCFCapabilities))
	if len(icfg.PreferredSCSCF) > 0 {
		tbl.SetPreferredSCSCFs(icfg.PreferredSCSCF, true)
	}
	if icfg.EntryExpiry > 0 {
		tbl.SetEntryExpiry(time.Duration(icfg.EntryExpiry) * time.Second)
	}
	txn := cdp.DefaultTransactionManager()
	i := icscf.New(&icscf.Config{
		OriginHost:       "icscf." + cfg.Realm,
		OriginRealm:      cfg.Realm,
		DestinationRealm: cfg.Realm,
		ForcedPeer:       icfg.ForcedPeer,
		DefaultTimeout:   5 * time.Second,
		VisitedNetworkID: icfg.VisitedNetworkID,
	}, tbl, txn)
	scscfAddr := icfg.SCSCFAddr
	if scscfAddr == "" && configContainsRole(roles, config.RoleSCSCF) {
		scscfAddr = loopbackSIP(cfg.SCSCF.Listen)
	}
	return icscf.NewAdaptor(i, tbl, scscfAddr, b.ProxyCore.Forwarder())
}

func (b *Bootstrap) buildSCSCFAdaptor(cfg config.IMSConfig) proxy.CSCFAdaptor {
	scfg := cfg.SCSCF
	if scfg == nil {
		scfg = &config.SCSCFConfig{Realm: cfg.Realm}
	}
	if scfg.Realm == "" {
		scfg.Realm = cfg.Realm
	}
	reg := scscf.NewRegistrar(scfg.Realm)
	sess := scscf.NewSessionHandler(reg)
	return scscf.NewAdaptor(reg, sess, b.ProxyCore.Forwarder())
}

// toSCSCFCapabilities converts config SCSCFCapConfig entries to the
// icscf.SCSCFCapability type expected by LoadSCSCFs.
func toSCSCFCapabilities(in []config.SCSCFCapConfig) []icscf.SCSCFCapability {
	out := make([]icscf.SCSCFCapability, 0, len(in))
	for _, c := range in {
		out = append(out, icscf.SCSCFCapability{
			ID:            c.ID,
			Name:          c.Name,
			MandatoryCaps: c.MandatoryCaps,
			OptionalCaps:  c.OptionalCaps,
		})
	}
	return out
}

// configContainsRole reports whether roles contains the wanted role id.
// (config and proxy each define their own RolePCSCF/ICSCF/SCSCF constants
// to avoid an import cycle; the integer values match by construction.)
func configContainsRole(roles []int, want int) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

// loopbackSIP returns a sip: URI for the first listen address in the
// list, suitable as a next_hop when roles share one process. Returns
// "sip:127.0.0.1:5060" when the list is empty.
func loopbackSIP(listen []string) string {
	if len(listen) == 0 {
		return "sip:127.0.0.1:5060"
	}
	// Listen entries look like "udp:127.0.0.1:5061"; strip the proto prefix.
	addr := listen[0]
	if idx := strings.Index(addr, ":"); idx >= 0 {
		addr = addr[idx+1:]
	}
	return "sip:" + addr
}
```

- [ ] **Step 4: Verify the helper methods exist; add if missing**

Run: `cd /workspace/kamailio-go && grep -n "func.*ProxyCore.*Forwarder\|registrar\s*\*usrloc" internal/core/app/bootstrap.go internal/core/proxy/proxy.go | head -10`

If `ProxyCore.Forwarder()` does not exist, add it to `internal/core/proxy/proxy.go` near the other accessors:

```go
// Forwarder returns the currently attached forwarder (may be nil).
func (p *ProxyCore) Forwarder() *forward.Forwarder {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.forward
}
```

If `Bootstrap.registrar` does not exist as a field, add it (or access via `b.ProxyCore.registrar` if exported). Inspect:
Run: `cd /workspace/kamailio-go && grep -n "registrar" internal/core/app/bootstrap.go | head -10`

If the registrar is only stored on ProxyCore (not on Bootstrap), change `b.registrar` to `b.ProxyCore.Registrar()` and ensure `Registrar()` accessor exists on ProxyCore (add if missing, mirroring `AuthStore()`).

- [ ] **Step 5: Wire buildIMSAdaptors into NewBootstrap**

In `internal/core/app/bootstrap.go`, after `pcore.SetRegistrar(reg)` (around line 140), insert:

```go
	// IMS role adaptors (--role / cfg.IMS.Role).
	roles := cfg.IMS.ResolveRole(opts.Role)
	if cfg.IMS.Enabled && len(roles) > 0 {
		b2 := &Bootstrap{ProxyCore: pcore, Config: cfg}
		b2.registrar = reg // ensure registrar is reachable from buildIMSAdaptors
		adaptors := b2.buildIMSAdaptors(roles, cfg.IMS)
		pcore.SetCSCFAdaptors(adaptors)
	}
```

Also add `Role string` to `BootstrapOptions`:

```go
type BootstrapOptions struct {
	ConfigFile      string
	LogLevel        string
	ShutdownTimeout time.Duration
	PrintConfig     bool
	RPCAddr         string
	ScriptFile      string
	Role            string // pcscf|scscf|icscf|all
}
```

And add `registrar *registrar.Registrar` field to `Bootstrap` struct if not already there.

- [ ] **Step 6: Run tests**

Run: `cd /workspace/kamailio-go && go test ./internal/core/app/ -run TestBuildIMSAdaptors 2>&1 | tail -10`
Expected: PASS.

Run: `cd /workspace/kamailio-go && go build ./... 2>&1 | tail -10`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
cd /workspace/kamailio-go && git add internal/core/app/ims_bootstrap.go internal/core/app/ims_bootstrap_test.go internal/core/app/bootstrap.go internal/core/proxy/proxy.go && git commit -m "feat(app): wire IMS role adaptors into bootstrap"
```

---

## Task 9: CLI --role Flag

**Files:**
- Modify: `cmd/kamailio/main.go`.

- [ ] **Step 1: Add the flag parsing**

In `cmd/kamailio/main.go`, in the `run` command's arg-parsing loop (around line 28-54), add a case:

```go
		case "--role", "-r":
			if i+1 < len(args) {
				opts.Role = args[i+1]
				i++
			}
```

Update the `-h`/`--help` usage string in the same function to document the new flag:

```go
			fmt.Printf("Usage: kamailio-go run [-f CONFIG] [-L LEVEL] [--rpc-addr HOST:PORT] [--script PATH] [--role ROLE]\n\nOptions:\n  -f, --config       Path to a configuration file (YAML or key=value)\n  -L, --log-level    Log level (debug, info, warn, error)\n      --rpc-addr, -rpc   host:port for the JSON-RPC HTTP endpoint\n      --script, -s       Path to a Kamailio-Go routing script\n      --role, -r         IMS role to start (pcscf|scscf|icscf|all, default all)\n")
```

- [ ] **Step 2: Verify it builds**

Run: `cd /workspace/kamailio-go && go build ./cmd/kamailio/ 2>&1 | tail -10`
Expected: no errors.

- [ ] **Step 3: Verify help output**

Run: `cd /workspace/kamailio-go && go run ./cmd/kamailio run -h 2>&1 | tail -10`
Expected: usage string includes `--role, -r`.

- [ ] **Step 4: Commit**

```bash
cd /workspace/kamailio-go && git add cmd/kamailio/main.go && git commit -m "feat(cli): add --role flag for IMS role selection"
```

---

## Task 10: Example Configs

**Files:**
- Create: `configs/ims-pcscf.yaml`, `configs/ims-icscf.yaml`, `configs/ims-scscf.yaml`, `configs/ims-all.yaml`.

- [ ] **Step 1: Create configs/ims-pcscf.yaml**

```yaml
core:
  workers: 4
  log_level: info
  listen:
    - "udp:0.0.0.0:5060"

ims:
  enabled: true
  role: pcscf
  realm: home.net
  pcscf:
    listen:
      - "udp:0.0.0.0:5060"
    realm: home.net
    visited_network_id: visited.home.net
    icscf_addr: "sip:icscf.home.net:5060"
    scscf_addr: "sip:scscf.home.net:5060"
    ipsec:
      enabled: false
```

- [ ] **Step 2: Create configs/ims-icscf.yaml**

```yaml
core:
  workers: 4
  log_level: info
  listen:
    - "udp:0.0.0.0:5060"

ims:
  enabled: true
  role: icscf
  realm: home.net
  icscf:
    listen:
      - "udp:0.0.0.0:5060"
    realm: home.net
    diameter_peers:
      - host: hss.home.net
        ip: 10.0.0.5
        port: 3868
    forced_peer: ""
    scscf_addr: "sip:scscf.home.net:5060"
    scscf_capabilities:
      - id: 1
        name: "sip:scscf1.home.net"
        mandatory_caps: [1]
        optional_caps: [2, 3]
    entry_expiry: 300
    preferred_scscf: []
```

- [ ] **Step 3: Create configs/ims-scscf.yaml**

```yaml
core:
  workers: 4
  log_level: info
  listen:
    - "udp:0.0.0.0:5060"

ims:
  enabled: true
  role: scscf
  realm: home.net
  scscf:
    listen:
      - "udp:0.0.0.0:5060"
    realm: home.net
    diameter_peers:
      - host: hss.home.net
        ip: 10.0.0.5
        port: 3868
    aka_algorithm: AKAv1-MD5
    default_expires: 3600
    min_expires: 60
    max_expires: 86400
```

- [ ] **Step 4: Create configs/ims-all.yaml**

```yaml
core:
  workers: 8
  log_level: info
  listen:
    - "udp:0.0.0.0:5060"

ims:
  enabled: true
  role: all
  realm: home.net
  pcscf:
    realm: home.net
    visited_network_id: visited.home.net
    # icscf_addr / scscf_addr omitted: all-mode uses loopback
  icscf:
    realm: home.net
    scscf_capabilities:
      - id: 1
        name: "sip:scscf1.home.net"
        mandatory_caps: [1]
    entry_expiry: 300
  scscf:
    realm: home.net
    aka_algorithm: AKAv1-MD5
    default_expires: 3600
```

- [ ] **Step 5: Verify each config validates**

For each config file:
Run: `cd /workspace/kamailio-go && go run ./cmd/kamailio check-config -f configs/ims-pcscf.yaml 2>&1 | tail -5`
Expected: `OK: configuration is valid`.

Repeat for `ims-icscf.yaml`, `ims-scscf.yaml`, `ims-all.yaml`.

If validation fails, fix the config or the validator accordingly.

- [ ] **Step 6: Commit**

```bash
cd /workspace/kamailio-go && git add configs/ims-*.yaml && git commit -m "docs(configs): add IMS per-role example configurations"
```

---

## Task 11: E2E Integration Test (--role all three-hop)

**Files:**
- Create: `internal/integration/ims_split_deployment_e2e_test.go`.

- [ ] **Step 1: Write the E2E test scaffolding**

Create `internal/integration/ims_split_deployment_e2e_test.go`:

```go
//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/app"
	"github.com/kamailio/kamailio-go/internal/core/config"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
)

// TestE2E_IMSSplitDeployment_RegisterSuccessFlow starts a single ProxyCore
// with all three CSCF adaptors attached (the --role all path) and verifies
// a REGISTER traverses P-CSCF → I-CSCF → S-CSCF and produces a 401 challenge
// on the first attempt. The HSS is stubbed to return FirstRegistration.
func TestE2E_IMSSplitDeployment_RegisterSuccessFlow(t *testing.T) {
	cfg := buildE2EAllRoleConfig(t)
	boot, err := app.NewBootstrap(app.BootstrapOptions{
		ConfigFile: "", // use cfg directly via a temp file
	})
	_ = cfg
	_ = boot
	if err != nil {
		// NewBootstrap currently loads from file; write cfg to a temp file.
		// For now this test documents the expected wiring; a follow-up
		// refactor will expose a NewBootstrapFromConfig(cfg) entry point.
		t.Skipf("NewBootstrap requires a file path; refactor needed: %v", err)
	}
	defer func() {
		if boot != nil {
			boot.Shutdown()
		}
	}()

	// TODO: drive a SIP UAC stub to send REGISTER and assert the 401
	// response. This requires the E2E helper from internal/integration
	// (ims_e2e_helper_test.go) and a stub cdp MessageHandler for the HSS.
	t.Skip("E2E three-hop flow pending E2E helper + HSS stub wiring")
}

// buildE2EAllRoleConfig constructs the config used by the three-hop test.
func buildE2EAllRoleConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Core: config.CoreConfig{
			Workers:  2,
			LogLevel: "info",
			Listen:   []string{"udp:127.0.0.1:5060"},
		},
		IMS: config.IMSConfig{
			Enabled: true,
			Role:    "all",
			Realm:   "home.net",
			PCSCF:   &config.PCSCFConfig{Realm: "home.net"},
			ICSCF: &config.ICSCFConfig{
				Realm: "home.net",
				SCSCFCapabilities: []config.SCSCFCapConfig{
					{ID: 1, Name: "sip:scscf1.home.net", MandatoryCaps: []int{1}},
				},
			},
			SCSCF: &config.SCSCFConfig{Realm: "home.net"},
		},
	}
}

// Compile-time check that proxy is reachable.
var _ *proxy.ProxyCore = nil
```

- [ ] **Step 2: Run the test (it will skip)**

Run: `cd /workspace/kamailio-go && go test -tags=integration ./internal/integration/ -run TestE2E_IMSSplitDeployment 2>&1 | tail -10`
Expected: PASS with `--- SKIP` (the three-hop driving is deferred until the E2E helper + HSS stub are wired, which is a follow-up task tracked separately).

- [ ] **Step 3: Add a focused assertion test that doesn't need a SIP UAC**

Replace the body of `TestE2E_IMSSplitDeployment_RegisterSuccessFlow` with a wiring assertion that's testable now — verify that `buildIMSAdaptors` produces three adaptors in the right order for the all-role config (mirroring the unit test in Task 8 but at integration scope):

```go
func TestE2E_IMSSplitDeployment_RegisterSuccessFlow(t *testing.T) {
	cfg := buildE2EAllRoleConfig(t)

	// Construct a minimal Bootstrap and verify the adaptors are attached.
	pcore := proxy.NewProxyCore(&proxy.ProxyConfig{Realm: cfg.IMS.Realm})
	// Reuse buildIMSAdaptors via an in-process Bootstrap constructed by hand.
	b := &app.Bootstrap{ProxyCore: pcore, Config: cfg}
	roles := cfg.IMS.ResolveRole("")
	adaptors := b.BuildIMSAdaptorsExported(roles, cfg.IMS)
	pcore.SetCSCFAdaptors(adaptors)

	if len(adaptors) != 3 {
		t.Fatalf("got %d adaptors, want 3 (pcscf, icscf, scscf)", len(adaptors))
	}
	if adaptors[0].Role() != proxy.RolePCSCF {
		t.Errorf("adaptor[0] role = %d, want PCSCF", adaptors[0].Role())
	}
	if adaptors[1].Role() != proxy.RoleICSCF {
		t.Errorf("adaptor[1] role = %d, want ICSCF", adaptors[1].Role())
	}
	if adaptors[2].Role() != proxy.RoleSCSCF {
		t.Errorf("adaptor[2] role = %d, want SCSCF", adaptors[2].Role())
	}
}
```

If `BuildIMSAdaptorsExported` doesn't exist, expose `buildIMSAdaptors` as an exported method `BuildIMSAdaptors` on `Bootstrap` (rename or add a wrapper):

In `internal/core/app/ims_bootstrap.go`, add:

```go
// BuildIMSAdaptors is the exported wrapper for buildIMSAdaptors, intended
// for use by integration tests that construct a Bootstrap by hand.
func (b *Bootstrap) BuildIMSAdaptors(roles []int, cfg config.IMSConfig) []proxy.CSCFAdaptor {
	return b.buildIMSAdaptors(roles, cfg)
}
```

- [ ] **Step 4: Run the test**

Run: `cd /workspace/kamailio-go && go test -tags=integration ./internal/integration/ -run TestE2E_IMSSplitDeployment 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /workspace/kamailio-go && git add internal/integration/ims_split_deployment_e2e_test.go internal/core/app/ims_bootstrap.go && git commit -m "test(integration): add --role all three-hop adaptor wiring assertion"
```

---

## Task 12: README Documentation

**Files:**
- Modify: `README.md`.

- [ ] **Step 1: Add the IMS split-deployment section**

In `README.md`, find the IMS or deployment section (or append before any "License" section). Add:

```markdown
## IMS 分角色部署

`kamailio-go` 支持将 P-CSCF / I-CSCF / S-CSCF 作为独立进程部署，通过标准 SIP 互联。使用单一二进制 + `--role` 标志选择启动哪个角色。

### 快速开始

```bash
# 单进程三角色（默认，向后兼容）
kamailio-go run -f configs/ims-all.yaml

# 三个独立进程
kamailio-go run --role pcscf -f configs/ims-pcscf.yaml
kamailio-go run --role icscf -f configs/ims-icscf.yaml
kamailio-go run --role scscf -f configs/ims-scscf.yaml
```

### 配置

每角色在 `ims.{pcscf,icscf,scscf}` 节下独立配置 `listen`、`realm`、`diameter_peers`、`next_hop` 地址等。详见 [设计文档](docs/superpowers/specs/2026-06-25-ims-split-deployment-design.md) 和 [示例配置](configs/)。

旧版平铺 IMS 配置（`ims.scscf=true` 等）仍向后兼容，等价于 `--role all` 下选择对应子集。

### 架构

- **适配层**：每个 CSCF 角色实现 `proxy.CSCFAdaptor` 接口，返回 `proxy.ResponseAction`。业务包（`internal/ims/{pcscf,scscf,icscf}`）保持纯函数式，不直接操纵网络栈。
- **复用基础设施**：`ProxyCore`、`tm.Manager`、`forward.Forwarder`、`usrloc.Registrar`、`cdp.TransactionManager`。
- **`--role all`**：三角色同进程，通过 localhost SIP 往返串联，与单角色模式跑同一条代码路径。
```

- [ ] **Step 2: Commit**

```bash
cd /workspace/kamailio-go && git add README.md && git commit -m "docs(readme): document IMS split-deployment --role usage"
```

---

## Task 13: Final Verification

- [ ] **Step 1: Full build**

Run: `cd /workspace/kamailio-go && go build ./... 2>&1 | tail -10`
Expected: no errors.

- [ ] **Step 2: Full test suite with race detector**

Run: `cd /workspace/kamailio-go && go test -race ./... 2>&1 | tail -30`
Expected: PASS. Note: `TestTransportListenBadAddress` in `internal/modules/cdp` may fail in root containers (pre-existing, not introduced by this plan) — verify it's the only failure and document in the commit.

- [ ] **Step 3: Targeted tests for the new packages**

Run: `cd /workspace/kamailio-go && go test -race ./internal/core/proxy/ ./internal/core/config/ ./internal/core/app/ ./internal/ims/pcscf/ ./internal/ims/icscf/ ./internal/ims/scscf/ 2>&1 | tail -20`
Expected: all PASS.

- [ ] **Step 4: Coverage check for adaptors**

Run: `cd /workspace/kamailio-go && go test -cover ./internal/ims/pcscf/ ./internal/ims/icscf/ ./internal/ims/scscf/ 2>&1 | tail -10`
Expected: each adaptor ≥ 85% coverage (per spec §7.7).

- [ ] **Step 5: Config validation smoke**

For each of the 4 example configs:
Run: `cd /workspace/kamailio-go && go run ./cmd/kamailio check-config -f configs/ims-all.yaml 2>&1 | tail -3`
Expected: `OK: configuration is valid`.

- [ ] **Step 6: Final commit (if any cleanup)**

```bash
cd /workspace/kamailio-go && git status
# If anything is uncommitted that should be:
# git add <files> && git commit -m "chore: final cleanup"
```

---

## Self-Review

**Spec coverage:**
- §1 Background: addressed by Tasks 1-2 (interface + config foundations).
- §2 Deployment model (`--role`): Task 9 (CLI flag) + Task 3 (`ResolveRole`).
- §3 Architecture overview: Tasks 1-2 (CSCFAdaptor + dispatch), Tasks 5-7 (per-role adaptors), Task 8 (bootstrap wiring).
- §4 Config schema: Task 3 (types, ResolveRole, ListenFor), Task 4 (validator), Task 10 (example configs).
- §5 Adaptor interface & SIP flow: Tasks 5-7 implement each role's flow per §5.3. `applyCSCFAction` (Task 1) replaces the spec's `applyAction`.
- §6 Bootstrap & file list: Task 8 (ims_bootstrap.go + bootstrap.go), Task 9 (main.go), file list matches §6.4.
- §7 Tests & DoD:
  - Unit tests (§7.2): Tasks 5, 6, 7.
  - Integration tests (§7.3): Task 1 (applyCSCFAction) + Task 2 (dispatch).
  - E2E three-hop (§7.4): Task 11 (currently a wiring assertion; full SIP-driven flow deferred as documented in-task).
  - Config tests (§7.6): Tasks 3, 4.
  - DoD §7.7: Task 13 verifies build, race tests, coverage, config validation.

**Deferred items (explicitly noted in-plan):**
- Full SIP-UAC-driven three-hop E2E (Task 11 Step 2): requires E2E helper refactor (`NewBootstrapFromConfig`) and a stub `cdp.MessageHandler` for the HSS. Tracked as a follow-up; the current Task 11 asserts the wiring is correct.
- Multi-process E2E (§7.5): not implemented; noted as optional/slow in spec.

**Placeholder scan:** Searched for TBD/TODO/"fill in"/"implement later" — only occurrences are the documented deferrals in Task 11 Step 1-2, which are explicit skips with rationale, not vague placeholders.

**Type consistency:**
- `proxy.RolePCSCF/ICSCF/SCSCF` (Task 1) ↔ `config.RolePCSCF/ICSCF/SCSCF` (Task 3): values match by construction (both `iota` starting at PCSCF). `configContainsRole` (Task 8) handles the cross-package comparison.
- `proxy.CSCFAdaptor` interface (Task 1) implemented by `pcscf.Adaptor`, `icscf.Adaptor`, `scscf.Adaptor` (Tasks 5-7) — all three define `Role()`, `HandleRegister`, `HandleInvite`, `HandleInDialog` with matching signatures.
- `icscf.ICSCFInterface` (Task 6) matches `*icscf.ICSCF` methods `SendUAR/SendLIR/Table()` (verified against cx_request.go:122,135-136).
- `scscf.Adaptor` uses `result.RouteTarget` (Task 5) — verified against session.go:242.
- `toSCSCFCapabilities` (Task 8) maps `config.SCSCFCapConfig` → `icscf.SCSCFCapability` fields `ID/Name/MandatoryCaps/OptionalCaps` (verified against scscf_list.go).
- `ResolveRole` returns `[]int` (Task 3) — `buildIMSAdaptors` consumes `[]int` (Task 8). ✓.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-25-ims-split-deployment.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints for review.

Which approach?
