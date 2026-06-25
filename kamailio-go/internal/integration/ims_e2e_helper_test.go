// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS End-to-End test helpers.
 *
 * Provides reusable SIP message builders, parsing utilities, and a mock HSS
 * for IMS integration tests based on 3GPP TS 23.228 / TS 33.203 / RFC 3310.
 */

package integration

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/ims/auth"
	"github.com/kamailio/kamailio-go/internal/ims/scscf"
)

// ---------------------------------------------------------------------------
// SIP message construction helpers
// ---------------------------------------------------------------------------

// extractDomain extracts the domain portion from a SIP URI.
// e.g. "sip:alice@ims.example.com" -> "ims.example.com"
func extractDomain(uri string) string {
	at := strings.IndexByte(uri, '@')
	if at < 0 {
		return ""
	}
	domain := uri[at+1:]
	if semi := strings.IndexByte(domain, ';'); semi >= 0 {
		domain = domain[:semi]
	}
	return domain
}

// buildIMSRegister builds an IMS REGISTER message.
// Parameters: impu (public identity), impi (private identity), contact,
// authHeader (full Authorization header value, empty for initial REGISTER),
// path (Path header URI, empty to omit), expires (>=0 to include Expires header).
//
// Per 3GPP TS 23.228 §5.3.2: the To header carries the IMPU; the Authorization
// header (when present) carries the IMPI as the digest username.
func buildIMSRegister(impu, impi, contact, authHeader, path string, expires int) []byte {
	domain := extractDomain(impu)
	if domain == "" {
		domain = "ims.example.com"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "REGISTER sip:%s SIP/2.0\r\n", domain)
	b.WriteString("Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bKimsreg\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: <%s>;tag=imsregtag\r\n", impu)
	fmt.Fprintf(&b, "To: <%s>\r\n", impu)
	b.WriteString("Call-ID: ims-reg-callid@192.168.1.100\r\n")
	b.WriteString("CSeq: 1 REGISTER\r\n")
	fmt.Fprintf(&b, "Contact: <%s>\r\n", contact)
	if path != "" {
		if strings.HasPrefix(path, "<") {
			fmt.Fprintf(&b, "Path: %s\r\n", path)
		} else {
			fmt.Fprintf(&b, "Path: <%s>\r\n", path)
		}
	}
	if authHeader != "" {
		fmt.Fprintf(&b, "Authorization: %s\r\n", authHeader)
	}
	if expires >= 0 {
		fmt.Fprintf(&b, "Expires: %d\r\n", expires)
	}
	b.WriteString("Content-Length: 0\r\n")
	b.WriteString("\r\n")
	return []byte(b.String())
}

// buildIMSRegisterWithRealm builds a REGISTER with an explicit realm in the
// Request-URI. Used for testing subscribers in different home domains.
func buildIMSRegisterWithRealm(impu, impi, contact, realm, authHeader string, expires int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "REGISTER sip:%s SIP/2.0\r\n", realm)
	b.WriteString("Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bKimsreg2\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: <%s>;tag=imsregtag2\r\n", impu)
	fmt.Fprintf(&b, "To: <%s>\r\n", impu)
	b.WriteString("Call-ID: ims-reg-callid2@192.168.1.100\r\n")
	b.WriteString("CSeq: 1 REGISTER\r\n")
	fmt.Fprintf(&b, "Contact: <%s>\r\n", contact)
	if authHeader != "" {
		fmt.Fprintf(&b, "Authorization: %s\r\n", authHeader)
	}
	if expires >= 0 {
		fmt.Fprintf(&b, "Expires: %d\r\n", expires)
	}
	b.WriteString("Content-Length: 0\r\n")
	b.WriteString("\r\n")
	return []byte(b.String())
}

// buildIMSInvite builds an IMS INVITE message.
// Parameters: fromURI, toURI, fromTag, callID, cseq, sdp (may be empty).
func buildIMSInvite(fromURI, toURI, fromTag, callID string, cseq int, sdp string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "INVITE %s SIP/2.0\r\n", toURI)
	b.WriteString("Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bKimsinv\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: <%s>;tag=%s\r\n", fromURI, fromTag)
	fmt.Fprintf(&b, "To: <%s>\r\n", toURI)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callID)
	fmt.Fprintf(&b, "CSeq: %d INVITE\r\n", cseq)
	fmt.Fprintf(&b, "Contact: <%s>\r\n", fromURI)
	if sdp != "" {
		b.WriteString("Content-Type: application/sdp\r\n")
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(sdp))
		b.WriteString("\r\n")
		b.WriteString(sdp)
	} else {
		b.WriteString("Content-Length: 0\r\n")
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

// buildIMSInviteWithHeaders builds an INVITE with additional headers.
func buildIMSInviteWithHeaders(fromURI, toURI, fromTag, callID string, cseq int, sdp string, extraHeaders map[string]string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "INVITE %s SIP/2.0\r\n", toURI)
	b.WriteString("Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bKimsinvh\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: <%s>;tag=%s\r\n", fromURI, fromTag)
	fmt.Fprintf(&b, "To: <%s>\r\n", toURI)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callID)
	fmt.Fprintf(&b, "CSeq: %d INVITE\r\n", cseq)
	fmt.Fprintf(&b, "Contact: <%s>\r\n", fromURI)
	for name, value := range extraHeaders {
		fmt.Fprintf(&b, "%s: %s\r\n", name, value)
	}
	if sdp != "" {
		b.WriteString("Content-Type: application/sdp\r\n")
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(sdp))
		b.WriteString("\r\n")
		b.WriteString(sdp)
	} else {
		b.WriteString("Content-Length: 0\r\n")
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

// buildIMSBye builds a BYE message within an established dialog.
func buildIMSBye(fromURI, toURI, fromTag, toTag, callID string, cseq int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "BYE %s SIP/2.0\r\n", toURI)
	b.WriteString("Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bKimsbye\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: <%s>;tag=%s\r\n", fromURI, fromTag)
	fmt.Fprintf(&b, "To: <%s>;tag=%s\r\n", toURI, toTag)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callID)
	fmt.Fprintf(&b, "CSeq: %d BYE\r\n", cseq)
	b.WriteString("Content-Length: 0\r\n")
	b.WriteString("\r\n")
	return []byte(b.String())
}

// buildIMSPrack and buildIMSUpdate are defined in ims_session_e2e_test.go
// and are reused by all IMS E2E tests in this package.

// buildIMSResponse builds a SIP response from a raw request.
// Parameters: statusCode, reason, request (raw bytes), toTag, extraHeaders, body.
func buildIMSResponse(statusCode int, reason string, request []byte, toTag string, extraHeaders map[string]string, body string) []byte {
	req, err := parser.ParseMsg(request)
	if err != nil {
		return nil
	}

	var extra [][2]string
	for k, v := range extraHeaders {
		extra = append(extra, [2]string{k, v})
	}

	reply, err := parser.CreateReply(req, parser.ReplyOptions{
		StatusCode:   statusCode,
		ReasonPhrase: reason,
		ToTag:        toTag,
		ExtraHeaders: extra,
		Body:         body,
	})
	if err != nil {
		return nil
	}

	built, err := parser.BuildMessage(reply)
	if err != nil {
		return nil
	}
	return built
}

// buildSDP builds an SDP offer/answer body.
// Parameters: ip (connection address), port (media port), codecs (list of codec names).
func buildSDP(ip string, port int, codecs []string) string {
	var b strings.Builder
	b.WriteString("v=0\r\n")
	fmt.Fprintf(&b, "o=- 12345 1 IN IP4 %s\r\n", ip)
	b.WriteString("s=-\r\n")
	fmt.Fprintf(&b, "c=IN IP4 %s\r\n", ip)
	b.WriteString("t=0 0\r\n")

	var pts []string
	for _, codec := range codecs {
		pts = append(pts, codecPayloadType(codec))
	}
	if len(pts) == 0 {
		pts = []string{"0"}
	}
	fmt.Fprintf(&b, "m=audio %d RTP/AVP %s\r\n", port, strings.Join(pts, " "))
	for _, codec := range codecs {
		pt := codecPayloadType(codec)
		fmt.Fprintf(&b, "a=rtpmap:%s %s/8000\r\n", pt, codec)
	}
	return b.String()
}

// codecPayloadType maps codec names to RTP payload type numbers.
func codecPayloadType(codec string) string {
	switch strings.ToLower(codec) {
	case "pcmu":
		return "0"
	case "pcma":
		return "8"
	case "g729":
		return "18"
	case "telephone-event":
		return "101"
	default:
		return "0"
	}
}

// ---------------------------------------------------------------------------
// SIP message parsing helpers
// ---------------------------------------------------------------------------

// parseSIPMsg parses a raw SIP message, failing the test on error.
func parseSIPMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse SIP message: %v\nmessage:\n%s", err, string(raw))
	}
	return msg
}

// extractHeader extracts a header value from a parsed SIP message (case-insensitive).
func extractHeader(msg *parser.SIPMsg, name string) string {
	for _, h := range msg.Headers {
		if strings.EqualFold(h.Name.String(), name) {
			return h.Body.String()
		}
	}
	return ""
}

// extractChallengeParams extracts the nonce and opaque from a WWW-Authenticate
// header value. Per RFC 3310 / TS 33.203, the nonce encodes RAND||AUTN.
func extractChallengeParams(wwwAuth string) (nonce, opaque string) {
	// Strip the "Digest " prefix if present
	s := strings.TrimSpace(wwwAuth)
	if strings.HasPrefix(strings.ToLower(s), "digest") {
		s = s[strings.Index(s, " ")+1:]
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(kv[0]))
		val := strings.Trim(strings.TrimSpace(kv[1]), "\"")
		switch key {
		case "nonce":
			nonce = val
		case "opaque":
			opaque = val
		}
	}
	return
}

// extractRealm extracts the realm from a WWW-Authenticate header value.
func extractRealm(wwwAuth string) string {
	s := strings.TrimSpace(wwwAuth)
	if strings.HasPrefix(strings.ToLower(s), "digest") {
		s = s[strings.Index(s, " ")+1:]
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		if strings.TrimSpace(strings.ToLower(kv[0])) == "realm" {
			return strings.Trim(strings.TrimSpace(kv[1]), "\"")
		}
	}
	return ""
}

// extractAlgorithm extracts the algorithm from a WWW-Authenticate header value.
func extractAlgorithm(wwwAuth string) string {
	s := strings.TrimSpace(wwwAuth)
	if strings.HasPrefix(strings.ToLower(s), "digest") {
		s = s[strings.Index(s, " ")+1:]
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		if strings.TrimSpace(strings.ToLower(kv[0])) == "algorithm" {
			return strings.Trim(strings.TrimSpace(kv[1]), "\"")
		}
	}
	return ""
}

// buildAuthHeader builds a Digest Authorization header value for AKA responses.
func buildAuthHeader(username, realm, nonce, uri, response, opaque string) string {
	return fmt.Sprintf(
		`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", algorithm=AKAv1-MD5, opaque="%s"`,
		username, realm, nonce, uri, response, opaque,
	)
}

// registerIMSUser performs the full AKA registration flow (401 challenge +
// correct response) and fails the test if any step deviates from the expected
// 3GPP TS 23.228 §5.3.2 flow.
func registerIMSUser(t *testing.T, registrar *scscf.Registrar, impu, impi, contact string) {
	t.Helper()
	realm := registrar.GetRealm()

	// Step 1: Initial REGISTER (no auth) -> 401 challenge
	raw1 := buildIMSRegister(impu, impi, contact, "", "", 3600)
	msg1 := parseSIPMsg(t, raw1)
	res1, err := registrar.HandleRegister(msg1)
	if err != nil {
		t.Fatalf("initial REGISTER failed: %v", err)
	}
	if res1.StatusCode != 401 {
		t.Fatalf("expected 401 challenge, got %d", res1.StatusCode)
	}

	// Step 2: Extract challenge and compute correct response
	nonce, opaque := extractChallengeParams(res1.Headers["WWW-Authenticate"].String())
	record := registrar.GetRecord(impu)
	if record == nil || record.AuthState == nil {
		t.Fatalf("no auth state for %s", impu)
	}
	resp := hex.EncodeToString(record.AuthState.AuthVector.XRES)
	authz := buildAuthHeader(impi, realm, nonce, impu, resp, opaque)

	// Step 3: REGISTER with Authorization -> 200 OK
	raw2 := buildIMSRegister(impu, impi, contact, authz, "", 3600)
	msg2 := parseSIPMsg(t, raw2)
	res2, err := registrar.HandleRegister(msg2)
	if err != nil {
		t.Fatalf("auth REGISTER failed: %v", err)
	}
	if res2.StatusCode != 200 {
		t.Fatalf("expected 200 OK, got %d", res2.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// MockHSS - Mock Home Subscriber Server
// ---------------------------------------------------------------------------

// MockSubscriber represents a subscriber record in the mock HSS.
type MockSubscriber struct {
	IMPI         string
	IMPU         string
	AuthVectors  []*auth.AuthVector
	UserProfile  string // XML
	ChargingInfo string
}

// MockHSS simulates the Home Subscriber Server for integration tests.
// It stores subscriber profiles and pre-generated authentication vectors.
type MockHSS struct {
	mu          sync.RWMutex
	subscribers map[string]*MockSubscriber // key: IMPI
}

// NewMockHSS creates a new mock HSS instance.
func NewMockHSS() *MockHSS {
	return &MockHSS{
		subscribers: make(map[string]*MockSubscriber),
	}
}

// AddSubscriber adds a subscriber with a pre-generated authentication vector.
func (h *MockHSS) AddSubscriber(impi, impu string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	av, err := auth.GenerateAuthVector()
	if err != nil {
		return
	}

	h.subscribers[impi] = &MockSubscriber{
		IMPI:        impi,
		IMPU:        impu,
		AuthVectors: []*auth.AuthVector{av},
		UserProfile: fmt.Sprintf(
			"<IMSSubscription><PrivateID>%s</PrivateID><PublicID>%s</PublicID></IMSSubscription>",
			impi, impu),
	}
}

// GetAuthVector returns the authentication vector for a subscriber.
func (h *MockHSS) GetAuthVector(impi string) (*auth.AuthVector, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sub, ok := h.subscribers[impi]
	if !ok || len(sub.AuthVectors) == 0 {
		return nil, fmt.Errorf("subscriber not found: %s", impi)
	}
	return sub.AuthVectors[0], nil
}

// VerifyAuthResponse verifies that the given response matches the stored XRES.
func (h *MockHSS) VerifyAuthResponse(impi string, response string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sub, ok := h.subscribers[impi]
	if !ok || len(sub.AuthVectors) == 0 {
		return false
	}

	av := sub.AuthVectors[0]
	expected := hex.EncodeToString(av.XRES)
	return strings.EqualFold(response, expected)
}

// verifyNonceFormat checks that the nonce is base64(RAND||AUTN) = 32 bytes decoded.
// Per RFC 3310: nonce = base64(RAND || AUTN), where RAND is 16 bytes and AUTN is 16 bytes.
func verifyNonceFormat(t *testing.T, nonce string, av *auth.AuthVector) {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(nonce)
	if err != nil {
		t.Fatalf("nonce is not valid base64: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("nonce decoded length = %d, want 32 (RAND||AUTN)", len(decoded))
	}
	// First 16 bytes = RAND, last 16 bytes = AUTN
	if !bytesEqual(decoded[:16], av.RAND) {
		t.Fatal("nonce RAND portion does not match AV.RAND")
	}
	if !bytesEqual(decoded[16:], av.AUTN) {
		t.Fatal("nonce AUTN portion does not match AV.AUTN")
	}
}

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
