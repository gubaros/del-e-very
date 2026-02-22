package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/gubaros/del-e-very/core-ledger/internal/domain"
	"github.com/gubaros/del-e-very/core-ledger/internal/ports"
)

// LedgerService orchestrates all money-moving operations.
// It enforces idempotency, delegates domain validation to the domain package,
// and drives the unit-of-work via ports.TxStore.
type LedgerService struct {
	store ports.TxStore
	clock Clock
}

// NewLedgerService constructs a LedgerService with the supplied store and clock.
func NewLedgerService(store ports.TxStore, clock Clock) *LedgerService {
	return &LedgerService{store: store, clock: clock}
}

// CreateAccount persists a new ledger account.
func (s *LedgerService) CreateAccount(ctx context.Context, cmd CreateAccountCmd) (*domain.LedgerAccount, error) {
	acct := domain.LedgerAccount{
		TenantID:    cmd.TenantID,
		AccountID:   cmd.AccountID,
		Name:        cmd.Name,
		AccountType: cmd.AccountType,
		Currency:    cmd.Currency,
		CreatedAt:   s.clock.Now(),
	}
	if err := s.store.CreateAccount(ctx, acct); err != nil {
		return nil, fmt.Errorf("creating account: %w", err)
	}
	return &acct, nil
}

// Post posts a new double-entry transaction idempotently.
//
// If a transaction with the same (tenant_id, idempotency_key) already exists
// it is returned as-is without applying any additional effects (exactly-once).
//
// The operation is atomic: tx header + postings + balance updates + outbox event
// are written in a single database transaction.
func (s *LedgerService) Post(ctx context.Context, cmd PostTransactionCmd) (*domain.LedgerTransaction, error) {
	// Fast path: check idempotency before acquiring any locks.
	existing, err := s.store.FindTxByIdempotency(ctx, cmd.TenantID, cmd.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("checking idempotency: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	// Resolve account metadata for domain validation (accounts are immutable once created).
	accounts := make(map[domain.AccountID]domain.LedgerAccount, len(cmd.Postings))
	for _, p := range cmd.Postings {
		if _, seen := accounts[p.LedgerAccountID]; seen {
			continue
		}
		acct, err := s.store.GetAccount(ctx, cmd.TenantID, p.LedgerAccountID)
		if err != nil {
			return nil, fmt.Errorf("fetching account %s: %w", p.LedgerAccountID, err)
		}
		if acct == nil {
			return nil, fmt.Errorf("%w: %s", domain.ErrAccountNotFound, p.LedgerAccountID)
		}
		accounts[p.LedgerAccountID] = *acct
	}

	// Assemble domain objects.
	txID := domain.TxID(uuid.New().String())
	now := s.clock.Now()

	postings := make([]domain.Posting, 0, len(cmd.Postings))
	for _, p := range cmd.Postings {
		postings = append(postings, domain.Posting{
			PostingID:       domain.PostingID(uuid.New().String()),
			TxID:            txID,
			TenantID:        cmd.TenantID,
			LedgerAccountID: p.LedgerAccountID,
			AmountMinor:     p.AmountMinor,
			Currency:        p.Currency,
			Narrative:       p.Narrative,
			ExternalRef:     p.ExternalRef,
		})
	}

	tx := domain.LedgerTransaction{
		TenantID:       cmd.TenantID,
		TxID:           txID,
		IdempotencyKey: cmd.IdempotencyKey,
		TxType:         cmd.TxType,
		Status:         domain.TxStatusPosted,
		CreatedAt:      now,
		ValueDate:      cmd.ValueDate,
		CorrelationID:  cmd.CorrelationID,
		ExternalRef:    cmd.ExternalRef,
		Postings:       postings,
	}

	// Pure domain validation — no I/O.
	if err := domain.ValidateTransaction(tx, accounts); err != nil {
		return nil, err
	}

	// Atomic persistence.
	var result *domain.LedgerTransaction
	err = s.store.WithTx(ctx, func(storeTx ports.TxStoreTx) error {
		// Re-check inside the transaction to handle concurrent replays.
		got, err := storeTx.FindTxByIdempotency(ctx, cmd.TenantID, cmd.IdempotencyKey)
		if err != nil {
			return fmt.Errorf("re-checking idempotency in tx: %w", err)
		}
		if got != nil {
			result = got
			return nil
		}

		// InsertTransaction uses ON CONFLICT DO NOTHING and returns
		// ErrIdempotencyConflict when another concurrent request wins the race.
		if err := storeTx.InsertTransaction(ctx, tx); err != nil {
			if errors.Is(err, ports.ErrIdempotencyConflict) {
				winner, err2 := storeTx.FindTxByIdempotency(ctx, cmd.TenantID, cmd.IdempotencyKey)
				if err2 != nil {
					return fmt.Errorf("fetching winner after conflict: %w", err2)
				}
				result = winner
				return nil
			}
			return fmt.Errorf("inserting transaction: %w", err)
		}

		if err := storeTx.InsertPostings(ctx, postings); err != nil {
			return fmt.Errorf("inserting postings: %w", err)
		}

		if err := storeTx.ApplyPostingsToBalances(ctx, postings); err != nil {
			return fmt.Errorf("applying balances: %w", err)
		}

		payload, err := json.Marshal(tx)
		if err != nil {
			return fmt.Errorf("marshalling outbox payload: %w", err)
		}
		if err := storeTx.InsertOutboxEvent(ctx, ports.OutboxEvent{
			EventID:     uuid.New().String(),
			TenantID:    cmd.TenantID,
			EventType:   "transaction.posted",
			PayloadJSON: payload,
			CreatedAt:   now,
		}); err != nil {
			return fmt.Errorf("inserting outbox event: %w", err)
		}

		result = &tx
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Reverse creates an inverse transaction that cancels every effect of originalTxID.
// The reversal is itself idempotent via (tenant_id, idempotency_key).
func (s *LedgerService) Reverse(ctx context.Context, cmd ReverseTransactionCmd) (*domain.LedgerTransaction, error) {
	// Fast-path idempotency check.
	existing, err := s.store.FindTxByIdempotency(ctx, cmd.TenantID, cmd.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("checking idempotency: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	// Fetch the original transaction (with postings) to invert.
	original, err := s.store.GetTransaction(ctx, cmd.TenantID, cmd.OriginalTxID)
	if err != nil {
		return nil, fmt.Errorf("fetching original transaction: %w", err)
	}
	if original == nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrTransactionNotFound, cmd.OriginalTxID)
	}

	txID := domain.TxID(uuid.New().String())
	now := s.clock.Now()

	narrative := cmd.Narrative
	if narrative == "" {
		narrative = fmt.Sprintf("Reversal of %s", original.TxID)
	}

	// Inverse postings: negate every amount.
	reversalPostings := make([]domain.Posting, 0, len(original.Postings))
	for _, p := range original.Postings {
		reversalPostings = append(reversalPostings, domain.Posting{
			PostingID:       domain.PostingID(uuid.New().String()),
			TxID:            txID,
			TenantID:        cmd.TenantID,
			LedgerAccountID: p.LedgerAccountID,
			AmountMinor:     -p.AmountMinor,
			Currency:        p.Currency,
			Narrative:       narrative,
			ExternalRef:     string(original.TxID),
		})
	}

	reversalTx := domain.LedgerTransaction{
		TenantID:       cmd.TenantID,
		TxID:           txID,
		IdempotencyKey: cmd.IdempotencyKey,
		TxType:         domain.TxType("REVERSAL"),
		Status:         domain.TxStatusPosted,
		CreatedAt:      now,
		ValueDate:      now,
		CorrelationID:  cmd.CorrelationID,
		ExternalRef:    string(original.TxID),
		Postings:       reversalPostings,
	}

	var result *domain.LedgerTransaction
	err = s.store.WithTx(ctx, func(storeTx ports.TxStoreTx) error {
		got, err := storeTx.FindTxByIdempotency(ctx, cmd.TenantID, cmd.IdempotencyKey)
		if err != nil {
			return err
		}
		if got != nil {
			result = got
			return nil
		}

		if err := storeTx.InsertTransaction(ctx, reversalTx); err != nil {
			if errors.Is(err, ports.ErrIdempotencyConflict) {
				winner, err2 := storeTx.FindTxByIdempotency(ctx, cmd.TenantID, cmd.IdempotencyKey)
				if err2 != nil {
					return err2
				}
				result = winner
				return nil
			}
			return err
		}

		if err := storeTx.InsertPostings(ctx, reversalPostings); err != nil {
			return err
		}
		if err := storeTx.ApplyPostingsToBalances(ctx, reversalPostings); err != nil {
			return err
		}

		payload, err := json.Marshal(reversalTx)
		if err != nil {
			return err
		}
		if err := storeTx.InsertOutboxEvent(ctx, ports.OutboxEvent{
			EventID:     uuid.New().String(),
			TenantID:    cmd.TenantID,
			EventType:   "transaction.reversed",
			PayloadJSON: payload,
			CreatedAt:   now,
		}); err != nil {
			return err
		}

		result = &reversalTx
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetByIdempotency returns an existing transaction by its idempotency key.
func (s *LedgerService) GetByIdempotency(ctx context.Context, tenantID domain.TenantID, key domain.IdempotencyKey) (*domain.LedgerTransaction, error) {
	return s.store.FindTxByIdempotency(ctx, tenantID, key)
}

// GetBalance returns the materialised booked balance for an account.
func (s *LedgerService) GetBalance(ctx context.Context, tenantID domain.TenantID, accountID domain.AccountID) (domain.Money, error) {
	return s.store.GetBalance(ctx, tenantID, accountID)
}
