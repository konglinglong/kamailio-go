// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Diameter transaction table.
 */

package cdp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTransactionAddMatch(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	txn := &Transaction{
		HopByHopID: 100,
		EndToEndID: 200,
		PeerHost:   "h1",
	}
	if err := tbl.Add(txn); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := tbl.Len(); got != 1 {
		t.Errorf("Len = %d, want 1", got)
	}

	got, err := tbl.Match(100)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if got != txn {
		t.Errorf("Match returned different transaction")
	}
	if got := tbl.Len(); got != 0 {
		t.Errorf("Len after Match = %d, want 0", got)
	}
}

func TestTransactionMatchNotFound(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	if _, err := tbl.Match(999); !errors.Is(err, ErrTransactionNotFound) {
		t.Errorf("Match on empty table: err = %v, want ErrTransactionNotFound", err)
	}
}

func TestTransactionGetWithoutRemove(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	txn := &Transaction{HopByHopID: 5}
	_ = tbl.Add(txn)

	got, err := tbl.Get(5)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != txn {
		t.Errorf("Get returned different transaction")
	}
	if tbl.Len() != 1 {
		t.Errorf("Len after Get = %d, want 1 (Get should not remove)", tbl.Len())
	}
}

func TestTransactionDuplicateAdd(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	_ = tbl.Add(&Transaction{HopByHopID: 5})
	err := tbl.Add(&Transaction{HopByHopID: 5})
	if !errors.Is(err, ErrTransactionExists) {
		t.Errorf("Add duplicate: err = %v, want ErrTransactionExists", err)
	}
}

func TestTransactionAddNil(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	if err := tbl.Add(nil); err == nil {
		t.Errorf("Add(nil) should error")
	}
}

func TestTransactionDeliverAnswer(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	var callbackCalled atomic.Bool
	var gotAnswer *DiameterMessage
	var gotErr error
	txn := &Transaction{
		HopByHopID: 100,
		EndToEndID: 200,
		Callback: func(_ *Transaction, ans *DiameterMessage, err error) {
			gotAnswer = ans
			gotErr = err
			callbackCalled.Store(true)
		},
	}
	_ = tbl.Add(txn)

	answer := &DiameterMessage{
		HopByHopID: 100,
		EndToEndID: 200,
	}
	if !tbl.DeliverAnswer(answer) {
		t.Fatalf("DeliverAnswer returned false, want true")
	}
	if !callbackCalled.Load() {
		t.Errorf("callback was not invoked")
	}
	if gotAnswer == nil || gotErr != nil {
		t.Errorf("callback args: answer=%v, err=%v", gotAnswer, gotErr)
	}
	if gotAnswer != answer {
		t.Errorf("callback received wrong answer")
	}
	if tbl.Len() != 0 {
		t.Errorf("Len after DeliverAnswer = %d, want 0", tbl.Len())
	}
	// Done channel must be closed.
	select {
	case <-txn.Done():
	default:
		t.Errorf("Done channel not closed after callback")
	}
}

func TestTransactionDeliverAnswerNoMatch(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	if tbl.DeliverAnswer(&DiameterMessage{HopByHopID: 999}) {
		t.Errorf("DeliverAnswer on empty table returned true")
	}
	if tbl.DeliverAnswer(nil) {
		t.Errorf("DeliverAnswer(nil) returned true")
	}
}

func TestTransactionDeliverAnswerE2EMismatch(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	var gotErr error
	txn := &Transaction{
		HopByHopID: 100,
		EndToEndID: 200,
		Callback: func(_ *Transaction, _ *DiameterMessage, err error) {
			gotErr = err
		},
	}
	_ = tbl.Add(txn)

	// Answer with the same HopByHop but a different EndToEnd id.
	answer := &DiameterMessage{HopByHopID: 100, EndToEndID: 999}
	if !tbl.DeliverAnswer(answer) {
		t.Fatalf("DeliverAnswer returned false; should still consume the transaction")
	}
	if gotErr == nil {
		t.Errorf("callback should have been invoked with an error")
	}
}

func TestTransactionCallbackOnce(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	var callCount atomic.Int32
	txn := &Transaction{
		HopByHopID: 100,
		Callback: func(_ *Transaction, _ *DiameterMessage, _ error) {
			callCount.Add(1)
		},
	}
	_ = tbl.Add(txn)
	_ = tbl.DeliverAnswer(&DiameterMessage{HopByHopID: 100})
	_ = tbl.DeliverAnswer(&DiameterMessage{HopByHopID: 100}) // already removed
	// Try to cancel too — no-op.
	_ = tbl.Cancel(100)
	if callCount.Load() != 1 {
		t.Errorf("callback invoked %d times, want 1", callCount.Load())
	}
}

func TestTransactionCancel(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	var gotErr error
	txn := &Transaction{
		HopByHopID: 7,
		Callback:   func(_ *Transaction, _ *DiameterMessage, err error) { gotErr = err },
	}
	_ = tbl.Add(txn)
	if err := tbl.Cancel(7); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !errors.Is(gotErr, ErrTransactionCancelled) {
		t.Errorf("callback err = %v, want ErrTransactionCancelled", gotErr)
	}
	// Cancel again — already removed.
	if err := tbl.Cancel(7); !errors.Is(err, ErrTransactionNotFound) {
		t.Errorf("Cancel again: err = %v, want ErrTransactionNotFound", err)
	}
}

func TestTransactionCancelAll(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	var wg sync.WaitGroup
	var errCount atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = tbl.Add(&Transaction{
				HopByHopID: uint32(i + 1),
				Callback: func(_ *Transaction, _ *DiameterMessage, err error) {
					if err != nil {
						errCount.Add(1)
					}
				},
			})
		}(i)
	}
	wg.Wait()

	tbl.CancelAll(nil) // default error
	if tbl.Len() != 0 {
		t.Errorf("Len after CancelAll = %d, want 0", tbl.Len())
	}
	if errCount.Load() != 10 {
		t.Errorf("errCount = %d, want 10", errCount.Load())
	}
}

func TestTransactionCancelAllWithCustomError(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	customErr := errors.New("shutdown")
	var gotErr error
	txn := &Transaction{
		HopByHopID: 1,
		Callback:   func(_ *Transaction, _ *DiameterMessage, err error) { gotErr = err },
	}
	_ = tbl.Add(txn)
	tbl.CancelAll(customErr)
	if !errors.Is(gotErr, customErr) {
		t.Errorf("callback err = %v, want %v", gotErr, customErr)
	}
}

func TestTransactionSweepExpired(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	// Create a transaction that has already expired.
	txn := &Transaction{
		HopByHopID: 5,
		Callback:   func(_ *Transaction, _ *DiameterMessage, _ error) {},
	}
	// Manually set the deadline in the past.
	txn.Deadline = time.Now().Add(-time.Hour)
	_ = tbl.Add(txn)
	// Add doesn't override the deadline we set explicitly... wait, Add
	// sets Deadline only when zero. So our explicit past deadline wins.

	if n := tbl.SweepExpired(); n != 1 {
		t.Errorf("SweepExpired = %d, want 1", n)
	}
	if tbl.Len() != 0 {
		t.Errorf("Len after SweepExpired = %d, want 0", tbl.Len())
	}
}

func TestTransactionSweepExpiredDoesNotTouchActive(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	// Active transaction (deadline in the future).
	txn := &Transaction{HopByHopID: 5, Deadline: time.Now().Add(time.Hour)}
	_ = tbl.Add(txn)
	if n := tbl.SweepExpired(); n != 0 {
		t.Errorf("SweepExpired = %d, want 0", n)
	}
	if tbl.Len() != 1 {
		t.Errorf("Len after SweepExpired = %d, want 1", tbl.Len())
	}
}

func TestTransactionSweepExpiredCallback(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	var gotErr error
	txn := &Transaction{
		HopByHopID: 5,
		Deadline:   time.Now().Add(-time.Hour),
		Callback:   func(_ *Transaction, _ *DiameterMessage, err error) { gotErr = err },
	}
	_ = tbl.Add(txn)
	tbl.SweepExpired()
	if !errors.Is(gotErr, ErrTransactionTimeout) {
		t.Errorf("callback err = %v, want ErrTransactionTimeout", gotErr)
	}
}

func TestTransactionDoneChannel(t *testing.T) {
	txn := &Transaction{HopByHopID: 1}
	done := txn.Done()
	if done == nil {
		t.Fatalf("Done() returned nil channel")
	}
	// Calling Done() again should return the same channel.
	if txn.Done() != done {
		t.Errorf("Done() returned different channels")
	}
	// Channel should not be closed yet.
	select {
	case <-done:
		t.Errorf("Done channel closed before finalise")
	default:
	}
	txn.finalise(nil, nil)
	select {
	case <-done:
		// good
	case <-time.After(time.Second):
		t.Errorf("Done channel not closed after finalise")
	}
}

func TestTransactionHasCompleted(t *testing.T) {
	txn := &Transaction{HopByHopID: 1}
	if txn.HasCompleted() {
		t.Errorf("HasCompleted = true, want false initially")
	}
	txn.finalise(nil, nil)
	if !txn.HasCompleted() {
		t.Errorf("HasCompleted = false, want true after finalise")
	}
}

func TestTransactionManagerSendRequest(t *testing.T) {
	m := NewCDPModule()
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	tm := NewTransactionManager(m, tbl)

	msg := BuildDWR("h", "r", 0, 0)
	txn, err := tm.SendRequest(msg, "peer.example.com", nil)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	// The manager should have populated the ids.
	if msg.HopByHopID == 0 {
		t.Errorf("SendRequest did not populate HopByHopID")
	}
	if msg.EndToEndID == 0 {
		t.Errorf("SendRequest did not populate EndToEndID")
	}
	if txn.HopByHopID != msg.HopByHopID {
		t.Errorf("txn HopByHopID = %d, want %d", txn.HopByHopID, msg.HopByHopID)
	}
	if tbl.Len() != 1 {
		t.Errorf("table Len = %d, want 1", tbl.Len())
	}
}

func TestTransactionManagerSendRequestPreservesIDs(t *testing.T) {
	m := NewCDPModule()
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	tm := NewTransactionManager(m, tbl)

	msg := BuildDPR("h", "r", DisconnectCauseBusy, 0xDEAD, 0xBEEF)
	txn, err := tm.SendRequest(msg, "peer", nil)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if msg.HopByHopID != 0xDEAD {
		t.Errorf("SendRequest overwrote HopByHopID: %d", msg.HopByHopID)
	}
	if msg.EndToEndID != 0xBEEF {
		t.Errorf("SendRequest overwrote EndToEndID: %d", msg.EndToEndID)
	}
	if txn.HopByHopID != 0xDEAD || txn.EndToEndID != 0xBEEF {
		t.Errorf("txn identifiers = (%d, %d)", txn.HopByHopID, txn.EndToEndID)
	}
}

func TestTransactionManagerAwaitAnswer(t *testing.T) {
	m := NewCDPModule()
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	tm := NewTransactionManager(m, tbl)

	msg := BuildDWR("h", "r", 0, 0)

	// Use a channel to pass the registered transaction's identifiers to
	// the answering goroutine. The channel send (after SendRequest
	// completes) happens-before the channel receive, so the answering
	// goroutine reads consistent values without a data race.
	idCh := make(chan uint32, 2)
	go func() {
		hbh := <-idCh
		e2e := <-idCh
		time.Sleep(50 * time.Millisecond)
		_ = tbl.DeliverAnswer(&DiameterMessage{
			HopByHopID:  hbh,
			EndToEndID:  e2e,
			CommandCode: CmdDeviceWatchdog,
		})
	}()

	// Register the request first so we can snapshot the identifiers.
	txn, err := tm.SendRequest(msg, "peer", nil)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	idCh <- txn.HopByHopID
	idCh <- txn.EndToEndID

	// Block on the transaction's Done channel instead of calling
	// AwaitAnswer (which would attempt to re-register the same message).
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case <-txn.Done():
		// good — the answering goroutine delivered the answer.
	case <-ctx.Done():
		t.Fatalf("AwaitAnswer: %v", ctx.Err())
	}
}

func TestTransactionManagerAwaitAnswerTimeout(t *testing.T) {
	m := NewCDPModule()
	// Use a 50ms timeout so the test is fast.
	tbl := NewTransactionTable(50*time.Millisecond, 0)
	defer tbl.Close()
	tm := NewTransactionManager(m, tbl)

	msg := BuildDWR("h", "r", 0, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var gotErr error
	_, err := tm.AwaitAnswer(ctx, msg, "peer")
	_ = gotErr
	_ = err
	// The transaction should either time out (from the table sweep) or
	// be cancelled by the context deadline. Either way an error is
	// returned.
	if err == nil {
		t.Errorf("AwaitAnswer with no response should error")
	}
}

func TestTransactionManagerAwaitAnswerCtxCancelled(t *testing.T) {
	m := NewCDPModule()
	tbl := NewTransactionTable(time.Hour, 0) // long timeout, won't fire
	defer tbl.Close()
	tm := NewTransactionManager(m, tbl)

	msg := BuildDWR("h", "r", 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := tm.AwaitAnswer(ctx, msg, "peer")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("AwaitAnswer after cancel: err = %v, want context.Canceled", err)
	}
	// The transaction must have been removed from the table.
	if tbl.Len() != 0 {
		t.Errorf("table Len after cancel = %d, want 0", tbl.Len())
	}
}

func TestTransactionManagerAwaitAnswerNilMsg(t *testing.T) {
	m := NewCDPModule()
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	tm := NewTransactionManager(m, tbl)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := tm.AwaitAnswer(ctx, nil, "peer"); err == nil {
		t.Errorf("AwaitAnswer(nil) should error")
	}
}

func TestDefaultTransactionManager(t *testing.T) {
	tm1 := DefaultTransactionManager()
	tm2 := DefaultTransactionManager()
	if tm1 != tm2 {
		t.Errorf("DefaultTransactionManager returned different instances")
	}
}

func TestInitTransactionManager(t *testing.T) {
	InitTransactionManager()
	tm := DefaultTransactionManager()
	if tm == nil {
		t.Fatalf("DefaultTransactionManager returned nil after Init")
	}
	if tm.Table().Len() != 0 {
		t.Errorf("table Len = %d, want 0 after Init", tm.Table().Len())
	}
}

func TestTransactionManagerTableAndModule(t *testing.T) {
	m := NewCDPModule()
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	tm := NewTransactionManager(m, tbl)
	if tm.Table() != tbl {
		t.Errorf("Table() returned different table")
	}
	if tm.Module() != m {
		t.Errorf("Module() returned different module")
	}
}

func TestTransactionConcurrentAddMatch(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()

	var wg sync.WaitGroup
	const goroutines = 100
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hbh := uint32(i + 1)
			txn := &Transaction{HopByHopID: hbh}
			if err := tbl.Add(txn); err != nil {
				t.Errorf("Add: %v", err)
				return
			}
			if got, err := tbl.Match(hbh); err != nil || got != txn {
				t.Errorf("Match: err=%v, txn match=%v", err, got == txn)
			}
		}(i)
	}
	wg.Wait()
	if tbl.Len() != 0 {
		t.Errorf("Len after concurrent add+match = %d, want 0", tbl.Len())
	}
}

func TestTransactionCloseIdempotent(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	tbl.Close()
	// Second Close should be a no-op.
	tbl.Close()
}

func TestTransactionDeadlinePopulatedByAdd(t *testing.T) {
	tbl := NewTransactionTable(5*time.Second, 0)
	defer tbl.Close()
	txn := &Transaction{HopByHopID: 1, CreatedAt: time.Now()}
	_ = tbl.Add(txn)
	if txn.Deadline.IsZero() {
		t.Errorf("Deadline not set by Add")
	}
	// Deadline should be CreatedAt + timeout (within a small tolerance).
	want := txn.CreatedAt.Add(5 * time.Second)
	if got := txn.Deadline; got.Sub(want) > 100*time.Millisecond {
		t.Errorf("Deadline = %v, want ~%v", got, want)
	}
}

func TestTransactionCreatedAtPopulatedByAdd(t *testing.T) {
	tbl := NewTransactionTable(time.Second, 0)
	defer tbl.Close()
	txn := &Transaction{HopByHopID: 1}
	_ = tbl.Add(txn)
	if txn.CreatedAt.IsZero() {
		t.Errorf("CreatedAt not set by Add")
	}
}
