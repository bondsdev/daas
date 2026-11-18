package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/adapter/routing"
	"github.com/danzt/daas/api/internal/domain/rider"
	"github.com/danzt/daas/api/internal/domain/shop"
)

// ErrRouteUnavailable wraps a routing-provider (ORS) failure in
// GetMyRoute, so the handler can tell it apart from a plain internal/DB
// error and answer the rider with "no se pudo calcular la ruta" instead of
// an opaque 500.
var ErrRouteUnavailable = errors.New("route unavailable")

// RiderService manages delivery riders (motorizados) and the delivery
// lifecycle (assignment, delivered, urgent fee, rider payout) on shop orders.
type RiderService struct {
	pool          *pgxpool.Pool
	routeProvider routing.Provider
}

// NewRiderService creates a RiderService backed by the given pool. The
// route-map feature (GetMyRoute) is unavailable until a routing provider is
// set via NewRiderServiceWithRouting or SetRouteProvider.
func NewRiderService(pool *pgxpool.Pool) *RiderService {
	return &RiderService{pool: pool}
}

// NewRiderServiceWithRouting creates a RiderService with a routing
// provider (e.g. routing.ORSClient) wired in, enabling GetMyRoute.
func NewRiderServiceWithRouting(pool *pgxpool.Pool, routeProvider routing.Provider) *RiderService {
	return &RiderService{pool: pool, routeProvider: routeProvider}
}

// SetRouteProvider wires a routing provider onto an already-constructed
// RiderService, mirroring this codebase's SetCashSessionService pattern.
func (s *RiderService) SetRouteProvider(p routing.Provider) {
	s.routeProvider = p
}

// ═══ Rider management ═════════════════════════════════════════════════════

// CreateRider registers a new rider for the tenant. Requires the tenant to
// have delivery enabled.
func (s *RiderService) CreateRider(ctx context.Context, tenantID uuid.UUID, req rider.CreateRiderRequest) (*rider.Rider, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	enabled, err := s.deliveryEnabled(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, rider.ErrDeliveryNotEnabled
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO riders (tenant_id, user_id, name, phone, rate_per_delivery, max_concurrent_deliveries)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, user_id, name, COALESCE(phone, ''), rate_per_delivery,
		          status, max_concurrent_deliveries, last_assigned_at, active, created_at, updated_at`,
		tenantID, req.UserID, req.Name, req.Phone, req.RatePerDelivery, req.MaxConcurrentDeliveries,
	)
	r, err := scanRider(row)
	if err != nil {
		return nil, fmt.Errorf("create rider: %w", err)
	}
	return r, nil
}

// ListRiders returns all riders for the tenant, ordered by name.
func (s *RiderService) ListRiders(ctx context.Context, tenantID uuid.UUID) ([]rider.Rider, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, name, COALESCE(phone, ''), rate_per_delivery,
		       status, max_concurrent_deliveries, last_assigned_at, active, created_at, updated_at
		FROM riders
		WHERE tenant_id = $1
		ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list riders: %w", err)
	}
	defer rows.Close()

	var riders []rider.Rider
	for rows.Next() {
		r, err := scanRider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan rider: %w", err)
		}
		riders = append(riders, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list riders rows: %w", err)
	}
	if riders == nil {
		riders = []rider.Rider{}
	}
	return riders, nil
}

// UpdateRider updates the mutable fields of an existing rider.
func (s *RiderService) UpdateRider(ctx context.Context, tenantID, riderID uuid.UUID, req rider.UpdateRiderRequest) (*rider.Rider, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	row := s.pool.QueryRow(ctx, `
		UPDATE riders
		SET status = $3, max_concurrent_deliveries = $4, rate_per_delivery = $5, active = $6, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, user_id, name, COALESCE(phone, ''), rate_per_delivery,
		          status, max_concurrent_deliveries, last_assigned_at, active, created_at, updated_at`,
		riderID, tenantID,
		req.Status, req.MaxConcurrentDeliveries, req.RatePerDelivery, req.Active,
	)
	r, err := scanRider(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rider.ErrRiderNotFound
		}
		return nil, fmt.Errorf("update rider: %w", err)
	}
	return r, nil
}

// ═══ Delivery assignment ══════════════════════════════════════════════════

// AssignRiderManual assigns a specific rider to an order, chosen by the
// tenant owner. Unlike AutoAssignRider, this does not check rider.HasCapacity
// — the owner can deliberately override capacity.
func (s *RiderService) AssignRiderManual(ctx context.Context, tenantID, orderID, riderID uuid.UUID) (*shop.ShopOrder, error) {
	r, err := s.loadRider(ctx, tenantID, riderID)
	if err != nil {
		return nil, err
	}
	if !r.Active {
		return nil, rider.ErrRiderUnavailable
	}
	return s.assignRider(ctx, tenantID, orderID, r)
}

// AutoAssignRider picks the eligible rider with the oldest LastAssignedAt
// (never-assigned riders go first) and assigns them to the order.
func (s *RiderService) AutoAssignRider(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	r, err := s.pickEligibleRider(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.assignRider(ctx, tenantID, orderID, r)
}

// pickEligibleRider returns the active+available rider with free capacity
// and the oldest LastAssignedAt among the tenant's riders.
func (s *RiderService) pickEligibleRider(ctx context.Context, tenantID uuid.UUID) (*rider.Rider, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.tenant_id, r.user_id, r.name, COALESCE(r.phone, ''), r.rate_per_delivery,
		       r.status, r.max_concurrent_deliveries, r.last_assigned_at, r.active, r.created_at, r.updated_at,
		       COALESCE(cnt.active_count, 0) AS active_count
		FROM riders r
		LEFT JOIN (
			SELECT rider_id, COUNT(*) AS active_count
			FROM shop_orders
			WHERE rider_id IS NOT NULL AND rider_delivered_at IS NULL AND status != 'cancelled'
			GROUP BY rider_id
		) cnt ON cnt.rider_id = r.id
		WHERE r.tenant_id = $1 AND r.active = TRUE AND r.status = 'available'
		ORDER BY r.last_assigned_at ASC NULLS FIRST`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("query eligible riders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		r, activeCount, err := scanRiderWithActiveCount(rows)
		if err != nil {
			return nil, fmt.Errorf("scan eligible rider: %w", err)
		}
		if r.HasCapacity(activeCount) {
			return r, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eligible riders rows: %w", err)
	}
	return nil, rider.ErrNoEligibleRider
}

// assignRider performs the shared assignment side effects: rejects an
// already-assigned order, snapshots the rider's current rate as rider_fee,
// stamps rider_assigned_at, and bumps the rider's last_assigned_at.
func (s *RiderService) assignRider(ctx context.Context, tenantID, orderID uuid.UUID, r *rider.Rider) (*shop.ShopOrder, error) {
	o, err := s.loadOrderDeliveryState(ctx, tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.RiderID != nil {
		return nil, shop.ErrRiderAlreadyAssigned
	}

	now := time.Now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin assign rider tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		UPDATE shop_orders
		SET rider_id = $1, rider_fee = $2, rider_assigned_at = $3, updated_at = $3
		WHERE id = $4 AND tenant_id = $5`,
		r.ID, r.RatePerDelivery, now, orderID, tenantID,
	); err != nil {
		return nil, fmt.Errorf("assign rider to order: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE riders SET last_assigned_at = $1, updated_at = $1 WHERE id = $2 AND tenant_id = $3`,
		now, r.ID, tenantID,
	); err != nil {
		return nil, fmt.Errorf("bump rider last_assigned_at: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit assign rider: %w", err)
	}

	return s.loadOrderDeliveryState(ctx, tenantID, orderID)
}

// ═══ Delivery lifecycle ═══════════════════════════════════════════════════

// MarkDelivered stamps rider_delivered_at. Requires a rider already assigned.
func (s *RiderService) MarkDelivered(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	o, err := s.loadOrderDeliveryState(ctx, tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.RiderID == nil {
		return nil, shop.ErrNoRiderAssigned
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE shop_orders SET rider_delivered_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2`,
		orderID, tenantID,
	); err != nil {
		return nil, fmt.Errorf("mark delivered: %w", err)
	}
	return s.loadOrderDeliveryState(ctx, tenantID, orderID)
}

// MarkUrgent sets or clears the order's urgent flag. Setting urgent=true
// snapshots the tenant's current urgent_delivery_fee into urgent_fee the
// first time (if it isn't already set). Setting urgent=false deliberately
// leaves urgent_fee untouched: it's a historical record of what was charged
// if the order was ever marked urgent, consistent with how this codebase
// snapshots prices elsewhere (e.g. order line prices, rider_fee).
func (s *RiderService) MarkUrgent(ctx context.Context, tenantID, orderID uuid.UUID, urgent bool) (*shop.ShopOrder, error) {
	o, err := s.loadOrderDeliveryState(ctx, tenantID, orderID)
	if err != nil {
		return nil, err
	}

	urgentFee := o.UrgentFee
	if urgent && urgentFee == nil {
		fee, err := s.urgentDeliveryFee(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		urgentFee = &fee
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE shop_orders SET is_urgent = $1, urgent_fee = $2, updated_at = NOW()
		WHERE id = $3 AND tenant_id = $4`,
		urgent, urgentFee, orderID, tenantID,
	); err != nil {
		return nil, fmt.Errorf("mark urgent: %w", err)
	}
	return s.loadOrderDeliveryState(ctx, tenantID, orderID)
}

// MarkRiderPaid stamps rider_paid_at. Requires a rider already assigned.
func (s *RiderService) MarkRiderPaid(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	o, err := s.loadOrderDeliveryState(ctx, tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.RiderID == nil {
		return nil, shop.ErrNoRiderAssigned
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE shop_orders SET rider_paid_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2`,
		orderID, tenantID,
	); err != nil {
		return nil, fmt.Errorf("mark rider paid: %w", err)
	}
	return s.loadOrderDeliveryState(ctx, tenantID, orderID)
}

// GetRiderBySupabaseUID resolves the rider record owned by the authenticated
// user (matched via tenant_users.supabase_uid). Used by HTTP handlers to let
// a rider act on their own deliveries without exposing their riders.id.
func (s *RiderService) GetRiderBySupabaseUID(ctx context.Context, tenantID uuid.UUID, supabaseUID string) (*rider.Rider, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT r.id, r.tenant_id, r.user_id, r.name, COALESCE(r.phone, ''), r.rate_per_delivery,
		       r.status, r.max_concurrent_deliveries, r.last_assigned_at, r.active, r.created_at, r.updated_at
		FROM riders r
		JOIN tenant_users tu ON tu.id = r.user_id
		WHERE r.tenant_id = $1 AND tu.supabase_uid = $2`,
		tenantID, supabaseUID,
	)
	r, err := scanRider(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rider.ErrRiderNotFound
		}
		return nil, fmt.Errorf("get rider by supabase uid: %w", err)
	}
	return r, nil
}

// GetOrderDeliveryState exposes the delivery-relevant subset of a shop order
// (status, rider assignment, urgent flag) for callers that need to inspect
// it without mutating anything, e.g. an authorization check.
func (s *RiderService) GetOrderDeliveryState(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	return s.loadOrderDeliveryState(ctx, tenantID, orderID)
}

// ListMyDeliveries returns the delivery-relevant subset of active (not yet
// delivered) orders assigned to the given rider, urgent first then
// oldest-assigned first.
func (s *RiderService) ListMyDeliveries(ctx context.Context, tenantID, riderID uuid.UUID) ([]shop.ShopOrder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, status,
		       customer_name, COALESCE(customer_phone, ''),
		       shipping_address, COALESCE(shipping_city, ''),
		       delivery_lat, delivery_lng, total,
		       rider_id, rider_fee, rider_assigned_at, rider_delivered_at, rider_paid_at,
		       is_urgent, urgent_fee
		FROM shop_orders
		WHERE tenant_id = $1 AND rider_id = $2 AND rider_delivered_at IS NULL
		ORDER BY is_urgent DESC, rider_assigned_at ASC`,
		tenantID, riderID,
	)
	if err != nil {
		return nil, fmt.Errorf("list my deliveries: %w", err)
	}
	defer rows.Close()

	orders, err := scanMyDeliveryRows(rows)
	if err != nil {
		return nil, err
	}
	return orders, nil
}

// DeliveryHistory is the response for a rider's completed-deliveries history
// screen: the completed orders plus this-week and all-time payout figures.
type DeliveryHistory struct {
	Deliveries         []shop.ShopOrder `json:"deliveries"`
	EarningsThisWeek   float64          `json:"earnings_this_week"`
	DeliveriesThisWeek int              `json:"deliveries_this_week"`
	PendingPayout      float64          `json:"pending_payout"`
}

// GetDeliveryHistory returns the rider's delivered orders plus this-week
// earnings/count and all-time unpaid (pending) payout.
func (s *RiderService) GetDeliveryHistory(ctx context.Context, tenantID, riderID uuid.UUID) (*DeliveryHistory, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, status,
		       customer_name, COALESCE(customer_phone, ''),
		       shipping_address, COALESCE(shipping_city, ''),
		       delivery_lat, delivery_lng, total,
		       rider_id, rider_fee, rider_assigned_at, rider_delivered_at, rider_paid_at,
		       is_urgent, urgent_fee
		FROM shop_orders
		WHERE tenant_id = $1 AND rider_id = $2 AND rider_delivered_at IS NOT NULL
		ORDER BY rider_delivered_at DESC`,
		tenantID, riderID,
	)
	if err != nil {
		return nil, fmt.Errorf("list delivery history: %w", err)
	}
	defer rows.Close()

	deliveries, err := scanMyDeliveryRows(rows)
	if err != nil {
		return nil, err
	}

	var h DeliveryHistory
	h.Deliveries = deliveries
	if err := s.pool.QueryRow(ctx, `
		WITH week_start AS (
		  SELECT DATE_TRUNC('week', NOW())::TIMESTAMPTZ AS ts
		)
		SELECT
		  COALESCE(SUM(rider_fee) FILTER (WHERE rider_delivered_at >= (SELECT ts FROM week_start)), 0)::float8,
		  COUNT(*) FILTER (WHERE rider_delivered_at >= (SELECT ts FROM week_start))::int,
		  COALESCE(SUM(rider_fee) FILTER (WHERE rider_paid_at IS NULL), 0)::float8
		FROM shop_orders
		WHERE tenant_id = $1 AND rider_id = $2 AND rider_delivered_at IS NOT NULL`,
		tenantID, riderID,
	).Scan(&h.EarningsThisWeek, &h.DeliveriesThisWeek, &h.PendingPayout); err != nil {
		return nil, fmt.Errorf("delivery history aggregates: %w", err)
	}
	return &h, nil
}

// RouteStop is one stop on a rider's multi-stop active-delivery route.
type RouteStop struct {
	OrderID  uuid.UUID `json:"order_id"`
	Customer string    `json:"customer_name"`
	Address  string    `json:"address"`
	RiderFee *float64  `json:"rider_fee,omitempty"`
	IsUrgent bool      `json:"is_urgent"`
	Lat      float64   `json:"lat"`
	Lng      float64   `json:"lng"`
}

// MyRoute is the response for the rider's route-map screen. When the rider
// has no active deliveries with a pin, Stops is empty and Route is nil —
// callers must treat that as a valid, non-error result.
type MyRoute struct {
	Stops []RouteStop    `json:"stops"`
	Route *routing.Route `json:"route,omitempty"`
}

// GetMyRoute builds a single multi-stop driving route covering ALL of this
// rider's current active deliveries that have a pin, starting from the
// rider's current position (riderLat/riderLng, a one-shot snapshot taken by
// the client). Deliveries without delivery_lat/delivery_lng — possible for
// orders assigned before this geolocation feature existed — are excluded
// here rather than crashing the route call.
//
// Stop order matches ListMyDeliveries (is_urgent DESC, rider_assigned_at
// ASC) — there is no route optimization/TSP in this version; that is a
// known, acceptable simplification for now.
func (s *RiderService) GetMyRoute(ctx context.Context, tenantID, riderID uuid.UUID, riderLat, riderLng float64) (*MyRoute, error) {
	orders, err := s.ListMyDeliveries(ctx, tenantID, riderID)
	if err != nil {
		return nil, err
	}

	stops := make([]RouteStop, 0, len(orders))
	waypoints := make([]routing.Waypoint, 0, len(orders)+1)
	waypoints = append(waypoints, routing.Waypoint{Lat: riderLat, Lng: riderLng})
	for _, o := range orders {
		if o.DeliveryLat == nil || o.DeliveryLng == nil {
			continue
		}
		stops = append(stops, RouteStop{
			OrderID:  o.ID,
			Customer: o.CustomerName,
			Address:  o.ShippingAddress,
			RiderFee: o.RiderFee,
			IsUrgent: o.IsUrgent,
			Lat:      *o.DeliveryLat,
			Lng:      *o.DeliveryLng,
		})
		waypoints = append(waypoints, routing.Waypoint{Lat: *o.DeliveryLat, Lng: *o.DeliveryLng})
	}

	result := &MyRoute{Stops: stops}
	if len(stops) == 0 {
		// Nothing mappable — a valid empty result, not an error. Calling ORS
		// with fewer than 2 waypoints would itself error.
		return result, nil
	}

	if s.routeProvider == nil {
		return nil, fmt.Errorf("%w: no routing provider configured", ErrRouteUnavailable)
	}

	route, err := s.routeProvider.GetRoute(ctx, waypoints)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRouteUnavailable, err)
	}
	result.Route = route
	return result, nil
}

// scanMyDeliveryRows scans the shared rider-delivery column set used by both
// ListMyDeliveries and GetDeliveryHistory.
func scanMyDeliveryRows(rows pgx.Rows) ([]shop.ShopOrder, error) {
	var orders []shop.ShopOrder
	for rows.Next() {
		var o shop.ShopOrder
		if err := rows.Scan(
			&o.ID, &o.TenantID, &o.Status,
			&o.CustomerName, &o.CustomerPhone,
			&o.ShippingAddress, &o.ShippingCity,
			&o.DeliveryLat, &o.DeliveryLng, &o.Total,
			&o.RiderID, &o.RiderFee, &o.RiderAssignedAt, &o.RiderDeliveredAt, &o.RiderPaidAt,
			&o.IsUrgent, &o.UrgentFee,
		); err != nil {
			return nil, fmt.Errorf("scan delivery row: %w", err)
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("delivery rows: %w", err)
	}
	if orders == nil {
		orders = []shop.ShopOrder{}
	}
	return orders, nil
}

// UpdateMyStatus narrowly updates only the status column on the rider's own
// row. Unlike UpdateRider (full-replace, owner/employee-only), this is the
// rider-self-service operation and never touches rate/capacity/active.
func (s *RiderService) UpdateMyStatus(ctx context.Context, tenantID, riderID uuid.UUID, status rider.Status) (*rider.Rider, error) {
	if status != rider.StatusAvailable && status != rider.StatusUnavailable {
		return nil, rider.ErrInvalidStatus
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE riders
		SET status = $3, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, user_id, name, COALESCE(phone, ''), rate_per_delivery,
		          status, max_concurrent_deliveries, last_assigned_at, active, created_at, updated_at`,
		riderID, tenantID, status,
	)
	r, err := scanRider(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rider.ErrRiderNotFound
		}
		return nil, fmt.Errorf("update my status: %w", err)
	}
	return r, nil
}

// ─── Private helpers ──────────────────────────────────────────────────────────

func (s *RiderService) deliveryEnabled(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var enabled bool
	if err := s.pool.QueryRow(ctx,
		`SELECT delivery_enabled FROM tenants WHERE id = $1`, tenantID,
	).Scan(&enabled); err != nil {
		return false, fmt.Errorf("check delivery_enabled: %w", err)
	}
	return enabled, nil
}

func (s *RiderService) urgentDeliveryFee(ctx context.Context, tenantID uuid.UUID) (float64, error) {
	var fee float64
	if err := s.pool.QueryRow(ctx,
		`SELECT urgent_delivery_fee FROM tenants WHERE id = $1`, tenantID,
	).Scan(&fee); err != nil {
		return 0, fmt.Errorf("get urgent_delivery_fee: %w", err)
	}
	return fee, nil
}

func (s *RiderService) loadRider(ctx context.Context, tenantID, riderID uuid.UUID) (*rider.Rider, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, name, COALESCE(phone, ''), rate_per_delivery,
		       status, max_concurrent_deliveries, last_assigned_at, active, created_at, updated_at
		FROM riders
		WHERE id = $1 AND tenant_id = $2`,
		riderID, tenantID,
	)
	r, err := scanRider(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rider.ErrRiderNotFound
		}
		return nil, fmt.Errorf("load rider: %w", err)
	}
	return r, nil
}

// loadOrderDeliveryState loads the delivery-relevant subset of a shop order:
// identity, status, and the 7 delivery columns. Lines are intentionally
// omitted — delivery operations don't need order line detail.
func (s *RiderService) loadOrderDeliveryState(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	var o shop.ShopOrder
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, status,
		       rider_id, rider_fee, rider_assigned_at, rider_delivered_at, rider_paid_at,
		       is_urgent, urgent_fee
		FROM shop_orders
		WHERE id = $1 AND tenant_id = $2`,
		orderID, tenantID,
	).Scan(
		&o.ID, &o.TenantID, &o.Status,
		&o.RiderID, &o.RiderFee, &o.RiderAssignedAt, &o.RiderDeliveredAt, &o.RiderPaidAt,
		&o.IsUrgent, &o.UrgentFee,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shop.ErrShopOrderNotFound
		}
		return nil, fmt.Errorf("load order delivery state: %w", err)
	}
	return &o, nil
}

type riderScanner interface {
	Scan(dest ...any) error
}

func scanRider(row riderScanner) (*rider.Rider, error) {
	var r rider.Rider
	if err := row.Scan(
		&r.ID, &r.TenantID, &r.UserID, &r.Name, &r.Phone, &r.RatePerDelivery,
		&r.Status, &r.MaxConcurrentDeliveries, &r.LastAssignedAt, &r.Active, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &r, nil
}

func scanRiderWithActiveCount(row riderScanner) (*rider.Rider, int, error) {
	var r rider.Rider
	var activeCount int
	if err := row.Scan(
		&r.ID, &r.TenantID, &r.UserID, &r.Name, &r.Phone, &r.RatePerDelivery,
		&r.Status, &r.MaxConcurrentDeliveries, &r.LastAssignedAt, &r.Active, &r.CreatedAt, &r.UpdatedAt,
		&activeCount,
	); err != nil {
		return nil, 0, err
	}
	return &r, activeCount, nil
}
