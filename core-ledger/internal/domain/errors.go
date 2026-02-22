package domain

import "errors"

// Domain-level sentinel errors. Use errors.Is / errors.As for matching.
var (
	// ErrInsufficientPostings is returned when a transaction has fewer than 2 postings.
	ErrInsufficientPostings = errors.New("transaction must have at least 2 postings")

	// ErrTenantMismatch is returned when a posting's tenant does not match the transaction tenant.
	ErrTenantMismatch = errors.New("posting tenant does not match transaction tenant")

	// ErrCurrencyMismatch is returned when incompatible currencies are used.
	ErrCurrencyMismatch = errors.New("currency mismatch")

	// ErrUnbalancedPostings is returned when posting amounts per currency do not sum to zero.
	ErrUnbalancedPostings = errors.New("postings do not sum to zero per currency")

	// ErrAccountNotFound is returned when a referenced ledger account does not exist.
	ErrAccountNotFound = errors.New("ledger account not found")

	// ErrTransactionNotFound is returned when a referenced transaction does not exist.
	ErrTransactionNotFound = errors.New("transaction not found")
)
