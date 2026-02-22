package application

import (
	"time"

	"github.com/gubaros/del-e-very/core-ledger/internal/domain"
)

// PostingInput is the input DTO for a single posting line within a PostTransactionCmd.
type PostingInput struct {
	LedgerAccountID domain.AccountID
	AmountMinor     int64
	Currency        domain.Currency
	Narrative       string
	ExternalRef     string
}

// PostTransactionCmd is the command to post a new double-entry transaction atomically.
type PostTransactionCmd struct {
	TenantID       domain.TenantID
	IdempotencyKey domain.IdempotencyKey
	TxType         domain.TxType
	// ValueDate is the business effective date of the transaction.
	ValueDate     time.Time
	Postings      []PostingInput
	CorrelationID domain.CorrelationID
	ExternalRef   string
	// Actor and Channel carry audit metadata about who/what initiated the transaction.
	Actor   string
	Channel string
}

// ReverseTransactionCmd is the command to create an inverse transaction linked to an original.
type ReverseTransactionCmd struct {
	TenantID       domain.TenantID
	IdempotencyKey domain.IdempotencyKey
	OriginalTxID   domain.TxID
	// Narrative is the reversal reason used on every inverse posting.
	Narrative     string
	CorrelationID domain.CorrelationID
}

// CreateAccountCmd is the command to create a new ledger account.
type CreateAccountCmd struct {
	TenantID    domain.TenantID
	AccountID   domain.AccountID
	Name        string
	AccountType domain.AccountType
	Currency    domain.Currency
}
