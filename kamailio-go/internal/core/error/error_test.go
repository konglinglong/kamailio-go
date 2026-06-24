// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the error package - matching C error.c / error.h.
 */

package error

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// ============================================================
// Error construction and Error() interface
// ============================================================

// TestNew verifies that New sets the code and message fields correctly.
func TestNew(t *testing.T) {
	e := New(E_BUG, "internal bug")
	if e.Code != E_BUG {
		t.Errorf("Code = %d, want %d", e.Code, E_BUG)
	}
	if e.Msg != "internal bug" {
		t.Errorf("Msg = %q, want %q", e.Msg, "internal bug")
	}
}

// TestNewf verifies the printf-style constructor.
func TestNewf(t *testing.T) {
	e := Newf(E_CFG, "bad config %s=%d", "timeout", 30)
	if e.Code != E_CFG {
		t.Errorf("Code = %d, want %d", e.Code, E_CFG)
	}
	want := "bad config timeout=30"
	if e.Msg != want {
		t.Errorf("Msg = %q, want %q", e.Msg, want)
	}
}

// TestErrorString verifies the Error() output format for both message and
// message-less errors.
func TestErrorString(t *testing.T) {
	e := New(E_OUT_OF_MEM, "out of memory")
	got := e.Error()
	want := "out of memory (code -2)"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	// Empty message falls back to a code-only format.
	e2 := New(E_UNSPEC, "")
	got2 := e2.Error()
	want2 := "kamailio error (code -1)"
	if got2 != want2 {
		t.Errorf("Error() empty msg = %q, want %q", got2, want2)
	}
}

// TestNilErrorString verifies that a nil *Error returns "<nil>".
func TestNilErrorString(t *testing.T) {
	var e *Error
	if got := e.Error(); got != "<nil>" {
		t.Errorf("nil Error() = %q, want %q", got, "<nil>")
	}
}

// TestErrorSatisfiesInterface verifies that *Error implements the error
// interface so it can be used in standard error-returning functions.
func TestErrorSatisfiesInterface(t *testing.T) {
	var err error = New(E_BAD_PARAM, "bad parameter")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !IsAbort(err) && err.Error() == "" {
		t.Error("error interface not satisfied")
	}
}

// ============================================================
// IsDrop / IsReturn / IsAbort
// ============================================================

// TestIsDrop verifies that IsDrop returns true only for E_DROP errors.
func TestIsDrop(t *testing.T) {
	if IsDrop(nil) {
		t.Error("IsDrop(nil) = true, want false")
	}
	if IsDrop(New(E_BUG, "bug")) {
		t.Error("IsDrop(E_BUG) = true, want false")
	}
	if !IsDrop(New(E_DROP, "script drop")) {
		t.Error("IsDrop(E_DROP) = false, want true")
	}
	// A non-*Error error must not be considered a drop.
	if IsDrop(errors.New("std error")) {
		t.Error("IsDrop(std error) = true, want false")
	}
}

// TestIsReturn verifies that IsReturn returns true only for E_RET errors.
func TestIsReturn(t *testing.T) {
	if IsReturn(nil) {
		t.Error("IsReturn(nil) = true, want false")
	}
	if IsReturn(New(E_DROP, "drop")) {
		t.Error("IsReturn(E_DROP) = true, want false")
	}
	if !IsReturn(New(E_RET, "script return")) {
		t.Error("IsReturn(E_RET) = false, want true")
	}
	if IsReturn(errors.New("std error")) {
		t.Error("IsReturn(std error) = true, want false")
	}
}

// TestIsAbort verifies that IsAbort returns true only for E_ABORT errors.
func TestIsAbort(t *testing.T) {
	if IsAbort(nil) {
		t.Error("IsAbort(nil) = true, want false")
	}
	if !IsAbort(New(E_ABORT, "abort")) {
		t.Error("IsAbort(E_ABORT) = false, want true")
	}
	if IsAbort(errors.New("std error")) {
		t.Error("IsAbort(std error) = true, want false")
	}
}

// ============================================================
// FromGoError
// ============================================================

// TestFromGoError verifies conversion from standard Go errors.
func TestFromGoError(t *testing.T) {
	// nil input yields nil.
	if e := FromGoError(nil); e != nil {
		t.Errorf("FromGoError(nil) = %v, want nil", e)
	}

	// A standard error is wrapped with E_UNSPEC.
	stdErr := fmt.Errorf("network timeout")
	e := FromGoError(stdErr)
	if e.Code != E_UNSPEC {
		t.Errorf("Code = %d, want %d", e.Code, E_UNSPEC)
	}
	if e.Msg != "network timeout" {
		t.Errorf("Msg = %q, want %q", e.Msg, "network timeout")
	}

	// An existing *Error is returned unchanged (same pointer).
	original := New(E_BAD_URI, "bad uri")
	e2 := FromGoError(original)
	if e2 != original {
		t.Error("FromGoError did not return the same *Error pointer")
	}
}

// ============================================================
// ErrorStore (thread-safe last-error storage)
// ============================================================

// TestErrorStoreBasic verifies SetLast / LastError / PrevError / LastCode.
func TestErrorStoreBasic(t *testing.T) {
	s := NewErrorStore()

	// Initially empty.
	if e := s.LastError(); e != nil {
		t.Errorf("LastError on fresh store = %v, want nil", e)
	}
	if c := s.LastCode(); c != E_OK {
		t.Errorf("LastCode on fresh store = %d, want %d", c, E_OK)
	}

	// Set first error.
	e1 := New(E_BUG, "first bug")
	s.SetLast(e1)
	if s.LastError() != e1 {
		t.Error("LastError does not return e1")
	}
	if s.LastCode() != E_BUG {
		t.Errorf("LastCode = %d, want %d", s.LastCode(), E_BUG)
	}

	// Set second error; previous must now be e1.
	e2 := New(E_CFG, "config error")
	s.SetLast(e2)
	if s.LastError() != e2 {
		t.Error("LastError does not return e2")
	}
	if s.PrevError() != e1 {
		t.Error("PrevError does not return e1")
	}
}

// TestErrorStoreClear verifies that Clear resets both slots.
func TestErrorStoreClear(t *testing.T) {
	s := NewErrorStore()
	s.SetLast(New(E_BUG, "bug"))
	s.SetLast(New(E_CFG, "cfg"))

	s.Clear()
	if s.LastError() != nil {
		t.Error("LastError after Clear != nil")
	}
	if s.PrevError() != nil {
		t.Error("PrevError after Clear != nil")
	}
	if s.LastCode() != E_OK {
		t.Errorf("LastCode after Clear = %d, want %d", s.LastCode(), E_OK)
	}
}

// TestErrorStoreNilSet verifies that setting nil clears the current error
// while preserving the previous one in the prev slot.
func TestErrorStoreNilSet(t *testing.T) {
	s := NewErrorStore()
	e1 := New(E_BUG, "bug")
	s.SetLast(e1)
	s.SetLast(nil)

	if s.LastError() != nil {
		t.Error("LastError after SetLast(nil) != nil")
	}
	if s.PrevError() != e1 {
		t.Error("PrevError after SetLast(nil) should be e1")
	}
}

// ============================================================
// Package-level singleton (DefaultErrorStore / Init)
// ============================================================

// TestDefaultStoreAndInit verifies the process-wide singleton lifecycle.
func TestDefaultStoreAndInit(t *testing.T) {
	Init()

	s1 := DefaultErrorStore()
	if s1 == nil {
		t.Fatal("DefaultErrorStore returned nil")
	}
	// A second call must return the same instance.
	s2 := DefaultErrorStore()
	if s1 != s2 {
		t.Error("DefaultErrorStore returned different instances")
	}

	// Set and retrieve via package helpers.
	SetLast(New(E_OUT_OF_MEM, "oom"))
	if LastError() == nil {
		t.Fatal("LastError() = nil after SetLast")
	}
	if LastCode() != E_OUT_OF_MEM {
		t.Errorf("LastCode() = %d, want %d", LastCode(), E_OUT_OF_MEM)
	}

	// Init must reset to a fresh store.
	Init()
	if LastError() != nil {
		t.Error("LastError() != nil after Init")
	}
	if LastCode() != E_OK {
		t.Errorf("LastCode() after Init = %d, want %d", LastCode(), E_OK)
	}
}

// TestPackageHelpers verifies the package-level SetLast / LastError / PrevError.
func TestPackageHelpers(t *testing.T) {
	Init()

	e1 := New(E_BAD_REQ, "bad request")
	SetLast(e1)
	if LastError() != e1 {
		t.Error("LastError does not match e1")
	}
	if PrevError() != nil {
		t.Error("PrevError should be nil after first SetLast")
	}

	e2 := New(E_BAD_TO, "bad to")
	SetLast(e2)
	if LastError() != e2 {
		t.Error("LastError does not match e2")
	}
	if PrevError() != e1 {
		t.Error("PrevError does not match e1")
	}
}

// ============================================================
// ErrorText (SIP status code -> reason phrase)
// ============================================================

// TestErrorText verifies the SIP status code to reason phrase mapping.
func TestErrorText(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{100, "Trying"},
		{180, "Ringing"},
		{200, "OK"},
		{302, "Moved Temporarily"},
		{400, "Bad Request"},
		{404, "Not Found"},
		{487, "Request Terminated"},
		{500, "Server Internal Error"},
		{503, "Service Unavailable"},
		{603, "Decline"},
	}
	for _, tt := range tests {
		if got := ErrorText(tt.code); got != tt.want {
			t.Errorf("ErrorText(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

// TestErrorTextFallbacks verifies the class-level generic fallbacks for
// codes not explicitly listed.
func TestErrorTextFallbacks(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{150, "Provisional"},
		{250, "Successful"},
		{350, "Redirection"},
		{450, "Request Failure"},
		{550, "Server Failure"},
		{650, "Global Failure"},
		{0, "Unspecified"},
		{99, "Unspecified"},
	}
	for _, tt := range tests {
		if got := ErrorText(tt.code); got != tt.want {
			t.Errorf("ErrorText(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

// ============================================================
// Err2ReasonPhrase (internal error -> SIP reason phrase)
// ============================================================

// TestErr2ReasonPhrase verifies the mapping from internal error codes to
// SIP reason phrases and status codes.
func TestErr2ReasonPhrase(t *testing.T) {
	// E_BAD_REQ -> "Bad Request", sip code = 15
	r := Err2ReasonPhrase(E_BAD_REQ, "")
	if r.SIPCode != 15 {
		t.Errorf("E_BAD_REQ SIPCode = %d, want 15", r.SIPCode)
	}
	if r.Phrase == "" {
		t.Error("E_BAD_REQ phrase is empty")
	}

	// E_OUT_OF_MEM -> 500
	r = Err2ReasonPhrase(E_OUT_OF_MEM, "")
	if r.SIPCode != 500 {
		t.Errorf("E_OUT_OF_MEM SIPCode = %d, want 500", r.SIPCode)
	}

	// E_OK -> 500 (per C code)
	r = Err2ReasonPhrase(E_OK, "")
	if r.SIPCode != 500 {
		t.Errorf("E_OK SIPCode = %d, want 500", r.SIPCode)
	}

	// Unknown error -> default 500
	r = Err2ReasonPhrase(E_CFG, "")
	if r.SIPCode != 500 {
		t.Errorf("E_CFG SIPCode = %d, want 500 (default)", r.SIPCode)
	}
}

// TestErr2ReasonPhraseSignature verifies that the signature string is
// appended in the C "(code/sig)" format.
func TestErr2ReasonPhraseSignature(t *testing.T) {
	r := Err2ReasonPhrase(E_BAD_URI, "test-sig")
	// E_BAD_URI = -7, so -int = 7
	want := "Regretfully, we were not able to process the URI (7/test-sig)"
	if r.Phrase != want {
		t.Errorf("Phrase = %q, want %q", r.Phrase, want)
	}
}

// TestErr2ReasonPhraseMaxLength verifies that the phrase is truncated to
// MaxReasonLen.
func TestErr2ReasonPhraseMaxLength(t *testing.T) {
	longSig := make([]byte, 300)
	for i := range longSig {
		longSig[i] = 'x'
	}
	r := Err2ReasonPhrase(E_BAD_REQ, string(longSig))
	if len(r.Phrase) > MaxReasonLen {
		t.Errorf("Phrase length = %d, want <= %d", len(r.Phrase), MaxReasonLen)
	}
}

// ============================================================
// Concurrency tests (run with -race)
// ============================================================

// TestConcurrentErrorStore exercises the ErrorStore under concurrent
// SetLast / LastError / PrevError / LastCode access.
func TestConcurrentErrorStore(t *testing.T) {
	s := NewErrorStore()

	var wg sync.WaitGroup
	const goroutines = 100

	// Writers: set errors concurrently.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			code := ErrCode(-(n%20 + 1))
			s.SetLast(New(code, fmt.Sprintf("err-%d", n)))
		}(i)
	}

	// Readers: read concurrently.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = s.LastError()
			_ = s.PrevError()
			_ = s.LastCode()
		}()
	}

	wg.Wait()

	// After the storm, the store must still be usable.
	s.Clear()
	if s.LastError() != nil {
		t.Error("LastError after Clear != nil")
	}
}

// TestConcurrentDefaultStore exercises the package-level singleton under
// concurrent Init / SetLast / LastError access.
func TestConcurrentDefaultStore(t *testing.T) {
	Init()

	var wg sync.WaitGroup
	const goroutines = 50

	var setCount int64

	// Writers.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			SetLast(New(E_BUG, fmt.Sprintf("bug-%d", n)))
			atomic.AddInt64(&setCount, 1)
		}(i)
	}

	// Readers.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = LastError()
			_ = LastCode()
		}()
	}

	wg.Wait()

	if setCount != goroutines {
		t.Errorf("setCount = %d, want %d", setCount, goroutines)
	}

	// Final state must be valid.
	if LastError() == nil {
		t.Error("LastError() = nil after concurrent writes")
	}
}
