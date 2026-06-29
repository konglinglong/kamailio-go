// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * NAPTR DNS resolution - matching C resolve_naptr() / get_naptr().
 *
 * Go's net.Resolver does not support NAPTR records, so this file
 * implements a minimal RFC 3403 NAPTR lookup by sending raw DNS
 * query messages over UDP. A NAPTRProvider interface is exposed so
 * callers can inject mock providers in tests.
 *
 * The wire format follows RFC 1035 (DNS messages) and RFC 3403
 * (NAPTR record RDATA):
 *
 *	NAPTR RDATA:
 *	  ORDER    uint16 (network order)
 *	  PREF     uint16 (network order)
 *	  FLAGS    length-prefixed string
 *	  SERVICE  length-prefixed string
 *	  REGEXP   length-prefixed string
 *	  REPLACEMENT  domain name (may be compressed)
 */

package dns

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// NAPTRProvider looks up NAPTR records for a domain. The default
// implementation sends real DNS queries; tests may inject a mock.
type NAPTRProvider interface {
	LookupNAPTR(ctx context.Context, domain string) ([]NAPTRRecord, error)
}

// Ensure the system provider implements the interface.
var _ NAPTRProvider = (*systemNAPTRProvider)(nil)

// systemNAPTRProvider queries the system's configured DNS servers for
// NAPTR records using raw DNS messages over UDP.
type systemNAPTRProvider struct {
	servers []string // DNS server addresses (host:port)
	timeout time.Duration
}

// defaultNAPTRProvider is the process-wide default NAPTR provider.
// It is initialised lazily from /etc/resolv.conf on first use.
var (
	defaultNAPTROnce  sync.Once
	defaultNAPTRProv  NAPTRProvider
)

// DefaultNAPTRProvider returns the process-wide NAPTR provider, creating
// a systemNAPTRProvider configured from /etc/resolv.conf on first use.
func DefaultNAPTRProvider() NAPTRProvider {
	defaultNAPTROnce.Do(func() {
		defaultNAPTRProv = newSystemNAPTRProvider()
	})
	return defaultNAPTRProv
}

// SetDefaultNAPTRProvider replaces the process-wide NAPTR provider. This
// is intended for tests that want to inject deterministic NAPTR results
// without touching the network.
func SetDefaultNAPTRProvider(p NAPTRProvider) {
	defaultNAPTRProv = p
}

// newSystemNAPTRProvider builds a provider from /etc/resolv.conf. If the
// file cannot be read (e.g. on non-Unix systems), it falls back to the
// well-known localhost resolver on port 53.
func newSystemNAPTRProvider() *systemNAPTRProvider {
	servers := readResolvConf()
	return &systemNAPTRProvider{
		servers: servers,
		timeout: 5 * time.Second,
	}
}

// readResolvConf parses /etc/resolv.conf and returns a slice of
// "host:53" addresses. Falls back to 127.0.0.1:53 on error.
func readResolvConf() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return []string{"127.0.0.1:53"}
	}
	var servers []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver ") {
			addr := strings.TrimSpace(strings.TrimPrefix(line, "nameserver "))
			if addr != "" {
				servers = append(servers, net.JoinHostPort(addr, "53"))
			}
		}
	}
	if len(servers) == 0 {
		return []string{"127.0.0.1:53"}
	}
	return servers
}

// LookupNAPTR sends a NAPTR query for domain and returns the parsed
// records. It tries each configured DNS server in order until one
// responds. Returns an error wrapping the underlying network or
// parse failure.
func (p *systemNAPTRProvider) LookupNAPTR(ctx context.Context, domain string) ([]NAPTRRecord, error) {
	if domain == "" {
		return nil, errors.New("dns: empty domain")
	}
	// NAPTR queries use the fully-qualified domain with a trailing dot.
	fqdn := domain
	if !strings.HasSuffix(fqdn, ".") {
		fqdn += "."
	}

	query := buildNAPTRQuery(fqdn)
	if len(p.servers) == 0 {
		return nil, errors.New("dns: no nameservers configured")
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(p.timeout)
	}

	var lastErr error
	for _, srv := range p.servers {
		if time.Now().After(deadline) {
			break
		}
		recs, err := p.queryServer(ctx, srv, query, deadline)
		if err == nil {
			return recs, nil
		}
		lastErr = err
		// Continue to next server on network errors.
	}
	if lastErr == nil {
		lastErr = ErrNoNAPTR
	}
	return nil, lastErr
}

// queryServer sends the query buffer to one server and parses the
// response. It respects the context deadline.
func (p *systemNAPTRProvider) queryServer(ctx context.Context, srv string, query []byte, deadline time.Time) ([]NAPTRRecord, error) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return nil, errors.New("dns: query deadline exceeded")
	}
	conn, err := net.DialTimeout("udp", srv, remaining)
	if err != nil {
		return nil, fmt.Errorf("dns: dial %s: %w", srv, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if _, err := conn.Write(query); err != nil {
		return nil, fmt.Errorf("dns: write to %s: %w", srv, err)
	}

	// DNS over UDP responses are limited to 512 bytes unless EDNS0
	// is negotiated; NAPTR responses rarely exceed this for SIP.
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("dns: read from %s: %w", srv, err)
	}
	return parseNAPTRResponse(buf[:n])
}

// dnsQueryID provides a monotonically increasing DNS query id source.
var dnsQueryID atomic.Uint32

func init() {
	// Seed with a random base to reduce collision with concurrent
	// queries from other processes.
	dnsQueryID.Store(uint32(rand.Intn(65536)))
}

// buildNAPTRQuery builds a DNS query message for NAPTR records of fqdn.
//
// DNS message format (RFC 1035 §4.1):
//
//	Header: ID(2) Flags(2) QDCOUNT(2) ANCOUNT(2) NSCOUNT(2) ARCOUNT(2)
//	Question: QNAME(var) QTYPE(2) QCLASS(2)
//
// QTYPE for NAPTR is 35.
const naptrQType = 35

func buildNAPTRQuery(fqdn string) []byte {
	id := uint16(dnsQueryID.Add(1) & 0xFFFF)
	var buf []byte
	// Header
	buf = append(buf, byte(id>>8), byte(id))
	// Flags: RD (recursion desired) = 0x0100
	buf = append(buf, 0x01, 0x00)
	// QDCOUNT=1, others=0
	buf = append(buf, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
	// Question: encode QNAME
	buf = append(buf, encodeQName(fqdn)...)
	// QTYPE = NAPTR (35)
	buf = append(buf, 0x00, naptrQType)
	// QCLASS = IN (1)
	buf = append(buf, 0x00, 0x01)
	return buf
}

// encodeQName encodes a fully-qualified domain name into DNS wire
// format (sequence of length-prefixed labels terminated by a zero
// length byte).
func encodeQName(fqdn string) []byte {
	var buf []byte
	// Strip trailing dot for label splitting.
	name := strings.TrimSuffix(fqdn, ".")
	if name == "" {
		return []byte{0}
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) > 63 {
			label = label[:63]
		}
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
	}
	buf = append(buf, 0)
	return buf
}

// parseNAPTRResponse parses a DNS response message and extracts NAPTR
// records from the answer section.
func parseNAPTRResponse(msg []byte) ([]NAPTRRecord, error) {
	if len(msg) < 12 {
		return nil, errors.New("dns: response too short")
	}
	// Read header.
	id := binary.BigEndian.Uint16(msg[0:2])
	_ = id
	flags := binary.BigEndian.Uint16(msg[2:4])
	qr := flags & 0x8000
	if qr == 0 {
		return nil, errors.New("dns: response is not a response (QR=0)")
	}
	rcode := flags & 0x000F
	if rcode != 0 {
		return nil, fmt.Errorf("dns: server returned rcode %d", rcode)
	}
	qdcount := int(binary.BigEndian.Uint16(msg[4:6]))
	ancount := int(binary.BigEndian.Uint16(msg[6:8]))

	off := 12
	// Skip question section.
	for i := 0; i < qdcount; i++ {
		_, n, err := decodeQName(msg, off)
		if err != nil {
			return nil, fmt.Errorf("dns: decode question: %w", err)
		}
		off = n + 4 // skip QTYPE(2) + QCLASS(2)
		if off > len(msg) {
			return nil, errors.New("dns: question truncated")
		}
	}

	var records []NAPTRRecord
	for i := 0; i < ancount; i++ {
		// Answer resource record: NAME QTYPE QCLASS TTL RDLENGTH RDATA
		_, n, err := decodeQName(msg, off)
		if err != nil {
			return nil, fmt.Errorf("dns: decode answer name: %w", err)
		}
		off = n
		if off+10 > len(msg) {
			return nil, errors.New("dns: answer header truncated")
		}
		qtype := binary.BigEndian.Uint16(msg[off : off+2])
		off += 8 // skip QTYPE(2) QCLASS(2) TTL(4)
		rdlength := int(binary.BigEndian.Uint16(msg[off : off+2]))
		off += 2
		if off+rdlength > len(msg) {
			return nil, errors.New("dns: RDATA truncated")
		}
		rdata := msg[off : off+rdlength]
		off += rdlength

		if qtype != naptrQType {
			continue
		}
		// rdataOffset is the offset of rdata within msg; needed so the
		// replacement domain name can resolve compression pointers.
		rdataOffset := off - rdlength
		rec, err := parseNAPTRRDATA(rdata, msg, rdataOffset)
		if err != nil {
			return nil, fmt.Errorf("dns: parse NAPTR RDATA: %w", err)
		}
		records = append(records, rec)
	}

	if len(records) == 0 {
		return nil, ErrNoNAPTR
	}
	return records, nil
}

// parseNAPTRRDATA parses a single NAPTR record's RDATA. rdataOffset is
// the offset of rdata within fullMsg, used so the replacement domain
// name can resolve DNS compression pointers that reference the full
// message.
func parseNAPTRRDATA(rdata, fullMsg []byte, rdataOffset int) (NAPTRRecord, error) {
	if len(rdata) < 4 {
		return NAPTRRecord{}, errors.New("NAPTR RDATA too short")
	}
	rec := NAPTRRecord{}
	rec.Order = binary.BigEndian.Uint16(rdata[0:2])
	rec.Preference = binary.BigEndian.Uint16(rdata[2:4])
	off := 4

	// FLAGS, SERVICE, REGEXP are length-prefixed strings.
	var err error
	if rec.Flags, off, err = readCharString(rdata, off); err != nil {
		return rec, err
	}
	if rec.Service, off, err = readCharString(rdata, off); err != nil {
		return rec, err
	}
	if rec.Regexp, off, err = readCharString(rdata, off); err != nil {
		return rec, err
	}
	// REPLACEMENT is a domain name starting at the current offset within
	// rdata. It may use compression pointers into the full message, so
	// decode it relative to fullMsg at (rdataOffset + off).
	name, _, err := decodeQName(fullMsg, rdataOffset+off)
	if err != nil {
		return rec, fmt.Errorf("decode replacement: %w", err)
	}
	rec.Replacement = name
	return rec, nil
}

// readCharString reads a length-prefixed character string from buf at
// offset. Returns the string and the new offset.
func readCharString(buf []byte, off int) (string, int, error) {
	if off >= len(buf) {
		return "", off, errors.New("char string: offset past end")
	}
	length := int(buf[off])
	off++
	if off+length > len(buf) {
		return "", off, errors.New("char string: data past end")
	}
	return string(buf[off : off+length]), off + length, nil
}

// decodeQName decodes a DNS domain name starting at offset in msg.
// Handles compression pointers (RFC 1035 §4.1.4). Returns the name
// (as a dotted string without trailing dot) and the offset just past
// the name in the message.
func decodeQName(msg []byte, off int) (string, int, error) {
	var labels []string
	origOff := off
	jumped := false
	hops := 0
	const maxHops = 16

	for {
		if off >= len(msg) {
			return "", origOff, errors.New("qname: offset past end")
		}
		b := msg[off]
		if b == 0 {
			off++
			break
		}
		if b&0xC0 == 0xC0 {
			// Compression pointer.
			if off+1 >= len(msg) {
				return "", origOff, errors.New("qname: compression pointer truncated")
			}
			ptr := int(binary.BigEndian.Uint16(msg[off:off+2]) & 0x3FFF)
			if !jumped {
				origOff = off + 2 // advance past the pointer
			}
			jumped = true
			off = ptr
			hops++
			if hops > maxHops {
				return "", origOff, errors.New("qname: too many compression hops")
			}
			continue
		}
		// Regular label.
		labelLen := int(b)
		off++
		if off+labelLen > len(msg) {
			return "", origOff, errors.New("qname: label past end")
		}
		labels = append(labels, string(msg[off:off+labelLen]))
		off += labelLen
	}

	name := strings.Join(labels, ".")
	if !jumped {
		return name, off, nil
	}
	return name, origOff, nil
}

// naptrProtoFromService maps a NAPTR service field to a SIP transport
// protocol. The service field uses the format "SIP+D2U" etc. per
// RFC 3263 §4.1.
func naptrProtoFromService(service string) (Proto, bool) {
	switch strings.ToUpper(service) {
	case "SIP+D2U":
		return ProtoUDP, true
	case "SIP+D2T":
		return ProtoTCP, true
	case "SIPS+D2T":
		return ProtoTLS, true
	case "SIP+D2S":
		return ProtoSCTP, true
	case "SIP+D2W":
		return ProtoWS, true
	case "SIPS+D2W":
		return ProtoWSS, true
	default:
		return ProtoUDP, false
	}
}

// ResolveNAPTRRecords is a public helper that queries the default
// NAPTR provider and returns the raw records (without SRV/A downgrade).
// Exposed so callers like enum/dialplan modules can inspect the raw
// NAPTR results.
func (r *Resolver) ResolveNAPTRRecords(ctx context.Context, domain string) ([]NAPTRRecord, error) {
	prov := DefaultNAPTRProvider()
	if prov == nil {
		return nil, ErrNoNAPTR
	}
	return prov.LookupNAPTR(ctx, domain)
}
