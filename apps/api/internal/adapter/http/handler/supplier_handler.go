package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/app"
	"github.com/danzt/daas/api/internal/domain/supplier"
)

// SupplierHandler exposes HTTP endpoints for supplier management and purchase orders.
type SupplierHandler struct {
	svc *app.SupplierService
}

func NewSupplierHandler(pool *pgxpool.Pool) *SupplierHandler {
	return &SupplierHandler{svc: app.NewSupplierService(pool)}
}

// ─── Request DTOs ─────────────────────────────────────────────────────────────

type createSupplierReq struct {
	Name        string `json:"name"`
	RIF         string `json:"rif"`
	ContactName string `json:"contact_name"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	Address     string `json:"address"`
	Notes       string `json:"notes"`
}

type updateSupplierReq struct {
	Name        *string `json:"name"`
	RIF         *string `json:"rif"`
	ContactName *string `json:"contact_name"`
	Email       *string `json:"email"`
	Phone       *string `json:"phone"`
	Address     *string `json:"address"`
	Notes       *string `json:"notes"`
	Active      *bool   `json:"active"`
}

type createPOLineReq struct {
	ProductID   string  `json:"product_id"`
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitCost    float64 `json:"unit_cost"`
	Unit        string  `json:"unit,omitempty"`
}

type createPOReq struct {
	SupplierID string            `json:"supplier_id"`
	Notes      string            `json:"notes"`
	Currency   string            `json:"currency,omitempty"`
	Lines      []createPOLineReq `json:"lines"`
}

// updatePOReq is the body for PUT /purchase-orders/:id — same shape as
// createPOReq, used to replace a draft PO's supplier/notes/currency/lines.
type updatePOReq struct {
	SupplierID string            `json:"supplier_id"`
	Notes      string            `json:"notes"`
	Currency   string            `json:"currency,omitempty"`
	Lines      []createPOLineReq `json:"lines"`
}

// ─── Supplier handlers ────────────────────────────────────────────────────────

func (h *SupplierHandler) CreateSupplier(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	var body createSupplierReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid body")
	}
	sup, err := h.svc.CreateSupplier(c.Request().Context(), tenantID, supplier.CreateSupplierRequest{
		Name: body.Name, RIF: body.RIF, ContactName: body.ContactName,
		Email: body.Email, Phone: body.Phone, Address: body.Address, Notes: body.Notes,
	})
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusCreated, sup)
}

func (h *SupplierHandler) ListSuppliers(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	activeOnly := c.QueryParam("active") != "false"
	suppliers, err := h.svc.ListSuppliers(c.Request().Context(), tenantID, activeOnly)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	if suppliers == nil {
		suppliers = []*supplier.Supplier{}
	}
	return c.JSON(http.StatusOK, suppliers)
}

func (h *SupplierHandler) GetSupplier(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid supplier id")
	}
	sup, err := h.svc.GetSupplier(c.Request().Context(), tenantID, supplierID)
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, sup)
}

func (h *SupplierHandler) UpdateSupplier(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid supplier id")
	}
	var body updateSupplierReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid body")
	}
	sup, err := h.svc.UpdateSupplier(c.Request().Context(), tenantID, supplierID, supplier.UpdateSupplierRequest{
		Name: body.Name, RIF: body.RIF, ContactName: body.ContactName,
		Email: body.Email, Phone: body.Phone, Address: body.Address,
		Notes: body.Notes, Active: body.Active,
	})
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, sup)
}

// GeneratePortalToken handles POST /suppliers/:id/portal-token.
func (h *SupplierHandler) GeneratePortalToken(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid supplier id")
	}
	sup, err := h.svc.GeneratePortalToken(c.Request().Context(), tenantID, supplierID)
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, sup)
}

// RevokePortalToken handles DELETE /suppliers/:id/portal-token.
func (h *SupplierHandler) RevokePortalToken(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid supplier id")
	}
	sup, err := h.svc.RevokePortalToken(c.Request().Context(), tenantID, supplierID)
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, sup)
}

// GetSupplierCatalog handles GET /suppliers/:id/catalog (authed, panel).
// Lista el catálogo que el proveedor cargó desde su portal.
func (h *SupplierHandler) GetSupplierCatalog(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid supplier id")
	}
	if _, err := h.svc.GetSupplier(c.Request().Context(), tenantID, supplierID); err != nil {
		return mapSupplierError(c, err)
	}
	catalog, err := h.svc.ListCatalog(c.Request().Context(), supplierID)
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, catalog)
}

// ImportCatalogItem handles POST /suppliers/:id/catalog/:itemId/import (authed).
// Crea un producto del tenant a partir de un ítem del catálogo del proveedor.
func (h *SupplierHandler) ImportCatalogItem(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid supplier id")
	}
	itemID, err := uuid.Parse(c.Param("itemId"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid item id")
	}
	pid, name, err := h.svc.ImportCatalogItem(c.Request().Context(), tenantID, supplierID, itemID)
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]string{"product_id": pid.String(), "name": name})
}

// ─── Portal público del proveedor (Fase 2b) — sin auth, resuelto por token ───

type portalInfoResponse struct {
	SupplierName string                 `json:"supplier_name"`
	StoreName    string                 `json:"store_name"`
	Catalog      []supplier.CatalogItem `json:"catalog"`
}

type addCatalogItemReq struct {
	Name        string  `json:"name"`
	SKU         string  `json:"sku"`
	Cost        float64 `json:"cost"`
	Unit        string  `json:"unit"`
	Barcode     string  `json:"barcode"`
	Description string  `json:"description"`
}

// resolvePortal valida el token y devuelve el proveedor. 404 si no existe.
func (h *SupplierHandler) resolvePortal(c echo.Context) (*supplier.Supplier, error) {
	sup, err := h.svc.SupplierByToken(c.Request().Context(), c.Param("token"))
	if err != nil {
		return nil, WriteProblem(c, http.StatusNotFound, "not-found", "portal no encontrado")
	}
	return sup, nil
}

// GetPortal handles GET /api/v1/supplier-portal/:token.
func (h *SupplierHandler) GetPortal(c echo.Context) error {
	sup, errResp := h.resolvePortal(c)
	if sup == nil {
		return errResp
	}
	catalog, err := h.svc.ListCatalog(c.Request().Context(), sup.ID)
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, portalInfoResponse{
		SupplierName: sup.Name,
		StoreName:    h.svc.StoreName(c.Request().Context(), sup.TenantID),
		Catalog:      catalog,
	})
}

// AddPortalCatalogItem handles POST /api/v1/supplier-portal/:token/catalog.
func (h *SupplierHandler) AddPortalCatalogItem(c echo.Context) error {
	sup, errResp := h.resolvePortal(c)
	if sup == nil {
		return errResp
	}
	var body addCatalogItemReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid body")
	}
	item, err := h.svc.AddCatalogItem(c.Request().Context(), sup.TenantID, sup.ID, supplier.CreateCatalogItemRequest{
		Name: body.Name, SKU: body.SKU, Cost: body.Cost,
		Unit: body.Unit, Barcode: body.Barcode, Description: body.Description,
	})
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusCreated, item)
}

// DeletePortalCatalogItem handles DELETE /api/v1/supplier-portal/:token/catalog/:itemId.
func (h *SupplierHandler) DeletePortalCatalogItem(c echo.Context) error {
	sup, errResp := h.resolvePortal(c)
	if sup == nil {
		return errResp
	}
	itemID, err := uuid.Parse(c.Param("itemId"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid item id")
	}
	if err := h.svc.DeleteCatalogItem(c.Request().Context(), sup.ID, itemID); err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "eliminado"})
}

// ImportPortalCatalogExcel handles POST /api/v1/supplier-portal/:token/catalog/import-excel.
// Recibe un .xlsx (multipart, campo "file") y carga el catálogo en masa.
func (h *SupplierHandler) ImportPortalCatalogExcel(c echo.Context) error {
	sup, errResp := h.resolvePortal(c)
	if sup == nil {
		return errResp
	}
	fh, err := c.FormFile("file")
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "subí un archivo Excel (.xlsx)")
	}
	src, err := fh.Open()
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "no se pudo abrir el archivo")
	}
	defer func() { _ = src.Close() }()
	data, err := io.ReadAll(src)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "no se pudo leer el archivo")
	}

	imported, skipped, err := h.svc.ImportCatalogExcel(c.Request().Context(), sup.TenantID, sup.ID, data)
	if err != nil {
		return WriteProblem(c, http.StatusUnprocessableEntity, "invalid-file", err.Error())
	}
	return c.JSON(http.StatusOK, map[string]int{"imported": imported, "skipped": skipped})
}

// ─── Purchase Order handlers ──────────────────────────────────────────────────

func (h *SupplierHandler) CreatePO(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supabaseUID, err := getSupabaseUID(c)
	if err != nil {
		return WriteProblem(c, http.StatusUnauthorized, "unauthorized", "missing user context")
	}
	var body createPOReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid body")
	}
	supplierID, err := uuid.Parse(body.SupplierID)
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid supplier_id")
	}
	lines, err := buildPOLines(body.Lines)
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", err.Error())
	}
	po, err := h.svc.CreatePO(c.Request().Context(), tenantID, supabaseUID, supplier.CreatePORequest{
		SupplierID: supplierID, Notes: body.Notes, Currency: body.Currency, Lines: lines,
	})
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusCreated, po)
}

// UpdatePO handles PUT /api/v1/purchase-orders/:id — full replace of a draft
// PO's supplier, notes, currency and lines.
func (h *SupplierHandler) UpdatePO(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	poID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid po id")
	}
	var body updatePOReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid body")
	}
	supplierID, err := uuid.Parse(body.SupplierID)
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid supplier_id")
	}
	lines, err := buildPOLines(body.Lines)
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", err.Error())
	}
	po, err := h.svc.UpdatePO(c.Request().Context(), tenantID, poID, supplier.UpdatePORequest{
		SupplierID: supplierID, Notes: body.Notes, Currency: body.Currency, Lines: lines,
	})
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, po)
}

func (h *SupplierHandler) ListPOs(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	var supplierID *uuid.UUID
	if s := c.QueryParam("supplier_id"); s != "" {
		id, err := uuid.Parse(s)
		if err == nil {
			supplierID = &id
		}
	}
	var status *supplier.POStatus
	if s := c.QueryParam("status"); s != "" {
		st := supplier.POStatus(s)
		status = &st
	}
	pos, err := h.svc.ListPOs(c.Request().Context(), tenantID, supplierID, status)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	if pos == nil {
		pos = []*supplier.PurchaseOrder{}
	}
	return c.JSON(http.StatusOK, pos)
}

func (h *SupplierHandler) GetPO(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	poID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid po id")
	}
	po, err := h.svc.GetPO(c.Request().Context(), tenantID, poID)
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, po)
}

func (h *SupplierHandler) OrderPO(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supabaseUID, err := getSupabaseUID(c)
	if err != nil {
		return WriteProblem(c, http.StatusUnauthorized, "unauthorized", "missing user context")
	}
	poID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid po id")
	}
	po, err := h.svc.OrderPO(c.Request().Context(), tenantID, poID, supabaseUID)
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, po)
}

func (h *SupplierHandler) ReceivePO(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supabaseUID, err := getSupabaseUID(c)
	if err != nil {
		return WriteProblem(c, http.StatusUnauthorized, "unauthorized", "missing user context")
	}
	poID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid po id")
	}
	po, err := h.svc.ReceivePO(c.Request().Context(), tenantID, poID, supabaseUID)
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, po)
}

func (h *SupplierHandler) CancelPO(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	supabaseUID, err := getSupabaseUID(c)
	if err != nil {
		return WriteProblem(c, http.StatusUnauthorized, "unauthorized", "missing user context")
	}
	poID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid po id")
	}
	po, err := h.svc.CancelPO(c.Request().Context(), tenantID, poID, supabaseUID)
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, po)
}

// setPurchaseInvoiceReq is the body for PUT /purchase-orders/:id/invoice.
type setPurchaseInvoiceReq struct {
	InvoiceNumber string  `json:"invoice_number"`
	InvoiceDate   string  `json:"invoice_date"` // "YYYY-MM-DD" (optional)
	TaxBase       float64 `json:"tax_base"`
	TaxAmount     float64 `json:"tax_amount"`
}

// SetPurchaseInvoice records the supplier's invoice on a purchase order.
func (h *SupplierHandler) SetPurchaseInvoice(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	poID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid po id")
	}
	var req setPurchaseInvoiceReq
	if err := c.Bind(&req); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid request body")
	}

	var invoiceDate *time.Time
	if s := strings.TrimSpace(req.InvoiceDate); s != "" {
		d, perr := time.Parse("2006-01-02", s)
		if perr != nil {
			return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error",
				"invoice_date must be YYYY-MM-DD")
		}
		invoiceDate = &d
	}

	po, err := h.svc.SetPurchaseInvoice(c.Request().Context(), tenantID, poID, supplier.SetPurchaseInvoiceRequest{
		InvoiceNumber: strings.TrimSpace(req.InvoiceNumber),
		InvoiceDate:   invoiceDate,
		TaxBase:       req.TaxBase,
		TaxAmount:     req.TaxAmount,
	})
	if err != nil {
		return mapSupplierError(c, err)
	}
	return c.JSON(http.StatusOK, po)
}

// ─── Private helpers ──────────────────────────────────────────────────────────

func buildPOLines(raw []createPOLineReq) ([]supplier.CreatePOLineRequest, error) {
	lines := make([]supplier.CreatePOLineRequest, 0, len(raw))
	for _, r := range raw {
		pid, err := uuid.Parse(r.ProductID)
		if err != nil {
			return nil, fmt.Errorf("invalid product_id: %s", r.ProductID)
		}
		lines = append(lines, supplier.CreatePOLineRequest{
			ProductID: pid, Description: r.Description,
			Quantity: r.Quantity, UnitCost: r.UnitCost, Unit: r.Unit,
		})
	}
	return lines, nil
}

func mapSupplierError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, supplier.ErrSupplierNotFound),
		errors.Is(err, supplier.ErrPONotFound),
		errors.Is(err, supplier.ErrCatalogItemNotFound):
		return WriteProblem(c, http.StatusNotFound, "not-found", err.Error())
	case errors.Is(err, supplier.ErrSupplierNameRequired),
		errors.Is(err, supplier.ErrEmptyPO),
		errors.Is(err, supplier.ErrPOAlreadyOrdered),
		errors.Is(err, supplier.ErrPOAlreadyReceived),
		errors.Is(err, supplier.ErrPOAlreadyCancelled),
		errors.Is(err, supplier.ErrPONotOrdered),
		errors.Is(err, supplier.ErrPONotDraft),
		errors.Is(err, supplier.ErrInvoiceNumberRequired),
		errors.Is(err, supplier.ErrCatalogNameRequired),
		errors.Is(err, supplier.ErrCatalogItemNoCost),
		errors.Is(err, supplier.ErrProductSKUExists):
		return WriteProblem(c, http.StatusUnprocessableEntity, "unprocessable", err.Error())
	default:
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
}
