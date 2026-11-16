ALTER TABLE shop_orders
    DROP COLUMN rider_id,
    DROP COLUMN rider_fee,
    DROP COLUMN rider_assigned_at,
    DROP COLUMN rider_delivered_at,
    DROP COLUMN rider_paid_at,
    DROP COLUMN is_urgent,
    DROP COLUMN urgent_fee;

DROP TABLE riders;

ALTER TABLE tenants
    DROP COLUMN delivery_enabled,
    DROP COLUMN urgent_delivery_fee;

ALTER TABLE tenant_users DROP CONSTRAINT tenant_users_role_check;
ALTER TABLE tenant_users ADD CONSTRAINT tenant_users_role_check
    CHECK (role IN ('owner', 'employee'));
