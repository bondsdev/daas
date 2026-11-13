ALTER TABLE tenants
    DROP COLUMN pos_search_mode;

ALTER TABLE internal_invoices
    DROP COLUMN cash_session_id,
    DROP COLUMN payment_method,
    DROP COLUMN currency;

DROP TABLE exchange_rates;
DROP TABLE cash_sessions;

DROP TYPE payment_method_type;
DROP TYPE exchange_rate_source;
DROP TYPE cash_session_status;
