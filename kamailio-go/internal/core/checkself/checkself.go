// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * check_self - matching C core check_self() / check_self_func_list.
 *
 * The check_self mechanism determines whether a host:port pair refers
 * to the local proxy. It is used by the registrar, dialog, and routing
 * code to decide whether to terminate a request locally or forward it.
 *
 * In the C core, check_self is implemented via a list of registered
 * callback functions; the first callback that returns true wins. This
 * Go port preserves that design so modules (corex, tls, etc.) can
 * register additional self-checks beyond the default alias table.
 */

package checkself

import (
	"net"
	"os"
	"strings"
	"sync"
)

// CheckSelfFunc is a callback that decides whether host[:port] is local.
// proto is the transport ("udp", "tcp", "tls", "sctp"). Returning true
// short-circuits the check.
type CheckSelfFunc func(host string, port int, proto string) bool

// Registry holds the list of check_self callbacks and the built-in
// alias table. It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	callbacks []CheckSelfFunc
	aliases   map[string]struct{} // host or host:port
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{aliases: make(map[string]struct{})}
}

// RegisterCallback adds a check_self callback. Callbacks are evaluated
// in registration order; the first to return true wins.
func (r *Registry) RegisterCallback(f CheckSelfFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callbacks = append(r.callbacks, f)
}

// AddAlias registers a host[:port] as a local address. The alias is
// matched case-insensitively.
//
//	C: add_alias(name, port, proto)
func (r *Registry) AddAlias(host string, port int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return
	}
	if port > 0 {
		r.aliases[host] = struct{}{}
		r.aliases[host+":"+itoa(port)] = struct{}{}
	} else {
		r.aliases[host] = struct{}{}
	}
}

// RemoveAlias removes a previously registered alias.
func (r *Registry) RemoveAlias(host string, port int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	host = strings.ToLower(strings.TrimSpace(host))
	delete(r.aliases, host)
	if port > 0 {
		delete(r.aliases, host+":"+itoa(port))
	}
}

// Aliases returns a snapshot of all registered aliases.
func (r *Registry) Aliases() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.aliases))
	for a := range r.aliases {
		out = append(out, a)
	}
	return out
}

// CheckSelf reports whether host[:port] refers to the local proxy.
// It first checks the alias table (with and without port), then
// invokes registered callbacks in order.
//
//	C: check_self(host, port, proto)
func (r *Registry) CheckSelf(host string, port int, proto string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}

	r.mu.RLock()
	// Try host:port first, then bare host.
	if port > 0 {
		if _, ok := r.aliases[host+":"+itoa(port)]; ok {
			r.mu.RUnlock()
			return true
		}
	}
	if _, ok := r.aliases[host]; ok {
		r.mu.RUnlock()
		return true
	}
	callbacks := r.callbacks
	r.mu.RUnlock()

	// Run callbacks (outside the lock so they may call back into the
	// registry without deadlocking).
	for _, cb := range callbacks {
		if cb(host, port, proto) {
			return true
		}
	}
	return false
}

// IsMyself is a convenience alias for CheckSelf with proto="udp".
func (r *Registry) IsMyself(host string, port int) bool {
	return r.CheckSelf(host, port, "udp")
}

// CheckSelfIP reports whether the given net.IP is a local address.
// This is a convenience wrapper that checks the IP string against the
// alias table and also considers loopback and link-local addresses.
func (r *Registry) CheckSelfIP(ip net.IP, port int, proto string) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	return r.CheckSelf(ip.String(), port, proto)
}

// Reset clears all aliases and callbacks. Used by tests and Init().
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases = make(map[string]struct{})
	r.callbacks = nil
}

// ---------------------------------------------------------------------------
// Process-wide singleton
// ---------------------------------------------------------------------------

var (
	defaultOnce sync.Once
	defaultReg  *Registry
)

// Default returns the process-wide Registry, initialising it with
// common defaults (localhost, 127.0.0.1, ::1, machine hostname) on
// first use.
func Default() *Registry {
	defaultOnce.Do(func() {
		defaultReg = NewRegistry()
		defaultReg.AddAlias("localhost", 0)
		defaultReg.AddAlias("127.0.0.1", 0)
		defaultReg.AddAlias("::1", 0)
		if name, err := hostname(); err == nil && name != "" {
			defaultReg.AddAlias(strings.ToLower(name), 0)
		}
	})
	return defaultReg
}

// Init reinitialises the process-wide Registry to a fresh state,
// mirroring Kamailio's mod_init reset semantics.
func Init() {
	defaultReg = NewRegistry()
	defaultReg.AddAlias("localhost", 0)
	defaultReg.AddAlias("127.0.0.1", 0)
	defaultReg.AddAlias("::1", 0)
	if name, err := hostname(); err == nil && name != "" {
		defaultReg.AddAlias(strings.ToLower(name), 0)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// itoa converts an int to its decimal string representation without
// importing strconv (keeps the dependency surface small).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// hostname wraps os.Hostname for testability.
var hostname = func() (string, error) {
	return os.Hostname()
}
