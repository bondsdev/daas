package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/adapter/http/middleware"
	"github.com/danzt/daas/api/internal/adapter/routing"
	"github.com/danzt/daas/api/internal/app"
	"github.com/danzt/daas/api/internal/domain/rider"
	"github.com/danzt/daas/api/internal/domain/shop"
)

// RiderHandler exposes HTTP endpoints for rider management and the
// delivery lifecycle on shop orders.
type RiderHandler struct {
	svc *app.RiderService
}

// NewRiderHandler creates a RiderHandler backed by the given pool. The
// route-map endpoint (GetMyRoute) will 502 until a routing provider is
// wired via NewRiderHandlerWithRouting.
func NewRiderHandler(pool *pgxpool.Pool) *RiderHandler {
	return &RiderHandler{svc: app.NewRiderService(pool)}
}

// NewRiderHandlerWithRouting creates a RiderHandler with a routing
// provider (e.g. routing.ORSClient) wired in, enabling GetMyRoute.
func NewRiderHandlerWithRouting(pool *pgxpool.Pool, routeProvider routing.Provider) *RiderHandler {
	return &RiderHandler{svc: app.NewRiderServiceWithRouting(pool, routeProvider)}
}

// ─── Request DTOs ─────────────────────────────────────────────────────────────

type createRiderReq struct {
	UserID                  string  `json:"user_id"`
	Name                    string  `json:"name"`
	Phone                   string  `json:"phone"`
	RatePerDelivery         float64 `json:"rate_per_delivery"`
	MaxConcurrentDeliveries int     `json:"max_concurrent_deliveries"`
}

type updateRiderReq struct {
	Status                  rider.Status `json:"status"`
	MaxConcurrentDeliveries int          `json:"max_concurrent_deliveries"`
	RatePerDelivery         float64      `json:"rate_per_delivery"`
	Active                  bool         `json:"active"`
}

type assignRiderReq struct {
	RiderID string `json:"rider_id"`
}

type markUrgentReq struct {
	Urgent bool `json:"urgent"`
}

type updateMyStatusReq struct {
	Status rider.Status `json:"status"`
}

// ─── Role gating ──────────────────────────────────────────────────────────────
//
// There is no existing middleware for "owner or employee, not rider" or
// "rider only" — OwnerGuard (owner_guard.go) only covers owner-exclusive
// routes. Riders are a new role added by this feature, so these checks are
// done inline here, reusing the exact same ContextKeyRole mechanism.

func currentRole(c echo.Context) string {
	role, _ := c.Get(string(middleware.ContextKeyRole)).(string)
	return role
}

func isStaffRole(role string) bool {
	return role == "owner" || role == "employee"
}

// requireStaff writes a 403 and returns false when the current role is not
// owner/employee. Callers must bail out when it returns false:
// `if !requireStaff(c) { return nil }`.
func requireStaff(c echo.Context) bool {
	if isStaffRole(currentRole(c)) {
		return true
	}
	_ = WriteProblem(c, http.StatusForbidden, "forbidden", "this action requires owner or employee role")
	return false
}

// requireRiderRole mirrors requireStaff for rider-only routes.
func requireRiderRole(c echo.Context) bool {
	if currentRole(c) == "rider" {
		return true
	}
	_ = WriteProblem(c, http.StatusForbidden, "forbidden", "this action requires rider role")
	return false
}

// ─── Rider management ─────────────────────────────────────────────────────────

// CreateRider handles POST /api/v1/riders. Owner/employee only.
func (h *RiderHandler) CreateRider(c echo.Context) error {
	if !requireStaff(c) {
		return nil
	}
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	var body createRiderReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid request body")
	}
	userID, err := uuid.Parse(body.UserID)
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid user_id")
	}

	r, err := h.svc.CreateRider(c.Request().Context(), tenantID, rider.CreateRiderRequest{
		UserID:                  userID,
		Name:                    body.Name,
		Phone:                   body.Phone,
		RatePerDelivery:         body.RatePerDelivery,
		MaxConcurrentDeliveries: body.MaxConcurrentDeliveries,
	})
	if err != nil {
		return mapRiderError(c, err)
	}
	return c.JSON(http.StatusCreated, r)
}

// ListRiders handles GET /api/v1/riders. Owner/employee only.
func (h *RiderHandler) ListRiders(c echo.Context) error {
	if !requireStaff(c) {
		return nil
	}
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	riders, err := h.svc.ListRiders(c.Request().Context(), tenantID)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, riders)
}

// UpdateRider handles PATCH /api/v1/riders/:id. Owner/employee only.
func (h *RiderHandler) UpdateRider(c echo.Context) error {
	if !requireStaff(c) {
		return nil
	}
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	riderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid rider id")
	}

	var body updateRiderReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid request body")
	}

	r, err := h.svc.UpdateRider(c.Request().Context(), tenantID, riderID, rider.UpdateRiderRequest{
		Status:                  body.Status,
		MaxConcurrentDeliveries: body.MaxConcurrentDeliveries,
		RatePerDelivery:         body.RatePerDelivery,
		Active:                  body.Active,
	})
	if err != nil {
		return mapRiderError(c, err)
	}
	return c.JSON(http.StatusOK, r)
}

// ─── Delivery lifecycle ───────────────────────────────────────────────────────

// AssignRiderManual handles POST /api/v1/orders/:orderId/assign-rider. Owner/employee only.
func (h *RiderHandler) AssignRiderManual(c echo.Context) error {
	if !requireStaff(c) {
		return nil
	}
	tenantID, orderID, err := tenantAndOrderID(c)
	if err != nil {
		return err
	}

	var body assignRiderReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid request body")
	}
	riderID, err := uuid.Parse(body.RiderID)
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid rider_id")
	}

	o, err := h.svc.AssignRiderManual(c.Request().Context(), tenantID, orderID, riderID)
	if err != nil {
		return mapRiderError(c, err)
	}
	return c.JSON(http.StatusOK, o)
}

// AutoAssignRider handles POST /api/v1/orders/:orderId/auto-assign-rider. Owner/employee only.
func (h *RiderHandler) AutoAssignRider(c echo.Context) error {
	if !requireStaff(c) {
		return nil
	}
	tenantID, orderID, err := tenantAndOrderID(c)
	if err != nil {
		return err
	}

	o, err := h.svc.AutoAssignRider(c.Request().Context(), tenantID, orderID)
	if err != nil {
		return mapRiderError(c, err)
	}
	return c.JSON(http.StatusOK, o)
}

// MarkDelivered handles POST /api/v1/orders/:orderId/mark-delivered.
// Owner/employee can mark any order delivered. A rider can only mark
// delivered an order assigned to themselves.
func (h *RiderHandler) MarkDelivered(c echo.Context) error {
	tenantID, orderID, err := tenantAndOrderID(c)
	if err != nil {
		return err
	}

	role := currentRole(c)
	if !isStaffRole(role) {
		if role != "rider" {
			return WriteProblem(c, http.StatusForbidden, "forbidden", "this action requires owner, employee, or rider role")
		}
		if err := h.requireOwnDelivery(c, tenantID, orderID); err != nil {
			return err
		}
	}

	o, err := h.svc.MarkDelivered(c.Request().Context(), tenantID, orderID)
	if err != nil {
		return mapRiderError(c, err)
	}
	return c.JSON(http.StatusOK, o)
}

// MarkUrgent handles PATCH /api/v1/orders/:orderId/urgent. Owner/employee only.
func (h *RiderHandler) MarkUrgent(c echo.Context) error {
	if !requireStaff(c) {
		return nil
	}
	tenantID, orderID, err := tenantAndOrderID(c)
	if err != nil {
		return err
	}

	var body markUrgentReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid request body")
	}

	o, err := h.svc.MarkUrgent(c.Request().Context(), tenantID, orderID, body.Urgent)
	if err != nil {
		return mapRiderError(c, err)
	}
	return c.JSON(http.StatusOK, o)
}

// MarkRiderPaid handles POST /api/v1/orders/:orderId/mark-rider-paid. Owner/employee only.
func (h *RiderHandler) MarkRiderPaid(c echo.Context) error {
	if !requireStaff(c) {
		return nil
	}
	tenantID, orderID, err := tenantAndOrderID(c)
	if err != nil {
		return err
	}

	o, err := h.svc.MarkRiderPaid(c.Request().Context(), tenantID, orderID)
	if err != nil {
		return mapRiderError(c, err)
	}
	return c.JSON(http.StatusOK, o)
}

// MyDeliveries handles GET /api/v1/my-deliveries. Rider only.
func (h *RiderHandler) MyDeliveries(c echo.Context) error {
	if !requireRiderRole(c) {
		return nil
	}
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	r, err := h.ownRider(c, tenantID)
	if err != nil {
		return err
	}

	orders, err := h.svc.ListMyDeliveries(c.Request().Context(), tenantID, r.ID)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, orders)
}

// MyProfile handles GET /api/v1/my-profile. Rider only.
func (h *RiderHandler) MyProfile(c echo.Context) error {
	if !requireRiderRole(c) {
		return nil
	}
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	r, err := h.ownRider(c, tenantID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, r)
}

// UpdateMyStatus handles PATCH /api/v1/my-profile/status. Rider only.
func (h *RiderHandler) UpdateMyStatus(c echo.Context) error {
	if !requireRiderRole(c) {
		return nil
	}
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	own, err := h.ownRider(c, tenantID)
	if err != nil {
		return err
	}

	var body updateMyStatusReq
	if err := c.Bind(&body); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid request body")
	}
	if body.Status != rider.StatusAvailable && body.Status != rider.StatusUnavailable {
		return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error", "status must be 'available' or 'unavailable'")
	}

	r, err := h.svc.UpdateMyStatus(c.Request().Context(), tenantID, own.ID, body.Status)
	if err != nil {
		return mapRiderError(c, err)
	}
	return c.JSON(http.StatusOK, r)
}

// MyDeliveryHistory handles GET /api/v1/my-deliveries/history. Rider only.
func (h *RiderHandler) MyDeliveryHistory(c echo.Context) error {
	if !requireRiderRole(c) {
		return nil
	}
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	r, err := h.ownRider(c, tenantID)
	if err != nil {
		return err
	}

	hist, err := h.svc.GetDeliveryHistory(c.Request().Context(), tenantID, r.ID)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, hist)
}

// GetMyRoute handles GET /api/v1/my-deliveries/route. Rider only. Builds a
// single multi-stop driving route covering all of the rider's active
// deliveries, starting from the rider's current position passed in the
// lat/lng query params.
func (h *RiderHandler) GetMyRoute(c echo.Context) error {
	if !requireRiderRole(c) {
		return nil
	}
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	lat, lng, err := parseLatLngQuery(c)
	if err != nil {
		return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error", err.Error())
	}

	r, err := h.ownRider(c, tenantID)
	if err != nil {
		return err
	}

	route, err := h.svc.GetMyRoute(c.Request().Context(), tenantID, r.ID, lat, lng)
	if err != nil {
		if errors.Is(err, app.ErrRouteUnavailable) {
			return WriteProblem(c, http.StatusBadGateway, "route-unavailable", "no se pudo calcular la ruta en este momento")
		}
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
	return c.JSON(http.StatusOK, route)
}

// parseLatLngQuery reads the required "lat"/"lng" query params as floats.
func parseLatLngQuery(c echo.Context) (lat, lng float64, err error) {
	latStr := c.QueryParam("lat")
	lngStr := c.QueryParam("lng")
	if latStr == "" || lngStr == "" {
		return 0, 0, errors.New("lat and lng query params are required")
	}
	lat, err = strconv.ParseFloat(latStr, 64)
	if err != nil {
		return 0, 0, errors.New("lat must be a valid number")
	}
	lng, err = strconv.ParseFloat(lngStr, 64)
	if err != nil {
		return 0, 0, errors.New("lng must be a valid number")
	}
	return lat, lng, nil
}

// ─── Private helpers ──────────────────────────────────────────────────────────

func tenantAndOrderID(c echo.Context) (uuid.UUID, uuid.UUID, error) {
	tenantID, err := getTenantID(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}
	orderID, err := uuid.Parse(c.Param("orderId"))
	if err != nil {
		return uuid.Nil, uuid.Nil, WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid order id")
	}
	return tenantID, orderID, nil
}

// ownRider resolves the authenticated rider's own rider record.
func (h *RiderHandler) ownRider(c echo.Context, tenantID uuid.UUID) (*rider.Rider, error) {
	supabaseUID, err := getSupabaseUID(c)
	if err != nil {
		return nil, WriteProblem(c, http.StatusForbidden, "forbidden", "user context missing")
	}
	r, err := h.svc.GetRiderBySupabaseUID(c.Request().Context(), tenantID, supabaseUID)
	if err != nil {
		return nil, mapRiderError(c, err)
	}
	return r, nil
}

// requireOwnDelivery rejects with 403 when the order's assigned rider isn't
// the authenticated rider.
func (h *RiderHandler) requireOwnDelivery(c echo.Context, tenantID, orderID uuid.UUID) error {
	own, err := h.ownRider(c, tenantID)
	if err != nil {
		return err
	}
	o, err := h.svc.GetOrderDeliveryState(c.Request().Context(), tenantID, orderID)
	if err != nil {
		return mapRiderError(c, err)
	}
	if o.RiderID == nil || *o.RiderID != own.ID {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "this delivery is not assigned to you")
	}
	return nil
}

func mapRiderError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, rider.ErrRiderNotFound):
		return WriteProblem(c, http.StatusNotFound, "rider-not-found", "rider not found")
	case errors.Is(err, rider.ErrRiderNameRequired),
		errors.Is(err, rider.ErrInvalidMaxConcurrent),
		errors.Is(err, rider.ErrInvalidRate),
		errors.Is(err, rider.ErrInvalidStatus):
		return WriteProblem(c, http.StatusBadRequest, "validation-error", err.Error())
	case errors.Is(err, rider.ErrDeliveryNotEnabled):
		return WriteProblem(c, http.StatusUnprocessableEntity, "delivery-not-enabled", err.Error())
	case errors.Is(err, rider.ErrRiderUnavailable),
		errors.Is(err, rider.ErrRiderAtCapacity),
		errors.Is(err, rider.ErrNoEligibleRider),
		errors.Is(err, shop.ErrRiderAlreadyAssigned):
		return WriteProblem(c, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, shop.ErrNoRiderAssigned):
		return WriteProblem(c, http.StatusUnprocessableEntity, "no-rider-assigned", err.Error())
	case errors.Is(err, shop.ErrShopOrderNotFound):
		return WriteProblem(c, http.StatusNotFound, "order-not-found", "order not found")
	default:
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", err.Error())
	}
}
