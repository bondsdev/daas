-- ─── Cash register (caja) for counter sales ────────────────────────────────

CREATE TYPE cash_session_status AS ENUM ('open', 'closed');
CREATE TYPE exchange_rate_source AS ENUM ('auto', 'manual');
CREATE TYPE payment_method_type AS ENUM ('cash', 'card', 'mobile_payment');

CREATE TABLE cash_sessions (
    id                 UUID                 PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id          UUID                 NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    status             cash_session_status  NOT NULL DEFAULT 'open',
    exchange_rate      NUMERIC(12,4)        NOT NULL CHECK (exchange_rate > 0),
    exchange_rate_source exchange_rate_source NOT NULL DEFAULT 'manual',
    opening_notes      TEXT,
    closing_notes      TEXT,
    opened_by          UUID                 NOT NULL REFERENCES tenant_users(id),
    opened_at          TIMESTAMPTZ          NOT NULL DEFAULT NOW(),
    closed_by          UUID                 REFERENCES tenant_users(id),
    closed_at          TIMESTAMPTZ,
    created_at         TIMESTAMPTZ          NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ          NOT NULL DEFAULT NOW()
);

-- Only one open session per tenant at a time.
CREATE UNIQUE INDEX idx_cash_sessions_one_open_per_tenant
    ON cash_sessions(tenant_id) WHERE status = 'open';
CREATE INDEX idx_cash_sessions_tenant_date ON cash_sessions(tenant_id, opened_at DESC);

CREATE TABLE exchange_rates (
    tenant_id    UUID                  NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    rate_date    DATE                  NOT NULL,
    rate         NUMERIC(12,4)         NOT NULL CHECK (rate > 0),
    source       exchange_rate_source  NOT NULL DEFAULT 'auto',
    fetched_at   TIMESTAMPTZ           NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, rate_date)
);

ALTER TABLE internal_invoices
    ADD COLUMN cash_session_id UUID REFERENCES cash_sessions(id),
    ADD COLUMN payment_method  payment_method_type,
    ADD COLUMN currency        VARCHAR(3) NOT NULL DEFAULT 'VES';

CREATE INDEX idx_internal_invoices_cash_session ON internal_invoices(cash_session_id);

ALTER TABLE tenants
    ADD COLUMN pos_search_mode VARCHAR(10) NOT NULL DEFAULT 'barcode'
        CHECK (pos_search_mode IN ('barcode', 'text', 'both'));

-- RLS — cash_sessions
ALTER TABLE cash_sessions ENABLE ROW LEVEL SECURITY;
CREATE POLICY service_role_bypass ON cash_sessions
    TO service_role USING (true);
CREATE POLICY tenant_isolation ON cash_sessions
    USING (tenant_id::text = current_setting('app.tenant_id', true));

-- RLS — exchange_rates
ALTER TABLE exchange_rates ENABLE ROW LEVEL SECURITY;
CREATE POLICY service_role_bypass ON exchange_rates
    TO service_role USING (true);
CREATE POLICY tenant_isolation ON exchange_rates
    USING (tenant_id::text = current_setting('app.tenant_id', true));
