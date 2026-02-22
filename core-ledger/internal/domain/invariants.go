package domain

import "fmt"

// ValidateTransaction checks all domain invariants for a proposed transaction.
//
// Invariants enforced:
//  1. The transaction must have at least 2 postings.
//  2. Every posting's TenantID must equal the transaction TenantID.
//  3. Every posting's Currency must equal the currency of the referenced LedgerAccount.
//  4. The sum of AmountMinor across all postings must equal zero per currency
//     (double-entry balance invariant).
//
// accounts is a map[AccountID]LedgerAccount used for currency validation.
// This function is pure — it performs no I/O.
func ValidateTransaction(tx LedgerTransaction, accounts map[AccountID]LedgerAccount) error {
	if len(tx.Postings) < 2 {
		return fmt.Errorf("%w: got %d", ErrInsufficientPostings, len(tx.Postings))
	}

	for _, p := range tx.Postings {
		// Invariant 2: tenant consistency.
		if p.TenantID != tx.TenantID {
			return fmt.Errorf("%w: posting %s has tenant %q, tx has %q",
				ErrTenantMismatch, p.PostingID, p.TenantID, tx.TenantID)
		}

		// Invariant 3: posting currency must match the account's currency.
		acct, ok := accounts[p.LedgerAccountID]
		if !ok {
			return fmt.Errorf("%w: %s", ErrAccountNotFound, p.LedgerAccountID)
		}
		if acct.Currency != p.Currency {
			return fmt.Errorf("%w: account %s uses %s but posting uses %s",
				ErrCurrencyMismatch, p.LedgerAccountID, acct.Currency, p.Currency)
		}
	}

	// Invariant 4: double-entry — sum per currency must be zero.
	sums := make(map[Currency]int64, 2)
	for _, p := range tx.Postings {
		sums[p.Currency] += p.AmountMinor
	}
	for cur, sum := range sums {
		if sum != 0 {
			return fmt.Errorf("%w: currency %s sum=%d", ErrUnbalancedPostings, cur, sum)
		}
	}

	return nil
}
