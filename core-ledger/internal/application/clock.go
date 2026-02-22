// Package application contains the orchestration layer for the ledger kernel.
// It coordinates domain validation, idempotency checking, and atomic persistence.
// No database/sql or HTTP concerns belong here.
package application

import "time"

// Clock abstracts time so that application services can be tested deterministically.
type Clock interface {
	Now() time.Time
}

// RealClock returns the actual wall-clock time in UTC.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// FixedClock is a test clock that always returns the same instant.
type FixedClock struct{ T time.Time }

func (f FixedClock) Now() time.Time { return f.T }
