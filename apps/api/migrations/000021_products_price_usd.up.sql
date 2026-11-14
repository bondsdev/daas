-- ─── USD pricing with automatic BCV conversion ─────────────────────────────
-- Adds an optional USD source-of-truth price to products. When set,
-- internal_price becomes a derived value recomputed whenever a new BS/USD
-- rate is recorded (see CashSessionService.Open and ProductService.Create/
-- Update in apps/api/internal/app). No RLS change needed — products already
-- has RLS from an earlier migration.

ALTER TABLE products
    ADD COLUMN price_usd NUMERIC(12,2);
