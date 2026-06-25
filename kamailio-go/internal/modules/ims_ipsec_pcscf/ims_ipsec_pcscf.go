// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS IPsec P-CSCF module - IPsec Security Association management for the
 * Proxy-CSCF.
 * Port of the kamailio ims_ipsec_pcscf module (src/modules/ims_ipsec_pcscf).
 *
 * The P-CSCF establishes IPsec tunnels with the UE as part of the IMS
 * registration flow (3GPP TS 33.203 §7.1). The UE sends a Security-Client
 * header in the initial REGISTER carrying its SPIs, ports and preferred
 * algorithms; the P-CSCF allocates its own SPI/port pair (spi_pc/spi_ps,
 * port_pc/port_ps), derives the cipher (CK) and integrity (IK) keys from
 * the AKA WWW-Authenticate challenge, and replies with a Security-Server
 * header. Eight kernel SAs + policies are then installed to protect
 * subsequent SIP signalling.
 *
 * This module provides:
 *  - Security-Client header parsing → IPsecSA
 *  - Security-Server / Security-Verify header building
 *  - SPI/port allocation (free-list pool + in-use hash, mirroring the C
 *    spi_generator_t)
 *  - SA table keyed by UE address with TTL-based expiry
 *  - An injectable TunnelManager interface for the kernel XFRM operations
 *    (add_sa / remove_sa / add_policy / remove_policy); the default
 *    NoopTunnelManager is a no-op so the module is usable in tests and
 *    non-Linux environments.
 *
 * It is safe for concurrent use.
 */

package ims_ipsec_pcscf

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// Security mechanism identifiers (RFC 3329 / TS 33.203).
const (
	SecMechIPsec3gpp = "ipsec-3gpp"
	SecMechTLS        = "tls"
	SecMechDigest     = "digest"
)

// Protocol / mode identifiers.
const (
	ProtESP  = "esp"
	ProtAH   = "ah"
	ModTrans = "trans"
	ModTun   = "tunnel"
)

// Default configuration values (mirroring the C modparams).
const (
	defaultClientPort      = 5062
	defaultServerPort      = 5063
	defaultMaxConnections  = 2
	defaultSpiStart        = 100
	defaultSpiRange        = 1000
	defaultReuseServerPort = true
)

// Forwarding flags (mirroring cmd.c IPSEC_* constants).
const (
	FlagForceSocket      = 1
	FlagReverseSearch    = 2
	FlagDstAddrSearch    = 4
	FlagRuriAddrSearch   = 8
	FlagNoAliasSearch    = 16
	FlagNoDstUriReset    = 32
	FlagTcpPortUEC       = 64
	FlagSetDstUriFull    = 128
	FlagForwardUseVia    = 256
	FlagForwardTryTcp    = 512
	FlagDeleteUnusedTun  = 1
)

// ---------------------------------------------------------------------------
// Data structures
// ---------------------------------------------------------------------------

// IPsecSA mirrors the C ipsec_t (usrloc.h). It carries the four SPIs,
// four ports, algorithms and keys that fully describe a pair of IPsec
// Security Associations between the UE and the P-CSCF.
type IPsecSA struct {
	// SPIs: UE-side (from Security-Client) and P-CSCF-side (allocated).
	SPIUC uint32 // SPI Client (UE)  — spi-c from Security-Client
	SPIUS uint32 // SPI Server (UE)  — spi-s from Security-Client
	SPIPC uint32 // SPI Client (P-CSCF) — allocated
	SPIPS uint32 // SPI Server (P-CSCF) — allocated

	// Ports: UE-side (from Security-Client) and P-CSCF-side (allocated).
	PortUC uint16 // port-c from Security-Client
	PortUS uint16 // port-s from Security-Client
	PortPC uint16 // allocated P-CSCF client port
	PortPS uint16 // allocated P-CSCF server port

	// Algorithms.
	EAlg  string // cipher algorithm (ESP) — normalised
	REAlg string // received cipher algorithm — raw from client
	Alg   string // integrity algorithm (AH) — normalised
	RAlg  string // received integrity algorithm — raw from client

	// Keys (from AKA WWW-Authenticate).
	CK []byte // cipher key
	IK []byte // integrity key

	// Protocol / mode.
	Prot string // "esp" or "ah"
	Mod  string // "trans" or "tunnel"
}

// SecurityType identifies the security mechanism in use.
type SecurityType int

const (
	SecurityNone  SecurityType = 0
	SecurityIPsec SecurityType = 1
	SecurityTLS   SecurityType = 2
)

// SecurityAgreement mirrors the C security_t. It wraps an IPsecSA with the
// mechanism type and q-value.
type SecurityAgreement struct {
	SecHeader string        // header name ("Security-Client")
	Type      SecurityType // IPsec or TLS
	IPsec     *IPsecSA     // SA details when Type == SecurityIPsec
	Q         float32      // q-value (preference)
}

// SAEntry tracks one live SA in the SA table.
type SAEntry struct {
	UEHost    string    // UE source IP
	UEPort    uint16    // UE source port
	AOR       string    // Address of Record (for lookup)
	SA        *IPsecSA  // the security association
	CreatedAt time.Time
	Expires   time.Time // zero = no expiry
}

// IsExpired reports whether the SA entry has passed its expiry.
func (e *SAEntry) IsExpired() bool {
	if e.Expires.IsZero() {
		return false
	}
	return time.Now().After(e.Expires)
}

// ---------------------------------------------------------------------------
// TunnelManager interface
// ---------------------------------------------------------------------------

// TunnelManager installs/removes kernel IPsec SAs and policies (XFRM).
// The default NoopTunnelManager is a no-op; production deployments inject
// a real implementation backed by netlink.
//
//	C: create_ipsec_tunnel() / destroy_ipsec_tunnel() (ipsec.c)
type TunnelManager interface {
	CreateTunnel(ueIP string, sa *IPsecSA) error
	DestroyTunnel(ueIP string, sa *IPsecSA) error
	CleanAll() error
}

// NoopTunnelManager is a TunnelManager that does nothing. It is the default
// so the module compiles and runs on non-Linux platforms and in tests.
type NoopTunnelManager struct{}

// CreateTunnel is a no-op.
func (NoopTunnelManager) CreateTunnel(string, *IPsecSA) error { return nil }

// DestroyTunnel is a no-op.
func (NoopTunnelManager) DestroyTunnel(string, *IPsecSA) error { return nil }

// CleanAll is a no-op.
func (NoopTunnelManager) CleanAll() error { return nil }

// ---------------------------------------------------------------------------
// SPI generator
// ---------------------------------------------------------------------------

// spiNode is one allocated SPI/port pair (mirrors spi_node_t).
type spiNode struct {
	spiCID uint32 // P-CSCF client SPI (spi_pc)
	spiSID uint32 // P-CSCF server SPI (spi_ps)
	cport  uint16 // P-CSCF client port (port_pc)
	sport  uint16 // P-CSCF server port (port_ps)
}

// SPIGenerator allocates and releases P-CSCF SPI/port pairs from a bounded
// pool. Each registration consumes one (spi_cid, spi_sid, cport, sport)
// tuple; on teardown the tuple is returned to the free list.
//
//	C: spi_generator_t / init_spi_gen / acquire_spi / release_spi
type SPIGenerator struct {
	mu         sync.Mutex
	used       map[uint32]*spiNode // spi_cid -> node (in-use)
	free       []*spiNode          // LIFO pool
	minSPI     uint32
	maxSPI     uint32
	serverPort uint16 // base P-CSCF server port
	clientPort uint16 // base P-CSCF client port
	portRange  int    // number of port pairs
}

// NewSPIGenerator creates a generator with the supplied SPI range and port
// bases. SPIs are allocated in pairs (spi_cid, spi_cid+1) stepping by 2
// across [minSPI, minSPI+spiRange).
func NewSPIGenerator(minSPI uint32, spiRange int, serverPort, clientPort uint16, portRange int) *SPIGenerator {
	if minSPI < 1 {
		minSPI = 1
	}
	if spiRange < 2 {
		spiRange = 2
	}
	if portRange < 1 {
		portRange = 1
	}
	g := &SPIGenerator{
		used:       make(map[uint32]*spiNode),
		minSPI:     minSPI,
		maxSPI:     minSPI + uint32(spiRange),
		serverPort: serverPort,
		clientPort: clientPort,
		portRange:  portRange,
	}
	g.initFree()
	return g
}

// initFree populates the free list with (spi_cid, spi_sid, cport, sport)
// tuples, stepping SPIs by 2 and cycling ports through portRange slots.
func (g *SPIGenerator) initFree() {
	spi := g.minSPI
	cport := g.clientPort
	sport := g.serverPort
	for i := 0; spi+1 < g.maxSPI; i++ {
		if i >= g.portRange {
			i = 0
			cport = g.clientPort
			sport = g.serverPort
		}
		g.free = append(g.free, &spiNode{
			spiCID: spi,
			spiSID: spi + 1,
			cport:  cport,
			sport:  sport,
		})
		spi += 2
		cport++
		sport++
	}
}

// Acquire pops one (spi_cid, spi_sid, cport, sport) tuple from the free
// list (FIFO, matching the C spi_remove_head semantics). Returns
// ErrSPIExhausted when the pool is empty.
//
//	C: acquire_spi()
func (g *SPIGenerator) Acquire() (spiCID, spiSID uint32, cport, sport uint16, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.free) == 0 {
		return 0, 0, 0, 0, ErrSPIExhausted
	}
	n := g.free[0]
	g.free = g.free[1:]
	g.used[n.spiCID] = n
	return n.spiCID, n.spiSID, n.cport, n.sport, nil
}

// Release returns one (spi_cid, spi_sid, cport, sport) tuple to the free
// list. It is a no-op when the SPI is not in use.
//
//	C: release_spi()
func (g *SPIGenerator) Release(spiCID, spiSID uint32, cport, sport uint16) {
	g.mu.Lock()
	defer g.mu.Unlock()
	n, ok := g.used[spiCID]
	if !ok {
		return
	}
	delete(g.used, spiCID)
	// Push back with the values provided (they may differ if reconfigured).
	n.spiSID = spiSID
	n.cport = cport
	n.sport = sport
	g.free = append(g.free, n)
}

// InUse returns the number of allocated SPI/port pairs.
func (g *SPIGenerator) InUse() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.used)
}

// FreeCount returns the number of available SPI/port pairs.
func (g *SPIGenerator) FreeCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.free)
}

// Clean resets the generator to its initial state, returning all in-use
// tuples to the free list.
//
//	C: clean_spi_list()
func (g *SPIGenerator) Clean() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.used = make(map[uint32]*spiNode)
	g.free = g.free[:0]
	g.initFree()
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config holds the ims_ipsec_pcscf configuration.
type Config struct {
	ListenAddr       string // IPv4 listen address for IPsec sockets
	ListenAddr6      string // IPv6 listen address
	ClientPort       uint16 // base P-CSCF client port
	ServerPort       uint16 // base P-CSCF server port
	ReuseServerPort bool   // reuse SPI/port on re-registration with same ck+ik
	MaxConnections   int    // number of port pairs
	SPIStart         uint32 // first SPI value
	SPIRange         int    // SPI range width
	PreferredAlg     string // preferred integrity algorithm
	PreferredEAlg    string // preferred cipher algorithm
	SATTL           time.Duration // SA table entry TTL (0 = no expiry)
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		ListenAddr:       "",
		ListenAddr6:      "",
		ClientPort:       defaultClientPort,
		ServerPort:       defaultServerPort,
		ReuseServerPort:  defaultReuseServerPort,
		MaxConnections:   defaultMaxConnections,
		SPIStart:         defaultSpiStart,
		SPIRange:         defaultSpiRange,
		PreferredAlg:     "",
		PreferredEAlg:    "",
		SATTL:           0,
	}
}

// ---------------------------------------------------------------------------
// IPsecModule
// ---------------------------------------------------------------------------

// IPsecModule provides the IMS IPsec P-CSCF service: Security-Client
// parsing, Security-Server/Verify building, SPI allocation, SA table
// management and tunnel lifecycle.
type IPsecModule struct {
	mu       sync.RWMutex
	config   Config
	spiGen   *SPIGenerator
	saTable  map[string]*SAEntry // key: UE host
	tunnel   TunnelManager
}

// NewIPsecModule creates a module with the default configuration and a
// NoopTunnelManager.
func NewIPsecModule() *IPsecModule {
	cfg := DefaultConfig()
	return &IPsecModule{
		config:  cfg,
		spiGen:  NewSPIGenerator(cfg.SPIStart, cfg.SPIRange, cfg.ServerPort, cfg.ClientPort, cfg.MaxConnections),
		saTable: make(map[string]*SAEntry),
		tunnel:  NoopTunnelManager{},
	}
}

// NewIPsecModuleWithConfig creates a module with the supplied configuration.
func NewIPsecModuleWithConfig(cfg Config) *IPsecModule {
	m := NewIPsecModule()
	m.config = cfg
	m.spiGen = NewSPIGenerator(cfg.SPIStart, cfg.SPIRange, cfg.ServerPort, cfg.ClientPort, cfg.MaxConnections)
	return m
}

// SetTunnelManager replaces the tunnel manager. Intended for dependency
// injection of a real XFRM-backed implementation.
func (m *IPsecModule) SetTunnelManager(tm TunnelManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tm == nil {
		tm = NoopTunnelManager{}
	}
	m.tunnel = tm
}

// TunnelManager returns the active tunnel manager.
func (m *IPsecModule) TunnelManager() TunnelManager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tunnel
}

// SetConfig replaces the configuration and rebuilds the SPI generator.
func (m *IPsecModule) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
	m.spiGen = NewSPIGenerator(cfg.SPIStart, cfg.SPIRange, cfg.ServerPort, cfg.ClientPort, cfg.MaxConnections)
}

// Config returns a copy of the current configuration.
func (m *IPsecModule) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// SPIGenerator returns the module's SPI generator.
func (m *IPsecModule) SPIGenerator() *SPIGenerator {
	return m.spiGen
}

// ---------------------------------------------------------------------------
// Security-Client header parsing
// ---------------------------------------------------------------------------

// ParseSecurityClient parses a Security-Client header into a
// SecurityAgreement. Only the ipsec-3gpp mechanism is supported; other
// mechanisms return ErrUnsupportedMechanism.
//
//	C: cscf_get_security() / parse_sec_agree() (sec_agree.c)
func ParseSecurityClient(hdr *parser.HdrField) (*SecurityAgreement, error) {
	if hdr == nil {
		return nil, errors.New("ims_ipsec_pcscf: nil header")
	}
	return parseSecAgree(hdr.Body.String())
}

// parseSecAgree parses the header body. The format is:
//
//	ipsec-3gpp;alg=hmac-sha-1-96;prot=esp;mod=trans;ealg=aes-cbc;
//	spi-c=12345;spi-s=12346;port-c=5060;port-s=5061
//
// Parameters may be separated by ';' or ','. The mechanism name is the
// first token (before the first ';' or space).
func parseSecAgree(body string) (*SecurityAgreement, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("ims_ipsec_pcscf: empty Security-Client body")
	}

	// Extract mechanism name.
	rest := body
	if idx := strings.IndexAny(body, ";, "); idx > 0 {
		rest = body[idx+1:]
	} else {
		rest = ""
	}
	mech := strings.TrimSpace(body[:len(body)-len(rest)])
	if idx := strings.IndexAny(mech, ";, "); idx > 0 {
		mech = mech[:idx]
	}
	mech = strings.ToLower(mech)

	sa := &SecurityAgreement{SecHeader: "Security-Client"}
	switch mech {
	case SecMechIPsec3gpp:
		sa.Type = SecurityIPsec
		sa.IPsec = &IPsecSA{}
	case SecMechTLS:
		sa.Type = SecurityTLS
		return sa, nil
	default:
		return nil, fmt.Errorf("ims_ipsec_pcscf: unsupported mechanism %q", mech)
	}

	// Tokenize the remaining body on ';', ',', and space.
	for _, tok := range splitSecParams(rest) {
		kv := strings.SplitN(tok, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(kv[0]))
		val := strings.TrimSpace(kv[1])
		processSecAgreeParam(sa.IPsec, key, val)
	}
	// Normalise algorithm names to lower-case.
	sa.IPsec.Prot = strings.ToLower(sa.IPsec.Prot)
	sa.IPsec.Mod = strings.ToLower(sa.IPsec.Mod)
	if sa.IPsec.Alg == "" {
		sa.IPsec.Alg = sa.IPsec.RAlg
	}
	if sa.IPsec.EAlg == "" {
		sa.IPsec.EAlg = sa.IPsec.REAlg
	}
	return sa, nil
}

// splitSecParams splits a Security-Client body remainder into individual
// name=value tokens, tolerating ';', ',', and whitespace separators.
func splitSecParams(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// Normalise separators to ';'.
	s = strings.ReplaceAll(s, ",", ";")
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// processSecAgreeParam maps a Security-Client parameter name to an
// IPsecSA field. Mirrors process_sec_agree_param() in sec_agree.c.
func processSecAgreeParam(sa *IPsecSA, key, val string) {
	switch key {
	case "alg":
		sa.RAlg = val
	case "prot":
		sa.Prot = val
	case "mod":
		sa.Mod = val
	case "ealg":
		sa.REAlg = val
	case "spi-c":
		sa.SPIUC = parseUint32(val)
	case "spi-s":
		sa.SPIUS = parseUint32(val)
	case "port-c":
		sa.PortUC = parseUint16(val)
	case "port-s":
		sa.PortUS = parseUint16(val)
	}
}

func parseUint32(s string) uint32 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 32)
	return uint32(v)
}

func parseUint16(s string) uint16 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 16)
	return uint16(v)
}

// ---------------------------------------------------------------------------
// Security-Server / Security-Verify header building
// ---------------------------------------------------------------------------

// BuildSecurityServer builds the Security-Server header value from the
// P-CSCF-side SPIs and ports. The algorithms are echoed back from the
// received Security-Client.
//
//	C: add_security_server_header() (cmd.c)
//	Format: ipsec-3gpp;q=0.1;prot=esp;mod=trans;spi-c=<spi_pc>;spi-s=<spi_ps>;
//	        port-c=<port_pc>;port-s=<port_ps>;alg=<r_alg>;ealg=<r_ealg>
func BuildSecurityServer(sa *IPsecSA) str.Str {
	if sa == nil {
		return str.Str{}
	}
	val := fmt.Sprintf(
		"ipsec-3gpp;q=0.1;prot=%s;mod=%s;spi-c=%d;spi-s=%d;port-c=%d;port-s=%d;alg=%s;ealg=%s",
		sa.Prot, sa.Mod,
		sa.SPIPC, sa.SPIPS,
		sa.PortPC, sa.PortPS,
		sa.RAlg, sa.REAlg,
	)
	return str.Mk(val)
}

// BuildSecurityVerify builds the Security-Verify header value, echoing the
// UE-side parameters from the received Security-Client.
//
//	C: (Security-Verify is sent by the UE; the P-CSCF builds it for
//	reference / testing.)
//	Format: ipsec-3gpp;q=0.1;prot=esp;mod=trans;spi-c=<spi_uc>;spi-s=<spi_us>;
//	        port-c=<port_uc>;port-s=<port_us>;alg=<r_alg>;ealg=<r_ealg>
func BuildSecurityVerify(sa *IPsecSA) str.Str {
	if sa == nil {
		return str.Str{}
	}
	val := fmt.Sprintf(
		"ipsec-3gpp;q=0.1;prot=%s;mod=%s;spi-c=%d;spi-s=%d;port-c=%d;port-s=%d;alg=%s;ealg=%s",
		sa.Prot, sa.Mod,
		sa.SPIUC, sa.SPIUS,
		sa.PortUC, sa.PortUS,
		sa.RAlg, sa.REAlg,
	)
	return str.Mk(val)
}

// ---------------------------------------------------------------------------
// CK / IK extraction from WWW-Authenticate
// ---------------------------------------------------------------------------

// ExtractCKIK extracts the cipher key (ck) and integrity key (ik) from a
// WWW-Authenticate header value. The keys are hex-encoded in the
// challenge parameters.
//
//	C: get_ck_ik() / get_www_auth_param() (cmd.c)
func ExtractCKIK(wwwAuth string) (ck, ik []byte, err error) {
	if wwwAuth == "" {
		return nil, nil, errors.New("ims_ipsec_pcscf: empty WWW-Authenticate")
	}
	ckHex := getAuthParam("ck", wwwAuth)
	ikHex := getAuthParam("ik", wwwAuth)
	if ckHex == "" {
		return nil, nil, errors.New("ims_ipsec_pcscf: no ck in WWW-Authenticate")
	}
	if ikHex == "" {
		return nil, nil, errors.New("ims_ipsec_pcscf: no ik in WWW-Authenticate")
	}
	ck, err = hex.DecodeString(ckHex)
	if err != nil {
		return nil, nil, fmt.Errorf("ims_ipsec_pcscf: invalid ck: %w", err)
	}
	ik, err = hex.DecodeString(ikHex)
	if err != nil {
		return nil, nil, fmt.Errorf("ims_ipsec_pcscf: invalid ik: %w", err)
	}
	return ck, ik, nil
}

// getAuthParam extracts a key="value" or key=value parameter from a
// Digest-style header body. Returns the unquoted value or "".
func getAuthParam(key, body string) string {
	lower := strings.ToLower(body)
	idx := strings.Index(lower, key+"=")
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(key)+1:]
	rest = strings.TrimSpace(rest)
	if len(rest) > 0 && rest[0] == '"' {
		end := strings.IndexByte(rest[1:], '"')
		if end >= 0 {
			return rest[1 : 1+end]
		}
		return rest[1:]
	}
	if semi := strings.IndexAny(rest, ",;"); semi >= 0 {
		rest = rest[:semi]
	}
	return strings.TrimSpace(rest)
}

// ---------------------------------------------------------------------------
// SA lifecycle
// ---------------------------------------------------------------------------

// CreateSA processes a REGISTER (or its reply) carrying a Security-Client
// header, allocates P-CSCF SPIs/ports, extracts CK/IK from the
// WWW-Authenticate header, and installs the tunnel. Returns the completed
// IPsecSA and stores it in the SA table keyed by the UE source address.
//
//	C: ipsec_create() (cmd.c)
func (m *IPsecModule) CreateSA(msg *parser.SIPMsg, wwwAuthBody string) (*IPsecSA, error) {
	if msg == nil {
		return nil, errors.New("ims_ipsec_pcscf: nil message")
	}
	// Find the Security-Client header.
	var secClientHdr *parser.HdrField
	if h := msg.GetHeaderByType(parser.HdrSecurityClient); h != nil {
		secClientHdr = h
	}
	if secClientHdr == nil {
		return nil, errors.New("ims_ipsec_pcscf: no Security-Client header")
	}
	sa, err := ParseSecurityClient(secClientHdr)
	if err != nil {
		return nil, err
	}
	if sa.Type != SecurityIPsec || sa.IPsec == nil {
		return nil, errors.New("ims_ipsec_pcscf: not an IPsec security agreement")
	}
	ipsec := sa.IPsec

	// Extract CK/IK from WWW-Authenticate.
	if wwwAuthBody != "" {
		ck, ik, err := ExtractCKIK(wwwAuthBody)
		if err != nil {
			return nil, err
		}
		ipsec.CK = ck
		ipsec.IK = ik
	}

	// Allocate P-CSCF SPIs and ports.
	spiPC, spiPS, portPC, portPS, err := m.spiGen.Acquire()
	if err != nil {
		return nil, err
	}
	ipsec.SPIPC = spiPC
	ipsec.SPIPS = spiPS
	ipsec.PortPC = portPC
	ipsec.PortPS = portPS

	// Derive UE address from the Via or received info.
	ueHost, uePort := extractUEAddress(msg)

	// Install the tunnel.
	tm := m.TunnelManager()
	if err := tm.CreateTunnel(ueHost, ipsec); err != nil {
		m.spiGen.Release(spiPC, spiPS, portPC, portPS)
		return nil, fmt.Errorf("ims_ipsec_pcscf: create tunnel: %w", err)
	}

	// Store in the SA table.
	m.mu.Lock()
	var exp time.Time
	if m.config.SATTL > 0 {
		exp = time.Now().Add(m.config.SATTL)
	}
	m.saTable[ueHost] = &SAEntry{
		UEHost:    ueHost,
		UEPort:    uePort,
		SA:        ipsec,
		CreatedAt: time.Now(),
		Expires:   exp,
	}
	m.mu.Unlock()

	return ipsec, nil
}

// DestroySA tears down the tunnel for the given UE address and releases
// the allocated SPIs/ports.
//
//	C: ipsec_destroy() / destroy_ipsec_tunnel() (cmd.c)
func (m *IPsecModule) DestroySA(ueHost string) error {
	m.mu.Lock()
	entry, ok := m.saTable[ueHost]
	if ok {
		delete(m.saTable, ueHost)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("ims_ipsec_pcscf: no SA for %q", ueHost)
	}
	tm := m.TunnelManager()
	if err := tm.DestroyTunnel(ueHost, entry.SA); err != nil {
		return fmt.Errorf("ims_ipsec_pcscf: destroy tunnel: %w", err)
	}
	m.spiGen.Release(entry.SA.SPIPC, entry.SA.SPIPS, entry.SA.PortPC, entry.SA.PortPS)
	return nil
}

// FindSA looks up the SA for the given UE address.
//
//	C: (looked up via pcontact->security_temp->data.ipsec in C)
func (m *IPsecModule) FindSA(ueHost string) *IPsecSA {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.saTable[ueHost]
	if !ok || entry.IsExpired() {
		return nil
	}
	return entry.SA
}

// SACount returns the number of live SAs in the table.
func (m *IPsecModule) SACount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.saTable)
}

// CleanupExpired removes expired SA entries and tears down their tunnels.
// Returns the number of removed entries.
//
//	C: ipsec_on_expire() (cmd.c) — called per contact on expiry.
func (m *IPsecModule) CleanupExpired() int {
	now := time.Now()
	var expired []*SAEntry
	m.mu.Lock()
	for host, e := range m.saTable {
		if !e.Expires.IsZero() && now.After(e.Expires) {
			expired = append(expired, e)
			delete(m.saTable, host)
		}
	}
	m.mu.Unlock()
	tm := m.TunnelManager()
	for _, e := range expired {
		_ = tm.DestroyTunnel(e.UEHost, e.SA)
		m.spiGen.Release(e.SA.SPIPC, e.SA.SPIPS, e.SA.PortPC, e.SA.PortPS)
	}
	return len(expired)
}

// CleanAll tears down all tunnels, releases all SPIs, and clears the SA
// table.
//
//	C: ipsec_cleanall() (cmd.c)
func (m *IPsecModule) CleanAll() {
	m.mu.Lock()
	entries := make([]*SAEntry, 0, len(m.saTable))
	for host, e := range m.saTable {
		entries = append(entries, e)
		delete(m.saTable, host)
	}
	m.mu.Unlock()
	tm := m.TunnelManager()
	for _, e := range entries {
		_ = tm.DestroyTunnel(e.UEHost, e.SA)
		m.spiGen.Release(e.SA.SPIPC, e.SA.SPIPS, e.SA.PortPC, e.SA.PortPS)
	}
	_ = tm.CleanAll()
}

// Reconfig resets the SPI generator and cleans all tunnels. Only safe when
// there are no active contacts.
//
//	C: ipsec_reconfig() (cmd.c)
func (m *IPsecModule) Reconfig() {
	m.CleanAll()
	m.spiGen.Clean()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// extractUEAddress derives the UE source IP and port from the message's
// Via header (preferring the received/rport params, falling back to the
// Via host/port). Returns ("", 0) when no Via is present.
func extractUEAddress(msg *parser.SIPMsg) (string, uint16) {
	if msg == nil {
		return "", 0
	}
	var via *parser.ViaBody
	if msg.Via1 != nil {
		via = msg.Via1
	}
	if via == nil {
		if h := msg.GetHeaderByType(parser.HdrVia); h != nil {
			// Fallback: parse the raw Via header body string.
			return parseViaHostPort(h.Body.String())
		}
		return "", 0
	}
	// Prefer the received= / rport= params (NAT-detected address).
	host := via.Host.String()
	port := via.Port
	if via.Received != nil && !via.Received.Value.IsEmpty() {
		host = via.Received.Value.String()
	}
	if via.RPort != nil {
		if p := parseUint16(via.RPort.Value.String()); p > 0 {
			port = p
		}
	}
	return host, port
}

// parseViaHostPort extracts the host and port from a raw Via header body
// string. Used as a fallback when the parsed ViaBody is not available.
func parseViaHostPort(body string) (string, uint16) {
	rest := body
	for i := 0; i < 3; i++ {
		if idx := strings.IndexByte(rest, '/'); idx >= 0 {
			rest = rest[idx+1:]
		}
	}
	rest = strings.TrimSpace(rest)
	if idx := strings.IndexByte(rest, ';'); idx >= 0 {
		rest = rest[:idx]
	}
	rest = strings.TrimSpace(rest)
	host, portStr, err := net.SplitHostPort(rest)
	if err != nil {
		return strings.TrimSpace(rest), 0
	}
	return host, parseUint16(portStr)
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrSPIExhausted is returned by SPIGenerator.Acquire when the pool
	// is empty.
	ErrSPIExhausted = errors.New("ims_ipsec_pcscf: SPI pool exhausted")
	// ErrUnsupportedMechanism is returned by ParseSecurityClient when the
	// mechanism is not ipsec-3gpp or tls.
	ErrUnsupportedMechanism = errors.New("ims_ipsec_pcscf: unsupported mechanism")
)

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultIM *IPsecModule
)

// DefaultIPsec returns the process-wide IPsecModule, creating one on
// first use.
func DefaultIPsec() *IPsecModule {
	defaultMu.RLock()
	im := defaultIM
	defaultMu.RUnlock()
	if im != nil {
		return im
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultIM == nil {
		defaultIM = NewIPsecModule()
	}
	return defaultIM
}

// Init (re)initialises the process-wide IPsecModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultIM = NewIPsecModule()
}
