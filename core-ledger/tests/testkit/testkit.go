// Package testkit provides shared utilities for integration tests.
// Integration tests require a running PostgreSQL instance.
// Set POSTGRES_DSN to override the default local DSN, or run:
//
//	docker compose up -d postgres
//
// and the default DSN will connect automatically.
package testkit

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	pgstore "github.com/gubaros/del-e-very/core-ledger/internal/adapters/postgres"
	"github.com/gubaros/del-e-very/core-ledger/internal/application"
	"github.com/gubaros/del-e-very/core-ledger/internal/domain"
)

const defaultDSN = "postgres://ledger:ledger@localhost:5432/ledger_test?sslmode=disable"

// OpenDB opens a connection to the test database and runs migrations.
// If the database is unreachable the test is skipped (not failed) so that
// unit tests can run without a Postgres dependency.
func OpenDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("skipping integration test – cannot open DB (%s): %v", dsn, err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("skipping integration test – cannot ping DB (%s): %v", dsn, err)
		return nil
	}

	if err := pgstore.Migrate(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	return db
}

// TruncateAll wipes all ledger tables so each test starts clean.
func TruncateAll(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		TRUNCATE TABLE outbox_events, ledger_postings, ledger_balances,
		               ledger_transactions, ledger_accounts
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("truncating tables: %v", err)
	}
}

// NewService returns a LedgerService connected to db with a fixed clock.
func NewService(db *sql.DB, fixedTime time.Time) *application.LedgerService {
	store := pgstore.NewStore(db)
	return application.NewLedgerService(store, application.FixedClock{T: fixedTime})
}

// MustCreateAccount creates a ledger account and fatals on error.
func MustCreateAccount(t *testing.T, svc *application.LedgerService, tenantID, accountID, name, accountType, currency string) {
	t.Helper()
	_, err := svc.CreateAccount(context.Background(), application.CreateAccountCmd{
		TenantID:    domain.TenantID(tenantID),
		AccountID:   domain.AccountID(accountID),
		Name:        name,
		AccountType: domain.AccountType(accountType),
		Currency:    domain.Currency(currency),
	})
	if err != nil {
		t.Fatalf("creating account %s: %v", accountID, err)
	}
}

// MustPostTx posts a simple two-legged transaction and fatals on error.
func MustPostTx(
	t *testing.T,
	svc *application.LedgerService,
	tenantID, idemKey, txType string,
	debitAccountID string,
	creditAccountID string,
	amountMinor int64,
	currency string,
) *domain.LedgerTransaction {
	t.Helper()
	tx, err := svc.Post(context.Background(), application.PostTransactionCmd{
		TenantID:       domain.TenantID(tenantID),
		IdempotencyKey: domain.IdempotencyKey(idemKey),
		TxType:         domain.TxType(txType),
		ValueDate:      time.Now().UTC(),
		Postings: []application.PostingInput{
			{
				LedgerAccountID: domain.AccountID(debitAccountID),
				AmountMinor:     amountMinor,
				Currency:        domain.Currency(currency),
				Narrative:       fmt.Sprintf("debit %s", idemKey),
			},
			{
				LedgerAccountID: domain.AccountID(creditAccountID),
				AmountMinor:     -amountMinor,
				Currency:        domain.Currency(currency),
				Narrative:       fmt.Sprintf("credit %s", idemKey),
			},
		},
	})
	if err != nil {
		t.Fatalf("posting transaction %s: %v", idemKey, err)
	}
	return tx
}
