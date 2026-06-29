// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Diameter transaction table.
 * Port of the kamailio cdp module's transaction.c.
 *
 * A Diameter transaction is identified by the pair (end-to-end id,
 * hop-by-hop id). When a request is sent, the local hop-by-hop id is
 * generated fresh (per RFC 6733 §6.2); the end-to-end id is preserved
 * across proxy hops. The matching answer carries the same hop-by-hop and
 * end-to-end ids and is correlated back to the originating request.
 *
 * This Go port maintains an in-memory table indexed by hop-by-hop id
 * (the correlation key used by RFC 6733 §6.2). Each entry carries the
 * original request, the originating peer (so the answer can be routed
 * back), the callback to invoke when the answer arrives, and a timer
 * used to fail the transaction if no answer is received within the
 * configured timeout.
 *
 * It is safe for concurrent use.
 */

package cdp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrTransactionNotFound is returned when no transaction matches the
	// given correlation key.
	ErrTransactionNotFound = errors.New("cdp: transaction not found")
	// ErrTransactionExists is returned when a transaction with the same
	// correlation key is already present.
	ErrTransactionExists = errors.New("cdp: transaction already exists")
	// ErrTransactionTimeout is the error returned to a transaction's
	// callback when the Tw timer has elapsed without an answer.
	ErrTransactionTimeout = errors.New("cdp: transaction timed out")
	// ErrTransactionCancelled is returned to a transaction's callback
	// when the transaction has been cancelled by the caller.
	ErrTransactionCancelled = errors.New("cdp: transaction cancelled")
)

// ---------------------------------------------------------------------------
// Transaction
// ---------------------------------------------------------------------------

// Transaction captures an in-flight Diameter request awaiting an answer.
type Transaction struct {
	mu sync.Mutex

	// Correlation identifiers (RFC 6733 §6.2).
	HopByHopID uint32
	EndToEndID uint32

	// The originating request message (for retransmission / inspection).
	Request *DiameterMessage

	// The peer the request was sent to.
	PeerHost string

	// CreatedAt / Deadline for the Tw timer.
	CreatedAt time.Time
	Deadline  time.Time

	// Callback is invoked when the answer arrives or the transaction
	// times out. Exactly one of (answer, err) is non-nil when the
	// callback runs.
	Callback TransactionCallback

	// done is closed when the transaction has completed (answer received,
	// timed out or cancelled). Callers may wait on it.
	done chan struct{}

	// finalised is true once the callback has been invoked. Protected
	// by mu.
	finalised bool
}

// Done returns a channel that is closed when the transaction completes.
// The channel is created lazily under the mutex so that concurrent
// callers always observe the same channel.
func (t *Transaction) Done() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done == nil {
		t.done = make(chan struct{})
	}
	return t.done
}

// HasCompleted reports whether the transaction has been finalised.
func (t *Transaction) HasCompleted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.finalised
}

// finalise invokes the callback (if registered and not yet invoked) and
// closes the done channel. Returns true if the callback was actually
// invoked, false when the transaction was already finalised.
func (t *Transaction) finalise(answer *DiameterMessage, err error) bool {
	t.mu.Lock()
	if t.finalised {
		t.mu.Unlock()
		return false
	}
	t.finalised = true
	if t.done == nil {
		t.done = make(chan struct{})
	}
	cb := t.Callback
	t.mu.Unlock()
	// Invoke the callback outside the lock so that callbacks may
	// register new transactions without deadlocking.
	if cb != nil {
		cb(t, answer, err)
	}
	close(t.done)
	return true
}

// TransactionCallback is invoked when a transaction completes. The
// callback receives the transaction, the answer (nil on error) and the
// error (nil on success).
type TransactionCallback func(txn *Transaction, answer *DiameterMessage, err error)

// ---------------------------------------------------------------------------
// TransactionTable
// ---------------------------------------------------------------------------

// TransactionTable tracks in-flight Diameter transactions and matches
// incoming answers to their originating requests. It runs a background
// goroutine that periodically sweeps expired transactions and invokes
// their callbacks with ErrTransactionTimeout.
type TransactionTable struct {
	mu          sync.Mutex
	transactions map[uint32]*Transaction // keyed by HopByHopID
	timeout      time.Duration
	sweepInterval time.Duration
	stopCh       chan struct{}
	stopped      bool
}

// NewTransactionTable creates a TransactionTable with the given timeout
// (Tw). The sweep interval defaults to one second; pass 0 to disable
// background sweeping (call SweepExpired manually in tests).
func NewTransactionTable(timeout, sweepInterval time.Duration) *TransactionTable {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if sweepInterval < 0 {
		sweepInterval = 0
	}
	t := &TransactionTable{
		transactions:  make(map[uint32]*Transaction),
		timeout:       timeout,
		sweepInterval: sweepInterval,
		stopCh:        make(chan struct{}),
	}
	if sweepInterval > 0 {
		go t.sweepLoop()
	}
	return t
}

// Add registers a new transaction. The transaction must have a unique
// HopByHopID (returns ErrTransactionExists otherwise). The transaction's
// Deadline is set to CreatedAt + timeout if not already set.
//
//	C: cdp_add_transaction()
func (t *TransactionTable) Add(txn *Transaction) error {
	if txn == nil {
		return errors.New("cdp: nil transaction")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.transactions[txn.HopByHopID]; ok {
		return fmt.Errorf("%w: hop-by-hop=%d", ErrTransactionExists, txn.HopByHopID)
	}
	if txn.CreatedAt.IsZero() {
		txn.CreatedAt = time.Now()
	}
	if txn.Deadline.IsZero() {
		txn.Deadline = txn.CreatedAt.Add(t.timeout)
	}
	t.transactions[txn.HopByHopID] = txn
	return nil
}

// Match removes the transaction identified by hopByHopID from the table
// and returns it. Returns ErrTransactionNotFound when no transaction is
// registered for the given id.
//
//	C: cdp_match_transaction()
func (t *TransactionTable) Match(hopByHopID uint32) (*Transaction, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	txn, ok := t.transactions[hopByHopID]
	if !ok {
		return nil, fmt.Errorf("%w: hop-by-hop=%d", ErrTransactionNotFound, hopByHopID)
	}
	delete(t.transactions, hopByHopID)
	return txn, nil
}

// Get returns the transaction identified by hopByHopID without removing
// it. Returns ErrTransactionNotFound when absent.
func (t *TransactionTable) Get(hopByHopID uint32) (*Transaction, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	txn, ok := t.transactions[hopByHopID]
	if !ok {
		return nil, fmt.Errorf("%w: hop-by-hop=%d", ErrTransactionNotFound, hopByHopID)
	}
	return txn, nil
}

// Len returns the number of in-flight transactions.
func (t *TransactionTable) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.transactions)
}

// DeliverAnswer finds the transaction that produced the request whose
// answer is msg and invokes its callback. Returns true when a matching
// transaction was found and the callback was invoked, false otherwise.
//
//	C: cdp_deliver_answer()
func (t *TransactionTable) DeliverAnswer(msg *DiameterMessage) bool {
	if msg == nil {
		return false
	}
	txn, err := t.Match(msg.HopByHopID)
	if err != nil {
		return false
	}
	// Optionally validate the end-to-end id: RFC 6733 §6.2 says the
	// answer must carry the same end-to-end id as the request.
	if txn.EndToEndID != 0 && msg.EndToEndID != 0 && txn.EndToEndID != msg.EndToEndID {
		// Mismatch — log and reject the answer.
		txn.finalise(nil, fmt.Errorf("cdp: e2e mismatch (req=%d, ans=%d)",
			txn.EndToEndID, msg.EndToEndID))
		return true
	}
	txn.finalise(msg, nil)
	return true
}

// Cancel removes the transaction identified by hopByHopID and invokes
// its callback with ErrTransactionCancelled. Returns
// ErrTransactionNotFound when no transaction matches.
//
//	C: cdp_cancel_transaction()
func (t *TransactionTable) Cancel(hopByHopID uint32) error {
	txn, err := t.Match(hopByHopID)
	if err != nil {
		return err
	}
	txn.finalise(nil, ErrTransactionCancelled)
	return nil
}

// CancelAll removes every transaction and invokes its callback with the
// supplied error (defaulting to ErrTransactionCancelled). Used during
// shutdown.
func (t *TransactionTable) CancelAll(err error) {
	if err == nil {
		err = ErrTransactionCancelled
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for hbh, txn := range t.transactions {
		delete(t.transactions, hbh)
		// Drop the lock for the callback.
		t.mu.Unlock()
		txn.finalise(nil, err)
		t.mu.Lock()
	}
}

// Close stops the background sweep goroutine and cancels every
// outstanding transaction.
func (t *TransactionTable) Close() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true
	close(t.stopCh)
	t.mu.Unlock()
	t.CancelAll(ErrTransactionCancelled)
}

// sweepLoop periodically removes expired transactions.
func (t *TransactionTable) sweepLoop() {
	ticker := time.NewTicker(t.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.SweepExpired()
		}
	}
}

// SweepExpired removes every transaction whose deadline is in the past
// and invokes its callback with ErrTransactionTimeout. Returns the
// number of transactions expired.
func (t *TransactionTable) SweepExpired() int {
	now := time.Now()
	var expired []*Transaction
	t.mu.Lock()
	for hbh, txn := range t.transactions {
		if !txn.Deadline.IsZero() && now.After(txn.Deadline) {
			delete(t.transactions, hbh)
			expired = append(expired, txn)
		}
	}
	t.mu.Unlock()
	for _, txn := range expired {
		txn.finalise(nil, ErrTransactionTimeout)
	}
	return len(expired)
}

// ---------------------------------------------------------------------------
// TransactionManager — convenience wrapper around TransactionTable that
// handles registration, callback wiring and id assignment.
// ---------------------------------------------------------------------------

// TransactionManager bundles a TransactionTable with a CDPModule for
// id allocation. It is the high-level API the rest of the code uses to
// send a request and wait for the answer.
type TransactionManager struct {
	module *CDPModule
	table  *TransactionTable
}

// NewTransactionManager creates a TransactionManager backed by the given
// CDPModule and TransactionTable.
func NewTransactionManager(m *CDPModule, t *TransactionTable) *TransactionManager {
	if m == nil {
		m = NewCDPModule()
	}
	if t == nil {
		t = NewTransactionTable(30*time.Second, time.Second)
	}
	return &TransactionManager{module: m, table: t}
}

// SendRequest registers a transaction for msg, populates its hop-by-hop
// and end-to-end ids (if zero), and returns the transaction. The caller
// is responsible for actually writing msg to the wire (the transport
// layer is not invoked from here).
//
//	cb is invoked when the answer arrives or the transaction times out.
//	Pass nil to skip the callback and rely on Done() for synchronisation.
func (tm *TransactionManager) SendRequest(
	msg *DiameterMessage, peerHost string, cb TransactionCallback,
) (*Transaction, error) {
	if msg == nil {
		return nil, errors.New("cdp: nil message")
	}
	if msg.HopByHopID == 0 {
		msg.HopByHopID = tm.module.NextHopByHop()
	}
	if msg.EndToEndID == 0 {
		msg.EndToEndID = tm.module.NextEndToEnd()
	}
	txn := &Transaction{
		HopByHopID: msg.HopByHopID,
		EndToEndID: msg.EndToEndID,
		Request:    msg,
		PeerHost:   peerHost,
		CreatedAt:  time.Now(),
		Callback:   cb,
	}
	if err := tm.table.Add(txn); err != nil {
		return nil, err
	}
	return txn, nil
}

// AwaitAnswer is a convenience that registers a request and waits for
// the answer (or the timeout). It blocks the calling goroutine.
func (tm *TransactionManager) AwaitAnswer(
	ctx context.Context, msg *DiameterMessage, peerHost string,
) (*DiameterMessage, error) {
	type result struct {
		answer *DiameterMessage
		err    error
	}
	resCh := make(chan result, 1)
	txn, err := tm.SendRequest(msg, peerHost, func(_ *Transaction, ans *DiameterMessage, err error) {
		resCh <- result{ans, err}
	})
	if err != nil {
		return nil, err
	}
	select {
	case r := <-resCh:
		return r.answer, r.err
	case <-ctx.Done():
		_ = tm.table.Cancel(txn.HopByHopID)
		return nil, ctx.Err()
	case <-txn.Done():
		// Should not happen — the callback always writes to resCh.
		return nil, errors.New("cdp: transaction completed without callback")
	}
}

// Table returns the underlying transaction table.
func (tm *TransactionManager) Table() *TransactionTable { return tm.table }

// Module returns the underlying CDPModule.
func (tm *TransactionManager) Module() *CDPModule { return tm.module }

// ---------------------------------------------------------------------------
// Default singleton — mirrors the C module's process-wide transaction table.
// ---------------------------------------------------------------------------

var (
	defaultTxnMu       sync.Mutex
	defaultTxnManager *TransactionManager
)

// DefaultTransactionManager returns the process-wide TransactionManager,
// creating one on first use.
func DefaultTransactionManager() *TransactionManager {
	defaultTxnMu.Lock()
	defer defaultTxnMu.Unlock()
	if defaultTxnManager == nil {
		defaultTxnManager = NewTransactionManager(DefaultCDP(),
			NewTransactionTable(30*time.Second, time.Second))
	}
	return defaultTxnManager
}

// InitTransactionManager resets the process-wide TransactionManager to a
// fresh state. Used by tests that need to clear the table between cases.
func InitTransactionManager() {
	defaultTxnMu.Lock()
	defer defaultTxnMu.Unlock()
	if defaultTxnManager != nil {
		defaultTxnManager.Table().Close()
	}
	defaultTxnManager = NewTransactionManager(DefaultCDP(),
		NewTransactionTable(30*time.Second, 0)) // no background sweep in tests
}
