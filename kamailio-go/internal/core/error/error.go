// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Centralised error-code definitions and reporting - matching C error.c / error.h.
 *
 * Provides:
 *   - A typed set of Kamailio error codes (E_OK, E_UNSPEC, E_BUG, ...).
 *   - An Error wrapper that carries a code and a human-readable message and
 *     satisfies the standard error interface.
 *   - Thread-safe "last error" storage mirroring C's global ser_error /
 *     prev_ser_error variables.
 *   - Helpers to detect script control-flow errors (drop / return).
 *   - ErrorText / Err2ReasonPhrase ported from C error_text() and
 *     err2reason_phrase() so internal error codes can be mapped to SIP
 *     reply reason phrases.
 *
 * The package exposes both a thread-safe ErrorStore (the Go counterpart of
 * the C globals) and package-level helpers backed by a default singleton
 * store, following the project New() / Default*() / Init() convention.
 */

package error

import (
	"fmt"
	"sync"
)

// ErrCode is the Kamailio internal error-code type.
// C counterpart: the int values used throughout error.h (E_OK, E_UNSPEC, ...).
type ErrCode int

// Kamailio error codes.
//
// These mirror the defines in C core/error/error.h. The numeric values follow
// the project convention used by the Go port (sequential negatives for the
// common codes, with E_DROP / E_ABORT / E_RET reserved in a separate range
// for script control-flow signalling).
const (
	E_OK                ErrCode = 0
	E_UNSPEC            ErrCode = -1
	E_OUT_OF_MEM        ErrCode = -2
	E_BUG               ErrCode = -3
	E_CFG               ErrCode = -4
	E_NO_SOCKET         ErrCode = -5
	E_BAD_PARAM         ErrCode = -6
	E_BAD_URI           ErrCode = -7
	E_BAD_TUPEL         ErrCode = -8
	E_BAD_ROUTE         ErrCode = -9
	E_EXEC              ErrCode = -10
	E_TOO_MANY_BRANCHES ErrCode = -11
	E_RURI_PARSE        ErrCode = -12
	E_BAD_TO            ErrCode = -13
	E_BAD_FROM          ErrCode = -14
	E_BAD_REQ           ErrCode = -15
	E_BAD_VIA           ErrCode = -16
	E_BAD_MSG           ErrCode = -17
	E_NO_DEST           ErrCode = -18
	E_DROP              ErrCode = -127 // special: script drop
	E_ABORT             ErrCode = -128
	E_RET               ErrCode = -129 // script return
)

// Additional error codes that appear in C error.h with unique numeric values
// (i.e. they do not collide with the primary codes above). They are kept here
// so the Go port can produce the same reason phrases as the C core for
// send / address / cancellation / server errors.
const (
	E_UNEXPECTED_STATE ErrCode = -20  // unexpected processing state
	E_SEND             ErrCode = -477 // error on sending to next hop
	E_BAD_ADDRESS      ErrCode = -478 // unresolvable next-hop address
	E_BAD_PROTO        ErrCode = -480 // bad protocol
	E_CANCELED         ErrCode = -487 // transaction already canceled
	E_BAD_SERVER       ErrCode = -500 // error in server
	E_ADM_PROHIBITED   ErrCode = -510 // administratively prohibited
	E_BLOCKLISTED      ErrCode = -520 // destination blocklisted
)

// MaxReasonLen mirrors C MAX_REASON_LEN.
const MaxReasonLen = 128

// Error wraps an error code together with a descriptive message. It
// satisfies the standard error interface so it can be passed through any
// API that expects error.
//
// C counterpart: there is no direct struct equivalent in C; the C core uses
// the bare int ser_error global plus ad-hoc LM_ERR logging. The Go port
// bundles the code and message into a value type for ergonomic handling.
type Error struct {
	Code ErrCode
	Msg  string
}

// Error implements the error interface. The format is
// "<message> (code <n>)" which keeps the numeric code visible for
// debugging while remaining human-readable.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Msg == "" {
		return fmt.Sprintf("kamailio error (code %d)", int(e.Code))
	}
	return fmt.Sprintf("%s (code %d)", e.Msg, int(e.Code))
}

// New creates an Error from an error code and an optional message.
// A nil message yields an Error whose text is derived solely from the code.
func New(code ErrCode, msg string) *Error {
	return &Error{Code: code, Msg: msg}
}

// Newf is the printf-style variant of New.
func Newf(code ErrCode, format string, args ...interface{}) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// IsDrop reports whether err represents a script "drop" control-flow signal.
// In Kamailio's C core, drop is signalled by returning E_DROP from a script
// function; the Go port encodes the same intent inside an *Error.
func IsDrop(err error) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*Error); ok && e != nil {
		return e.Code == E_DROP
	}
	return false
}

// IsReturn reports whether err represents a script "return" control-flow
// signal (E_RET).
func IsReturn(err error) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*Error); ok && e != nil {
		return e.Code == E_RET
	}
	return false
}

// IsAbort reports whether err represents an abort control-flow signal (E_ABORT).
func IsAbort(err error) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*Error); ok && e != nil {
		return e.Code == E_ABORT
	}
	return false
}

// FromGoError converts a standard Go error into a Kamailio *Error. A nil
// input yields nil. If the error is already an *Error it is returned
// unchanged; otherwise it is wrapped with E_UNSPEC so the original message
// is preserved.
func FromGoError(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	return &Error{Code: E_UNSPEC, Msg: err.Error()}
}

// ============================================================
// Thread-safe "last error" storage
// ============================================================

// ErrorStore is the thread-safe counterpart of C's global ser_error /
// prev_ser_error pair. It remembers the most recent error so that callers
// can inspect it after a function returns, exactly as C code inspects
// ser_error.
type ErrorStore struct {
	mu   sync.RWMutex
	last *Error
	prev *Error
}

// NewErrorStore creates an empty ErrorStore.
func NewErrorStore() *ErrorStore {
	return &ErrorStore{}
}

// SetLast records err as the current error, pushing the previous current
// error into the "prev" slot (mirroring C's prev_ser_error = ser_error
// assignment). A nil err clears the current error.
func (s *ErrorStore) SetLast(err *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prev = s.last
	s.last = err
}

// LastError returns the most recently set error, or nil if none has been
// set (or if the last error was cleared).
func (s *ErrorStore) LastError() *Error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.last
}

// PrevError returns the error that was current before the most recent
// SetLast call, mirroring C's prev_ser_error.
func (s *ErrorStore) PrevError() *Error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prev
}

// LastCode returns the code of the most recent error, or E_OK if none.
func (s *ErrorStore) LastCode() ErrCode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.last == nil {
		return E_OK
	}
	return s.last.Code
}

// Clear resets both the current and previous errors.
func (s *ErrorStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = nil
	s.prev = nil
}

// defaultStore is the process-wide ErrorStore used by the package-level
// helpers. defaultMu guards the pointer; Init replaces it.
var (
	defaultStore *ErrorStore
	defaultMu    sync.RWMutex
)

// DefaultErrorStore returns the process-wide ErrorStore, creating it on
// first use (lazy initialisation with double-checked locking).
func DefaultErrorStore() *ErrorStore {
	defaultMu.RLock()
	s := defaultStore
	defaultMu.RUnlock()
	if s != nil {
		return s
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultStore == nil {
		defaultStore = &ErrorStore{}
	}
	return defaultStore
}

// Init (re)initialises the process-wide ErrorStore to an empty state.
// It is safe to call multiple times and is intended for test isolation.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultStore = &ErrorStore{}
}

// SetLast sets the last error on the process-wide store.
func SetLast(err *Error) {
	DefaultErrorStore().SetLast(err)
}

// LastError returns the last error from the process-wide store.
func LastError() *Error {
	return DefaultErrorStore().LastError()
}

// PrevError returns the previous error from the process-wide store.
func PrevError() *Error {
	return DefaultErrorStore().PrevError()
}

// LastCode returns the code of the last error from the process-wide store.
func LastCode() ErrCode {
	return DefaultErrorStore().LastCode()
}

// ============================================================
// SIP reason-phrase mapping (ported from C error.c)
// ============================================================

// ErrorText returns the SIP reason phrase for a numeric SIP status code,
// matching C's error_text(). For unknown codes it falls back to the
// class-level generic phrase ("Provisional", "Successful", ...).
func ErrorText(code int) string {
	switch code {
	case 100:
		return "Trying"
	case 180:
		return "Ringing"
	case 181:
		return "Call Is Being Forwarded"
	case 182:
		return "Queued"
	case 183:
		return "Session Progress"

	case 200:
		return "OK"

	case 300:
		return "Multiple Choices"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Moved Temporarily"
	case 305:
		return "Use Proxy"
	case 380:
		return "Alternative Service"

	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 402:
		return "Payment Required"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 406:
		return "Not Acceptable"
	case 407:
		return "Proxy Authentication Required"
	case 408:
		return "Request Timeout"
	case 410:
		return "Gone"
	case 413:
		return "Request Entity Too Large"
	case 414:
		return "Request-URI Too Long"
	case 415:
		return "Unsupported Media Type"
	case 416:
		return "Unsupported URI Scheme"
	case 417:
		return "Bad Extension"
	case 421:
		return "Extension Required"
	case 423:
		return "Interval Too Brief"
	case 480:
		return "Temporarily Unavailable"
	case 481:
		return "Call/Transaction Does Not Exist"
	case 482:
		return "Loop Detected"
	case 483:
		return "Too Many Hops"
	case 484:
		return "Address Incomplete"
	case 485:
		return "Ambiguous"
	case 486:
		return "Busy Here"
	case 487:
		return "Request Terminated"
	case 488:
		return "Not Acceptable Here"
	case 491:
		return "Request Pending"

	case 500:
		return "Server Internal Error"
	case 501:
		return "Not Implemented"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	case 504:
		return "Server Time-Out"
	case 505:
		return "Version Not Supported"
	case 513:
		return "Message Too Large"

	case 600:
		return "Busy Everywhere"
	case 603:
		return "Decline"
	case 604:
		return "Does Not Exist Anywhere"
	case 606:
		return "Not Acceptable"
	}

	// Class-level fallbacks (matching C error_text default branches).
	switch {
	case code >= 600:
		return "Global Failure"
	case code >= 500:
		return "Server Failure"
	case code >= 400:
		return "Request Failure"
	case code >= 300:
		return "Redirection"
	case code >= 200:
		return "Successful"
	case code >= 100:
		return "Provisional"
	default:
		return "Unspecified"
	}
}

// ReasonResult holds the output of Err2ReasonPhrase: the human-readable
// phrase and the corresponding SIP status code.
type ReasonResult struct {
	Phrase  string
	SIPCode int
}

// Err2ReasonPhrase maps a Kamailio internal error code to a SIP reply
// reason phrase and status code, mirroring C's err2reason_phrase().
//
// The signature differs from C (which writes into a caller buffer and an
// out-parameter) but the mapping table is identical. The optional
// signature string is appended in the same "(<code>/<signature>)" format
// used by the C implementation.
func Err2ReasonPhrase(intError ErrCode, signature string) ReasonResult {
	var txt string
	var sip int

	switch intError {
	case E_SEND:
		txt = "Unfortunately error on sending to next hop occurred"
		sip = -int(intError)
	case E_BAD_ADDRESS:
		txt = "Unresolvable destination"
		sip = -int(intError)
	case E_BAD_REQ:
		txt = "Bad Request"
		sip = -int(intError)
	case E_BAD_URI:
		txt = "Regretfully, we were not able to process the URI"
		sip = -int(intError)
	case E_BAD_TUPEL:
		txt = "Transaction tuple incomplete"
		sip = -int(E_BAD_REQ)
	case E_BAD_TO:
		txt = "Bad To"
		sip = -int(E_BAD_REQ)
	case E_EXEC:
		txt = "Error in external logic"
		sip = -int(E_BAD_SERVER)
	case E_TOO_MANY_BRANCHES:
		txt = "Forking capacity exceeded"
		sip = -int(E_BAD_SERVER)
	case E_CANCELED:
		txt = "Transaction canceled"
		sip = -int(intError)
	case E_OUT_OF_MEM:
		txt = "Message processing error"
		sip = 500
	case E_UNEXPECTED_STATE:
		txt = "Internal processing error"
		sip = 500
	case E_OK:
		txt = "No error"
		sip = 500
	default:
		txt = "I'm terribly sorry, server error occurred"
		sip = 500
	}

	var phrase string
	if signature != "" {
		phrase = fmt.Sprintf("%s (%d/%s)", txt, -int(intError), signature)
	} else {
		phrase = fmt.Sprintf("%s (%d)", txt, -int(intError))
	}
	if len(phrase) > MaxReasonLen {
		phrase = phrase[:MaxReasonLen]
	}
	return ReasonResult{Phrase: phrase, SIPCode: sip}
}
