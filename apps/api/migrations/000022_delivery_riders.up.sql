-- ─── Delivery riders (motorizados) ─────────────────────────────────────────

-- Allow a tenant_user to hold the rider role.
ALTER TABLE tenant_users DROP CONSTRAINT tenant_users_role_check;
ALTER TABLE tenant_users ADD CONSTRAINT tenant_users_role_check
    CHECK (role IN ('owner', 'employee', 'rider'));

ALTER TABLE tenants
    ADD COLUMN delivery_enabled     BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN urgent_delivery_fee  NUMERIC(12,2) NOT NULL DEFAULT 1.00;

CREATE TABLE riders (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                 UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id                   UUID NOT NULL REFERENCES tenant_users(id) ON DELETE CASCADE,
    name                      VARCHAR(255) NOT NULL,
    phone                     VARCHAR(50),
    rate_per_delivery         NUMERIC(12,2) NOT NULL DEFAULT 0,
    status                    VARCHAR(20) NOT NULL DEFAULT 'available'
        CHECK (status IN ('available', 'unavailable')),
    max_concurrent_deliveries INT NOT NULL DEFAULT 1 CHECK (max_concurrent_deliveries > 0),
    last_assigned_at         TIMESTAMPTZ,
    active                    BOOLEAN NOT NULL DEFAULT true,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, user_id)
);

CREATE INDEX idx_riders_tenant_id ON riders(tenant_id);

ALTER TABLE shop_orders
    ADD COLUMN rider_id            UUID REFERENCES riders(id),
    ADD COLUMN rider_fee           NUMERIC(12,2),
    ADD COLUMN rider_assigned_at   TIMESTAMPTZ,
    ADD COLUMN rider_delivered_at  TIMESTAMPTZ,
    ADD COLUMN rider_paid_at       TIMESTAMPTZ,
    ADD COLUMN is_urgent           BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN urgent_fee          NUMERIC(12,2);

CREATE INDEX idx_shop_orders_rider_id ON shop_orders(rider_id);

-- RLS — riders
ALTER TABLE riders ENABLE ROW LEVEL SECURITY;
CREATE POLICY service_role_bypass ON riders
    TO service_role USING (TRUE);
CREATE POLICY tenant_isolation ON riders
    USING (tenant_id = current_setting('app.tenant_id', TRUE)::UUID);

CREATE TRIGGER update_riders_updated_at BEFORE UPDATE ON riders
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();
