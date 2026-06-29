// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Configuration tests
 */

package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("expected default config")
	}

	if cfg.Core.Workers != 8 {
		t.Errorf("expected 8 workers, got %d", cfg.Core.Workers)
	}
	if cfg.Core.LogLevel != "info" {
		t.Errorf("expected info log level, got %s", cfg.Core.LogLevel)
	}
	if len(cfg.Core.Listen) != 1 {
		t.Errorf("expected 1 listen address, got %d", len(cfg.Core.Listen))
	}
	if cfg.IsIMSEnabled() {
		t.Error("expected IMS to be disabled by default")
	}
}

func TestConfigValidate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Core.Listen = []string{"udp:0.0.0.0:5060"}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("validate error: %v", err)
	}

	// Test with no listen addresses
	cfg.Core.Listen = nil
	err = cfg.Validate()
	if err == nil {
		t.Error("expected error for no listen addresses")
	}
}

func TestConfigSaveLoad(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Core.Workers = 16
	cfg.IMS.Enabled = true
	cfg.IMS.SCSCF_ = true

	tmpFile := "/tmp/kamailio_test_config.yaml"
	defer os.Remove(tmpFile)

	// Save
	err := cfg.Save(tmpFile)
	if err != nil {
		t.Fatalf("save error: %v", err)
	}

	// Load
	loaded, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if loaded.Core.Workers != 16 {
		t.Errorf("expected 16 workers, got %d", loaded.Core.Workers)
	}
	if !loaded.IsIMSEnabled() {
		t.Error("expected IMS enabled")
	}
	if !loaded.IMS.SCSCF_ {
		t.Error("expected S-CSCF enabled")
	}
}

func TestConfigLoadNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestGetModule(t *testing.T) {
	cfg := DefaultConfig()

	mod := cfg.GetModule("tm")
	if mod == nil {
		t.Error("expected to find tm module")
	}

	mod = cfg.GetModule("nonexistent")
	if mod != nil {
		t.Error("expected nil for nonexistent module")
	}
}

func TestGetListenAddresses(t *testing.T) {
	cfg := DefaultConfig()
	addrs := cfg.GetListenAddresses()
	if len(addrs) != 1 {
		t.Errorf("expected 1 address, got %d", len(addrs))
	}
	if addrs[0] != "udp:0.0.0.0:5060" {
		t.Errorf("unexpected address: %s", addrs[0])
	}
}

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

func TestValidate_IMSRoleInvalid(t *testing.T) {
	cfg := &Config{
		Core: CoreConfig{Listen: []string{"udp:0.0.0.0:5060"}, LogLevel: "info", Workers: 1},
		IMS:  IMSConfig{Enabled: true, Role: "foo", Realm: "home.net"},
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
			Realm:   "home.net",
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
			Realm:   "home.net",
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
			Realm:   "home.net",
			ICSCF:   &ICSCFConfig{SCSCFAddr: "sip:scscf.home.net:5060"}, // no diameter_peers
		},
	}
	report := cfg.ValidateStrict()
	if !report.HasErrors() {
		t.Fatalf("expected error for missing diameter_peers, got none")
	}
}
