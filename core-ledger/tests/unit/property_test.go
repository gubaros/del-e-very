package unit_test

import (
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/gubaros/del-e-very/core-ledger/internal/domain"
)

var testCurrencies = []domain.Currency{"USD", "EUR", "GBP", "JPY"}

// genCurrency draws a random Currency from the supported set.
func genCurrency(t *rapid.T) domain.Currency {
	return rapid.SampledFrom(testCurrencies).Draw(t, "currency")
}

// TestProperty_BalancedPostingsAlwaysValid generates random balanced two-legged
// postings and asserts that ValidateTransaction always returns nil.
func TestProperty_BalancedPostingsAlwaysValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency(t)
		amount := rapid.Int64Range(1, 1_000_000_000).Draw(t, "amount")

		acctA := domain.AccountID("prop-acct-a")
		acctB := domain.AccountID("prop-acct-b")
		tenantID := domain.TenantID("tenant-prop")

		tx := domain.LedgerTransaction{
			TenantID:  tenantID,
			TxID:      "prop-tx",
			CreatedAt: time.Now(),
			Postings: []domain.Posting{
				{
					PostingID:       "p1",
					TxID:            "prop-tx",
					TenantID:        tenantID,
					LedgerAccountID: acctA,
					AmountMinor:     amount,
					Currency:        currency,
				},
				{
					PostingID:       "p2",
					TxID:            "prop-tx",
					TenantID:        tenantID,
					LedgerAccountID: acctB,
					AmountMinor:     -amount,
					Currency:        currency,
				},
			},
		}
		accounts := map[domain.AccountID]domain.LedgerAccount{
			acctA: {TenantID: tenantID, AccountID: acctA, Currency: currency},
			acctB: {TenantID: tenantID, AccountID: acctB, Currency: currency},
		}

		if err := domain.ValidateTransaction(tx, accounts); err != nil {
			t.Fatalf("balanced postings (amount=%d, currency=%s) should be valid: %v", amount, currency, err)
		}
	})
}

// TestProperty_UnbalancedPostingsAlwaysFail generates postings with a deliberate
// imbalance and asserts that ValidateTransaction always returns ErrUnbalancedPostings.
func TestProperty_UnbalancedPostingsAlwaysFail(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency(t)
		// imbalance is ≥1 so the sum is always non-zero.
		debit := rapid.Int64Range(1, 1_000_000_000).Draw(t, "debit")
		imbalance := rapid.Int64Range(1, 1_000_000_000).Draw(t, "imbalance")
		credit := -(debit + imbalance) // sum = debit + credit = imbalance ≠ 0

		acctA := domain.AccountID("prop-acct-a")
		acctB := domain.AccountID("prop-acct-b")
		tenantID := domain.TenantID("tenant-prop")

		tx := domain.LedgerTransaction{
			TenantID: tenantID,
			TxID:     "prop-tx",
			Postings: []domain.Posting{
				{
					PostingID:       "p1",
					TxID:            "prop-tx",
					TenantID:        tenantID,
					LedgerAccountID: acctA,
					AmountMinor:     debit,
					Currency:        currency,
				},
				{
					PostingID:       "p2",
					TxID:            "prop-tx",
					TenantID:        tenantID,
					LedgerAccountID: acctB,
					AmountMinor:     credit,
					Currency:        currency,
				},
			},
		}
		accounts := map[domain.AccountID]domain.LedgerAccount{
			acctA: {TenantID: tenantID, AccountID: acctA, Currency: currency},
			acctB: {TenantID: tenantID, AccountID: acctB, Currency: currency},
		}

		err := domain.ValidateTransaction(tx, accounts)
		if err == nil {
			t.Fatalf("unbalanced postings (debit=%d credit=%d) should fail validation", debit, credit)
		}
	})
}

// TestProperty_MultiLegBalancedAlwaysValid generates a random number of posting
// legs, ensures they balance, and verifies validation passes.
func TestProperty_MultiLegBalancedAlwaysValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency(t)
		tenantID := domain.TenantID("tenant-prop")

		// Generate between 2 and 10 accounts.
		n := rapid.IntRange(2, 10).Draw(t, "n_accounts")

		amounts := make([]int64, n)
		var total int64
		for i := 0; i < n-1; i++ {
			amounts[i] = rapid.Int64Range(-1_000_000, 1_000_000).Draw(t, "amount")
			total += amounts[i]
		}
		// Last posting closes the balance.
		amounts[n-1] = -total

		postings := make([]domain.Posting, n)
		accounts := make(map[domain.AccountID]domain.LedgerAccount, n)
		for i := 0; i < n; i++ {
			id := domain.AccountID(rapid.StringMatching(`[a-z]{4,8}`).Draw(t, "acct_id") + rapid.StringN(4, 4, -1).Draw(t, "suffix"))
			postings[i] = domain.Posting{
				PostingID:       domain.PostingID(rapid.StringN(8, 8, -1).Draw(t, "pid")),
				TxID:            "multi-tx",
				TenantID:        tenantID,
				LedgerAccountID: id,
				AmountMinor:     amounts[i],
				Currency:        currency,
			}
			accounts[id] = domain.LedgerAccount{TenantID: tenantID, AccountID: id, Currency: currency}
		}

		tx := domain.LedgerTransaction{TenantID: tenantID, TxID: "multi-tx", Postings: postings}
		if err := domain.ValidateTransaction(tx, accounts); err != nil {
			t.Fatalf("multi-leg balanced tx should be valid: %v", err)
		}
	})
}

// TestProperty_MoneyAddCommutative verifies that Money.Add is commutative.
func TestProperty_MoneyAddCommutative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency(t)
		a := rapid.Int64().Draw(t, "a")
		b := rapid.Int64().Draw(t, "b")

		ma := domain.NewMoney(a, currency)
		mb := domain.NewMoney(b, currency)

		ab, err1 := ma.Add(mb)
		ba, err2 := mb.Add(ma)

		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected error: %v %v", err1, err2)
		}
		if ab.MinorUnits != ba.MinorUnits {
			t.Fatalf("Add not commutative: %v != %v", ab, ba)
		}
	})
}

// TestProperty_MoneyNegateIsInvolutory verifies that Negate(Negate(m)) == m.
func TestProperty_MoneyNegateIsInvolutory(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency(t)
		amount := rapid.Int64().Draw(t, "amount")
		m := domain.NewMoney(amount, currency)
		if m.Negate().Negate().MinorUnits != m.MinorUnits {
			t.Fatalf("double negate should be identity for %v", m)
		}
	})
}
