package handler

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/adapter/exchangerate"
	"github.com/danzt/daas/api/internal/adapter/http/middleware"
	"github.com/danzt/daas/api/internal/adapter/storage"
	"github.com/danzt/daas/api/internal/app"
	"github.com/danzt/daas/api/internal/domain/product"
)

// ProductHandler handles the product catalog HTTP endpoints.
// It is a thin adapter: parse → call service → format response.
type ProductHandler struct {
	service *app.ProductService
	storage storage.Storage // optional — required only for image upload/delete
}

// SetStorage injects the storage adapter used for product image uploads.
// Must be called before UploadProductImage or DeleteProductImage are used.
func (h *ProductHandler) SetStorage(st storage.Storage) {
	h.storage = st
}

// NewProductHandler creates a ProductHandler backed by the given service.
// fetcher is used to resolve a USD-priced product's internal_price when the
// tenant has no exchange_rates row recorded yet.
func NewProductHandler(pool *pgxpool.Pool, fetcher exchangerate.Fetcher) *ProductHandler {
	return &ProductHandler{service: app.NewProductService(pool, fetcher)}
}

// ----- Request / Response types -----

type createProductRequest struct {
	Name          string     `json:"name"`
	SKU           string     `json:"sku,omitempty"`
	Barcode       string     `json:"barcode,omitempty"`
	Unit          string     `json:"unit,omitempty"`
	Description   string     `json:"description,omitempty"`
	CategoryID    *uuid.UUID `json:"category_id,omitempty"`
	IsFiscal      bool       `json:"is_fiscal"`
	FiscalPrice   *float64   `json:"fiscal_price,omitempty"`
	InternalPrice *float64   `json:"internal_price,omitempty"`
	PriceUSD      *float64   `json:"price_usd,omitempty"`
	TaxRate       *float64   `json:"tax_rate,omitempty"`
}

type updateProductRequest struct {
	Name          string     `json:"name"`
	SKU           string     `json:"sku,omitempty"`
	Barcode       string     `json:"barcode,omitempty"`
	Unit          string     `json:"unit,omitempty"`
	Description   string     `json:"description,omitempty"`
	CategoryID    *uuid.UUID `json:"category_id,omitempty"`
	FiscalPrice   *float64   `json:"fiscal_price,omitempty"`
	InternalPrice *float64   `json:"internal_price,omitempty"`
	PriceUSD      *float64   `json:"price_usd,omitempty"`
	TaxRate       *float64   `json:"tax_rate,omitempty"`
}

type createCategoryRequest struct {
	Name     string     `json:"name"`
	ParentID *uuid.UUID `json:"parent_id,omitempty"`
}

type productResponse struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	Name          string     `json:"name"`
	SKU           string     `json:"sku,omitempty"`
	Barcode       string     `json:"barcode,omitempty"`
	Unit          string     `json:"unit,omitempty"`
	Description   string     `json:"description,omitempty"`
	CategoryID    *uuid.UUID `json:"category_id,omitempty"`
	IsFiscal      bool       `json:"is_fiscal"`
	FiscalPrice   *float64   `json:"fiscal_price,omitempty"`
	InternalPrice *float64   `json:"internal_price,omitempty"`
	PriceUSD      *float64   `json:"price_usd,omitempty"`
	TaxRate       *float64   `json:"tax_rate,omitempty"`
	Active        bool       `json:"active"`
	ImageURL      string     `json:"image_url,omitempty"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
}

type productResultResponse struct {
	Product  productResponse `json:"product"`
	Warnings []string        `json:"warnings,omitempty"`
}

type categoryResponse struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Name      string     `json:"name"`
	ParentID  *uuid.UUID `json:"parent_id,omitempty"`
	CreatedAt string     `json:"created_at"`
}

// ----- Helpers -----

func toProductResponse(p *product.Product) productResponse {
	return productResponse{
		ID:            p.ID.String(),
		TenantID:      p.TenantID.String(),
		Name:          p.Name,
		SKU:           p.SKU,
		Barcode:       p.Barcode,
		Unit:          p.Unit,
		Description:   p.Description,
		CategoryID:    p.CategoryID,
		IsFiscal:      p.IsFiscal,
		FiscalPrice:   p.FiscalPrice,
		InternalPrice: p.InternalPrice,
		PriceUSD:      p.PriceUSD,
		TaxRate:       p.TaxRate,
		Active:        p.Active,
		ImageURL:      p.ImageURL,
		CreatedAt:     p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     p.UpdatedAt.Format(time.RFC3339),
	}
}

func mapProductError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, product.ErrFiscalPriceMissing):
		return WriteProblem(c, http.StatusUnprocessableEntity, "fiscal-price-missing",
			"fiscal product must have a positive fiscal_price")
	case errors.Is(err, product.ErrInternalPriceMissing):
		return WriteProblem(c, http.StatusUnprocessableEntity, "internal-price-missing",
			"internal product must have a positive internal_price")
	case errors.Is(err, product.ErrTaxRateMissing):
		return WriteProblem(c, http.StatusUnprocessableEntity, "tax-rate-missing",
			"fiscal product must have a tax_rate")
	case errors.Is(err, product.ErrTaxRateOnInternal):
		return WriteProblem(c, http.StatusUnprocessableEntity, "tax-rate-on-internal",
			"internal product must not have a tax_rate")
	case errors.Is(err, product.ErrZeroPrice):
		return WriteProblem(c, http.StatusUnprocessableEntity, "zero-price", "price must be greater than zero")
	case errors.Is(err, product.ErrProductNotFound):
		return WriteProblem(c, http.StatusNotFound, "product-not-found", "product not found in this tenant")
	case errors.Is(err, product.ErrDuplicateSKU):
		return WriteProblem(c, http.StatusConflict, "duplicate-sku", "a product with this SKU already exists")
	default:
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "an unexpected error occurred")
	}
}

func getTenantID(c echo.Context) (uuid.UUID, error) {
	raw, _ := c.Get(string(middleware.ContextKeyTenantID)).(string)
	if raw == "" {
		return uuid.Nil, errors.New("tenant context missing")
	}
	return uuid.Parse(raw)
}

// ----- Handlers -----

// ListProducts handles GET /api/v1/products
// Query params: active, is_fiscal, category_id, q, page, limit
func (h *ProductHandler) ListProducts(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	filters := app.ProductFilters{}

	if v := c.QueryParam("active"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return WriteProblem(c, http.StatusBadRequest, "bad-request", "active must be true or false")
		}
		filters.Active = &b
	}
	if v := c.QueryParam("is_fiscal"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return WriteProblem(c, http.StatusBadRequest, "bad-request", "is_fiscal must be true or false")
		}
		filters.IsFiscal = &b
	}
	if v := c.QueryParam("category_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return WriteProblem(c, http.StatusBadRequest, "bad-request", "category_id must be a valid UUID")
		}
		filters.CategoryID = &id
	}
	filters.Query = c.QueryParam("q")

	if v := c.QueryParam("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filters.Page = n
		}
	}
	if v := c.QueryParam("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filters.Limit = n
		}
	}

	products, err := h.service.List(c.Request().Context(), tenantID, filters)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to list products")
	}

	resp := make([]productResponse, 0, len(products))
	for _, p := range products {
		resp = append(resp, toProductResponse(p))
	}
	return c.JSON(http.StatusOK, resp)
}

// GetProduct handles GET /api/v1/products/:id
func (h *ProductHandler) GetProduct(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "id must be a valid UUID")
	}

	p, err := h.service.Get(c.Request().Context(), tenantID, id)
	if err != nil {
		return mapProductError(c, err)
	}
	return c.JSON(http.StatusOK, toProductResponse(p))
}

// CreateProduct handles POST /api/v1/products
func (h *ProductHandler) CreateProduct(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	var req createProductRequest
	if err := c.Bind(&req); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid JSON body")
	}
	if req.Name == "" {
		return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error", "name is required")
	}

	result, err := h.service.Create(c.Request().Context(), tenantID, app.CreateProductRequest{
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
	})
	if err != nil {
		return mapProductError(c, err)
	}

	return c.JSON(http.StatusCreated, productResultResponse{
		Product:  toProductResponse(result.Product),
		Warnings: result.Warnings,
	})
}

// UpdateProduct handles PUT /api/v1/products/:id
func (h *ProductHandler) UpdateProduct(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "id must be a valid UUID")
	}

	var req updateProductRequest
	if err := c.Bind(&req); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid JSON body")
	}
	if req.Name == "" {
		return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error", "name is required")
	}

	p, err := h.service.Update(c.Request().Context(), tenantID, id, app.UpdateProductRequest{
		Name:          req.Name,
		SKU:           req.SKU,
		Barcode:       req.Barcode,
		Unit:          req.Unit,
		Description:   req.Description,
		CategoryID:    req.CategoryID,
		FiscalPrice:   req.FiscalPrice,
		InternalPrice: req.InternalPrice,
		PriceUSD:      req.PriceUSD,
		TaxRate:       req.TaxRate,
	})
	if err != nil {
		return mapProductError(c, err)
	}
	return c.JSON(http.StatusOK, toProductResponse(p))
}

// DeleteProduct handles DELETE /api/v1/products/:id (soft delete)
func (h *ProductHandler) DeleteProduct(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "id must be a valid UUID")
	}

	p, err := h.service.Delete(c.Request().Context(), tenantID, id)
	if err != nil {
		return mapProductError(c, err)
	}
	return c.JSON(http.StatusOK, toProductResponse(p))
}

// ListCategories handles GET /api/v1/products/categories
func (h *ProductHandler) ListCategories(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	cats, err := h.service.ListCategories(c.Request().Context(), tenantID)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to list categories")
	}

	resp := make([]categoryResponse, 0, len(cats))
	for _, cat := range cats {
		resp = append(resp, categoryResponse{
			ID:        cat.ID.String(),
			TenantID:  cat.TenantID.String(),
			Name:      cat.Name,
			ParentID:  cat.ParentID,
			CreatedAt: cat.CreatedAt.Format(time.RFC3339),
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// CreateCategory handles POST /api/v1/products/categories
func (h *ProductHandler) CreateCategory(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	var req createCategoryRequest
	if err := c.Bind(&req); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid JSON body")
	}
	if req.Name == "" {
		return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error", "name is required")
	}

	cat, err := h.service.CreateCategory(c.Request().Context(), tenantID, app.CreateCategoryRequest{
		Name:     req.Name,
		ParentID: req.ParentID,
	})
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to create category")
	}

	return c.JSON(http.StatusCreated, categoryResponse{
		ID:        cat.ID.String(),
		TenantID:  cat.TenantID.String(),
		Name:      cat.Name,
		ParentID:  cat.ParentID,
		CreatedAt: cat.CreatedAt.Format(time.RFC3339),
	})
}

// DeleteCategory handles DELETE /api/v1/products/categories/:id
func (h *ProductHandler) DeleteCategory(c echo.Context) error {
	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid category id")
	}

	if err := h.service.DeleteCategory(c.Request().Context(), tenantID, id); err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to delete category")
	}

	return c.NoContent(http.StatusNoContent)
}

// allowedImageTypes lists the MIME types accepted for product images.
var allowedImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

const maxProductImageSize = 5 * 1024 * 1024 // 5 MB

// UploadProductImage handles POST /api/v1/products/:id/image
//
// Multipart field "image" (required). Accepts image/jpeg, image/png, image/webp.
// Max 5 MB. Stores via the injected storage adapter and sets image_url on the product.
func (h *ProductHandler) UploadProductImage(c echo.Context) error {
	if h.storage == nil {
		return WriteProblem(c, http.StatusInternalServerError, "storage-unavailable", "storage adapter not configured")
	}

	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "id must be a valid UUID")
	}

	fileHeader, err := c.FormFile("image")
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "image-required", "multipart field 'image' is required")
	}

	// Validate content-type.
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// Strip parameters (e.g. "image/jpeg; name=foo.jpg" → "image/jpeg")
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	ext, ok := allowedImageTypes[contentType]
	if !ok {
		return WriteProblem(c, http.StatusUnsupportedMediaType, "unsupported-media-type",
			"image must be image/jpeg, image/png, or image/webp")
	}

	// Validate size.
	if fileHeader.Size > maxProductImageSize {
		return WriteProblem(c, http.StatusRequestEntityTooLarge, "file-too-large",
			fmt.Sprintf("image must not exceed %d MB", maxProductImageSize/1024/1024))
	}

	// Override ext from filename when the declared content-type seems generic.
	if fnExt := strings.ToLower(filepath.Ext(fileHeader.Filename)); fnExt != "" {
		switch fnExt {
		case ".jpg", ".jpeg":
			ext = ".jpg"
		case ".png":
			ext = ".png"
		case ".webp":
			ext = ".webp"
		}
	}

	file, err := fileHeader.Open()
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "cannot open uploaded file")
	}
	defer func() { _ = file.Close() }()

	storageKey := fmt.Sprintf("product-images/%s/%s/%d%s", tenantID, id, time.Now().UnixMilli(), ext)

	publicURL, err := h.storage.Save(c.Request().Context(), storageKey, file, contentType)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to store image")
	}

	updated, err := h.service.SetImageURL(c.Request().Context(), tenantID, id, publicURL)
	if err != nil {
		return mapProductError(c, err)
	}
	return c.JSON(http.StatusOK, toProductResponse(updated))
}

// DeleteProductImage handles DELETE /api/v1/products/:id/image
//
// Removes the stored image (if any) and clears image_url on the product.
// Idempotent — products without an image return 200 with the unchanged product.
func (h *ProductHandler) DeleteProductImage(c echo.Context) error {
	if h.storage == nil {
		return WriteProblem(c, http.StatusInternalServerError, "storage-unavailable", "storage adapter not configured")
	}

	tenantID, err := getTenantID(c)
	if err != nil {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "id must be a valid UUID")
	}

	// Fetch current product to get the existing image_url.
	p, err := h.service.Get(c.Request().Context(), tenantID, id)
	if err != nil {
		return mapProductError(c, err)
	}

	// If there is an image, delete it from storage.
	if p.ImageURL != "" {
		// Extract storage key from the URL.
		// URL format for LocalFS:  http://host/files/product-images/...
		// URL format for Supabase: https://host/storage/v1/object/public/bucket/product-images/...
		// We only need the path after the bucket/files prefix.
		storageKey := extractStorageKey(p.ImageURL)
		if storageKey != "" {
			_ = h.storage.Delete(c.Request().Context(), storageKey) // best-effort
		}
	}

	updated, err := h.service.ClearImageURL(c.Request().Context(), tenantID, id)
	if err != nil {
		return mapProductError(c, err)
	}
	return c.JSON(http.StatusOK, toProductResponse(updated))
}

// extractStorageKey tries to extract the storage key from a public URL.
// Returns empty string when the URL pattern is not recognised.
func extractStorageKey(publicURL string) string {
	// Supabase pattern: .../object/public/<bucket>/<key>
	if idx := strings.Index(publicURL, "/object/public/"); idx != -1 {
		after := publicURL[idx+len("/object/public/"):]
		// Strip bucket prefix (first segment).
		if slashIdx := strings.Index(after, "/"); slashIdx != -1 {
			return after[slashIdx+1:]
		}
		return after
	}
	// LocalFS pattern: http://host/files/<key>
	if idx := strings.Index(publicURL, "/files/"); idx != -1 {
		return publicURL[idx+len("/files/"):]
	}
	return ""
}
