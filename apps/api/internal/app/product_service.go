package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/adapter/exchangerate"
	"github.com/danzt/daas/api/internal/domain/product"
)

// ProductFilters holds the optional filters for listing products.
type ProductFilters struct {
	Active     *bool
	IsFiscal   *bool
	CategoryID *uuid.UUID
	Query      string // searches name, sku, barcode
	Page       int    // 1-based
	Limit      int    // max items per page
}

// CreateProductRequest is the input for creating a new product.
type CreateProductRequest struct {
	Name          string
	SKU           string
	Barcode       string
	Unit          string
	Description   string
	CategoryID    *uuid.UUID
	IsFiscal      bool
	FiscalPrice   *float64
	InternalPrice *float64
	// PriceUSD, when set, makes InternalPrice a derived value: it is
	// computed immediately as PriceUSD * the tenant's current BS/USD rate,
	// overriding whatever InternalPrice was also passed.
	PriceUSD *float64
	TaxRate  *float64
}

// UpdateProductRequest is the input for updating an existing product.
type UpdateProductRequest struct {
	Name          string
	SKU           string
	Barcode       string
	Unit          string
	Description   string
	CategoryID    *uuid.UUID
	FiscalPrice   *float64
	InternalPrice *float64
	// PriceUSD, when set, makes InternalPrice a derived value: it is
	// computed immediately as PriceUSD * the tenant's current BS/USD rate,
	// overriding whatever InternalPrice was also passed.
	PriceUSD *float64
	TaxRate  *float64
}

// CreateCategoryRequest is the input for creating a product category.
type CreateCategoryRequest struct {
	Name     string
	ParentID *uuid.UUID
}

// ProductResult wraps a product with optional warnings.
type ProductResult struct {
	Product  *product.Product
	Warnings []string // e.g. "barcode_duplicate"
}

// ProductService handles the product catalog use cases.
// It orchestrates domain validation and persistence via pgxpool.
type ProductService struct {
	pool    *pgxpool.Pool
	fetcher exchangerate.Fetcher
}

// NewProductService creates a ProductService backed by the given connection
// pool and BS/USD rate fetcher. The fetcher is only consulted when a USD
// price is being resolved for a tenant that has no exchange_rates row yet —
// see currentExchangeRate.
func NewProductService(pool *pgxpool.Pool, fetcher exchangerate.Fetcher) *ProductService {
	return &ProductService{pool: pool, fetcher: fetcher}
}

// resolveUSDPrice computes internal_price = priceUSD * the tenant's current
// BS/USD rate, via the shared ResolveUSDAmount helper.
func (s *ProductService) resolveUSDPrice(ctx context.Context, tenantID uuid.UUID, priceUSD float64) (float64, error) {
	price, err := ResolveUSDAmount(ctx, s.pool, s.fetcher, tenantID, priceUSD)
	if err != nil {
		return 0, fmt.Errorf("resolve usd price: %w", err)
	}
	return price, nil
}

// List returns a paginated list of products for the given tenant.
func (s *ProductService) List(ctx context.Context, tenantID uuid.UUID, f ProductFilters) ([]*product.Product, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	var query *string
	if f.Query != "" {
		query = &f.Query
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, name, sku, barcode, description, category_id,
		       is_fiscal, fiscal_price, internal_price, price_usd, tax_rate, active, image_url, created_at, updated_at, unit
		FROM products
		WHERE tenant_id = $1
		  AND ($2::boolean IS NULL OR active = $2)
		  AND ($3::boolean IS NULL OR is_fiscal = $3)
		  AND ($4::uuid IS NULL OR category_id = $4)
		  AND ($5::text IS NULL OR name ILIKE '%' || $5 || '%' OR sku ILIKE '%' || $5 || '%' OR barcode ILIKE '%' || $5 || '%')
		ORDER BY created_at DESC
		LIMIT $6 OFFSET $7`,
		tenantID, f.Active, f.IsFiscal, f.CategoryID, query, f.Limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	var products []*product.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list products rows: %w", err)
	}
	return products, nil
}

// Get returns a single product by ID within the given tenant.
func (s *ProductService) Get(ctx context.Context, tenantID, id uuid.UUID) (*product.Product, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, sku, barcode, description, category_id,
		       is_fiscal, fiscal_price, internal_price, price_usd, tax_rate, active, image_url, created_at, updated_at, unit
		FROM products
		WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	p, err := scanProduct(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, product.ErrProductNotFound
		}
		return nil, fmt.Errorf("get product: %w", err)
	}
	return p, nil
}

// Create validates and inserts a new product. If the barcode already exists on
// another active product, it returns a ProductResult with a "barcode_duplicate"
// warning but does NOT block the creation.
func (s *ProductService) Create(ctx context.Context, tenantID uuid.UUID, req CreateProductRequest) (*ProductResult, error) {
	// PriceUSD is the source of truth when set: compute internal_price from
	// the tenant's current rate right now, overriding any InternalPrice also
	// passed, so the product is never left with a stale/zero price.
	if req.PriceUSD != nil {
		computed, err := s.resolveUSDPrice(ctx, tenantID, *req.PriceUSD)
		if err != nil {
			return nil, err
		}
		req.InternalPrice = &computed
	}
	if req.Unit == "" {
		req.Unit = "unidad"
	}

	// Domain validation first — pure Go, no I/O.
	p := &product.Product{
		TenantID:      tenantID,
		Name:          req.Name,
		SKU:           req.SKU,
		Barcode:       req.Barcode,
		Unit:          req.Unit,
		Description:   req.Description,
		CategoryID:    req.CategoryID,
		IsFiscal:      req.IsFiscal,
		FiscalPrice:   req.FiscalPrice,
		InternalPrice: req.InternalPrice,
		PriceUSD:      req.PriceUSD,
		TaxRate:       req.TaxRate,
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}

	var warnings []string

	// Check for barcode duplicate (warning only, not blocking).
	if req.Barcode != "" {
		dups, err := s.checkBarcodeDuplicate(ctx, tenantID, req.Barcode)
		if err != nil {
			return nil, err
		}
		if len(dups) > 0 {
			warnings = append(warnings, "barcode_duplicate")
		}
	}

	// Insert.
	row := s.pool.QueryRow(ctx, `
		INSERT INTO products (tenant_id, name, sku, barcode, description, category_id,
		                      is_fiscal, fiscal_price, internal_price, price_usd, tax_rate, unit)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, tenant_id, name, sku, barcode, description, category_id,
		          is_fiscal, fiscal_price, internal_price, price_usd, tax_rate, active, image_url, created_at, updated_at, unit`,
		tenantID,
		nullableString(req.Name),
		nullableString(req.SKU),
		nullableString(req.Barcode),
		nullableString(req.Description),
		req.CategoryID,
		req.IsFiscal,
		req.FiscalPrice,
		req.InternalPrice,
		req.PriceUSD,
		req.TaxRate,
		req.Unit,
	)
	created, err := scanProduct(row)
	if err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}

	return &ProductResult{Product: created, Warnings: warnings}, nil
}

// Update modifies mutable fields of an existing product.
func (s *ProductService) Update(ctx context.Context, tenantID, id uuid.UUID, req UpdateProductRequest) (*product.Product, error) {
	// PriceUSD is the source of truth when set: compute internal_price from
	// the tenant's current rate right now, overriding any InternalPrice also
	// passed, so the product is never left with a stale/zero price.
	if req.PriceUSD != nil {
		computed, err := s.resolveUSDPrice(ctx, tenantID, *req.PriceUSD)
		if err != nil {
			return nil, err
		}
		req.InternalPrice = &computed
	}

	if req.Unit == "" {
		req.Unit = "unidad"
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE products
		SET name = $3, sku = $4, barcode = $5, description = $6, category_id = $7,
		    fiscal_price = $8, internal_price = $9, price_usd = $10, tax_rate = $11, unit = $12, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, name, sku, barcode, description, category_id,
		          is_fiscal, fiscal_price, internal_price, price_usd, tax_rate, active, image_url, created_at, updated_at, unit`,
		id, tenantID,
		req.Name, nullableString(req.SKU), nullableString(req.Barcode),
		nullableString(req.Description), req.CategoryID,
		req.FiscalPrice, req.InternalPrice, req.PriceUSD, req.TaxRate, req.Unit,
	)
	p, err := scanProduct(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, product.ErrProductNotFound
		}
		return nil, fmt.Errorf("update product: %w", err)
	}
	return p, nil
}

// Delete soft-deletes a product by setting active=false.
func (s *ProductService) Delete(ctx context.Context, tenantID, id uuid.UUID) (*product.Product, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE products SET active = FALSE, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, name, sku, barcode, description, category_id,
		          is_fiscal, fiscal_price, internal_price, price_usd, tax_rate, active, image_url, created_at, updated_at, unit`,
		id, tenantID,
	)
	p, err := scanProduct(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, product.ErrProductNotFound
		}
		return nil, fmt.Errorf("delete product: %w", err)
	}
	return p, nil
}

// ListCategories returns all categories for the given tenant, ordered by name.
func (s *ProductService) ListCategories(ctx context.Context, tenantID uuid.UUID) ([]*product.Category, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, name, parent_id, created_at
		FROM product_categories
		WHERE tenant_id = $1
		ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	var cats []*product.Category
	for rows.Next() {
		var c product.Category
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.ParentID, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		cats = append(cats, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list categories rows: %w", err)
	}
	return cats, nil
}

// CreateCategory inserts a new product category.
func (s *ProductService) CreateCategory(ctx context.Context, tenantID uuid.UUID, req CreateCategoryRequest) (*product.Category, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("category name is required")
	}

	var cat product.Category
	err := s.pool.QueryRow(ctx, `
		INSERT INTO product_categories (tenant_id, name, parent_id)
		VALUES ($1, $2, $3)
		RETURNING id, tenant_id, name, parent_id, created_at`,
		tenantID, req.Name, req.ParentID,
	).Scan(&cat.ID, &cat.TenantID, &cat.Name, &cat.ParentID, &cat.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create category: %w", err)
	}
	return &cat, nil
}

// checkBarcodeDuplicate returns any active products in the tenant with the given barcode.
func (s *ProductService) checkBarcodeDuplicate(ctx context.Context, tenantID uuid.UUID, barcode string) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM products
		WHERE tenant_id = $1 AND barcode = $2 AND active = TRUE`,
		tenantID, barcode,
	)
	if err != nil {
		return nil, fmt.Errorf("check barcode duplicate: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan barcode duplicate: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// scanProduct scans a single product row from any pgx.Row or pgx.Rows.
func scanProduct(row interface {
	Scan(dest ...any) error
}) (*product.Product, error) {
	var p product.Product
	var sku, barcode, description, imageURL *string
	var createdAt, updatedAt time.Time

	err := row.Scan(
		&p.ID, &p.TenantID, &p.Name,
		&sku, &barcode, &description,
		&p.CategoryID,
		&p.IsFiscal, &p.FiscalPrice, &p.InternalPrice, &p.PriceUSD, &p.TaxRate,
		&p.Active, &imageURL, &createdAt, &updatedAt, &p.Unit,
	)
	if err != nil {
		return nil, err
	}
	if sku != nil {
		p.SKU = *sku
	}
	if barcode != nil {
		p.Barcode = *barcode
	}
	if description != nil {
		p.Description = *description
	}
	if imageURL != nil {
		p.ImageURL = *imageURL
	}
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	return &p, nil
}

// SetImageURL sets image_url on a product and returns the updated product.
func (s *ProductService) SetImageURL(ctx context.Context, tenantID, id uuid.UUID, imageURL string) (*product.Product, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE products SET image_url = $3, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, name, sku, barcode, description, category_id,
		          is_fiscal, fiscal_price, internal_price, price_usd, tax_rate, active, image_url, created_at, updated_at`,
		id, tenantID, imageURL,
	)
	p, err := scanProduct(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, product.ErrProductNotFound
		}
		return nil, fmt.Errorf("set image_url: %w", err)
	}
	return p, nil
}

// ClearImageURL sets image_url to NULL on a product and returns the updated product.
func (s *ProductService) ClearImageURL(ctx context.Context, tenantID, id uuid.UUID) (*product.Product, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE products SET image_url = NULL, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, name, sku, barcode, description, category_id,
		          is_fiscal, fiscal_price, internal_price, price_usd, tax_rate, active, image_url, created_at, updated_at`,
		id, tenantID,
	)
	p, err := scanProduct(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, product.ErrProductNotFound
		}
		return nil, fmt.Errorf("clear image_url: %w", err)
	}
	return p, nil
}

// nullableString converts an empty string to nil for nullable DB columns.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// DeleteCategory removes a category by ID for the given tenant.
// Returns pgx.ErrNoRows if the category does not exist or belongs to another tenant.
func (s *ProductService) DeleteCategory(ctx context.Context, tenantID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM product_categories WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
