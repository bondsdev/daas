package app

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// ExchangeRateDetail is the full picture of the tenant's current BS/USD rate
// as actually known to the API right now, for read-only display (the
// GET /exchange-rate/current endpoint). RateDate and Source are empty when
// the rate was never persisted (CurrentExchangeRateDetail's live-fetch
// fallback) — there is no stored row to report them from.
type ExchangeRateDetail struct {
	Rate     float64
	RateDate string // YYYY-MM-DD; empty if not yet persisted
	Source   string // exchange_rates.source ('auto'/'manual'); empty if not yet persisted
}

// CurrentExchangeRateDetail returns the same rate as currentExchangeRate, plus
// the stored rate_date/source when the rate came from a persisted row. Exported
// for the exchange-rate handler; everything else in this codebase that only
// needs the numeric rate keeps using currentExchangeRate.
func CurrentExchangeRateDetail(ctx context.Context, pool *pgxpool.Pool, fetcher exchangerate.Fetcher, tenantID uuid.UUID) (ExchangeRateDetail, error) {
	var d ExchangeRateDetail
	var rateDate time.Time
	err := pool.QueryRow(ctx,
		`SELECT rate, rate_date, source FROM exchange_rates WHERE tenant_id=$1 ORDER BY rate_date DESC LIMIT 1`,
		tenantID,
	).Scan(&d.Rate, &rateDate, &d.Source)
	if err == nil {
		d.RateDate = rateDate.Format("2006-01-02")
		return d, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ExchangeRateDetail{}, fmt.Errorf("load current exchange rate: %w", err)
	}

	rate, err := fetchRate(ctx, fetcher)
	if err != nil {
		return ExchangeRateDetail{}, fmt.Errorf("fetch exchange rate: %w", err)
	}
	return ExchangeRateDetail{Rate: rate}, nil
}

// ResolveUSDAmount computes amountBS = amountUSD * the tenant's current
// BS/USD rate, resolved via currentExchangeRate. Exported so any package
// pricing something in USD (products today; riders/tenant fees once that
// domain lands) converts through the same single rate resolver instead of
// duplicating the multiplication.
func ResolveUSDAmount(ctx context.Context, pool *pgxpool.Pool, fetcher exchangerate.Fetcher, tenantID uuid.UUID, amountUSD float64) (float64, error) {
	rate, err := currentExchangeRate(ctx, pool, fetcher, tenantID)
	if err != nil {
		return 0, err
	}
	return amountUSD * rate, nil
}
