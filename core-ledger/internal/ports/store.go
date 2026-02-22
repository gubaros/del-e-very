// Package ports defines the persistence interfaces (output ports) for the ledger kernel.
// Adapters implement these interfaces; the domain and application layers depend only on these.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/gubaros/del-e-very/core-ledger/internal/domain"
)

// ErrIdempotencyConflict is returned by InsertTransaction when the unique
// (tenant_id, idempotency_key) constraint fires, indicating a concurrent
// replay. The caller should fetch and return the existing transaction.
var ErrIdempotencyConflict = errors.New("idempotency key conflict: transaction already exists")

// OutboxEvent is an at-least-once domain event written atomically in the same
// database transaction as the ledger entries.
type OutboxEvent struct {
	EventID     string
	TenantID    domain.TenantID
	EventType   string
	PayloadJSON []byte
	CreatedAt   time.Time
}

// TxStore is the top-level persistence port.
// It owns the unit-of-work boundary and exposes read-only helpers
// that do not require a transaction.
type TxStore interface {
	// WithTx executes fn within a single serialisable database transaction.
	// On error, the transaction is rolled back; on success it is committed.
	WithTx(ctx context.Context, fn func(TxStoreTx) error) error

	// FindTxByIdempotency returns the existing transaction for the given key, or nil.
	FindTxByIdempotency(ctx context.Context, tenantID domain.TenantID, key domain.IdempotencyKey) (*domain.LedgerTransaction, error)

	// GetAccount returns the ledger account or nil if not found.
	GetAccount(ctx context.Context, tenantID domain.TenantID, accountID domain.AccountID) (*domain.LedgerAccount, error)

	// GetTransaction returns a transaction (with postings) by tx_id.
	GetTransaction(ctx context.Context, tenantID domain.TenantID, txID domain.TxID) (*domain.LedgerTransaction, error)

	// GetBalance returns the materialised booked balance for an account.
	GetBalance(ctx context.Context, tenantID domain.TenantID, accountID domain.AccountID) (domain.Money, error)

	// CreateAccount persists a new ledger account.
	CreateAccount(ctx context.Context, account domain.LedgerAccount) error
}

// TxStoreTx is the transactional sub-port.
// All methods execute within the database transaction started by TxStore.WithTx.
// The adapter must use the same sql.Tx for all calls within a single WithTx scope.
type TxStoreTx interface {
	// FindTxByIdempotency fetches an existing tx inside the transaction (for re-check after lock).
	FindTxByIdempotency(ctx context.Context, tenantID domain.TenantID, key domain.IdempotencyKey) (*domain.LedgerTransaction, error)

	// InsertTransaction writes the transaction header.
	// Returns ErrIdempotencyConflict if the unique (tenant, idem_key) constraint fires.
	InsertTransaction(ctx context.Context, tx domain.LedgerTransaction) error

	// InsertPostings appends posting lines to the ledger (append-only).
	InsertPostings(ctx context.Context, postings []domain.Posting) error

	// ApplyPostingsToBalances atomically upserts the materialised balance table
	// using INSERT … ON CONFLICT DO UPDATE balance = balance + delta.
	ApplyPostingsToBalances(ctx context.Context, postings []domain.Posting) error

	// InsertOutboxEvent writes a domain event to the outbox table.
	InsertOutboxEvent(ctx context.Context, event OutboxEvent) error
}
