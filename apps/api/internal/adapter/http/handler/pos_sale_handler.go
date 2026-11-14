package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/app"
	"github.com/danzt/daas/api/internal/domain/cashsession"
	"github.com/danzt/daas/api/internal/domain/invoice"
)

// PosSaleHandler exposes the POS counter-sale endpoint.
type PosSaleHandler struct {
	svc *app.InternalInvoiceService
}

// NewPosSaleHandler creates a PosSaleHandler backed by an existing
// InternalInvoiceService instance (shared with InvoiceHandler / auto-invoice).
func NewPosSaleHandler(svc *app.InternalInvoiceService) *PosSaleHandler {
	return &PosSaleHandler{svc: svc}
}

// ─── Request DTOs ─────────────────────────────────────────────────────────────

type posSaleLineReq struct {
	ProductID string  `json:"product_id"`
	Quantity  float64 `json:"quantity"`
}

type posSaleReq struct {
	Lines         []posSaleLineReq      `json:"lines"`
	PaymentMethod invoice.PaymentMethod `json:"payment_method"`
	Currency      string                `json:"currency"`
}

// ─── Handlers ────────────────────────────────────────────────────────────────

// Create handles POST /api/v1/pos/sales
func (h *PosSaleHandler) Create(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supabaseUID, err := getSupabaseUID(c)
	if err != nil {
		return WriteProblem(c, http.StatusUnauthorized, "unauthorized", "missing user context")
	}

	var body posSaleReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid request body")
	}

	lines, err := buildPosSaleLines(body.Lines)
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", err.Error())
	}

	req := invoice.PosSaleRequest{
		Lines:         lines,
		PaymentMethod: body.PaymentMethod,
		Currency:      body.Currency,
	}

	inv, err := h.svc.CreatePOSSale(c.Request().Context(), tenantID, supabaseUID, req)
	if err != nil {
		return mapPosSaleError(c, err)
	}
	return c.JSON(http.StatusCreated, inv)
}

// ─── private helpers ──────────────────────────────────────────────────────────

func buildPosSaleLines(raw []posSaleLineReq) ([]invoice.PosSaleLine, error) {
	lines := make([]invoice.PosSaleLine, 0, len(raw))
	for _, r := range raw {
		pid, err := uuid.Parse(r.ProductID)
		if err != nil {
			return nil, fmt.Errorf("invalid product_id: %s", r.ProductID)
		}
		lines = append(lines, invoice.PosSaleLine{
			ProductID: pid,
			Quantity:  r.Quantity,
		})
	}
	return lines, nil
}

func mapPosSaleError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, cashsession.ErrNoOpenSession):
		return WriteProblem(c, http.StatusConflict, "conflict", "no open cash session for this tenant — open one before selling")
	case errors.Is(err, invoice.ErrInsufficientStock):
		return WriteProblem(c, http.StatusUnprocessableEntity, "unprocessable", err.Error())
	case errors.Is(err, invoice.ErrFiscalProductForbidden),
		errors.Is(err, invoice.ErrEmptyInvoice):
		return WriteProblem(c, http.StatusUnprocessableEntity, "unprocessable", err.Error())
	default:
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
}
