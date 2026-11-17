package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/adapter/storage"
	"github.com/danzt/daas/api/internal/app"
	"github.com/danzt/daas/api/internal/domain/shop"
)

// ShopOrderHandler exposes HTTP endpoints for the public storefront checkout
// flow and the tenant admin order lifecycle.
type ShopOrderHandler struct {
	svc *app.ShopOrderService
}

// NewShopOrderHandler creates a ShopOrderHandler backed by the given pool.
// notifier is optional — when nil, lifecycle state changes skip email sends.
// st is optional — when nil, payment proof uploads return a 500.
func NewShopOrderHandler(pool *pgxpool.Pool, notifier *app.ShopOrderNotifier, st storage.Storage) *ShopOrderHandler {
	svc := app.NewShopOrderService(pool)
	svc.SetNotifier(notifier)
	if st != nil {
		svc.SetStorage(st)
	}
	return &ShopOrderHandler{svc: svc}
}

// SetInvoiceService injects the InternalInvoiceService used for auto-invoicing on MarkPaid.
// Must be called after NewShopOrderHandler, before the server starts accepting requests.
func (h *ShopOrderHandler) SetInvoiceService(svc *app.InternalInvoiceService) {
	h.svc.SetInvoiceService(svc)
}

// SetFiscalInvoiceService wires the fiscal invoice service for auto-fiscal-invoice on MarkPaid.
func (h *ShopOrderHandler) SetFiscalInvoiceService(svc *app.FiscalInvoiceService) {
	h.svc.SetFiscalInvoiceService(svc)
}

// ─── Request DTOs ─────────────────────────────────────────────────────────────

type checkoutLineReq struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type checkoutReq struct {
	CustomerName    string            `json:"customer_name"`
	CustomerEmail   string            `json:"customer_email"`
	CustomerPhone   string            `json:"customer_phone"`
	ShippingAddress string            `json:"shipping_address"`
	DeliveryLat     *float64          `json:"delivery_lat"`
	DeliveryLng     *float64          `json:"delivery_lng"`
	ShippingCity    string            `json:"shipping_city"`
	ShippingNotes   string            `json:"shipping_notes"`
	Notes           string            `json:"notes"`
	Lines           []checkoutLineReq `json:"lines"`
}

// ─── Public endpoints ─────────────────────────────────────────────────────────

// Checkout handles POST /t/:tenantSlug/shop/v1/checkout
//
// Public. No auth required. Tenant comes from PublicTenantMiddleware context.
// Returns 201 with the full order JSON including the one-time access_token.
func (h *ShopOrderHandler) Checkout(c echo.Context) error {
	tenantID, err := getPublicTenantID(c)
	if err != nil {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "tenant_context_missing"})
	}

	var body checkoutReq
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_body"})
	}

	lines, err := buildCheckoutLines(body.Lines)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error":   "invalid_param",
			"message": err.Error(),
		})
	}

	order, err := h.svc.Checkout(c.Request().Context(), tenantID, shop.CreateOrderRequest{
		CustomerName:    body.CustomerName,
		CustomerEmail:   body.CustomerEmail,
		CustomerPhone:   body.CustomerPhone,
		ShippingAddress: body.ShippingAddress,
		DeliveryLat:     body.DeliveryLat,
		DeliveryLng:     body.DeliveryLng,
		ShippingCity:    body.ShippingCity,
		ShippingNotes:   body.ShippingNotes,
		Notes:           body.Notes,
		Lines:           lines,
	})
	if err != nil {
		return mapShopOrderError(c, err)
	}
	return c.JSON(http.StatusCreated, order)
}

// GetOrder handles GET /t/:tenantSlug/shop/v1/orders/:id?access_token=...
//
// Public. Validates access_token against the order.
// Returns 404 if not found or token mismatch (intentionally opaque).
// The response does NOT include the access_token field.
func (h *ShopOrderHandler) GetOrder(c echo.Context) error {
	tenantID, err := getPublicTenantID(c)
	if err != nil {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "tenant_context_missing"})
	}

	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid_param",
			"field": "id",
		})
	}

	token := c.QueryParam("access_token")
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "access_token_required"})
	}

	order, err := h.svc.GetPublicOrder(c.Request().Context(), tenantID, orderID, token)
	if err != nil {
		return mapShopOrderError(c, err)
	}
	// access_token is already cleared by the service; ensure it's not leaked.
	order.AccessToken = ""
	return c.JSON(http.StatusOK, order)
}

// CancelOrder handles POST /t/:tenantSlug/shop/v1/orders/:id/cancel?access_token=...
//
// Public. Customer can cancel if the order status is pending.
func (h *ShopOrderHandler) CancelOrder(c echo.Context) error {
	tenantID, err := getPublicTenantID(c)
	if err != nil {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "tenant_context_missing"})
	}

	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid_param",
			"field": "id",
		})
	}

	token := c.QueryParam("access_token")
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "access_token_required"})
	}

	order, err := h.svc.CustomerCancel(c.Request().Context(), tenantID, orderID, token)
	if err != nil {
		return mapShopOrderError(c, err)
	}
	order.AccessToken = ""
	return c.JSON(http.StatusOK, order)
}

// MyOrders handles GET /t/:tenantSlug/shop/v1/my-orders?email=X
//
// Public. Returns the order history for a customer identified by email.
// The access_token is included in each order so the customer can navigate to
// the order detail page. Returns an empty array when no orders are found —
// never null.
func (h *ShopOrderHandler) MyOrders(c echo.Context) error {
	tenantID, err := getPublicTenantID(c)
	if err != nil {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "tenant_context_missing"})
	}

	email := c.QueryParam("email")
	if email == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error":   "invalid_param",
			"message": "email query parameter is required",
		})
	}
	// Simple email sanity check — just requires an @.
	if !strings.Contains(email, "@") {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error":   "invalid_param",
			"message": "email must be a valid email address",
		})
	}

	orders, err := h.svc.ListCustomerOrders(c.Request().Context(), tenantID, email)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, orders)
}

// ─── Tenant admin endpoints ───────────────────────────────────────────────────

// AdminList handles GET /api/v1/shop-orders
//
// Auth required. Lists orders for the authenticated tenant.
// Optional query params: status, limit (default 20), offset (default 0).
func (h *ShopOrderHandler) AdminList(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	var status *shop.OrderStatus
	if s := c.QueryParam("status"); s != "" {
		st := shop.OrderStatus(s)
		status = &st
	}

	limit := 20
	if v := c.QueryParam("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return WriteProblem(c, http.StatusBadRequest, "bad-request", "limit must be 1-100")
		}
		limit = n
	}

	offset := 0
	if v := c.QueryParam("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return WriteProblem(c, http.StatusBadRequest, "bad-request", "offset must be >= 0")
		}
		offset = n
	}

	orders, total, err := h.svc.ListTenantOrders(c.Request().Context(), tenantID, status, limit, offset)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{
		"data":  orders,
		"total": total,
	})
}

// AdminGet handles GET /api/v1/shop-orders/:id
func (h *ShopOrderHandler) AdminGet(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid order id")
	}

	order, err := h.svc.GetAdminOrder(c.Request().Context(), tenantID, orderID)
	if err != nil {
		return mapShopOrderError(c, err)
	}
	order.AccessToken = ""
	return c.JSON(http.StatusOK, order)
}

// MarkPaid handles POST /api/v1/shop-orders/:id/mark-paid
func (h *ShopOrderHandler) MarkPaid(c echo.Context) error {
	return h.adminTransition(c, func(tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
		return h.svc.MarkPaid(c.Request().Context(), tenantID, orderID)
	})
}

// MarkFulfilled handles POST /api/v1/shop-orders/:id/mark-fulfilled
func (h *ShopOrderHandler) MarkFulfilled(c echo.Context) error {
	return h.adminTransition(c, func(tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
		return h.svc.MarkFulfilled(c.Request().Context(), tenantID, orderID)
	})
}

// MarkDelivered handles POST /api/v1/shop-orders/:id/mark-delivered
func (h *ShopOrderHandler) MarkDelivered(c echo.Context) error {
	return h.adminTransition(c, func(tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
		return h.svc.MarkDelivered(c.Request().Context(), tenantID, orderID)
	})
}

// AdminCancel handles POST /api/v1/shop-orders/:id/cancel (admin version, no token)
func (h *ShopOrderHandler) AdminCancel(c echo.Context) error {
	return h.adminTransition(c, func(tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
		return h.svc.Cancel(c.Request().Context(), tenantID, orderID)
	})
}

// UploadPaymentProof handles POST /t/:tenantSlug/shop/v1/orders/:id/payment-proof?access_token=...
//
// Public. Accepts multipart/form-data with a required "file" field and optional
// "payment_method_id" and "reference" fields. Streams the file into the storage
// adapter — does NOT buffer the full file in memory.
// Returns 200 with the updated order JSON (access_token cleared).
func (h *ShopOrderHandler) UploadPaymentProof(c echo.Context) error {
	tenantID, err := getPublicTenantID(c)
	if err != nil {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "tenant_context_missing"})
	}

	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid_param",
			"field": "id",
		})
	}

	token := c.QueryParam("access_token")
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "access_token_required"})
	}

	// Parse the multipart form (file is streamed — not buffered via ReadAll).
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error":   "payment_proof_required",
			"message": shop.ErrPaymentProofRequired.Error(),
		})
	}

	file, err := fileHeader.Open()
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "cannot open uploaded file")
	}
	defer func() { _ = file.Close() }()

	// Detect content type: trust the browser-provided value only if non-empty;
	// otherwise fall back to the filename extension. We pass it to the service
	// which validates it against the whitelist.
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Optional fields.
	var paymentMethodID *uuid.UUID
	if pmStr := c.FormValue("payment_method_id"); pmStr != "" {
		pmID, err := uuid.Parse(pmStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid_param",
				"field": "payment_method_id",
			})
		}
		paymentMethodID = &pmID
	}

	reference := c.FormValue("reference")
	if len(reference) > 160 {
		reference = reference[:160]
	}

	order, err := h.svc.UploadPaymentProof(
		c.Request().Context(),
		tenantID,
		orderID,
		token,
		fileHeader.Filename,
		contentType,
		fileHeader.Size,
		file,
		paymentMethodID,
		reference,
	)
	if err != nil {
		return mapShopOrderError(c, err)
	}
	order.AccessToken = ""
	return c.JSON(http.StatusOK, order)
}

// ─── Private helpers ──────────────────────────────────────────────────────────

type transitionFn func(tenantID, orderID uuid.UUID) (*shop.ShopOrder, error)

func (h *ShopOrderHandler) adminTransition(c echo.Context, fn transitionFn) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid order id")
	}
	order, err := fn(tenantID, orderID)
	if err != nil {
		return mapShopOrderError(c, err)
	}
	order.AccessToken = ""
	return c.JSON(http.StatusOK, order)
}

func buildCheckoutLines(raw []checkoutLineReq) ([]shop.CreateOrderLineRequest, error) {
	lines := make([]shop.CreateOrderLineRequest, 0, len(raw))
	for _, r := range raw {
		pid, err := uuid.Parse(r.ProductID)
		if err != nil {
			return nil, errors.New("invalid product_id: " + r.ProductID)
		}
		lines = append(lines, shop.CreateOrderLineRequest{
			ProductID: pid,
			Quantity:  r.Quantity,
		})
	}
	return lines, nil
}

func mapShopOrderError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, shop.ErrShopOrderNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{
			"error":   "not_found",
			"message": "Order not found",
		})
	case errors.Is(err, shop.ErrInsufficientStock):
		return c.JSON(http.StatusConflict, map[string]string{
			"error":   "insufficient_stock",
			"message": err.Error(),
		})
	case errors.Is(err, shop.ErrProductNotPurchasable):
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{
			"error":   "product_not_purchasable",
			"message": err.Error(),
		})
	case errors.Is(err, shop.ErrCustomerNameRequired),
		errors.Is(err, shop.ErrCustomerEmailRequired),
		errors.Is(err, shop.ErrShippingAddressRequired),
		errors.Is(err, shop.ErrDeliveryLocationRequired),
		errors.Is(err, shop.ErrCustomerPhoneRequired),
		errors.Is(err, shop.ErrEmptyOrder),
		errors.Is(err, shop.ErrInvalidQuantity):
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error":   "validation_error",
			"message": err.Error(),
		})
	case errors.Is(err, shop.ErrInvalidTransition):
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{
			"error":   "invalid_transition",
			"message": err.Error(),
		})
	case errors.Is(err, shop.ErrInvalidAccessToken):
		return c.JSON(http.StatusNotFound, map[string]string{
			"error":   "not_found",
			"message": "Order not found",
		})
	case errors.Is(err, shop.ErrPaymentProofRequired):
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error":   "payment_proof_required",
			"message": err.Error(),
		})
	case errors.Is(err, shop.ErrPaymentProofInvalidType):
		return c.JSON(http.StatusUnsupportedMediaType, map[string]string{
			"error":   "unsupported_media_type",
			"message": err.Error(),
		})
	case errors.Is(err, shop.ErrPaymentProofTooLarge):
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{
			"error":   "file_too_large",
			"message": err.Error(),
		})
	case errors.Is(err, shop.ErrPaymentProofAlreadyExists):
		return c.JSON(http.StatusConflict, map[string]string{
			"error":   "proof_already_submitted",
			"message": err.Error(),
		})
	default:
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
}
