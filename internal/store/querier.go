package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotFound is returned, wrapped, by every single-row read or update whose
// row does not exist in the scope asked for: another project's rows count as
// absent. Core maps it to a not_found error with errors.Is.
var ErrNotFound = errors.New("not found")

// ErrProjectMismatch is returned, wrapped, by InsertEvent when the event's project
// is not its task's project. The schema refuses such a row (migration 0003). The
// sentinel lets a caller tell that refusal from a failed write with errors.Is.
var ErrProjectMismatch = errors.New("event project differs from the project of its task")

// Querier is the part of *sql.DB and *sql.Tx that the store uses. Every query
// function takes one, so core can run several of them inside one transaction
// from WithTx.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// The store is a Querier too, by delegating to its pool. Pass s for reads
// outside a transaction, and the q that WithTx hands you for everything inside
// one. Passing s inside WithTx bypasses the transaction.
var _ Querier = (*Store)(nil)
var _ Querier = (*sql.Tx)(nil)

// ExecContext runs a statement on the pool, outside any transaction.
func (s *Store) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}

// QueryContext runs a query on the pool, outside any transaction.
func (s *Store) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, query, args...)
}

// QueryRowContext runs a single-row query on the pool, outside any transaction.
func (s *Store) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, query, args...)
}

// WithTx runs fn inside one write transaction and commits when fn returns nil.
// When fn returns an error, or the commit fails, the transaction is rolled back
// and nothing from fn is kept. fn's error is returned unwrapped, so a caller can
// match it with errors.Is or errors.As.
//
// The driver is opened with _txlock=immediate, so BeginTx takes the write lock
// at once. Every transaction therefore serialises with every other writer, and
// a read inside fn sees a consistent state. Calling WithTx again on the same
// Store from inside fn waits for the write lock the outer call holds, and fails
// at the 5 s busy timeout; it does not wait forever. Use WithTx for writes
// only. Reads that need no write run on the Store itself, or in WithReadTx when
// several of them must agree. A read inside WithTx still waits for the write
// lock, which serialises it against every writer for no gain.
//
// Allocation (AllocateTaskID) reads a counter and then inserts, so it must run
// inside WithTx: the lock is what makes two allocations get different numbers.
func (s *Store) WithTx(ctx context.Context, fn func(q Querier) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}
	// Deferred so that a panic in fn also releases the write lock. Rollback is
	// a no-op once Commit has succeeded.
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	return nil
}

// WithReadTx runs fn inside one read transaction and returns fn's error
// unwrapped. Every read fn makes through q sees the same snapshot of the
// database, so a caller that reads several tables gets one consistent state.
//
// Unlike WithTx, it takes no write lock. The driver opens a transaction with
// ReadOnly set as a plain, deferred BEGIN, where WithTx gets BEGIN IMMEDIATE
// from the _txlock option. In WAL mode a snapshot starts at the first read and
// lasts until the transaction ends, so writers are not held up. fn must not
// write through q. Passing s inside fn bypasses the snapshot.
func (s *Store) WithReadTx(ctx context.Context, fn func(q Querier) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("store: begin read transaction: %w", err)
	}
	// Nothing was written, so the rollback only ends the transaction and frees
	// its snapshot. It runs on a panic in fn too.
	defer tx.Rollback()
	return fn(tx)
}
