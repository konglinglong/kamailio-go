// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Configuration system - matching C cfg.y / coreparam.c
 *
 * Supports YAML configuration files for defining:
 * - Listening sockets
 * - Modules and their parameters
 * - Routing logic (simplified)
 * - IMS-specific settings
 */

package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the server configuration
type Config struct {
	Core     CoreConfig     `yaml:"core"`
	IMS      IMSConfig      `yaml:"ims,omitempty"`
	Modules  []ModuleConfig `yaml:"modules,omitempty"`
	Routes   RouteConfig    `yaml:"routes,omitempty"`

	// Flat fields for simple key/value configuration overlays used by
	// the boot manager.
	ListenIP         string `yaml:"listen_ip,omitempty"`
	ListenPort       int    `yaml:"listen_port,omitempty"`
	Realm            string `yaml:"realm,omitempty"`
	LogLevel         string `yaml:"log_level,omitempty"`
	EnableMediaProxy bool   `yaml:"enable_media_proxy,omitempty"`
	MediaProxyHost   string `yaml:"media_proxy_host,omitempty"`
	MediaProxyPort   int    `yaml:"media_proxy_port,omitempty"`
	AuthEnabled      bool   `yaml:"auth_enabled,omitempty"`
	NATEnabled       bool   `yaml:"nat_enabled,omitempty"`
	PresenceEnabled  bool   `yaml:"presence_enabled,omitempty"`
	HealthListenAddr string `yaml:"health_listen_addr,omitempty"`
}

// CoreConfig represents core server settings
type CoreConfig struct {
	Debug      int      `yaml:"debug"`
	LogLevel   string   `yaml:"log_level"`
	LogStderr  bool     `yaml:"log_stderr"`
	User       string   `yaml:"user,omitempty"`
	Group      string   `yaml:"group,omitempty"`
	Workers    int      `yaml:"workers"`
	Listen     []string `yaml:"listen"`
	Aliases    []string `yaml:"aliases,omitempty"`
	MaxBufSize int      `yaml:"max_buffer_size,omitempty"`
}

// CSCF role identifiers (mirror proxy.RolePCSCF/ICSCF/SCSCF to avoid an
// import cycle between config and proxy).
const (
	RolePCSCF = iota
	RoleICSCF
	RoleSCSCF
)

// IMSConfig represents IMS-specific settings. It supports both the new
// per-role section style (pcscf/icscf/scscf as maps) and the legacy flat
// style (pcscf/icscf/scscf as booleans). The polymorphic pcscf/icscf/scscf
// keys are handled by a custom UnmarshalYAML.
type IMSConfig struct {
	Enabled bool   `yaml:"enabled"`
	Role    string `yaml:"role,omitempty"`
	Realm   string `yaml:"realm,omitempty"`

	PCSCF *PCSCFConfig `yaml:"pcscf,omitempty"`
	ICSCF *ICSCFConfig `yaml:"icscf,omitempty"`
	SCSCF *SCSCFConfig `yaml:"scscf,omitempty"`

	SCSCF_ bool `yaml:"scscf,omitempty"`
	PCSCF_ bool `yaml:"pcscf,omitempty"`
	ICSCF_ bool `yaml:"icscf,omitempty"`

	AKAAlgorithm     string `yaml:"aka_algorithm,omitempty"`
	DefaultExpires   int    `yaml:"default_expires,omitempty"`
	MinExpires       int    `yaml:"min_expires,omitempty"`
	MaxExpires       int    `yaml:"max_expires,omitempty"`
	VisitedNetworkID string `yaml:"visited_network_id,omitempty"`
}

// PCSCFConfig holds P-CSCF role-specific settings.
type PCSCFConfig struct {
	Listen           []string    `yaml:"listen,omitempty"`
	Realm            string      `yaml:"realm,omitempty"`
	VisitedNetworkID string      `yaml:"visited_network_id,omitempty"`
	ICSCFAddr        string      `yaml:"icscf_addr,omitempty"`
	SCSCFAddr        string      `yaml:"scscf_addr,omitempty"`
	IPSEC            IPSECConfig `yaml:"ipsec,omitempty"`
}

// ICSCFConfig holds I-CSCF role-specific settings.
type ICSCFConfig struct {
	Listen            []string           `yaml:"listen,omitempty"`
	Realm             string             `yaml:"realm,omitempty"`
	DiameterPeers     []DiameterPeerConfig `yaml:"diameter_peers,omitempty"`
	ForcedPeer        string             `yaml:"forced_peer,omitempty"`
	SCSCFAddr         string             `yaml:"scscf_addr,omitempty"`
	SCSCFCapabilities []SCSCFCapConfig   `yaml:"scscf_capabilities,omitempty"`
	EntryExpiry       int                `yaml:"entry_expiry,omitempty"`
	PreferredSCSCF    []string           `yaml:"preferred_scscf,omitempty"`
}

// SCSCFConfig holds S-CSCF role-specific settings.
type SCSCFConfig struct {
	Listen         []string             `yaml:"listen,omitempty"`
	Realm          string               `yaml:"realm,omitempty"`
	DiameterPeers  []DiameterPeerConfig `yaml:"diameter_peers,omitempty"`
	AKAAlgorithm   string               `yaml:"aka_algorithm,omitempty"`
	DefaultExpires int                  `yaml:"default_expires,omitempty"`
	MinExpires     int                  `yaml:"min_expires,omitempty"`
	MaxExpires     int                  `yaml:"max_expires,omitempty"`
}

// DiameterPeerConfig describes a single Diameter peer connection.
type DiameterPeerConfig struct {
	Host string `yaml:"host"`
	IP   string `yaml:"ip"`
	Port int    `yaml:"port"`
}

// SCSCFCapConfig describes an S-CSCF capability entry for I-CSCF selection.
type SCSCFCapConfig struct {
	ID            int    `yaml:"id"`
	Name          string `yaml:"name"`
	MandatoryCaps []int  `yaml:"mandatory_caps,omitempty"`
	OptionalCaps  []int  `yaml:"optional_caps,omitempty"`
}

// IPSECConfig holds IPSec settings used by the P-CSCF.
type IPSECConfig struct {
	Enabled bool `yaml:"enabled"`
}

// ModuleConfig represents a module configuration
type ModuleConfig struct {
	Name   string                 `yaml:"name"`
	Params map[string]interface{} `yaml:"params,omitempty"`
}

// RouteConfig represents routing configuration
type RouteConfig struct {
	Request []RouteRule `yaml:"request,omitempty"`
	Reply   []RouteRule `yaml:"reply,omitempty"`
}

// RouteRule represents a single routing rule
type RouteRule struct {
	Method  string `yaml:"method,omitempty"`
	Header  string `yaml:"header,omitempty"`
	Action  string `yaml:"action"`
	Target  string `yaml:"target,omitempty"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Core: CoreConfig{
			Debug:      3,
			LogLevel:   "info",
			LogStderr:  true,
			Workers:    8,
			Listen:     []string{"udp:0.0.0.0:5060"},
			MaxBufSize: 65535,
		},
		IMS: IMSConfig{
			Enabled:        false,
			Realm:          "ims.mnc001.mcc460.gprs",
			Role:           "",
			AKAAlgorithm:   "AKAv1-MD5",
			DefaultExpires: 3600,
			MinExpires:     60,
			MaxExpires:     86400,
		},
		Modules: []ModuleConfig{
			{Name: "tm"},
			{Name: "sl"},
		},
		Routes: RouteConfig{
			Request: []RouteRule{
				{Method: "REGISTER", Action: "handle_register"},
				{Method: "INVITE", Action: "handle_invite"},
				{Method: "BYE", Action: "handle_bye"},
				{Method: "ACK", Action: "relay"},
				{Method: "CANCEL", Action: "handle_cancel"},
			},
			Reply: []RouteRule{
				{Action: "relay"},
			},
		},
	}
}

// Load loads configuration from a YAML file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// LoadFromBytes parses a YAML config from a byte slice, starting from the
// DefaultConfig and applying the provided overlay. It performs the same
// validation as Load.
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

// ResolveRole determines which CSCF roles this instance should run.
// The flagRole (e.g. from --role) takes precedence over the configured
// IMS.Role field. An empty role resolves to "all" unless one of the legacy
// per-role booleans (SCSCF_/PCSCF_/ICSCF_) is set, in which case only the
// explicitly-enabled roles are returned.
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

// ListenFor returns the listen addresses for the first role in roles that
// has a per-role listen list configured. If none of the roles define their
// own listen list, the core listen list is returned as a fallback.
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

// UnmarshalYAML implements polymorphic decoding for the ims section. The
// pcscf/icscf/scscf keys may be either a boolean (legacy flat style:
// `scscf: true`) or a mapping (new per-role section style:
// `scscf: {listen: [...]}`). The struct tags alone cannot express this
// because the pointer and the legacy boolean share the same yaml key, so
// we decode the polymorphic keys by inspecting the node kind and leave the
// remaining scalar/map fields to a plain decode.
func (c *IMSConfig) UnmarshalYAML(value *yaml.Node) error {
	type plain struct {
		Enabled          bool   `yaml:"enabled"`
		Role             string `yaml:"role,omitempty"`
		Realm            string `yaml:"realm,omitempty"`
		AKAAlgorithm     string `yaml:"aka_algorithm,omitempty"`
		DefaultExpires   int    `yaml:"default_expires,omitempty"`
		MinExpires       int    `yaml:"min_expires,omitempty"`
		MaxExpires       int    `yaml:"max_expires,omitempty"`
		VisitedNetworkID string `yaml:"visited_network_id,omitempty"`
	}
	var p plain
	// value.Decode ignores unknown keys (the polymorphic scscf/pcscf/icscf
	// keys are not members of plain), so this populates only the flat fields.
	if err := value.Decode(&p); err != nil {
		return err
	}
	c.Enabled = p.Enabled
	c.Role = p.Role
	c.Realm = p.Realm
	c.AKAAlgorithm = p.AKAAlgorithm
	c.DefaultExpires = p.DefaultExpires
	c.MinExpires = p.MinExpires
	c.MaxExpires = p.MaxExpires
	c.VisitedNetworkID = p.VisitedNetworkID

	// Decode the polymorphic pcscf/icscf/scscf keys by node kind.
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		valNode := value.Content[i+1]
		switch keyNode.Value {
		case "scscf":
			if valNode.Kind == yaml.ScalarNode {
				c.SCSCF_ = valNode.Value == "true"
			} else if valNode.Kind == yaml.MappingNode {
				var s SCSCFConfig
				if err := valNode.Decode(&s); err != nil {
					return err
				}
				c.SCSCF = &s
			}
		case "pcscf":
			if valNode.Kind == yaml.ScalarNode {
				c.PCSCF_ = valNode.Value == "true"
			} else if valNode.Kind == yaml.MappingNode {
				var s PCSCFConfig
				if err := valNode.Decode(&s); err != nil {
					return err
				}
				c.PCSCF = &s
			}
		case "icscf":
			if valNode.Kind == yaml.ScalarNode {
				c.ICSCF_ = valNode.Value == "true"
			} else if valNode.Kind == yaml.MappingNode {
				var s ICSCFConfig
				if err := valNode.Decode(&s); err != nil {
					return err
				}
				c.ICSCF = &s
			}
		}
	}
	return nil
}

// MarshalYAML serializes IMSConfig without triggering yaml.v3's
// "duplicated key" panic that would otherwise arise from the pointer and
// legacy boolean sharing the scscf/pcscf/icscf tags. Each polymorphic key
// is emitted once: as the per-role mapping when the pointer is set, as the
// boolean `true` when only the legacy flag is set, and omitted otherwise.
func (c IMSConfig) MarshalYAML() (interface{}, error) {
	type out struct {
		Enabled          bool        `yaml:"enabled"`
		Role             string      `yaml:"role,omitempty"`
		Realm            string      `yaml:"realm,omitempty"`
		PCSCF            interface{} `yaml:"pcscf,omitempty"`
		ICSCF            interface{} `yaml:"icscf,omitempty"`
		SCSCF            interface{} `yaml:"scscf,omitempty"`
		AKAAlgorithm     string      `yaml:"aka_algorithm,omitempty"`
		DefaultExpires   int         `yaml:"default_expires,omitempty"`
		MinExpires       int         `yaml:"min_expires,omitempty"`
		MaxExpires       int         `yaml:"max_expires,omitempty"`
		VisitedNetworkID string      `yaml:"visited_network_id,omitempty"`
	}
	o := out{
		Enabled:          c.Enabled,
		Role:             c.Role,
		Realm:            c.Realm,
		AKAAlgorithm:     c.AKAAlgorithm,
		DefaultExpires:   c.DefaultExpires,
		MinExpires:       c.MinExpires,
		MaxExpires:       c.MaxExpires,
		VisitedNetworkID: c.VisitedNetworkID,
	}
	if c.PCSCF != nil {
		o.PCSCF = c.PCSCF
	} else if c.PCSCF_ {
		o.PCSCF = true
	}
	if c.ICSCF != nil {
		o.ICSCF = c.ICSCF
	} else if c.ICSCF_ {
		o.ICSCF = true
	}
	if c.SCSCF != nil {
		o.SCSCF = c.SCSCF
	} else if c.SCSCF_ {
		o.SCSCF = true
	}
	return o, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Core.Workers <= 0 {
		c.Core.Workers = 8
	}
	if len(c.Core.Listen) == 0 {
		return fmt.Errorf("no listen sockets configured")
	}
	if c.Core.LogLevel == "" {
		c.Core.LogLevel = "info"
	}
	return nil
}

// Save saves the configuration to a YAML file
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetModule returns a module configuration by name
func (c *Config) GetModule(name string) *ModuleConfig {
	for i := range c.Modules {
		if c.Modules[i].Name == name {
			return &c.Modules[i]
		}
	}
	return nil
}

// GetListenAddresses returns all listen addresses
func (c *Config) GetListenAddresses() []string {
	return c.Core.Listen
}

// IsIMSEnabled returns true if IMS is enabled
func (c *Config) IsIMSEnabled() bool {
	return c.IMS.Enabled
}
