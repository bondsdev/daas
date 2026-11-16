package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/domain/rider"
	"github.com/danzt/daas/api/internal/domain/shop"
)

// RiderService manages delivery riders (motorizados) and the delivery
// lifecycle (assignment, delivered, urgent fee, rider payout) on shop orders.
type RiderService struct {
	pool *pgxpool.Pool
}

// NewRiderService creates a RiderService backed by the given pool.
func NewRiderService(pool *pgxpool.Pool) *RiderService {
	return &RiderService{pool: pool}
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

// ListMyDeliveries returns the delivery-relevant subset of orders assigned to
// the given rider, urgent first then oldest-assigned first.
func (s *RiderService) ListMyDeliveries(ctx context.Context, tenantID, riderID uuid.UUID) ([]shop.ShopOrder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, status,
		       rider_id, rider_fee, rider_assigned_at, rider_delivered_at, rider_paid_at,
		       is_urgent, urgent_fee
		FROM shop_orders
		WHERE tenant_id = $1 AND rider_id = $2
		ORDER BY is_urgent DESC, rider_assigned_at ASC`,
		tenantID, riderID,
	)
	if err != nil {
		return nil, fmt.Errorf("list my deliveries: %w", err)
	}
	defer rows.Close()

	var orders []shop.ShopOrder
	for rows.Next() {
		var o shop.ShopOrder
		if err := rows.Scan(
			&o.ID, &o.TenantID, &o.Status,
			&o.RiderID, &o.RiderFee, &o.RiderAssignedAt, &o.RiderDeliveredAt, &o.RiderPaidAt,
			&o.IsUrgent, &o.UrgentFee,
		); err != nil {
			return nil, fmt.Errorf("scan my delivery: %w", err)
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("my deliveries rows: %w", err)
	}
	if orders == nil {
		orders = []shop.ShopOrder{}
	}
	return orders, nil
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
