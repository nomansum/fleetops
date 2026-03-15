-- Billing Service schema

CREATE TABLE IF NOT EXISTS pricing_rules (
    id            TEXT             PRIMARY KEY,
    tenant_id     TEXT             NOT NULL,
    name          TEXT             NOT NULL,
    base_cents    BIGINT           NOT NULL DEFAULT 0,
    currency      TEXT             NOT NULL DEFAULT 'USD',
    rate_per_km   DOUBLE PRECISION NOT NULL DEFAULT 0,
    surcharge_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    active_from   TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    active_until  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS invoices (
    id            TEXT        PRIMARY KEY,
    tenant_id     TEXT        NOT NULL,
    order_id      TEXT        NOT NULL UNIQUE,
    reference     TEXT        NOT NULL UNIQUE,
    status        TEXT        NOT NULL DEFAULT 'DRAFT',
    subtotal_cents BIGINT     NOT NULL DEFAULT 0,
    tax_cents     BIGINT      NOT NULL DEFAULT 0,
    total_cents   BIGINT      NOT NULL DEFAULT 0,
    currency      TEXT        NOT NULL DEFAULT 'USD',
    issued_at     TIMESTAMPTZ,
    due_date      TIMESTAMPTZ,
    paid_at       TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS line_items (
    id          TEXT   PRIMARY KEY,
    invoice_id  TEXT   NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    description TEXT   NOT NULL,
    quantity    INT    NOT NULL DEFAULT 1,
    unit_cents  BIGINT NOT NULL,
    total_cents BIGINT NOT NULL,
    currency    TEXT   NOT NULL DEFAULT 'USD'
);

CREATE TABLE IF NOT EXISTS invoice_audit_log (
    id         BIGSERIAL   PRIMARY KEY,
    invoice_id TEXT        NOT NULL,
    action     TEXT        NOT NULL,
    actor      TEXT,
    detail     JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_invoices_tenant_status ON invoices(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_invoices_due_date      ON invoices(due_date) WHERE status='ISSUED';
