package handler

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/app"
	"github.com/danzt/daas/api/internal/domain/cashsession"
	"github.com/danzt/daas/api/internal/domain/report"
)

// ReportHandler exposes read-only reporting endpoints.
type ReportHandler struct {
	svc *app.ReportService
}

func NewReportHandler(pool *pgxpool.Pool) *ReportHandler {
	return &ReportHandler{svc: app.NewReportService(pool)}
}

// parseDateRange reads ?from= and ?to= query params.
// Defaults: from = 30 days ago, to = now.
func parseDateRange(c echo.Context) report.DateRange {
	now := time.Now().UTC()
	defaultFrom := now.AddDate(0, 0, -30)

	dr := report.DateRange{From: defaultFrom, To: now}

	if f := c.QueryParam("from"); f != "" {
		if t, err := time.Parse("2006-01-02", f); err == nil {
			dr.From = t.UTC()
		}
	}
	if t := c.QueryParam("to"); t != "" {
		if parsed, err := time.Parse("2006-01-02", t); err == nil {
			dr.To = parsed.Add(24*time.Hour - time.Second).UTC() // inclusive end-of-day
		}
	}

	return dr
}

// SalesReport godoc
// GET /api/v1/reports/sales?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *ReportHandler) SalesReport(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	dr := parseDateRange(c)
	result, err := h.svc.SalesReport(c.Request().Context(), tenantID, dr)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// InventoryReport godoc
// GET /api/v1/reports/inventory
func (h *ReportHandler) InventoryReport(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	result, err := h.svc.InventorySnapshot(c.Request().Context(), tenantID)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// PurchaseReport godoc
// GET /api/v1/reports/purchases?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *ReportHandler) PurchaseReport(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	dr := parseDateRange(c)
	result, err := h.svc.PurchaseReport(c.Request().Context(), tenantID, dr)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// SalesBook godoc
// GET /api/v1/reports/sales-book?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *ReportHandler) SalesBook(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	dr := parseDateRange(c)
	result, err := h.svc.SalesBook(c.Request().Context(), tenantID, dr)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// CashSessionSummary godoc
// GET /api/v1/cash-sessions/:id/summary
func (h *ReportHandler) CashSessionSummary(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid cash session id")
	}
	result, err := h.svc.CashSessionSummary(c.Request().Context(), tenantID, sessionID)
	if err != nil {
		if errors.Is(err, cashsession.ErrSessionNotFound) {
			return WriteProblem(c, http.StatusNotFound, "not-found", err.Error())
		}
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// ─── CSV Export handlers ──────────────────────────────────────────────────────

func writeCSV(c echo.Context, filename string, header []string, rows [][]string) error {
	c.Response().Header().Set("Content-Type", "text/csv; charset=utf-8")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Response().WriteHeader(http.StatusOK)

	w := csv.NewWriter(c.Response().Writer)
	if err := w.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// ExportSalesCSV godoc
// GET /api/v1/reports/sales/export?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *ReportHandler) ExportSalesCSV(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	dr := parseDateRange(c)
	result, err := h.svc.SalesReport(c.Request().Context(), tenantID, dr)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}

	header := []string{"Fecha", "Ingresos", "Facturas"}
	rows := make([][]string, 0, len(result.ByDay))
	for _, d := range result.ByDay {
		rows = append(rows, []string{
			d.Date.Format("2006-01-02"),
			fmt.Sprintf("%.2f", d.Revenue),
			fmt.Sprintf("%d", d.Count),
		})
	}

	filename := fmt.Sprintf("ventas-%s-%s.csv", dr.From.Format("20060102"), dr.To.Format("20060102"))
	return writeCSV(c, filename, header, rows)
}

// ExportInventoryCSV godoc
// GET /api/v1/reports/inventory/export
func (h *ReportHandler) ExportInventoryCSV(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	result, err := h.svc.InventorySnapshot(c.Request().Context(), tenantID)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}

	header := []string{"Producto", "SKU", "Categoría", "Tipo", "Stock"}
	rows := make([][]string, 0, len(result.Items))
	for _, item := range result.Items {
		kind := "Interno"
		if item.IsFiscal {
			kind = "Fiscal"
		}
		rows = append(rows, []string{
			item.ProductName,
			item.SKU,
			item.CategoryName,
			kind,
			fmt.Sprintf("%.3f", item.QuantityOnHand),
		})
	}

	filename := fmt.Sprintf("inventario-%s.csv", time.Now().Format("20060102"))
	return writeCSV(c, filename, header, rows)
}

// ExportPurchasesCSV godoc
// GET /api/v1/reports/purchases/export?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *ReportHandler) ExportPurchasesCSV(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	dr := parseDateRange(c)
	result, err := h.svc.PurchaseReport(c.Request().Context(), tenantID, dr)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}

	header := []string{"Proveedor", "Órdenes", "Monto total"}
	rows := make([][]string, 0, len(result.BySupplier))
	for _, sup := range result.BySupplier {
		rows = append(rows, []string{
			sup.SupplierName,
			fmt.Sprintf("%d", sup.OrderCount),
			fmt.Sprintf("%.2f", sup.TotalSpend),
		})
	}

	filename := fmt.Sprintf("compras-%s-%s.csv", dr.From.Format("20060102"), dr.To.Format("20060102"))
	return writeCSV(c, filename, header, rows)
}
