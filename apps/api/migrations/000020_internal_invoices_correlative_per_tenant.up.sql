-- Bugfix: internal_invoices.correlative was UNIQUE across the whole table,
-- not per tenant. Since the correlative is formatted as "INT-<year>-<seq>"
-- with the sequence reset per tenant per year (invoice_correlative_seq), any
-- two tenants issuing their first invoice of the year collide on the exact
-- same string. Scope the uniqueness to (tenant_id, correlative) instead.
ALTER TABLE internal_invoices
    DROP CONSTRAINT internal_invoices_correlative_key;

ALTER TABLE internal_invoices
    ADD CONSTRAINT internal_invoices_tenant_correlative_key UNIQUE (tenant_id, correlative);
