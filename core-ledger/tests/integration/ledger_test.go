package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	pgstore "github.com/gubaros/del-e-very/core-ledger/internal/adapters/postgres"
	"github.com/gubaros/del-e-very/core-ledger/internal/application"
	"github.com/gubaros/del-e-very/core-ledger/internal/domain"
	"github.com/gubaros/del-e-very/core-ledger/tests/testkit"
)

// ---------------------------------------------------------------------------
// Idempotency under concurrency
// ---------------------------------------------------------------------------

// TestIdempotency_Concurrent posts the same (tenant, idem_key) from 20
// goroutines simultaneously. Exactly one transaction and two postings must
// be persisted; all goroutines must receive the same tx_id.
func TestIdempotency_Concurrent(t *testing.T) {
	db := testkit.OpenDB(t)
	testkit.TruncateAll(t, db)

	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	svc := testkit.NewService(db, now)

	testkit.MustCreateAccount(t, svc, "tenant-1", "cash", "Cash", "ASSET", "USD")
	testkit.MustCreateAccount(t, svc, "tenant-1", "revenue", "Revenue", "REVENUE", "USD")

	const goroutines = 20
	results := make([]*domain.LedgerTransaction, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			tx, err := svc.Post(context.Background(), application.PostTransactionCmd{
				TenantID:       "tenant-1",
				IdempotencyKey: "concurrent-idem-key",
				TxType:         "DEPOSIT",
				ValueDate:      now,
				Postings: []application.PostingInput{
					{LedgerAccountID: "cash", AmountMinor: 10000, Currency: "USD", Narrative: "debit"},
					{LedgerAccountID: "revenue", AmountMinor: -10000, Currency: "USD", Narrative: "credit"},
				},
			})
			results[i] = tx
			errs[i] = err
		}()
	}

	wg.Wait()

	// All goroutines must succeed.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d returned error: %v", i, err)
		}
	}

	// All goroutines must see the same tx_id.
	canonicalID := results[0].TxID
	for i, r := range results {
		if r.TxID != canonicalID {
			t.Fatalf("goroutine %d got tx_id %s, want %s", i, r.TxID, canonicalID)
		}
	}

	// Verify that exactly one transaction and two postings exist in the DB.
	store := pgstore.NewStore(db)
	tx, err := store.FindTxByIdempotency(context.Background(), "tenant-1", "concurrent-idem-key")
	if err != nil {
		t.Fatalf("finding tx: %v", err)
	}
	if tx == nil {
		t.Fatal("transaction not found after concurrent post")
	}
	if len(tx.Postings) != 2 {
		t.Fatalf("expected 2 postings, got %d", len(tx.Postings))
	}
}

// ---------------------------------------------------------------------------
// Reversal correctness
// ---------------------------------------------------------------------------

// TestReversal_BalancesReturnToOriginal posts a deposit, reverses it, and
// verifies that both account balances return to zero.
func TestReversal_BalancesReturnToOriginal(t *testing.T) {
	db := testkit.OpenDB(t)
	testkit.TruncateAll(t, db)

	now := time.Date(2024, 3, 10, 9, 0, 0, 0, time.UTC)
	svc := testkit.NewService(db, now)

	testkit.MustCreateAccount(t, svc, "tenant-2", "cash", "Cash", "ASSET", "USD")
	testkit.MustCreateAccount(t, svc, "tenant-2", "revenue", "Revenue", "REVENUE", "USD")

	// Post original deposit: +10 000 cash, -10 000 revenue.
	original := testkit.MustPostTx(t, svc,
		"tenant-2", "deposit-001", "DEPOSIT",
		"cash", "revenue", 10000, "USD",
	)

	// Verify balances after deposit.
	cashBalance, err := svc.GetBalance(context.Background(), "tenant-2", "cash")
	if err != nil {
		t.Fatal(err)
	}
	if cashBalance.MinorUnits != 10000 {
		t.Fatalf("expected cash balance 10000, got %d", cashBalance.MinorUnits)
	}

	revenueBalance, err := svc.GetBalance(context.Background(), "tenant-2", "revenue")
	if err != nil {
		t.Fatal(err)
	}
	if revenueBalance.MinorUnits != -10000 {
		t.Fatalf("expected revenue balance -10000, got %d", revenueBalance.MinorUnits)
	}

	// Reverse the deposit.
	_, err = svc.Reverse(context.Background(), application.ReverseTransactionCmd{
		TenantID:       "tenant-2",
		IdempotencyKey: "reversal-deposit-001",
		OriginalTxID:   original.TxID,
		Narrative:      "Reversing test deposit",
		CorrelationID:  "corr-rev-001",
	})
	if err != nil {
		t.Fatalf("reversing transaction: %v", err)
	}

	// Both balances must return to zero.
	cashBalance, err = svc.GetBalance(context.Background(), "tenant-2", "cash")
	if err != nil {
		t.Fatal(err)
	}
	if cashBalance.MinorUnits != 0 {
		t.Fatalf("expected cash balance 0 after reversal, got %d", cashBalance.MinorUnits)
	}

	revenueBalance, err = svc.GetBalance(context.Background(), "tenant-2", "revenue")
	if err != nil {
		t.Fatal(err)
	}
	if revenueBalance.MinorUnits != 0 {
		t.Fatalf("expected revenue balance 0 after reversal, got %d", revenueBalance.MinorUnits)
	}
}

// ---------------------------------------------------------------------------
// Outbox presence
// ---------------------------------------------------------------------------

// TestOutbox_CreatedWithTransaction verifies that posting a transaction
// atomically writes an outbox_events row in the same commit.
func TestOutbox_CreatedWithTransaction(t *testing.T) {
	db := testkit.OpenDB(t)
	testkit.TruncateAll(t, db)

	now := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	svc := testkit.NewService(db, now)

	testkit.MustCreateAccount(t, svc, "tenant-3", "cash", "Cash", "ASSET", "USD")
	testkit.MustCreateAccount(t, svc, "tenant-3", "revenue", "Revenue", "REVENUE", "USD")

	tx := testkit.MustPostTx(t, svc,
		"tenant-3", "outbox-test-001", "DEPOSIT",
		"cash", "revenue", 5000, "USD",
	)

	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM outbox_events
		WHERE  tenant_id  = $1
		AND    event_type = 'transaction.posted'
		AND    payload_json->>'TxID' = $2
	`, "tenant-3", string(tx.TxID)).Scan(&count)
	if err != nil {
		t.Fatalf("querying outbox: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 outbox event, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Reversal idempotency
// ---------------------------------------------------------------------------

// TestReversal_Idempotent confirms that reversing with the same idempotency
// key twice returns the same reversal transaction without double-applying effects.
func TestReversal_Idempotent(t *testing.T) {
	db := testkit.OpenDB(t)
	testkit.TruncateAll(t, db)

	now := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	svc := testkit.NewService(db, now)

	testkit.MustCreateAccount(t, svc, "tenant-4", "cash", "Cash", "ASSET", "USD")
	testkit.MustCreateAccount(t, svc, "tenant-4", "revenue", "Revenue", "REVENUE", "USD")

	original := testkit.MustPostTx(t, svc,
		"tenant-4", "dep-idem", "DEPOSIT",
		"cash", "revenue", 7500, "USD",
	)

	cmd := application.ReverseTransactionCmd{
		TenantID:       "tenant-4",
		IdempotencyKey: "rev-dep-idem",
		OriginalTxID:   original.TxID,
		Narrative:      "test reversal",
	}

	rev1, err := svc.Reverse(context.Background(), cmd)
	if err != nil {
		t.Fatalf("first reversal: %v", err)
	}
	rev2, err := svc.Reverse(context.Background(), cmd)
	if err != nil {
		t.Fatalf("second reversal: %v", err)
	}

	if rev1.TxID != rev2.TxID {
		t.Fatalf("reversal not idempotent: got %s then %s", rev1.TxID, rev2.TxID)
	}

	// Balance should be 0, not double-reversed (-7500).
	cashBalance, err := svc.GetBalance(context.Background(), "tenant-4", "cash")
	if err != nil {
		t.Fatal(err)
	}
	if cashBalance.MinorUnits != 0 {
		t.Fatalf("expected balance 0 after idempotent reversal, got %d", cashBalance.MinorUnits)
	}
}

// ---------------------------------------------------------------------------
// Tenant isolation
// ---------------------------------------------------------------------------

// TestTenantIsolation verifies that accounts and balances cannot be mixed
// across tenants.
func TestTenantIsolation(t *testing.T) {
	db := testkit.OpenDB(t)
	testkit.TruncateAll(t, db)

	now := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)
	svc := testkit.NewService(db, now)

	// Create identical account IDs in two different tenants.
	testkit.MustCreateAccount(t, svc, "tenant-A", "cash", "Cash", "ASSET", "USD")
	testkit.MustCreateAccount(t, svc, "tenant-A", "revenue", "Revenue", "REVENUE", "USD")
	testkit.MustCreateAccount(t, svc, "tenant-B", "cash", "Cash", "ASSET", "USD")
	testkit.MustCreateAccount(t, svc, "tenant-B", "revenue", "Revenue", "REVENUE", "USD")

	testkit.MustPostTx(t, svc, "tenant-A", "idem-A", "DEPOSIT", "cash", "revenue", 1000, "USD")
	testkit.MustPostTx(t, svc, "tenant-B", "idem-B", "DEPOSIT", "cash", "revenue", 2000, "USD")

	balA, _ := svc.GetBalance(context.Background(), "tenant-A", "cash")
	balB, _ := svc.GetBalance(context.Background(), "tenant-B", "cash")

	if balA.MinorUnits != 1000 {
		t.Fatalf("tenant-A cash balance: want 1000, got %d", balA.MinorUnits)
	}
	if balB.MinorUnits != 2000 {
		t.Fatalf("tenant-B cash balance: want 2000, got %d", balB.MinorUnits)
	}
}

// ---------------------------------------------------------------------------
// GetByIdempotency
// ---------------------------------------------------------------------------

func TestGetByIdempotency(t *testing.T) {
	db := testkit.OpenDB(t)
	testkit.TruncateAll(t, db)

	now := time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC)
	svc := testkit.NewService(db, now)

	testkit.MustCreateAccount(t, svc, "tenant-5", "cash", "Cash", "ASSET", "USD")
	testkit.MustCreateAccount(t, svc, "tenant-5", "revenue", "Revenue", "REVENUE", "USD")

	posted := testkit.MustPostTx(t, svc, "tenant-5", "idem-lookup", "DEPOSIT", "cash", "revenue", 3000, "USD")

	found, err := svc.GetByIdempotency(context.Background(), "tenant-5", "idem-lookup")
	if err != nil {
		t.Fatal(err)
	}
	if found == nil {
		t.Fatal("expected transaction, got nil")
	}
	if found.TxID != posted.TxID {
		t.Fatalf("tx_id mismatch: want %s, got %s", posted.TxID, found.TxID)
	}
}
