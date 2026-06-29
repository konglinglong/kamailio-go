// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Enum module - E.164 number to URI resolution via ENUM (RFC 3761).
 *
 * Port of the kamailio enum module (src/modules/enum). An E.164 number is
 * turned into an ENUM domain name by stripping the leading '+', reversing
 * the digits, separating them with dots and appending the configured
 * suffix (default "e164.arpa"). NAPTR records stored against that name
 * carry a regexp of the form !pattern!replacement!flags that rewrites the
 * number into a SIP URI.
 *
 * The package is safe for concurrent use.
 */
package enum

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// EnumConfig holds the ENUM domain configuration. Suffix is the ENUM
// domain suffix (default "e164.arpa"); Domain is an alternative suffix
// used when Suffix is empty.
type EnumConfig struct {
	Domain string
	Suffix string
}

// EnumRecord is one NAPTR record (Kamailio enum NAPTR). Regexp is the
// NAPTR regexp field of the form !pattern!replacement!flags. Replacement
// is the NAPTR replacement field (usually ".").
type EnumRecord struct {
	Order       int
	Preference  int
	Flags       string
	Service     string
	Regexp      string
	Replacement string
}

// EnumModule implements the enum module. It is safe for concurrent use:
// the record store is guarded by mu.
type EnumModule struct {
	mu      sync.RWMutex
	config  EnumConfig
	records map[string][]*EnumRecord // keyed by ENUM domain name
}

// NewEnumModule creates a new EnumModule with the default e164.arpa suffix.
func NewEnumModule() *EnumModule {
	return &EnumModule{
		config:  EnumConfig{Suffix: "e164.arpa"},
		records: make(map[string][]*EnumRecord),
	}
}

// enumSuffix returns the effective ENUM suffix: Suffix if set, else
// Domain, else the default "e164.arpa".
func (m *EnumModule) enumSuffix() string {
	if m.config.Suffix != "" {
		return m.config.Suffix
	}
	if m.config.Domain != "" {
		return m.config.Domain
	}
	return "e164.arpa"
}

// AddRecord stores a NAPTR record against the ENUM domain name derived
// from number. It is the in-memory equivalent of a DNS NAPTR answer and
// lets Lookup/Query operate without a live resolver.
func (m *EnumModule) AddRecord(number string, record *EnumRecord) {
	if record == nil {
		return
	}
	name := m.BuildEnumName(number)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[name] = append(m.records[name], record)
}

// Lookup resolves number to a URI via ENUM. It builds the ENUM domain
// name, fetches the stored NAPTR records, orders them by (order,
// preference) and applies the first usable regexp. Returns an error if
// no records exist or none yield a URI.
func (m *EnumModule) Lookup(number string) (string, error) {
	name := m.BuildEnumName(number)
	m.mu.RLock()
	src := m.records[name]
	m.mu.RUnlock()
	if len(src) == 0 {
		return "", errors.New("enum: no records for number")
	}
	ordered := make([]*EnumRecord, len(src))
	copy(ordered, src)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Order != ordered[j].Order {
			return ordered[i].Order < ordered[j].Order
		}
		return ordered[i].Preference < ordered[j].Preference
	})
	input := digits(number)
	for _, r := range ordered {
		if r.Regexp != "" {
			uri, err := m.ApplyRegexp(r, input)
			if err == nil && uri != "" {
				return uri, nil
			}
		}
		if r.Replacement != "" && r.Replacement != "." {
			return r.Replacement, nil
		}
	}
	return "", errors.New("enum: no usable record")
}

// Query returns the NAPTR records stored for number under the given
// domain suffix. An empty domain uses the configured suffix. The
// returned slice is a copy.
func (m *EnumModule) Query(number string, domain string) (records []*EnumRecord, err error) {
	suffix := domain
	if suffix == "" {
		suffix = m.enumSuffix()
	}
	name := buildEnumName(number, suffix)
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.records[name]
	out := make([]*EnumRecord, len(src))
	for i, r := range src {
		cp := *r
		out[i] = &cp
	}
	return out, nil
}

// BuildEnumName builds the ENUM domain name for number: the digits of
// number (after stripping '+') reversed, dot-separated, with the
// configured suffix appended. For example "+35831234567" yields
// "7.6.5.4.3.2.1.3.8.5.3.e164.arpa".
func (m *EnumModule) BuildEnumName(number string) string {
	return buildEnumName(number, m.enumSuffix())
}

// buildEnumName is the suffix-parameterised core of BuildEnumName.
func buildEnumName(number, suffix string) string {
	d := digits(number)
	if d == "" {
		return suffix
	}
	rev := reverseString(d)
	parts := make([]string, 0, len(d))
	for _, r := range rev {
		parts = append(parts, string(r))
	}
	return strings.Join(parts, ".") + "." + suffix
}

// ParseNAPTR parses a NAPTR record string of the form
//
//	order preference flags service regexp replacement
//
// where the text fields may be double-quoted. Returns an error if the
// record does not have at least six fields or the order/preference are
// not integers.
func (m *EnumModule) ParseNAPTR(record string) (*EnumRecord, error) {
	fields := tokenizeNAPTR(record)
	if len(fields) < 6 {
		return nil, fmt.Errorf("enum: expected 6 fields, got %d", len(fields))
	}
	order, err := strconv.Atoi(fields[0])
	if err != nil {
		return nil, fmt.Errorf("enum: invalid order %q: %w", fields[0], err)
	}
	pref, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, fmt.Errorf("enum: invalid preference %q: %w", fields[1], err)
	}
	return &EnumRecord{
		Order:       order,
		Preference:  pref,
		Flags:       fields[2],
		Service:     fields[3],
		Regexp:      fields[4],
		Replacement: fields[5],
	}, nil
}

// ApplyRegexp applies a NAPTR regexp to input and returns the rewritten
// string. The regexp field has the form !pattern!replacement!flags; the
// POSIX-style backreferences (\1, \2) in the replacement are converted
// to Go regexp syntax ($1, $2). Returns an error if the regexp is
// malformed.
func (m *EnumModule) ApplyRegexp(record *EnumRecord, input string) (string, error) {
	if record == nil {
		return "", errors.New("enum: nil record")
	}
	re := record.Regexp
	if re == "" {
		return "", errors.New("enum: empty regexp")
	}
	delim := re[0]
	body := strings.Split(re[1:], string(delim))
	if len(body) < 2 {
		return "", errors.New("enum: invalid regexp field")
	}
	pattern := body[0]
	replacement := naptrToGoReplacement(body[1])
	rgx, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("enum: compile regexp: %w", err)
	}
	return rgx.ReplaceAllString(input, replacement), nil
}

// IsEnumURI reports whether uri points at an ENUM domain, i.e. its host
// part ends with the configured suffix.
func (m *EnumModule) IsEnumURI(uri string) bool {
	suffix := m.enumSuffix()
	u, err := parser.ParseURI(uri)
	if err != nil || u == nil {
		return strings.HasSuffix(uri, suffix)
	}
	host := u.Host.String()
	if host != "" {
		return strings.HasSuffix(host, suffix)
	}
	return strings.HasSuffix(uri, suffix)
}

// digits returns only the decimal digits of s (stripping '+' and any
// other characters).
func digits(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// reverseString returns s reversed.
func reverseString(s string) string {
	rs := []rune(s)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return string(rs)
}

// naptrToGoReplacement converts POSIX backreferences (\1) to Go regexp
// syntax ($1).
func naptrToGoReplacement(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			next := s[i+1]
			if next >= '0' && next <= '9' {
				sb.WriteByte('$')
				sb.WriteByte(next)
				i++
				continue
			}
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

// tokenizeNAPTR splits a NAPTR record into fields, honouring
// double-quoted strings and stripping the surrounding quotes.
func tokenizeNAPTR(s string) []string {
	var fields []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case (c == ' ' || c == '\t') && !inQuote:
			if cur.Len() > 0 {
				fields = append(fields, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		fields = append(fields, cur.String())
	}
	return fields
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultEM *EnumModule
)

// DefaultEnum returns the process-wide EnumModule, creating one on first
// use.
func DefaultEnum() *EnumModule {
	defaultMu.RLock()
	e := defaultEM
	defaultMu.RUnlock()
	if e != nil {
		return e
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultEM == nil {
		defaultEM = NewEnumModule()
	}
	return defaultEM
}

// Init (re)initialises the process-wide EnumModule to a fresh state,
// mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultEM = NewEnumModule()
}
