// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * CoreX module - core extensions, matching Kamailio modules/corex
 * (corex_mod.c / corex_lib.c).
 *
 * CoreX bundles a set of small but commonly needed helpers that the core
 * itself does not expose as script functions: appending destination branches,
 * forcing the send/receive socket, "is this host mine" checks against the
 * R-URI / From / To, and presence checks for Expires / body / Content-Length.
 *
 * The C module delegates the real branch/socket work to the core dset and
 * socket_info APIs; here we keep an equivalent in-memory branch list and
 * socket override on the CoreXModule so the helpers remain usable and
 * testable without a running proxy core.
 */

package corex

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Branch represents a destination-set entry created by AppendBranch.
// C: struct branch / dset entry built by core append_branch().
type Branch struct {
	URI    string
	Q      int
	Socket string
}

// CoreXModule implements the corex module functionality.
// C: struct module corex (corex_mod.c) + corex_alias_t host list.
type CoreXModule struct {
	mu     sync.RWMutex
	hosts  map[string]bool // lower-cased host names we consider "ours"
	alias  []string        // subdomain aliases (corex_alias_t list)
	branch []Branch        // destination-set branches
}

// NewCoreXModule creates a new CoreXModule pre-populated with the loopback
// host names and the machine's hostname, mirroring the core check_self()
// defaults that the C module relies on.
func NewCoreXModule() *CoreXModule {
	c := &CoreXModule{hosts: make(map[string]bool)}
	c.AddMyHost("localhost")
	c.AddMyHost("127.0.0.1")
	c.AddMyHost("::1")
	if hn, err := os.Hostname(); err == nil && hn != "" {
		c.AddMyHost(hn)
	}
	return c
}

// AddMyHost registers an additional host name that IsMyself / IsMyselfRURI /
// IsMyselfFrom / IsMyselfTo will treat as belonging to us. Matching is
// case-insensitive. Mirrors corex_add_alias() / check_self().
func (c *CoreXModule) AddMyHost(host string) {
	if c == nil {
		return
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hosts[host] = true
}

// AddAliasSubdomains registers a subdomain alias, mirroring
// corex_add_alias_subdomains() (a host plus all of its subdomains are
// considered "ours").
func (c *CoreXModule) AddAliasSubdomains(alias string) {
	if c == nil {
		return
	}
	alias = strings.ToLower(strings.TrimSpace(alias))
	if alias == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.alias = append(c.alias, alias)
	c.hosts[alias] = true
}

// ---------------------------------------------------------------------------
// Branch management (C: w_append_branch / corex_append_branch)
// ---------------------------------------------------------------------------

// AppendBranch appends a new destination branch to the message's destination
// set. uri is the branch URI; q is the Q value (use 0 for "unspecified").
// Returns 1 on success or -1 on error, matching the C return convention.
// C: corex_append_branch() / append_branch().
func (c *CoreXModule) AppendBranch(msg *parser.SIPMsg, uri string, q int) int {
	if c == nil {
		return -1
	}
	if msg == nil {
		return -1
	}
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return -1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.branch = append(c.branch, Branch{URI: uri, Q: q})
	return 1
}

// Branches returns a snapshot of the branches accumulated via AppendBranch.
func (c *CoreXModule) Branches() []Branch {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Branch, len(c.branch))
	copy(out, c.branch)
	return out
}

// ResetBranches clears the accumulated branch list.
func (c *CoreXModule) ResetBranches() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.branch = c.branch[:0]
}

// ---------------------------------------------------------------------------
// Socket overrides (C: w_set_send_socket / w_set_recv_socket)
// ---------------------------------------------------------------------------

// ForceSendSocket forces the socket used to send replies/forwarded requests
// for msg. socket is a "host:port" or "proto:host:port" string. Returns 1 on
// success or -1 on error.
// C: w_set_send_socket() / set_force_socket().
func (c *CoreXModule) ForceSendSocket(msg *parser.SIPMsg, socket string) int {
	if c == nil || msg == nil {
		return -1
	}
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return -1
	}
	msg.ForceSendSocket = socket
	return 1
}

// SetSendSocket sets the per-message send socket override. Equivalent to
// ForceSendSocket in this Go port (the C module distinguishes them only by
// whether the socket is looked up by name or by address).
// C: w_set_send_socket() / w_set_send_socket_name().
func (c *CoreXModule) SetSendSocket(msg *parser.SIPMsg, socket string) int {
	return c.ForceSendSocket(msg, socket)
}

// SetRecvSocket records the receive socket override for msg. The Go parser
// does not expose a dedicated recv-socket field, so the value is stored on
// the message's ForceSendSocket field as a marker; callers can inspect it.
// Returns 1 on success or -1 on error.
// C: w_set_recv_socket().
func (c *CoreXModule) SetRecvSocket(msg *parser.SIPMsg, socket string) int {
	if c == nil || msg == nil {
		return -1
	}
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return -1
	}
	// The Go SIPMsg has no separate recv-socket field; record on the
	// ForceSendSocket slot so the override is observable by callers/tests.
	msg.ForceSendSocket = socket
	return 1
}

// ---------------------------------------------------------------------------
// "Is myself" checks (C: check_self() based helpers)
// ---------------------------------------------------------------------------

// IsMyself returns true if the host part of uri is one of the host names
// registered as ours (case-insensitive), or matches a registered subdomain
// alias.
// C: is_myself() / check_self().
func (c *CoreXModule) IsMyself(uri string) bool {
	if c == nil {
		return false
	}
	u, err := parser.ParseURI(uri)
	if err != nil || u == nil {
		return false
	}
	return c.isMyHost(u.Host.String())
}

// IsMyselfRURI returns true if the host part of the message's Request-URI
// is one of ours.
// C: is_myself_ruri() (kemi).
func (c *CoreXModule) IsMyselfRURI(msg *parser.SIPMsg) bool {
	if msg == nil {
		return false
	}
	var uri string
	// NewURI takes precedence over the first-line R-URI (C: msg->new_uri).
	if msg.NewURI.Len > 0 {
		uri = msg.NewURI.String()
	} else if msg.FirstLine != nil && msg.FirstLine.Req != nil {
		uri = msg.FirstLine.Req.URI.String()
	}
	if uri == "" {
		return false
	}
	return c.IsMyself(uri)
}

// IsMyselfFrom returns true if the host part of the From URI is one of ours.
// C: is_myself_from() (kemi).
func (c *CoreXModule) IsMyselfFrom(msg *parser.SIPMsg) bool {
	if msg == nil || msg.From == nil {
		return false
	}
	tb, err := parser.ParseToBody(msg.From.Body)
	if err != nil || tb == nil || tb.URI == nil {
		return false
	}
	return c.isMyHost(tb.URI.Host.String())
}

// IsMyselfTo returns true if the host part of the To URI is one of ours.
// C: is_myself_to() (kemi).
func (c *CoreXModule) IsMyselfTo(msg *parser.SIPMsg) bool {
	if msg == nil || msg.To == nil {
		return false
	}
	tb, err := parser.ParseToBody(msg.To.Body)
	if err != nil || tb == nil || tb.URI == nil {
		return false
	}
	return c.isMyHost(tb.URI.Host.String())
}

// isMyHost is the shared host-matching helper used by the IsMyself* family.
func (c *CoreXModule) isMyHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.hosts[host] {
		return true
	}
	// Subdomain alias match: "x.example.com" matches alias "example.com".
	for _, a := range c.alias {
		if host == a {
			return true
		}
		if strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Header / body presence checks (C: kemi has_* helpers)
// ---------------------------------------------------------------------------

// HasExpires returns true if msg carries an Expires header.
// C: has_expires() (kemi).
func (c *CoreXModule) HasExpires(msg *parser.SIPMsg) bool {
	if msg == nil {
		return false
	}
	return msg.Expires != nil || msg.GetHeaderByType(parser.HdrExpires) != nil
}

// HasExpiresGT returns true if msg carries an Expires header whose value is
// strictly greater than seconds. Returns false when the header is absent or
// unparseable.
// C: has_expires_gt() (kemi).
func (c *CoreXModule) HasExpiresGT(msg *parser.SIPMsg, seconds int) bool {
	if msg == nil {
		return false
	}
	h := msg.Expires
	if h == nil {
		h = msg.GetHeaderByType(parser.HdrExpires)
	}
	if h == nil {
		return false
	}
	eb, err := parser.ParseExpiresBody(h.Body)
	if err != nil || eb == nil {
		return false
	}
	return int(eb.Value) > seconds
}

// HasBody returns true if msg carries a non-empty body.
// C: has_body() (kemi).
func (c *CoreXModule) HasBody(msg *parser.SIPMsg) bool {
	if msg == nil {
		return false
	}
	if b, ok := msg.Body.([]byte); ok && len(b) > 0 {
		return true
	}
	return false
}

// HasContentLength returns true if msg carries a Content-Length header.
// C: has_content_length() (kemi).
func (c *CoreXModule) HasContentLength(msg *parser.SIPMsg) bool {
	if msg == nil {
		return false
	}
	return msg.ContentLength != nil || msg.GetHeaderByType(parser.HdrContentLength) != nil
}

// ContentLength returns the parsed Content-Length value, or -1 if the header
// is absent or unparseable. Convenience helper not present in the C module
// but useful for tests.
func (c *CoreXModule) ContentLength(msg *parser.SIPMsg) int {
	if msg == nil {
		return -1
	}
	h := msg.ContentLength
	if h == nil {
		h = msg.GetHeaderByType(parser.HdrContentLength)
	}
	if h == nil {
		return -1
	}
	v, err := strconv.Atoi(strings.TrimSpace(h.Body.String()))
	if err != nil || v < 0 {
		return -1
	}
	return v
}

// ---------------------------------------------------------------------------
// Package-level default instance and global functions
// ---------------------------------------------------------------------------

var (
	defaultMu    sync.Mutex
	defaultCoreX *CoreXModule
	defaultOnce  sync.Once
)

// DefaultCoreX returns the package-level default CoreXModule instance.
func DefaultCoreX() *CoreXModule {
	defaultOnce.Do(func() {
		defaultCoreX = NewCoreXModule()
	})
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultCoreX == nil {
		defaultCoreX = NewCoreXModule()
	}
	return defaultCoreX
}

// Init initialises the corex module (resets the default instance).
// Mirrors C mod_init().
func Init() error {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultCoreX = NewCoreXModule()
	return nil
}

// AppendBranch is the package-level wrapper around the default instance.
func AppendBranch(msg *parser.SIPMsg, uri string, q int) int {
	return DefaultCoreX().AppendBranch(msg, uri, q)
}

// IsMyself is the package-level wrapper around the default instance.
func IsMyself(uri string) bool {
	return DefaultCoreX().IsMyself(uri)
}

// HasExpires is the package-level wrapper around the default instance.
func HasExpires(msg *parser.SIPMsg) bool {
	return DefaultCoreX().HasExpires(msg)
}
