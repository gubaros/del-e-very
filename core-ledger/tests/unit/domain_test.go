package unit_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gubaros/del-e-very/core-ledger/internal/domain"
)

// buildAccounts is a helper that returns a minimal account map for validation.
func buildAccounts(currency domain.Currency, ids ...domain.AccountID) map[domain.AccountID]domain.LedgerAccount {
	m := make(map[domain.AccountID]domain.LedgerAccount, len(ids))
	for _, id := range ids {
		m[id] = domain.LedgerAccount{
			TenantID:    "tenant-a",
			AccountID:   id,
			Name:        string(id),
			AccountType: domain.AccountTypeAsset,
			Currency:    currency,
			CreatedAt:   time.Now(),
		}
	}
	return m
}

func twoPostings(tenantID domain.TenantID, acctA, acctB domain.AccountID, amount int64, currency domain.Currency) []domain.Posting {
	return []domain.Posting{
		{
			PostingID:       "p1",
			TxID:            "tx1",
			TenantID:        tenantID,
			LedgerAccountID: acctA,
			AmountMinor:     amount,
			Currency:        currency,
		},
		{
			PostingID:       "p2",
			TxID:            "tx1",
			TenantID:        tenantID,
			LedgerAccountID: acctB,
			AmountMinor:     -amount,
			Currency:        currency,
		},
	}
}

// --- Balanced postings ---

func TestValidateTransaction_Balanced(t *testing.T) {
	acctA := domain.AccountID("acct-a")
	acctB := domain.AccountID("acct-b")
	currency := domain.Currency("USD")
	tenantID := domain.TenantID("tenant-a")

	tx := domain.LedgerTransaction{
		TenantID:       tenantID,
		TxID:           "tx1",
		IdempotencyKey: "idem-1",
		TxType:         "DEPOSIT",
		Status:         domain.TxStatusPosted,
		Postings:       twoPostings(tenantID, acctA, acctB, 10000, currency),
	}
	accounts := buildAccounts(currency, acctA, acctB)

	if err := domain.ValidateTransaction(tx, accounts); err != nil {
		t.Fatalf("expected valid transaction, got: %v", err)
	}
}

func TestValidateTransaction_BalancedMultiPosting(t *testing.T) {
	tenantID := domain.TenantID("tenant-a")
	currency := domain.Currency("USD")
	acctA := domain.AccountID("a")
	acctB := domain.AccountID("b")
	acctC := domain.AccountID("c")

	tx := domain.LedgerTransaction{
		TenantID: tenantID,
		TxID:     "tx2",
		Postings: []domain.Posting{
			{PostingID: "p1", TxID: "tx2", TenantID: tenantID, LedgerAccountID: acctA, AmountMinor: 5000, Currency: currency},
			{PostingID: "p2", TxID: "tx2", TenantID: tenantID, LedgerAccountID: acctB, AmountMinor: 3000, Currency: currency},
			{PostingID: "p3", TxID: "tx2", TenantID: tenantID, LedgerAccountID: acctC, AmountMinor: -8000, Currency: currency},
		},
	}
	accounts := buildAccounts(currency, acctA, acctB, acctC)

	if err := domain.ValidateTransaction(tx, accounts); err != nil {
		t.Fatalf("expected valid 3-legged transaction, got: %v", err)
	}
}

// --- Unbalanced postings ---

func TestValidateTransaction_Unbalanced(t *testing.T) {
	tenantID := domain.TenantID("tenant-a")
	currency := domain.Currency("USD")
	acctA := domain.AccountID("acct-a")
	acctB := domain.AccountID("acct-b")

	tx := domain.LedgerTransaction{
		TenantID: tenantID,
		TxID:     "tx3",
		Postings: []domain.Posting{
			{PostingID: "p1", TxID: "tx3", TenantID: tenantID, LedgerAccountID: acctA, AmountMinor: 100, Currency: currency},
			{PostingID: "p2", TxID: "tx3", TenantID: tenantID, LedgerAccountID: acctB, AmountMinor: -99, Currency: currency},
		},
	}
	accounts := buildAccounts(currency, acctA, acctB)

	if err := domain.ValidateTransaction(tx, accounts); !errors.Is(err, domain.ErrUnbalancedPostings) {
		t.Fatalf("expected ErrUnbalancedPostings, got: %v", err)
	}
}

// --- Minimum postings rule ---

func TestValidateTransaction_SinglePosting(t *testing.T) {
	tenantID := domain.TenantID("tenant-a")
	currency := domain.Currency("USD")
	acctA := domain.AccountID("acct-a")

	tx := domain.LedgerTransaction{
		TenantID: tenantID,
		TxID:     "tx4",
		Postings: []domain.Posting{
			{PostingID: "p1", TxID: "tx4", TenantID: tenantID, LedgerAccountID: acctA, AmountMinor: 100, Currency: currency},
		},
	}
	accounts := buildAccounts(currency, acctA)

	if err := domain.ValidateTransaction(tx, accounts); !errors.Is(err, domain.ErrInsufficientPostings) {
		t.Fatalf("expected ErrInsufficientPostings, got: %v", err)
	}
}

func TestValidateTransaction_ZeroPostings(t *testing.T) {
	tx := domain.LedgerTransaction{TenantID: "tenant-a", TxID: "tx5"}
	if err := domain.ValidateTransaction(tx, nil); !errors.Is(err, domain.ErrInsufficientPostings) {
		t.Fatalf("expected ErrInsufficientPostings, got: %v", err)
	}
}

// --- Currency mismatch ---

func TestValidateTransaction_CurrencyMismatch(t *testing.T) {
	tenantID := domain.TenantID("tenant-a")
	acctA := domain.AccountID("acct-usd")
	acctB := domain.AccountID("acct-eur")

	tx := domain.LedgerTransaction{
		TenantID: tenantID,
		TxID:     "tx6",
		Postings: []domain.Posting{
			{PostingID: "p1", TxID: "tx6", TenantID: tenantID, LedgerAccountID: acctA, AmountMinor: 100, Currency: "USD"},
			{PostingID: "p2", TxID: "tx6", TenantID: tenantID, LedgerAccountID: acctB, AmountMinor: -100, Currency: "EUR"},
		},
	}
	// account acctA is USD, posting says USD → OK; account acctB is EUR, posting says EUR → OK
	// But the two currencies don't sum to zero (each sums to a non-zero value independently)
	accounts := map[domain.AccountID]domain.LedgerAccount{
		acctA: {TenantID: tenantID, AccountID: acctA, Currency: "USD"},
		acctB: {TenantID: tenantID, AccountID: acctB, Currency: "EUR"},
	}

	if err := domain.ValidateTransaction(tx, accounts); !errors.Is(err, domain.ErrUnbalancedPostings) {
		t.Fatalf("expected ErrUnbalancedPostings for cross-currency tx, got: %v", err)
	}
}

func TestValidateTransaction_PostingCurrencyDoesNotMatchAccount(t *testing.T) {
	tenantID := domain.TenantID("tenant-a")
	acctA := domain.AccountID("acct-a") // EUR account
	acctB := domain.AccountID("acct-b") // EUR account

	tx := domain.LedgerTransaction{
		TenantID: tenantID,
		TxID:     "tx7",
		Postings: []domain.Posting{
			// posting says USD but account is EUR → mismatch
			{PostingID: "p1", TxID: "tx7", TenantID: tenantID, LedgerAccountID: acctA, AmountMinor: 100, Currency: "USD"},
			{PostingID: "p2", TxID: "tx7", TenantID: tenantID, LedgerAccountID: acctB, AmountMinor: -100, Currency: "USD"},
		},
	}
	accounts := buildAccounts("EUR", acctA, acctB)

	if err := domain.ValidateTransaction(tx, accounts); !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("expected ErrCurrencyMismatch, got: %v", err)
	}
}

// --- Tenant mismatch ---

func TestValidateTransaction_TenantMismatch(t *testing.T) {
	currency := domain.Currency("USD")
	acctA := domain.AccountID("acct-a")
	acctB := domain.AccountID("acct-b")

	tx := domain.LedgerTransaction{
		TenantID: "tenant-a",
		TxID:     "tx8",
		Postings: []domain.Posting{
			{PostingID: "p1", TxID: "tx8", TenantID: "tenant-a", LedgerAccountID: acctA, AmountMinor: 100, Currency: currency},
			// This posting belongs to a different tenant – should be rejected.
			{PostingID: "p2", TxID: "tx8", TenantID: "tenant-b", LedgerAccountID: acctB, AmountMinor: -100, Currency: currency},
		},
	}
	accounts := buildAccounts(currency, acctA, acctB)

	if err := domain.ValidateTransaction(tx, accounts); !errors.Is(err, domain.ErrTenantMismatch) {
		t.Fatalf("expected ErrTenantMismatch, got: %v", err)
	}
}

// --- Account not found ---

func TestValidateTransaction_AccountNotFound(t *testing.T) {
	tenantID := domain.TenantID("tenant-a")
	currency := domain.Currency("USD")

	tx := domain.LedgerTransaction{
		TenantID: tenantID,
		TxID:     "tx9",
		Postings: []domain.Posting{
			{PostingID: "p1", TxID: "tx9", TenantID: tenantID, LedgerAccountID: "missing", AmountMinor: 100, Currency: currency},
			{PostingID: "p2", TxID: "tx9", TenantID: tenantID, LedgerAccountID: "also-missing", AmountMinor: -100, Currency: currency},
		},
	}

	if err := domain.ValidateTransaction(tx, map[domain.AccountID]domain.LedgerAccount{}); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got: %v", err)
	}
}

// --- Money value object ---

func TestMoney_Add_SameCurrency(t *testing.T) {
	a := domain.NewMoney(100, "USD")
	b := domain.NewMoney(200, "USD")
	sum, err := a.Add(b)
	if err != nil {
		t.Fatal(err)
	}
	if sum.MinorUnits != 300 || sum.Currency != "USD" {
		t.Fatalf("unexpected sum: %v", sum)
	}
}

func TestMoney_Add_DifferentCurrency(t *testing.T) {
	a := domain.NewMoney(100, "USD")
	b := domain.NewMoney(100, "EUR")
	if _, err := a.Add(b); !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("expected ErrCurrencyMismatch, got: %v", err)
	}
}

func TestMoney_Negate(t *testing.T) {
	m := domain.NewMoney(500, "GBP")
	neg := m.Negate()
	if neg.MinorUnits != -500 || neg.Currency != "GBP" {
		t.Fatalf("unexpected negation: %v", neg)
	}
}

func TestMoney_IsZero(t *testing.T) {
	if !domain.NewMoney(0, "USD").IsZero() {
		t.Fatal("0 USD should be zero")
	}
	if domain.NewMoney(1, "USD").IsZero() {
		t.Fatal("1 USD should not be zero")
	}
}
