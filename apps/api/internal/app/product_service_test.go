package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/adapter/exchangerate"
)

// errMockFetcherCalled is returned by a MockFetcher that a test expects to
// never be invoked — used to prove the manual (no price_usd) path never
// touches rate resolution at all.
var errMockFetcherCalled = errors.New("fetcher should not have been called")

// setupProductTest connects to TEST_DATABASE_URL and seeds a tenant.
// Skips the test when the env var is unset, following the same convention
// as setupCashSessionTest in cash_session_service_test.go.
func setupProductTest(t *testing.T) (*pgxpool.Pool, uuid.UUID) {
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
		"product-usd-test-"+uuid.NewString(), uuid.NewString(),
	).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	return pool, tenantID
}

// TestProductService_CreateWithPriceUSDComputesInternalPrice covers the
// acceptance criterion: price_usd=1 with rate 45 -> internal_price=45.
func TestProductService_CreateWithPriceUSDComputesInternalPrice(t *testing.T) {
	pool, tenantID := setupProductTest(t)
	svc := NewProductService(pool, &exchangerate.MockFetcher{Rate: 45})

	priceUSD := 1.0
	result, err := svc.Create(context.Background(), tenantID, CreateProductRequest{
		Name:     "Harina de maíz",
		IsFiscal: false,
		PriceUSD: &priceUSD,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if result.Product.PriceUSD == nil || *result.Product.PriceUSD != 1.0 {
		t.Fatalf("expected price_usd=1.0, got %v", result.Product.PriceUSD)
	}
	if result.Product.InternalPrice == nil || *result.Product.InternalPrice != 45.0 {
		t.Fatalf("expected internal_price=45.0, got %v", result.Product.InternalPrice)
	}
}

// TestProductService_CreateWithoutPriceUSDBehavesAsBefore covers the
// acceptance criterion: a product with no price_usd behaves exactly as
// before (manual internal_price, untouched by rate resolution).
func TestProductService_CreateWithoutPriceUSDBehavesAsBefore(t *testing.T) {
	pool, tenantID := setupProductTest(t)
	// Fetcher that would fail if ever called — proves the manual path never
	// touches rate resolution at all.
	svc := NewProductService(pool, &exchangerate.MockFetcher{Err: errMockFetcherCalled})

	internalPrice := 12.5
	result, err := svc.Create(context.Background(), tenantID, CreateProductRequest{
		Name:          "Jabón de tocador",
		IsFiscal:      false,
		InternalPrice: &internalPrice,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if result.Product.PriceUSD != nil {
		t.Fatalf("expected price_usd=nil, got %v", result.Product.PriceUSD)
	}
	if result.Product.InternalPrice == nil || *result.Product.InternalPrice != 12.5 {
		t.Fatalf("expected internal_price=12.5 unchanged, got %v", result.Product.InternalPrice)
	}
}

// TestProductService_UpdateWithPriceUSDRecomputesInternalPrice covers
// updating an existing product to carry a USD price.
func TestProductService_UpdateWithPriceUSDRecomputesInternalPrice(t *testing.T) {
	pool, tenantID := setupProductTest(t)
	svc := NewProductService(pool, &exchangerate.MockFetcher{Rate: 40})

	internalPrice := 10.0
	created, err := svc.Create(context.Background(), tenantID, CreateProductRequest{
		Name:          "Arroz",
		IsFiscal:      false,
		InternalPrice: &internalPrice,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	priceUSD := 2.0
	updated, err := svc.Update(context.Background(), tenantID, created.Product.ID, UpdateProductRequest{
		Name:     "Arroz",
		PriceUSD: &priceUSD,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.InternalPrice == nil || *updated.InternalPrice != 80.0 {
		t.Fatalf("expected internal_price=80.0 (2*40), got %v", updated.InternalPrice)
	}
}

// TestCashSessionService_OpenRecomputesUSDPricedProductsOnly covers the
// acceptance criterion: opening a new cash session with a new rate
// recomputes internal_price for all USD-priced products of that tenant and
// leaves non-USD products untouched.
func TestCashSessionService_OpenRecomputesUSDPricedProductsOnly(t *testing.T) {
	pool, tenantID, supabaseUID := setupCashSessionTest(t)
	productSvc := NewProductService(pool, &exchangerate.MockFetcher{Rate: 45})
	cashSvc := NewCashSessionService(pool, &exchangerate.MockFetcher{Rate: 45})

	// First rate recorded so the product's initial internal_price is 45.
	usdPrice := 1.0
	usdProduct, err := productSvc.Create(context.Background(), tenantID, CreateProductRequest{
		Name:     "Harina",
		IsFiscal: false,
		PriceUSD: &usdPrice,
	})
	if err != nil {
		t.Fatalf("create usd product: %v", err)
	}

	manualPrice := 99.0
	manualProduct, err := productSvc.Create(context.Background(), tenantID, CreateProductRequest{
		Name:          "Jabón",
		IsFiscal:      false,
		InternalPrice: &manualPrice,
	})
	if err != nil {
		t.Fatalf("create manual product: %v", err)
	}

	// Open the cash session with a NEW rate (46) — must recompute the USD
	// product's internal_price in the same operation, and must not touch
	// the manually-priced product.
	newRate := 46.0
	if _, err := cashSvc.Open(context.Background(), tenantID, supabaseUID, &newRate, "opening"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	gotUSD, err := productSvc.Get(context.Background(), tenantID, usdProduct.Product.ID)
	if err != nil {
		t.Fatalf("get usd product: %v", err)
	}
	if gotUSD.InternalPrice == nil || *gotUSD.InternalPrice != 46.0 {
		t.Errorf("expected recomputed internal_price=46.0, got %v", gotUSD.InternalPrice)
	}

	gotManual, err := productSvc.Get(context.Background(), tenantID, manualProduct.Product.ID)
	if err != nil {
		t.Fatalf("get manual product: %v", err)
	}
	if gotManual.InternalPrice == nil || *gotManual.InternalPrice != 99.0 {
		t.Errorf("expected manual product untouched at 99.0, got %v", gotManual.InternalPrice)
	}
}
