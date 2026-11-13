package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/adapter/exchangerate"
	"github.com/danzt/daas/api/internal/domain/cashsession"
)

// setupCashSessionTest connects to TEST_DATABASE_URL and seeds a tenant + user.
// Skips the test when the env var is unset, following the existing convention
// in scan_integrity_test.go (no mocking library for pgxpool.Pool in this repo).
func setupCashSessionTest(t *testing.T) (*pgxpool.Pool, uuid.UUID, string) {
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
		`INSERT INTO tenants (name, country_code) VALUES ($1, 'VE') RETURNING id`,
		"cash-session-test-"+uuid.NewString(),
	).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	supabaseUID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenant_users (tenant_id, supabase_uid, email, role)
		 VALUES ($1, $2, $3, 'owner')`,
		tenantID, supabaseUID, "owner-"+supabaseUID+"@test.local",
	); err != nil {
		t.Fatalf("create tenant user: %v", err)
	}

	return pool, tenantID, supabaseUID
}

func TestCashSessionService_OpenWithAutoRate(t *testing.T) {
	pool, tenantID, supabaseUID := setupCashSessionTest(t)
	svc := NewCashSessionService(pool, &exchangerate.MockFetcher{Rate: 40.5})

	sess, err := svc.Open(context.Background(), tenantID, supabaseUID, nil, "opening")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if sess.ExchangeRate != 40.5 || sess.ExchangeRateSource != cashsession.RateSourceAuto {
		t.Errorf("unexpected rate/source: %v %v", sess.ExchangeRate, sess.ExchangeRateSource)
	}
}

func TestCashSessionService_OpenWithManualRate(t *testing.T) {
	pool, tenantID, supabaseUID := setupCashSessionTest(t)
	svc := NewCashSessionService(pool, &exchangerate.MockFetcher{Err: errors.New("fetch failed")})

	manual := 42.0
	sess, err := svc.Open(context.Background(), tenantID, supabaseUID, &manual, "manual rate")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if sess.ExchangeRate != manual || sess.ExchangeRateSource != cashsession.RateSourceManual {
		t.Errorf("unexpected rate/source: %v %v", sess.ExchangeRate, sess.ExchangeRateSource)
	}
}

func TestCashSessionService_OpenWithoutFetcherOrManualRateFails(t *testing.T) {
	pool, tenantID, supabaseUID := setupCashSessionTest(t)
	svc := NewCashSessionService(pool, &exchangerate.MockFetcher{Err: errors.New("fetch failed")})

	if _, err := svc.Open(context.Background(), tenantID, supabaseUID, nil, ""); !errors.Is(err, cashsession.ErrRateRequired) {
		t.Errorf("expected ErrRateRequired, got %v", err)
	}
}

func TestCashSessionService_OpenWhenAlreadyOpenFails(t *testing.T) {
	pool, tenantID, supabaseUID := setupCashSessionTest(t)
	svc := NewCashSessionService(pool, &exchangerate.MockFetcher{Rate: 40.5})

	if _, err := svc.Open(context.Background(), tenantID, supabaseUID, nil, ""); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := svc.Open(context.Background(), tenantID, supabaseUID, nil, ""); !errors.Is(err, cashsession.ErrSessionAlreadyOpen) {
		t.Errorf("expected ErrSessionAlreadyOpen, got %v", err)
	}
}

func TestCashSessionService_Close(t *testing.T) {
	pool, tenantID, supabaseUID := setupCashSessionTest(t)
	svc := NewCashSessionService(pool, &exchangerate.MockFetcher{Rate: 40.5})

	if _, err := svc.Open(context.Background(), tenantID, supabaseUID, nil, ""); err != nil {
		t.Fatalf("Open: %v", err)
	}
	sess, err := svc.Close(context.Background(), tenantID, supabaseUID, "closing")
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if sess.Status != cashsession.StatusClosed || sess.ClosedAt == nil {
		t.Errorf("session not properly closed: %+v", sess)
	}
}

func TestCashSessionService_CloseWithNoOpenSessionFails(t *testing.T) {
	pool, tenantID, supabaseUID := setupCashSessionTest(t)
	svc := NewCashSessionService(pool, &exchangerate.MockFetcher{Rate: 40.5})

	if _, err := svc.Close(context.Background(), tenantID, supabaseUID, ""); !errors.Is(err, cashsession.ErrNoOpenSession) {
		t.Errorf("expected ErrNoOpenSession, got %v", err)
	}
}
