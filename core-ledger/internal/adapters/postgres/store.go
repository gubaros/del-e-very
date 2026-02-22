// Package postgres implements the persistence ports using PostgreSQL and
// database/sql. No ORM is used; all queries are explicit SQL.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/gubaros/del-e-very/core-ledger/internal/domain"
	"github.com/gubaros/del-e-very/core-ledger/internal/ports"
)

// pgUniqueViolation is the PostgreSQL error code for unique_violation.
const pgUniqueViolation = "23505"

// Store implements ports.TxStore against a *sql.DB.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store. The caller must call db.Ping() before use.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// -------------------------------------------------------------------
// ports.TxStore – non-transactional read helpers
// -------------------------------------------------------------------

// WithTx starts a database transaction, calls fn, and commits or rolls back.
func (s *Store) WithTx(ctx context.Context, fn func(ports.TxStoreTx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	stx := &storeTx{tx: tx}

	if err := fn(stx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// FindTxByIdempotency returns the existing transaction for the key, or nil.
func (s *Store) FindTxByIdempotency(ctx context.Context, tenantID domain.TenantID, key domain.IdempotencyKey) (*domain.LedgerTransaction, error) {
	return findTxByIdempotency(ctx, s.db, tenantID, key)
}

// GetAccount returns the ledger account, or nil if not found.
func (s *Store) GetAccount(ctx context.Context, tenantID domain.TenantID, accountID domain.AccountID) (*domain.LedgerAccount, error) {
	return getAccount(ctx, s.db, tenantID, accountID)
}

// GetTransaction returns a transaction with its postings, or nil if not found.
func (s *Store) GetTransaction(ctx context.Context, tenantID domain.TenantID, txID domain.TxID) (*domain.LedgerTransaction, error) {
	return getTransaction(ctx, s.db, tenantID, txID)
}

// GetBalance returns the materialised balance for an account, or zero-balance if none exists.
func (s *Store) GetBalance(ctx context.Context, tenantID domain.TenantID, accountID domain.AccountID) (domain.Money, error) {
	// We need the account currency to return a properly typed Money even when balance is 0.
	acct, err := getAccount(ctx, s.db, tenantID, accountID)
	if err != nil {
		return domain.Money{}, err
	}
	if acct == nil {
		return domain.Money{}, fmt.Errorf("%w: %s", domain.ErrAccountNotFound, accountID)
	}

	var balanceMinor int64
	err = s.db.QueryRowContext(ctx, `
		SELECT balance_minor
		FROM   ledger_balances
		WHERE  tenant_id  = $1
		AND    account_id = $2
		AND    currency   = $3
	`, string(tenantID), string(accountID), string(acct.Currency)).Scan(&balanceMinor)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NewMoney(0, acct.Currency), nil
	}
	if err != nil {
		return domain.Money{}, fmt.Errorf("querying balance: %w", err)
	}
	return domain.NewMoney(balanceMinor, acct.Currency), nil
}

// CreateAccount persists a new ledger account.
func (s *Store) CreateAccount(ctx context.Context, account domain.LedgerAccount) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ledger_accounts (tenant_id, account_id, name, account_type, currency, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		string(account.TenantID),
		string(account.AccountID),
		account.Name,
		string(account.AccountType),
		string(account.Currency),
		account.CreatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pgUniqueViolation {
			return fmt.Errorf("account %s already exists: %w", account.AccountID, err)
		}
		return fmt.Errorf("inserting account: %w", err)
	}
	return nil
}

// -------------------------------------------------------------------
// storeTx implements ports.TxStoreTx (all calls within one sql.Tx)
// -------------------------------------------------------------------

type storeTx struct {
	tx *sql.Tx
}

func (s *storeTx) FindTxByIdempotency(ctx context.Context, tenantID domain.TenantID, key domain.IdempotencyKey) (*domain.LedgerTransaction, error) {
	return findTxByIdempotency(ctx, s.tx, tenantID, key)
}

// InsertTransaction writes the transaction header using ON CONFLICT DO NOTHING.
// Returns ErrIdempotencyConflict when the unique key already exists (concurrent replay).
func (s *storeTx) InsertTransaction(ctx context.Context, tx domain.LedgerTransaction) error {
	result, err := s.tx.ExecContext(ctx, `
		INSERT INTO ledger_transactions
		    (tenant_id, tx_id, idempotency_key, tx_type, status,
		     created_at, value_date, correlation_id, external_ref)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
	`,
		string(tx.TenantID),
		string(tx.TxID),
		string(tx.IdempotencyKey),
		string(tx.TxType),
		string(tx.Status),
		tx.CreatedAt,
		tx.ValueDate,
		string(tx.CorrelationID),
		tx.ExternalRef,
	)
	if err != nil {
		return fmt.Errorf("inserting transaction: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return ports.ErrIdempotencyConflict
	}
	return nil
}

// InsertPostings bulk-inserts posting lines. The ledger is append-only.
func (s *storeTx) InsertPostings(ctx context.Context, postings []domain.Posting) error {
	if len(postings) == 0 {
		return nil
	}

	// Build a parameterised bulk INSERT.
	const cols = 8
	placeholders := make([]string, 0, len(postings))
	args := make([]interface{}, 0, len(postings)*cols)

	for i, p := range postings {
		base := i * cols
		placeholders = append(placeholders,
			fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
				base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8),
		)
		args = append(args,
			string(p.PostingID),
			string(p.TxID),
			string(p.TenantID),
			string(p.LedgerAccountID),
			p.AmountMinor,
			string(p.Currency),
			p.Narrative,
			p.ExternalRef,
		)
	}

	q := `INSERT INTO ledger_postings
		(posting_id, tx_id, tenant_id, ledger_account_id, amount_minor, currency, narrative, external_ref)
		VALUES ` + strings.Join(placeholders, ",")

	if _, err := s.tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("inserting postings: %w", err)
	}
	return nil
}

// ApplyPostingsToBalances atomically upserts balances using the delta pattern.
// Safe under concurrent transactions touching the same account.
func (s *storeTx) ApplyPostingsToBalances(ctx context.Context, postings []domain.Posting) error {
	for _, p := range postings {
		_, err := s.tx.ExecContext(ctx, `
			INSERT INTO ledger_balances (tenant_id, account_id, currency, balance_minor, updated_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (tenant_id, account_id, currency)
			DO UPDATE SET
			    balance_minor = ledger_balances.balance_minor + EXCLUDED.balance_minor,
			    updated_at    = NOW()
		`,
			string(p.TenantID),
			string(p.LedgerAccountID),
			string(p.Currency),
			p.AmountMinor,
		)
		if err != nil {
			return fmt.Errorf("upserting balance for account %s: %w", p.LedgerAccountID, err)
		}
	}
	return nil
}

// InsertOutboxEvent writes a domain event to the outbox table in the same tx.
func (s *storeTx) InsertOutboxEvent(ctx context.Context, event ports.OutboxEvent) error {
	_, err := s.tx.ExecContext(ctx, `
		INSERT INTO outbox_events (event_id, tenant_id, event_type, payload_json, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`,
		event.EventID,
		string(event.TenantID),
		event.EventType,
		event.PayloadJSON,
		event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting outbox event: %w", err)
	}
	return nil
}

// -------------------------------------------------------------------
// Shared query helpers (work with both *sql.DB and *sql.Tx via querier)
// -------------------------------------------------------------------

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

func findTxByIdempotency(ctx context.Context, q querier, tenantID domain.TenantID, key domain.IdempotencyKey) (*domain.LedgerTransaction, error) {
	var (
		txID          string
		txType        string
		status        string
		createdAt     time.Time
		valueDate     time.Time
		correlationID string
		externalRef   string
	)
	err := q.QueryRowContext(ctx, `
		SELECT tx_id, tx_type, status, created_at, value_date, correlation_id, external_ref
		FROM   ledger_transactions
		WHERE  tenant_id       = $1
		AND    idempotency_key = $2
	`, string(tenantID), string(key)).Scan(
		&txID, &txType, &status, &createdAt, &valueDate, &correlationID, &externalRef,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying transaction by idempotency: %w", err)
	}

	tx := &domain.LedgerTransaction{
		TenantID:       tenantID,
		TxID:           domain.TxID(txID),
		IdempotencyKey: key,
		TxType:         domain.TxType(txType),
		Status:         domain.TxStatus(status),
		CreatedAt:      createdAt,
		ValueDate:      valueDate,
		CorrelationID:  domain.CorrelationID(correlationID),
		ExternalRef:    externalRef,
	}

	postings, err := fetchPostings(ctx, q, tenantID, domain.TxID(txID))
	if err != nil {
		return nil, err
	}
	tx.Postings = postings
	return tx, nil
}

func getTransaction(ctx context.Context, q querier, tenantID domain.TenantID, txID domain.TxID) (*domain.LedgerTransaction, error) {
	var (
		idemKey       string
		txType        string
		status        string
		createdAt     time.Time
		valueDate     time.Time
		correlationID string
		externalRef   string
	)
	err := q.QueryRowContext(ctx, `
		SELECT idempotency_key, tx_type, status, created_at, value_date, correlation_id, external_ref
		FROM   ledger_transactions
		WHERE  tenant_id = $1
		AND    tx_id     = $2
	`, string(tenantID), string(txID)).Scan(
		&idemKey, &txType, &status, &createdAt, &valueDate, &correlationID, &externalRef,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying transaction: %w", err)
	}

	tx := &domain.LedgerTransaction{
		TenantID:       tenantID,
		TxID:           txID,
		IdempotencyKey: domain.IdempotencyKey(idemKey),
		TxType:         domain.TxType(txType),
		Status:         domain.TxStatus(status),
		CreatedAt:      createdAt,
		ValueDate:      valueDate,
		CorrelationID:  domain.CorrelationID(correlationID),
		ExternalRef:    externalRef,
	}

	postings, err := fetchPostings(ctx, q, tenantID, txID)
	if err != nil {
		return nil, err
	}
	tx.Postings = postings
	return tx, nil
}

func getAccount(ctx context.Context, q querier, tenantID domain.TenantID, accountID domain.AccountID) (*domain.LedgerAccount, error) {
	var (
		name        string
		accountType string
		currency    string
		createdAt   time.Time
	)
	err := q.QueryRowContext(ctx, `
		SELECT name, account_type, currency, created_at
		FROM   ledger_accounts
		WHERE  tenant_id  = $1
		AND    account_id = $2
	`, string(tenantID), string(accountID)).Scan(&name, &accountType, &currency, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying account: %w", err)
	}
	return &domain.LedgerAccount{
		TenantID:    tenantID,
		AccountID:   accountID,
		Name:        name,
		AccountType: domain.AccountType(accountType),
		Currency:    domain.Currency(currency),
		CreatedAt:   createdAt,
	}, nil
}

func fetchPostings(ctx context.Context, q querier, tenantID domain.TenantID, txID domain.TxID) ([]domain.Posting, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT posting_id, ledger_account_id, amount_minor, currency, narrative, external_ref
		FROM   ledger_postings
		WHERE  tenant_id = $1
		AND    tx_id     = $2
		ORDER  BY posting_id
	`, string(tenantID), string(txID))
	if err != nil {
		return nil, fmt.Errorf("querying postings: %w", err)
	}
	defer rows.Close()

	var postings []domain.Posting
	for rows.Next() {
		var (
			postingID  string
			accountID  string
			amount     int64
			currency   string
			narrative  string
			externalRef string
		)
		if err := rows.Scan(&postingID, &accountID, &amount, &currency, &narrative, &externalRef); err != nil {
			return nil, fmt.Errorf("scanning posting: %w", err)
		}
		postings = append(postings, domain.Posting{
			PostingID:       domain.PostingID(postingID),
			TxID:            txID,
			TenantID:        tenantID,
			LedgerAccountID: domain.AccountID(accountID),
			AmountMinor:     amount,
			Currency:        domain.Currency(currency),
			Narrative:       narrative,
			ExternalRef:     externalRef,
		})
	}
	return postings, rows.Err()
}
