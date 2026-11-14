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
	"github.com/danzt/daas/api/internal/domain/invoice"
)

// setupPOSSaleTest connects to TEST_DATABASE_URL and seeds a tenant, user and
// one non-fiscal product with stock. Follows the same convention as
// setupCashSessionTest (no mocking library for pgxpool.Pool in this repo).
func setupPOSSaleTest(t *testing.T, stock float64) (*InternalInvoiceService, *CashSessionService, uuid.UUID, string, uuid.UUID) {
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
		`INSERT INTO tenants (name, country_code, slug) VALUES ($1, 'VE', $2) RETURNING id`,
		"pos-sale-test-"+uuid.NewString(), uuid.NewString(),
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

	var productID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO products (tenant_id, name, is_fiscal, internal_price, active)
		 VALUES ($1, 'Pan', false, 2.5, true) RETURNING id`,
		tenantID,
	).Scan(&productID); err != nil {
		t.Fatalf("create product: %v", err)
	}
	// products has an initialize_product_stock trigger that already inserts a
	// zeroed product_stock row, so the seed updates it instead of inserting.
	if _, err := pool.Exec(ctx,
		`UPDATE product_stock SET quantity_on_hand = $1, last_updated_at = NOW() WHERE product_id = $2`,
		stock, productID,
	); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	cashSvc := NewCashSessionService(pool, &exchangerate.MockFetcher{Rate: 40.5})
	invoiceSvc := NewInternalInvoiceService(pool)
	invoiceSvc.SetCashSessionService(cashSvc)

	return invoiceSvc, cashSvc, tenantID, supabaseUID, productID
}

func TestInternalInvoiceService_CreatePOSSale_NoOpenSession(t *testing.T) {
	invoiceSvc, _, tenantID, supabaseUID, productID := setupPOSSaleTest(t, 10)

	_, err := invoiceSvc.CreatePOSSale(context.Background(), tenantID, supabaseUID, invoice.PosSaleRequest{
		Lines:         []invoice.PosSaleLine{{ProductID: productID, Quantity: 1}},
		PaymentMethod: invoice.PaymentMethodCash,
	})
	if !errors.Is(err, cashsession.ErrNoOpenSession) {
		t.Fatalf("expected ErrNoOpenSession, got %v", err)
	}
}

func TestInternalInvoiceService_CreatePOSSale_InsufficientStock(t *testing.T) {
	invoiceSvc, cashSvc, tenantID, supabaseUID, productID := setupPOSSaleTest(t, 1)

	if _, err := cashSvc.Open(context.Background(), tenantID, supabaseUID, nil, "opening"); err != nil {
		t.Fatalf("open cash session: %v", err)
	}

	_, err := invoiceSvc.CreatePOSSale(context.Background(), tenantID, supabaseUID, invoice.PosSaleRequest{
		Lines:         []invoice.PosSaleLine{{ProductID: productID, Quantity: 5}},
		PaymentMethod: invoice.PaymentMethodCash,
	})
	if !errors.Is(err, invoice.ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock, got %v", err)
	}
}

func TestInternalInvoiceService_CreatePOSSale_Success(t *testing.T) {
	invoiceSvc, cashSvc, tenantID, supabaseUID, productID := setupPOSSaleTest(t, 10)

	sess, err := cashSvc.Open(context.Background(), tenantID, supabaseUID, nil, "opening")
	if err != nil {
		t.Fatalf("open cash session: %v", err)
	}

	inv, err := invoiceSvc.CreatePOSSale(context.Background(), tenantID, supabaseUID, invoice.PosSaleRequest{
		Lines:         []invoice.PosSaleLine{{ProductID: productID, Quantity: 3}},
		PaymentMethod: invoice.PaymentMethodCard,
		Currency:      "USD",
	})
	if err != nil {
		t.Fatalf("CreatePOSSale: %v", err)
	}
	if inv.Status != invoice.StatusIssued {
		t.Errorf("expected status issued, got %s", inv.Status)
	}
	if inv.CashSessionID == nil || *inv.CashSessionID != sess.ID {
		t.Errorf("expected cash_session_id %s, got %v", sess.ID, inv.CashSessionID)
	}
	if inv.PaymentMethod != invoice.PaymentMethodCard {
		t.Errorf("expected payment_method card, got %s", inv.PaymentMethod)
	}
	if inv.Currency != "USD" {
		t.Errorf("expected currency USD, got %s", inv.Currency)
	}
	if inv.Total != 7.5 {
		t.Errorf("expected total 7.5 (3 x 2.5), got %.2f", inv.Total)
	}

	// Stock must have been deducted: 10 - 3 = 7.
	pool := invoiceSvc.pool
	var onHand float64
	if err := pool.QueryRow(context.Background(),
		`SELECT quantity_on_hand FROM product_stock WHERE tenant_id=$1 AND product_id=$2`,
		tenantID, productID,
	).Scan(&onHand); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if onHand != 7 {
		t.Errorf("expected remaining stock 7, got %.2f", onHand)
	}
}
