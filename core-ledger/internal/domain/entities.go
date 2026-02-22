package domain

import "time"

// AccountType classifies the normal-balance side of a ledger account.
type AccountType string

const (
	AccountTypeAsset     AccountType = "ASSET"
	AccountTypeLiability AccountType = "LIABILITY"
	AccountTypeEquity    AccountType = "EQUITY"
	AccountTypeRevenue   AccountType = "REVENUE"
	AccountTypeExpense   AccountType = "EXPENSE"
)

// LedgerAccount is a named bucket for money movements belonging to a tenant.
// Once created, the currency and type of an account are immutable.
type LedgerAccount struct {
	TenantID    TenantID
	AccountID   AccountID
	Name        string
	AccountType AccountType
	Currency    Currency
	CreatedAt   time.Time
}

// LedgerTransaction is the header record for a balanced set of postings.
// A transaction is immutable once posted; corrections are new transactions.
type LedgerTransaction struct {
	TenantID       TenantID
	TxID           TxID
	IdempotencyKey IdempotencyKey
	TxType         TxType
	Status         TxStatus
	CreatedAt      time.Time
	ValueDate      time.Time
	CorrelationID  CorrelationID
	ExternalRef    string
	// Postings are the double-entry lines belonging to this transaction.
	Postings []Posting
}

// Posting is a single immutable line in a LedgerTransaction.
// AmountMinor is a signed int64 in the account's currency minor units.
// By convention, a positive amount is a debit and negative is a credit,
// but the invariant only requires the sum across the transaction to be zero.
type Posting struct {
	PostingID       PostingID
	TxID            TxID
	TenantID        TenantID
	LedgerAccountID AccountID
	// AmountMinor is the signed amount in minor units (e.g. cents).
	AmountMinor int64
	Currency    Currency
	Narrative   string
	ExternalRef string
}

// BalanceEntry is a materialized running balance for a (tenant, account, currency).
type BalanceEntry struct {
	TenantID    TenantID
	AccountID   AccountID
	Currency    Currency
	BalanceMinor int64
}
