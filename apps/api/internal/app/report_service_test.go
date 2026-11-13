package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/domain/cashsession"
	"github.com/danzt/daas/api/internal/domain/report"
)

// setupReportTest connects to TEST_DATABASE_URL and seeds a tenant, user, and
// an open cash session with the given exchange rate, following the same
// skip-when-unset convention as cash_session_service_test.go.
func setupReportTest(t *testing.T, rate float64) (*pgxpool.Pool, uuid.UUID, uuid.UUID) {
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
		"report-test-"+uuid.NewString(),
	).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	supabaseUID := uuid.NewString()
	var userID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenant_users (tenant_id, supabase_uid, email, role)
		 VALUES ($1, $2, $3, 'owner') RETURNING id`,
		tenantID, supabaseUID, "owner-"+supabaseUID+"@test.local",
	).Scan(&userID); err != nil {
		t.Fatalf("create tenant user: %v", err)
	}

	var sessionID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO cash_sessions (tenant_id, exchange_rate, opened_by) VALUES ($1,$2,$3) RETURNING id`,
		tenantID, rate, userID,
	).Scan(&sessionID); err != nil {
		t.Fatalf("create cash session: %v", err)
	}

	return pool, tenantID, sessionID
}

func TestReportService_CashSessionSummary(t *testing.T) {
	pool, tenantID, sessionID := setupReportTest(t, 40.0)
	ctx := context.Background()

	// One VES sale and one USD sale on the same session.
	if _, err := pool.Exec(ctx,
		`INSERT INTO internal_invoices (tenant_id, status, subtotal, total, cash_session_id, payment_method, currency)
		 VALUES ($1,'issued',400,400,$2,'cash','VES')`,
		tenantID, sessionID,
	); err != nil {
		t.Fatalf("insert VES invoice: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO internal_invoices (tenant_id, status, subtotal, total, cash_session_id, payment_method, currency)
		 VALUES ($1,'issued',10,10,$2,'card','USD')`,
		tenantID, sessionID,
	); err != nil {
		t.Fatalf("insert USD invoice: %v", err)
	}

	svc := NewReportService(pool)
	sum, err := svc.CashSessionSummary(ctx, tenantID, sessionID)
	if err != nil {
		t.Fatalf("CashSessionSummary: %v", err)
	}
	if sum.SaleCount != 2 || sum.CustomerCount != 2 {
		t.Errorf("unexpected counts: %+v", sum)
	}
	wantBS := 400.0 + 10.0*40.0
	wantUSD := 400.0/40.0 + 10.0
	if sum.TotalBS != wantBS || sum.TotalUSD != wantUSD {
		t.Errorf("unexpected totals: got BS=%v USD=%v, want BS=%v USD=%v", sum.TotalBS, sum.TotalUSD, wantBS, wantUSD)
	}
	if len(sum.ByPaymentMethod) != 2 {
		t.Errorf("expected 2 payment methods, got %+v", sum.ByPaymentMethod)
	}
	if sum.InventoryRemaining == nil {
		t.Error("expected InventoryRemaining to be populated")
	}
}

func TestReportService_SalesBook(t *testing.T) {
	pool, tenantID, _ := setupReportTest(t, 40.0)
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`INSERT INTO internal_invoices (tenant_id, status, subtotal, total, correlative, customer_name, customer_id_number, issued_at)
		 VALUES ($1,'issued',100,100,'INT-2026-00001','Cliente Mostrador','V-1', now())`,
		tenantID,
	); err != nil {
		t.Fatalf("insert internal invoice: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO fiscal_invoices (tenant_id, status, subtotal_base, tax_amount, total, fiscal_number, customer_name, customer_id_number, issued_at)
		 VALUES ($1,'issued',200,32,232,'FIS-00001','Cliente Fiscal','J-1', now())`,
		tenantID,
	); err != nil {
		t.Fatalf("insert fiscal invoice: %v", err)
	}

	dr := report.DateRange{From: time.Now().Add(-24 * time.Hour), To: time.Now().Add(24 * time.Hour)}
	svc := NewReportService(pool)
	sum, err := svc.SalesBook(ctx, tenantID, dr)
	if err != nil {
		t.Fatalf("SalesBook: %v", err)
	}

	if sum.DocumentCount != 2 {
		t.Fatalf("expected 2 documents, got %d: %+v", sum.DocumentCount, sum.Entries)
	}
	if sum.TotalRevenue != 332 {
		t.Errorf("expected total revenue 332, got %v", sum.TotalRevenue)
	}
	if sum.TotalTaxBase != 300 {
		t.Errorf("expected total tax base 300, got %v", sum.TotalTaxBase)
	}
	if sum.TotalTaxDebit != 32 {
		t.Errorf("expected total tax debit 32, got %v", sum.TotalTaxDebit)
	}

	var internalEntry, fiscalEntry *report.SalesBookEntry
	for i := range sum.Entries {
		switch sum.Entries[i].DocumentType {
		case "internal":
			internalEntry = &sum.Entries[i]
		case "fiscal":
			fiscalEntry = &sum.Entries[i]
		}
	}
	if internalEntry == nil || internalEntry.TaxBase != 100 || internalEntry.TaxAmount != 0 {
		t.Errorf("unexpected internal entry: %+v", internalEntry)
	}
	if fiscalEntry == nil || fiscalEntry.TaxBase != 200 || fiscalEntry.TaxAmount != 32 {
		t.Errorf("unexpected fiscal entry: %+v", fiscalEntry)
	}
}

func TestReportService_CashSessionSummaryNotFoundForOtherTenant(t *testing.T) {
	pool, _, sessionID := setupReportTest(t, 40.0)
	ctx := context.Background()

	var otherTenantID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, country_code) VALUES ($1, 'VE') RETURNING id`,
		"report-test-other-"+uuid.NewString(),
	).Scan(&otherTenantID); err != nil {
		t.Fatalf("create other tenant: %v", err)
	}

	svc := NewReportService(pool)
	if _, err := svc.CashSessionSummary(ctx, otherTenantID, sessionID); !errors.Is(err, cashsession.ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}
