package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/domain/rider"
	"github.com/danzt/daas/api/internal/domain/shop"
)

// setupRiderTest connects to TEST_DATABASE_URL and seeds a tenant (with
// delivery enabled) + a tenant_user to own riders. Mirrors the convention in
// cash_session_service_test.go — real DB integration tests, no mocking.
func setupRiderTest(t *testing.T) (*pgxpool.Pool, uuid.UUID) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, country_code, slug, delivery_enabled, urgent_delivery_fee)
		 VALUES ($1, 'VE', $2, TRUE, 2.50) RETURNING id`,
		"rider-test-"+uuid.NewString(), uuid.NewString(),
	).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	return pool, tenantID
}

// seedRiderUser creates a tenant_user with the 'rider' role and returns its ID,
// suitable as CreateRiderRequest.UserID.
func seedRiderUser(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	supabaseUID := uuid.NewString()
	var userID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenant_users (tenant_id, supabase_uid, email, role)
		 VALUES ($1, $2, $3, 'rider') RETURNING id`,
		tenantID, supabaseUID, "rider-"+supabaseUID+"@test.local",
	).Scan(&userID); err != nil {
		t.Fatalf("create tenant user: %v", err)
	}
	return userID
}

// seedShopOrder creates a minimal shop order for delivery tests.
func seedShopOrder(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var orderID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO shop_orders
		    (tenant_id, customer_name, customer_email, shipping_address, subtotal, shipping_cost, total, status, access_token)
		 VALUES ($1, 'Test Customer', 'customer@test.local', 'Test Address', 10, 0, 10, 'pending', $2)
		 RETURNING id`,
		tenantID, uuid.NewString(),
	).Scan(&orderID); err != nil {
		t.Fatalf("create shop order: %v", err)
	}
	return orderID
}

func TestRiderService_CreateRiderRequiresDeliveryEnabled(t *testing.T) {
	pool, tenantID := setupRiderTest(t)
	svc := NewRiderService(pool)

	if _, err := pool.Exec(context.Background(),
		`UPDATE tenants SET delivery_enabled = FALSE WHERE id = $1`, tenantID,
	); err != nil {
		t.Fatalf("disable delivery: %v", err)
	}

	userID := seedRiderUser(t, pool, tenantID)
	req := rider.CreateRiderRequest{UserID: userID, Name: "Rider A", MaxConcurrentDeliveries: 2}
	if _, err := svc.CreateRider(context.Background(), tenantID, req); !errors.Is(err, rider.ErrDeliveryNotEnabled) {
		t.Errorf("expected ErrDeliveryNotEnabled, got %v", err)
	}
}

func TestRiderService_AssignRiderManualRejectsDuplicate(t *testing.T) {
	pool, tenantID := setupRiderTest(t)
	svc := NewRiderService(pool)

	userID := seedRiderUser(t, pool, tenantID)
	r, err := svc.CreateRider(context.Background(), tenantID, rider.CreateRiderRequest{
		UserID: userID, Name: "Rider A", MaxConcurrentDeliveries: 2,
	})
	if err != nil {
		t.Fatalf("CreateRider: %v", err)
	}
	orderID := seedShopOrder(t, pool, tenantID)

	if _, err := svc.AssignRiderManual(context.Background(), tenantID, orderID, r.ID); err != nil {
		t.Fatalf("first AssignRiderManual: %v", err)
	}
	if _, err := svc.AssignRiderManual(context.Background(), tenantID, orderID, r.ID); !errors.Is(err, shop.ErrRiderAlreadyAssigned) {
		t.Errorf("expected ErrRiderAlreadyAssigned, got %v", err)
	}
}

func TestRiderService_AutoAssignSkipsRiderAtCapacity(t *testing.T) {
	pool, tenantID := setupRiderTest(t)
	svc := NewRiderService(pool)

	fullUser := seedRiderUser(t, pool, tenantID)
	full, err := svc.CreateRider(context.Background(), tenantID, rider.CreateRiderRequest{
		UserID: fullUser, Name: "Full Rider", MaxConcurrentDeliveries: 1,
	})
	if err != nil {
		t.Fatalf("CreateRider full: %v", err)
	}
	freeUser := seedRiderUser(t, pool, tenantID)
	free, err := svc.CreateRider(context.Background(), tenantID, rider.CreateRiderRequest{
		UserID: freeUser, Name: "Free Rider", MaxConcurrentDeliveries: 1,
	})
	if err != nil {
		t.Fatalf("CreateRider free: %v", err)
	}

	// Fill the first rider to capacity with an undelivered order.
	fillOrder := seedShopOrder(t, pool, tenantID)
	if _, err := svc.AssignRiderManual(context.Background(), tenantID, fillOrder, full.ID); err != nil {
		t.Fatalf("fill rider capacity: %v", err)
	}

	orderID := seedShopOrder(t, pool, tenantID)
	assigned, err := svc.AutoAssignRider(context.Background(), tenantID, orderID)
	if err != nil {
		t.Fatalf("AutoAssignRider: %v", err)
	}
	if assigned.RiderID == nil || *assigned.RiderID != free.ID {
		t.Errorf("expected free rider %s assigned, got %+v", free.ID, assigned.RiderID)
	}
}

func TestRiderService_AutoAssignRoundRobinsOldestLastAssigned(t *testing.T) {
	pool, tenantID := setupRiderTest(t)
	svc := NewRiderService(pool)

	user1 := seedRiderUser(t, pool, tenantID)
	r1, err := svc.CreateRider(context.Background(), tenantID, rider.CreateRiderRequest{
		UserID: user1, Name: "Rider 1", MaxConcurrentDeliveries: 5,
	})
	if err != nil {
		t.Fatalf("CreateRider r1: %v", err)
	}
	user2 := seedRiderUser(t, pool, tenantID)
	r2, err := svc.CreateRider(context.Background(), tenantID, rider.CreateRiderRequest{
		UserID: user2, Name: "Rider 2", MaxConcurrentDeliveries: 5,
	})
	if err != nil {
		t.Fatalf("CreateRider r2: %v", err)
	}

	// Both riders start with last_assigned_at = NULL. Assign r1 an order first,
	// bumping its last_assigned_at — r2 (never assigned) must then be picked next.
	firstOrder := seedShopOrder(t, pool, tenantID)
	first, err := svc.AutoAssignRider(context.Background(), tenantID, firstOrder)
	if err != nil {
		t.Fatalf("first AutoAssignRider: %v", err)
	}
	if first.RiderID == nil {
		t.Fatalf("expected a rider assigned")
	}

	secondOrder := seedShopOrder(t, pool, tenantID)
	second, err := svc.AutoAssignRider(context.Background(), tenantID, secondOrder)
	if err != nil {
		t.Fatalf("second AutoAssignRider: %v", err)
	}
	if second.RiderID == nil || *second.RiderID == *first.RiderID {
		t.Errorf("expected the other rider assigned second, got first=%v second=%v", *first.RiderID, second.RiderID)
	}
	if *first.RiderID != r1.ID && *first.RiderID != r2.ID {
		t.Errorf("unexpected rider assigned: %v", *first.RiderID)
	}
}

func TestRiderService_MarkDeliveredWithoutRiderFails(t *testing.T) {
	pool, tenantID := setupRiderTest(t)
	svc := NewRiderService(pool)

	orderID := seedShopOrder(t, pool, tenantID)
	if _, err := svc.MarkDelivered(context.Background(), tenantID, orderID); !errors.Is(err, shop.ErrNoRiderAssigned) {
		t.Errorf("expected ErrNoRiderAssigned, got %v", err)
	}
}

func TestRiderService_MarkUrgentSnapshotsTenantFee(t *testing.T) {
	pool, tenantID := setupRiderTest(t)
	svc := NewRiderService(pool)

	orderID := seedShopOrder(t, pool, tenantID)
	o, err := svc.MarkUrgent(context.Background(), tenantID, orderID, true)
	if err != nil {
		t.Fatalf("MarkUrgent: %v", err)
	}
	if !o.IsUrgent || o.UrgentFee == nil || *o.UrgentFee != 2.50 {
		t.Errorf("expected urgent fee snapshot 2.50, got %+v", o.UrgentFee)
	}

	// Unmarking urgent must preserve the historical fee, not null it.
	o, err = svc.MarkUrgent(context.Background(), tenantID, orderID, false)
	if err != nil {
		t.Fatalf("MarkUrgent false: %v", err)
	}
	if o.IsUrgent {
		t.Errorf("expected is_urgent = false")
	}
	if o.UrgentFee == nil || *o.UrgentFee != 2.50 {
		t.Errorf("expected urgent_fee preserved as 2.50, got %+v", o.UrgentFee)
	}
}

func TestRiderService_MarkRiderPaidRequiresAssignedRider(t *testing.T) {
	pool, tenantID := setupRiderTest(t)
	svc := NewRiderService(pool)

	userID := seedRiderUser(t, pool, tenantID)
	r, err := svc.CreateRider(context.Background(), tenantID, rider.CreateRiderRequest{
		UserID: userID, Name: "Rider A", MaxConcurrentDeliveries: 2,
	})
	if err != nil {
		t.Fatalf("CreateRider: %v", err)
	}
	orderID := seedShopOrder(t, pool, tenantID)

	if _, err := svc.AssignRiderManual(context.Background(), tenantID, orderID, r.ID); err != nil {
		t.Fatalf("AssignRiderManual: %v", err)
	}
	o, err := svc.MarkRiderPaid(context.Background(), tenantID, orderID)
	if err != nil {
		t.Fatalf("MarkRiderPaid: %v", err)
	}
	if o.RiderPaidAt == nil {
		t.Errorf("expected rider_paid_at set")
	}
	if time.Since(*o.RiderPaidAt) > time.Minute {
		t.Errorf("rider_paid_at not recent: %v", o.RiderPaidAt)
	}
}
