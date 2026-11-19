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
// recorded for the tenant, or the stored row predates today (nobody opened a
// cash session today, so nothing refreshed it), it tries the fetcher directly
// — falling back to the stale stored value only if that live fetch also fails.
//
// This is read-only/best-effort: a freshly fetched fallback rate is NOT
// persisted here — persisting into exchange_rates remains
// CashSessionService.Open's job. Both CashSessionService.CurrentRate and
// ProductService call this so there is exactly one implementation of
// "what's the tenant's current rate right now".
func currentExchangeRate(ctx context.Context, pool *pgxpool.Pool, fetcher exchangerate.Fetcher, tenantID uuid.UUID) (float64, error) {
	var rate float64
	var rateDate time.Time
	err := pool.QueryRow(ctx,
		`SELECT rate, rate_date FROM exchange_rates WHERE tenant_id=$1 ORDER BY rate_date DESC LIMIT 1`,
		tenantID,
	).Scan(&rate, &rateDate)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("load current exchange rate: %w", err)
	}
	haveStoredRate := err == nil

	if !haveStoredRate || !isToday(rateDate) {
		if fresh, fetchErr := fetchRate(ctx, fetcher); fetchErr == nil {
			return fresh, nil
		} else if !haveStoredRate {
			return 0, fmt.Errorf("fetch exchange rate: %w", fetchErr)
		}
		// Live fetch failed but a stale stored rate exists — degrade to it
		// rather than erroring out the caller entirely.
	}
	return rate, nil
}

// isToday reports whether t falls on today's UTC calendar date. exchange_rates
// stores one row per tenant per day, so "today" is the freshness boundary.
func isToday(t time.Time) bool {
	now := time.Now().UTC()
	y1, m1, d1 := t.UTC().Date()
	y2, m2, d2 := now.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
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
// the stored rate_date/source when the rate came from a persisted row that is
// actually fresh (today). A stale stored row (no cash session opened today)
// is treated the same as no row at all: a live fetch is tried first, and
// RateDate/Source stay empty since nothing fresh was actually persisted.
// Exported for the exchange-rate handler; everything else in this codebase
// that only needs the numeric rate keeps using currentExchangeRate.
func CurrentExchangeRateDetail(ctx context.Context, pool *pgxpool.Pool, fetcher exchangerate.Fetcher, tenantID uuid.UUID) (ExchangeRateDetail, error) {
	var d ExchangeRateDetail
	var rateDate time.Time
	err := pool.QueryRow(ctx,
		`SELECT rate, rate_date, source FROM exchange_rates WHERE tenant_id=$1 ORDER BY rate_date DESC LIMIT 1`,
		tenantID,
	).Scan(&d.Rate, &rateDate, &d.Source)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ExchangeRateDetail{}, fmt.Errorf("load current exchange rate: %w", err)
	}
	haveStoredRate := err == nil

	if haveStoredRate && isToday(rateDate) {
		d.RateDate = rateDate.Format("2006-01-02")
		return d, nil
	}

	if fresh, fetchErr := fetchRate(ctx, fetcher); fetchErr == nil {
		return ExchangeRateDetail{Rate: fresh}, nil
	} else if !haveStoredRate {
		return ExchangeRateDetail{}, fmt.Errorf("fetch exchange rate: %w", fetchErr)
	}

	// Live fetch failed but a stale stored rate exists — degrade to it,
	// still reporting its real (stale) date/source rather than hiding it.
	d.RateDate = rateDate.Format("2006-01-02")
	return d, nil
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
