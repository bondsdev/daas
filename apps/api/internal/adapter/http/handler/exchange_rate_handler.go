package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/adapter/exchangerate"
	"github.com/danzt/daas/api/internal/app"
)

// ExchangeRateHandler exposes read-only access to the tenant's current BS/USD
// exchange rate, independent of the cash-session/product flows that already
// consume it internally.
type ExchangeRateHandler struct {
	pool    *pgxpool.Pool
	fetcher exchangerate.Fetcher
}

// NewExchangeRateHandler creates an ExchangeRateHandler backed by the given
// pool, using the real BCV public-rate fetcher.
func NewExchangeRateHandler(pool *pgxpool.Pool) *ExchangeRateHandler {
	return &ExchangeRateHandler{pool: pool, fetcher: exchangerate.NewBCVFetcher("")}
}

type currentExchangeRateResp struct {
	Rate     float64 `json:"rate"`
	RateDate string  `json:"rate_date,omitempty"`
	Source   string  `json:"source,omitempty"`
}

// Current handles GET /api/v1/exchange-rate/current
func (h *ExchangeRateHandler) Current(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	detail, err := app.CurrentExchangeRateDetail(c.Request().Context(), h.pool, h.fetcher, tenantID)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}

	return c.JSON(http.StatusOK, currentExchangeRateResp{
		Rate:     detail.Rate,
		RateDate: detail.RateDate,
		Source:   detail.Source,
	})
}
