package domain

import "fmt"

// Money is an immutable value object representing an amount in minor units
// (e.g. cents for USD) plus a currency. No floating-point arithmetic is used
// anywhere in the kernel.
type Money struct {
	MinorUnits int64
	Currency   Currency
}

// NewMoney constructs a Money value.
func NewMoney(minor int64, currency Currency) Money {
	return Money{MinorUnits: minor, Currency: currency}
}

// Add returns the sum of two Money values. Returns an error if currencies differ.
func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.Currency, other.Currency)
	}
	return Money{MinorUnits: m.MinorUnits + other.MinorUnits, Currency: m.Currency}, nil
}

// Negate returns a Money with the sign flipped.
func (m Money) Negate() Money {
	return Money{MinorUnits: -m.MinorUnits, Currency: m.Currency}
}

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.MinorUnits == 0 }

// String formats the Money for display as "<minor> <currency>".
func (m Money) String() string {
	return fmt.Sprintf("%d %s", m.MinorUnits, m.Currency)
}
