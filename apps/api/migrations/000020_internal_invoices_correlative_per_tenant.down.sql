ALTER TABLE internal_invoices
    DROP CONSTRAINT internal_invoices_tenant_correlative_key;

ALTER TABLE internal_invoices
    ADD CONSTRAINT internal_invoices_correlative_key UNIQUE (correlative);
