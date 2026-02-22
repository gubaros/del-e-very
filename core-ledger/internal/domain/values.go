// Package domain contains the pure domain model for the ledger kernel.
// No database/sql, no HTTP, no external dependencies.
package domain

// TenantID is the opaque identifier for an isolated tenant.
type TenantID string

// AccountID is the unique identifier for a ledger account within a tenant.
type AccountID string

// TxID is the unique identifier for a ledger transaction.
type TxID string

// PostingID is the unique identifier for a single posting line.
type PostingID string

// IdempotencyKey is a client-provided key used to guarantee exactly-once semantics.
// The combination (TenantID, IdempotencyKey) must be globally unique per committed tx.
type IdempotencyKey string

// CorrelationID links related operations for distributed tracing and audit.
type CorrelationID string

// Currency is an ISO-4217 currency code (e.g. "USD", "EUR").
type Currency string

// TxType categorises the business meaning of a transaction.
type TxType string

// TxStatus represents the lifecycle state of a ledger transaction.
type TxStatus string

const (
	TxStatusPending  TxStatus = "PENDING"
	TxStatusPosted   TxStatus = "POSTED"
	TxStatusRejected TxStatus = "REJECTED"
)
