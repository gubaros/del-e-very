# core-ledger — Money Kernel v0

A correctness-first, append-only double-entry subledger written in Go with
PostgreSQL. This is the system of record for all money movement in Zorro.

---

## Kernel invariants

- **Double-entry**: the sum of `amount_minor` across all postings in a
  transaction must be zero per currency.
- **Minimum postings**: every transaction must have at least two posting lines.
- **Append-only**: postings are never updated or deleted. Corrections are new
  transactions (reversals or adjustments) linked to the original.
- **Idempotency**: submitting the same `(tenant_id, idempotency_key)` pair
  multiple times (even concurrently) produces exactly one committed transaction
  and returns the same result to every caller.
- **Tenant isolation**: every row carries `tenant_id`; queries always filter by
  it. Postings cannot reference accounts from a different tenant.
- **Currency integrity**: a posting's currency must match the currency of the
  referenced ledger account; no implicit conversion.
- **Atomicity**: posting a transaction writes the header, all posting lines,
  balance updates, and an outbox event in a single database transaction. Either
  all succeed or none do.

---

## Repository structure

```
core-ledger/
├── cmd/ledgerd/main.go               Entry point
├── docker-compose.yml                Local Postgres for development & tests
├── go.mod
├── internal/
│   ├── domain/                       Pure domain model — no I/O
│   │   ├── values.go                 Value objects (TenantID, Money, …)
│   │   ├── money.go                  Money (int64 minor units, no floats)
│   │   ├── entities.go               LedgerAccount, LedgerTransaction, Posting
│   │   ├── invariants.go             ValidateTransaction (pure function)
│   │   └── errors.go                 Sentinel domain errors
│   ├── ports/
│   │   └── store.go                  TxStore / TxStoreTx persistence interfaces
│   ├── application/
│   │   ├── clock.go                  Clock interface + RealClock/FixedClock
│   │   ├── commands.go               PostTransactionCmd, ReverseTransactionCmd
│   │   └── service.go                LedgerService orchestration
│   ├── adapters/postgres/
│   │   ├── store.go                  TxStore implementation (database/sql)
│   │   ├── migrate.go                Embed + execute SQL migrations
│   │   └── schema/001_init.sql       DDL for all ledger tables
│   └── api/http/
│       ├── handler.go                HTTP request handlers
│       └── server.go                 chi router setup
└── tests/
    ├── unit/
    │   ├── domain_test.go            Unit tests for invariants & Money
    │   └── property_test.go          Property-based tests (pgregory.net/rapid)
    ├── integration/
    │   └── ledger_test.go            Integration tests against real Postgres
    └── testkit/
        └── testkit.go                Shared helpers: OpenDB, TruncateAll, …
```

---

## How to run migrations

Migrations run automatically on server start-up. To run them manually against
a local database:

```bash
export DATABASE_URL="postgres://ledger:ledger@localhost:5432/ledger_test?sslmode=disable"
go run ./cmd/ledgerd   # starts server; migrations execute on boot
```

Or apply the SQL file directly with psql:

```bash
psql "$DATABASE_URL" -f internal/adapters/postgres/schema/001_init.sql
```

---

## How to run the server

```bash
# 1. Start Postgres
docker compose up -d postgres

# 2. Run the server (migrations applied automatically)
DATABASE_URL="postgres://ledger:ledger@localhost:5432/ledger_test?sslmode=disable" \
PORT=8080 \
go run ./cmd/ledgerd
```

---

## How to run tests

### Unit + property-based (no Postgres required)

```bash
cd core-ledger
go test ./tests/unit/...
```

### Integration tests (Postgres required)

```bash
# Start Postgres
docker compose up -d postgres

# Run integration tests
POSTGRES_DSN="postgres://ledger:ledger@localhost:5432/ledger_test?sslmode=disable" \
go test -v -count=1 -timeout 60s ./tests/integration/...
```

### All tests

```bash
docker compose up -d postgres
POSTGRES_DSN="postgres://ledger:ledger@localhost:5432/ledger_test?sslmode=disable" \
go test ./...
```

---

## Example curl requests

> Assume the server is running on `localhost:8080`.

### 1 — Create accounts

```bash
# Asset account (e.g. customer wallet)
curl -s -X POST http://localhost:8080/v1/accounts \
  -H 'Content-Type: application/json' \
  -d '{
    "tenant_id":    "tenant-acme",
    "account_id":   "wallet-customer-001",
    "name":         "Customer Wallet",
    "account_type": "ASSET",
    "currency":     "USD"
  }' | jq .

# Liability account (e.g. deposits due)
curl -s -X POST http://localhost:8080/v1/accounts \
  -H 'Content-Type: application/json' \
  -d '{
    "tenant_id":    "tenant-acme",
    "account_id":   "deposits-due",
    "name":         "Deposits Due",
    "account_type": "LIABILITY",
    "currency":     "USD"
  }' | jq .
```

### 2 — Post a deposit (debit wallet, credit deposits-due)

```bash
curl -s -X POST http://localhost:8080/v1/transactions \
  -H 'Content-Type: application/json' \
  -H 'X-Correlation-Id: corr-dep-001' \
  -d '{
    "tenant_id":       "tenant-acme",
    "idempotency_key": "deposit-2024-001",
    "tx_type":         "DEPOSIT",
    "value_date":      "2024-01-15T00:00:00Z",
    "postings": [
      {
        "account_id":   "wallet-customer-001",
        "amount_minor": 10000,
        "currency":     "USD",
        "narrative":    "Initial deposit"
      },
      {
        "account_id":   "deposits-due",
        "amount_minor": -10000,
        "currency":     "USD",
        "narrative":    "Initial deposit liability"
      }
    ]
  }' | jq .
```

### 3 — Check balance

```bash
curl -s "http://localhost:8080/v1/balances/wallet-customer-001?tenant=tenant-acme" | jq .
```

### 4 — Retrieve by idempotency key

```bash
curl -s "http://localhost:8080/v1/transactions/by-idempotency?tenant=tenant-acme&key=deposit-2024-001" | jq .
```

### 5 — Reverse the deposit

```bash
# Use the tx_id returned from step 2
TX_ID="<tx_id from step 2>"

curl -s -X POST "http://localhost:8080/v1/transactions/${TX_ID}/reverse" \
  -H 'Content-Type: application/json' \
  -H 'X-Correlation-Id: corr-rev-001' \
  -d '{
    "tenant_id":       "tenant-acme",
    "idempotency_key": "reversal-deposit-2024-001",
    "narrative":       "Customer requested reversal"
  }' | jq .
```

### 6 — Post a transfer (three-legged split example)

```bash
# First create a fee account
curl -s -X POST http://localhost:8080/v1/accounts \
  -H 'Content-Type: application/json' \
  -d '{
    "tenant_id":    "tenant-acme",
    "account_id":   "fee-revenue",
    "name":         "Fee Revenue",
    "account_type": "REVENUE",
    "currency":     "USD"
  }' | jq .

# Transfer $95 to recipient, $5 fee to house
curl -s -X POST http://localhost:8080/v1/transactions \
  -H 'Content-Type: application/json' \
  -d '{
    "tenant_id":       "tenant-acme",
    "idempotency_key": "transfer-2024-001",
    "tx_type":         "TRANSFER",
    "value_date":      "2024-01-16T00:00:00Z",
    "postings": [
      {"account_id": "wallet-customer-001", "amount_minor": -10000, "currency": "USD", "narrative": "Send funds"},
      {"account_id": "deposits-due",        "amount_minor":  9500,  "currency": "USD", "narrative": "Recipient credit"},
      {"account_id": "fee-revenue",         "amount_minor":   500,  "currency": "USD", "narrative": "Transfer fee"}
    ]
  }' | jq .
```

---

## Append-only and idempotency guarantees

**Append-only**: `ledger_postings` and `ledger_transactions` are never updated
or deleted. The application layer and database schema have no `UPDATE`/`DELETE`
paths for these tables. Corrections are always new transactions that reverse the
effects of the original (see `ReverseTransactionCmd`).

**Idempotency**: the unique constraint `UNIQUE (tenant_id, idempotency_key)` on
`ledger_transactions` acts as the database-level guard. The application layer
performs a double-check (once before the transaction, once inside) to handle the
race window. The adapter uses `INSERT … ON CONFLICT DO NOTHING` so the database
transaction is never aborted by a concurrent replay — instead the winning row is
fetched and returned to all callers.

**Outbox**: every committed transaction writes a row to `outbox_events` in the
same database transaction. This guarantees that downstream event consumers
cannot miss an event due to a crash between persistence and publish.

---

## Non-goals for v0

- Holds / available balance / pending balance
- Card authorization / capture lifecycle
- Interest accrual
- FX / multi-currency conversion
- KYC / AML
- Personalization / decisioning
- Outbox publisher (table is written; delivery is out of scope)
