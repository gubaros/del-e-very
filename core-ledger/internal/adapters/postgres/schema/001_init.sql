-- =============================================================================
-- Money Kernel v0 – Initial Schema
-- All tables are append-only (no DELETEs, no UPDATE on ledger rows).
-- Corrections are new transactions. Balances are the only mutable aggregate.
-- =============================================================================

-- ---------------------------------------------------------------------------
-- Ledger Accounts
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ledger_accounts (
    tenant_id    TEXT        NOT NULL,
    account_id   TEXT        NOT NULL,
    name         TEXT        NOT NULL,
    account_type TEXT        NOT NULL,
    currency     TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, account_id)
);

-- ---------------------------------------------------------------------------
-- Ledger Transactions (header only; postings are a child table)
-- The unique constraint on (tenant_id, idempotency_key) enforces exactly-once.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ledger_transactions (
    tenant_id       TEXT        NOT NULL,
    tx_id           TEXT        NOT NULL,
    idempotency_key TEXT        NOT NULL,
    tx_type         TEXT        NOT NULL,
    status          TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    value_date      TIMESTAMPTZ NOT NULL,
    correlation_id  TEXT        NOT NULL DEFAULT '',
    external_ref    TEXT        NOT NULL DEFAULT '',

    PRIMARY KEY (tenant_id, tx_id),
    UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_ledger_transactions_tenant
    ON ledger_transactions (tenant_id);

CREATE INDEX IF NOT EXISTS idx_ledger_transactions_idem
    ON ledger_transactions (tenant_id, idempotency_key);

-- ---------------------------------------------------------------------------
-- Ledger Postings (append-only – never updated, never deleted)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ledger_postings (
    posting_id        TEXT   NOT NULL,
    tx_id             TEXT   NOT NULL,
    tenant_id         TEXT   NOT NULL,
    ledger_account_id TEXT   NOT NULL,
    amount_minor      BIGINT NOT NULL,
    currency          TEXT   NOT NULL,
    narrative         TEXT   NOT NULL DEFAULT '',
    external_ref      TEXT   NOT NULL DEFAULT '',

    PRIMARY KEY (posting_id),
    FOREIGN KEY (tenant_id, tx_id)
        REFERENCES ledger_transactions (tenant_id, tx_id),
    FOREIGN KEY (tenant_id, ledger_account_id)
        REFERENCES ledger_accounts (tenant_id, account_id)
);

CREATE INDEX IF NOT EXISTS idx_ledger_postings_tx
    ON ledger_postings (tenant_id, tx_id);

CREATE INDEX IF NOT EXISTS idx_ledger_postings_account
    ON ledger_postings (tenant_id, ledger_account_id);

-- ---------------------------------------------------------------------------
-- Ledger Balances (materialised BOOKED balance; updated atomically with postings)
-- The balance is updated via INSERT … ON CONFLICT DO UPDATE to be safe under
-- concurrent transactions touching the same account.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ledger_balances (
    tenant_id     TEXT        NOT NULL,
    account_id    TEXT        NOT NULL,
    currency      TEXT        NOT NULL,
    balance_minor BIGINT      NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, account_id, currency),
    FOREIGN KEY (tenant_id, account_id)
        REFERENCES ledger_accounts (tenant_id, account_id)
);

-- ---------------------------------------------------------------------------
-- Outbox Events (append-only; written atomically in the same tx as postings)
-- A background publisher (out of scope for v0) marks delivered_at when the
-- event has been dispatched to downstream consumers.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS outbox_events (
    event_id     TEXT        NOT NULL PRIMARY KEY,
    tenant_id    TEXT        NOT NULL,
    event_type   TEXT        NOT NULL,
    payload_json JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_outbox_undelivered
    ON outbox_events (tenant_id, created_at)
    WHERE delivered_at IS NULL;
