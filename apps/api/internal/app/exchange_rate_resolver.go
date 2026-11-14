package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/adapter/exchangerate"
)

// fetchRate calls the injected fetcher and validates the result. This is the
// SINGLE place in the codebase that invokes exchangerate.Fetcher.FetchRate —
// CashSessionService.resolveRate (the Open auto-rate branch) and
// currentExchangeRate (the read-only resolver below) both route through it
// instead of duplicating the call.
func fetchRate(ctx context.Context, fetcher exchangerate.Fetcher) (float64, error) {
	rate, err := fetcher.FetchRate(ctx)
	if err != nil {
		return 0, err
	}
	if rate <= 0 {
		return 0, fmt.Errorf("fetcher returned non-positive rate")
	}
	return rate, nil
}

// currentExchangeRate returns the tenant's most recently recorded BS/USD
// exchange rate (ORDER BY rate_date DESC LIMIT 1). When no rate has ever been
// recorded for the tenant, it falls back to the injected fetcher directly.
//
// This is read-only/best-effort: a freshly fetched fallback rate is NOT
// persisted here — persisting into exchange_rates remains
// CashSessionService.Open's job. Both CashSessionService.CurrentRate and
// ProductService call this so there is exactly one implementation of
// "what's the tenant's current rate right now".
func currentExchangeRate(ctx context.Context, pool *pgxpool.Pool, fetcher exchangerate.Fetcher, tenantID uuid.UUID) (float64, error) {
	var rate float64
	err := pool.QueryRow(ctx,
		`SELECT rate FROM exchange_rates WHERE tenant_id=$1 ORDER BY rate_date DESC LIMIT 1`,
		tenantID,
	).Scan(&rate)
	if err == nil {
		return rate, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("load current exchange rate: %w", err)
	}

	rate, err = fetchRate(ctx, fetcher)
	if err != nil {
		return 0, fmt.Errorf("fetch exchange rate: %w", err)
	}
	return rate, nil
}
