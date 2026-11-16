//go:build integration

// Integration tests for ExchangeRateHandler. Build with:
// go test -tags integration ./internal/adapter/http/handler/...
package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/adapter/http/handler"
)

// seedExchangeRate inserts a current-rate row for the given tenant.
func seedExchangeRate(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID, rate float64, source string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO exchange_rates (tenant_id, rate_date, rate, source)
		VALUES ($1, CURRENT_DATE, $2, $3)`,
		tenantID, rate, source,
	)
	if err != nil {
		t.Fatalf("seedExchangeRate: %v", err)
	}
}

func TestExchangeRateHandler_Current_ReturnsTenantRate(t *testing.T) {
	pool := setupTestDB(t)
	tenantID, _ := seedTenant(t, pool, "Exchange Rate Tenant")
	seedExchangeRate(t, pool, tenantID, 42.5, "manual")

	h := handler.NewExchangeRateHandler(pool)
	c, rec := shopTestCtx(t, http.MethodGet, "/api/v1/exchange-rate/current", tenantID.String())

	if err := h.Current(c); err != nil {
		t.Fatalf("Current: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Rate     float64 `json:"rate"`
		RateDate string  `json:"rate_date"`
		Source   string  `json:"source"`
	}
	decodeBody(t, rec, &resp)
	if resp.Rate != 42.5 {
		t.Errorf("expected rate 42.5, got %v", resp.Rate)
	}
	if resp.Source != "manual" {
		t.Errorf("expected source manual, got %q", resp.Source)
	}
	if resp.RateDate == "" {
		t.Errorf("expected non-empty rate_date")
	}
}

func TestExchangeRateHandler_Current_MissingTenantContext_403(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/exchange-rate/current", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := handler.NewExchangeRateHandler(nil)
	if err := h.Current(c); err != nil {
		t.Fatalf("Current: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
