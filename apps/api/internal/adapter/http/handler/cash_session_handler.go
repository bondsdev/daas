package handler

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/adapter/exchangerate"
	"github.com/danzt/daas/api/internal/app"
	"github.com/danzt/daas/api/internal/domain/cashsession"
)

// CashSessionHandler exposes HTTP endpoints for cash register sessions.
type CashSessionHandler struct {
	svc *app.CashSessionService
}

// NewCashSessionHandler creates a CashSessionHandler backed by the given pool,
// using the real BCV public-rate fetcher.
func NewCashSessionHandler(pool *pgxpool.Pool) *CashSessionHandler {
	return &CashSessionHandler{svc: app.NewCashSessionService(pool, exchangerate.NewBCVFetcher(""))}
}

// ─── Request DTOs ─────────────────────────────────────────────────────────────

type openCashSessionReq struct {
	ManualRate *float64 `json:"manual_rate"`
	Notes      string   `json:"notes"`
}

type closeCashSessionReq struct {
	Notes string `json:"notes"`
}

// ─── Handlers ────────────────────────────────────────────────────────────────

// Open handles POST /api/v1/cash-sessions/open
func (h *CashSessionHandler) Open(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supabaseUID, err := getSupabaseUID(c)
	if err != nil {
		return WriteProblem(c, http.StatusUnauthorized, "unauthorized", "missing user context")
	}

	var body openCashSessionReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid request body")
	}

	sess, err := h.svc.Open(c.Request().Context(), tenantID, supabaseUID, body.ManualRate, body.Notes)
	if err != nil {
		return mapCashSessionError(c, err)
	}
	return c.JSON(http.StatusCreated, sess)
}

// Close handles POST /api/v1/cash-sessions/close
func (h *CashSessionHandler) Close(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supabaseUID, err := getSupabaseUID(c)
	if err != nil {
		return WriteProblem(c, http.StatusUnauthorized, "unauthorized", "missing user context")
	}

	var body closeCashSessionReq
	_ = c.Bind(&body)

	sess, err := h.svc.Close(c.Request().Context(), tenantID, supabaseUID, body.Notes)
	if err != nil {
		return mapCashSessionError(c, err)
	}
	return c.JSON(http.StatusOK, sess)
}

// GetOpen handles GET /api/v1/cash-sessions/open
func (h *CashSessionHandler) GetOpen(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	sess, err := h.svc.GetOpen(c.Request().Context(), tenantID)
	if err != nil {
		return mapCashSessionError(c, err)
	}
	return c.JSON(http.StatusOK, sess)
}

// ─── Private helpers ──────────────────────────────────────────────────────────

func mapCashSessionError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, cashsession.ErrNoOpenSession):
		return WriteProblem(c, http.StatusNotFound, "not-found", err.Error())
	case errors.Is(err, cashsession.ErrSessionAlreadyOpen),
		errors.Is(err, cashsession.ErrInvalidRate),
		errors.Is(err, cashsession.ErrRateRequired):
		return WriteProblem(c, http.StatusUnprocessableEntity, "unprocessable", err.Error())
	default:
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
}
