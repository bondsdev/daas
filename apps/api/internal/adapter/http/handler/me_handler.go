package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	mw "github.com/danzt/daas/api/internal/adapter/http/middleware"
)

// MeHandler handles the GET /api/v1/tenants/me endpoint.
// Returns the current tenant's profile data needed by the frontend store.
type MeHandler struct {
	pool *pgxpool.Pool
}

// NewMeHandler creates a MeHandler backed by the given connection pool.
func NewMeHandler(pool *pgxpool.Pool) *MeHandler {
	return &MeHandler{pool: pool}
}

// tenantMeResponse is the JSON shape returned by GET /api/v1/tenants/me.
type tenantMeResponse struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	FiscalID        string          `json:"fiscal_id"`
	CountryCode     string          `json:"country_code"`
	Status          string          `json:"status"`
	CreatedAt       string          `json:"created_at"`
	Slug            string          `json:"slug"`
	Features        map[string]bool `json:"features"`
	PosSearchMode   string          `json:"pos_search_mode"`
	DeliveryEnabled bool            `json:"delivery_enabled"`
}

// GetMe handles GET /api/v1/tenants/me.
// Returns the authenticated tenant's profile. Uses the tenant_id from the JWT
// (set by auth middleware) to look up the tenant row.
func (h *MeHandler) GetMe(c echo.Context) error {
	tenantID, _ := c.Get(string(mw.ContextKeyTenantID)).(string)
	if tenantID == "" {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	var resp tenantMeResponse
	var fiscalID *string
	var createdAt time.Time

	err := h.pool.QueryRow(c.Request().Context(),
		`SELECT id, name, fiscal_id, country_code, status, created_at, slug, pos_search_mode, delivery_enabled
		 FROM tenants
		 WHERE id = $1`,
		tenantID,
	).Scan(&resp.ID, &resp.Name, &fiscalID, &resp.CountryCode, &resp.Status, &createdAt, &resp.Slug, &resp.PosSearchMode, &resp.DeliveryEnabled)
	if err != nil {
		if err == pgx.ErrNoRows {
			return WriteProblem(c, http.StatusNotFound, "tenant-not-found", "tenant not found")
		}
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to fetch tenant")
	}

	if fiscalID != nil {
		resp.FiscalID = *fiscalID
	}
	resp.CreatedAt = createdAt.Format(time.RFC3339)

	resp.Features = loadTenantFeatures(c.Request().Context(), h.pool, tenantID)

	return c.JSON(http.StatusOK, resp)
}

// loadTenantFeatures fetches the tenant_features rows for a tenant and
// returns them merged over the known-features default map (missing rows
// default to false). Shared by GetMe, UpdateMe, and UpdateFeatures so all
// three return the same features shape.
func loadTenantFeatures(ctx context.Context, pool *pgxpool.Pool, tenantID string) map[string]bool {
	features := map[string]bool{
		"storefront":       false,
		"fiscal_invoicing": false,
	}
	rows, err := pool.Query(ctx,
		`SELECT feature, enabled FROM tenant_features WHERE tenant_id=$1`,
		tenantID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var feat string
			var featEnabled bool
			if scanErr := rows.Scan(&feat, &featEnabled); scanErr == nil {
				features[feat] = featEnabled
			}
		}
	}
	return features
}

// updateMeRequest is the JSON body for PATCH /api/v1/tenants/me.
type updateMeRequest struct {
	Name     string `json:"name"`
	FiscalID string `json:"fiscal_id"`
}

// UpdateMe handles PATCH /api/v1/tenants/me.
// Allows the owner to update the tenant's display name and fiscal ID.
func (h *MeHandler) UpdateMe(c echo.Context) error {
	tenantID, _ := c.Get(string(mw.ContextKeyTenantID)).(string)
	if tenantID == "" {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	var req updateMeRequest
	if err := c.Bind(&req); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid request body")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error", "name is required")
	}

	// Pass nil when fiscal_id is empty so the DB column is set to NULL.
	var fiscalIDArg any
	if v := strings.TrimSpace(req.FiscalID); v != "" {
		fiscalIDArg = v
	}

	var resp tenantMeResponse
	var fiscalIDVal *string
	var createdAt time.Time

	err := h.pool.QueryRow(c.Request().Context(),
		`UPDATE tenants
		 SET name = $2, fiscal_id = $3
		 WHERE id = $1
		 RETURNING id, name, fiscal_id, country_code, status, created_at, slug`,
		tenantID, name, fiscalIDArg,
	).Scan(&resp.ID, &resp.Name, &fiscalIDVal, &resp.CountryCode, &resp.Status, &createdAt, &resp.Slug)
	if err != nil {
		if err == pgx.ErrNoRows {
			return WriteProblem(c, http.StatusNotFound, "tenant-not-found", "tenant not found")
		}
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to update tenant")
	}

	if fiscalIDVal != nil {
		resp.FiscalID = *fiscalIDVal
	}
	resp.CreatedAt = createdAt.Format(time.RFC3339)

	resp.Features = loadTenantFeatures(c.Request().Context(), h.pool, tenantID)

	return c.JSON(http.StatusOK, resp)
}

// updateFeaturesRequest is the JSON body for PATCH /api/v1/tenants/me/features.
// Both fields are optional pointers so that an omitted field leaves that
// flag unchanged, letting callers toggle storefront and delivery independently
// or together in one request.
type updateFeaturesRequest struct {
	Storefront *bool `json:"storefront"`
	Delivery   *bool `json:"delivery"`
}

// UpdateFeatures handles PATCH /api/v1/tenants/me/features.
// Allows the owner to self-service enable/disable the storefront and
// delivery features for their own tenant, replacing the previous
// manual-SQL-only process.
func (h *MeHandler) UpdateFeatures(c echo.Context) error {
	tenantID, _ := c.Get(string(mw.ContextKeyTenantID)).(string)
	if tenantID == "" {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	var req updateFeaturesRequest
	if err := c.Bind(&req); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid request body")
	}

	if req.Storefront == nil && req.Delivery == nil {
		return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error", "at least one of storefront or delivery is required")
	}

	ctx := c.Request().Context()

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to start transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if req.Storefront != nil {
		if _, err := tx.Exec(ctx,
			`INSERT INTO tenant_features (tenant_id, feature, enabled, enabled_at)
			 VALUES ($1, 'storefront', $2, NOW())
			 ON CONFLICT (tenant_id, feature) DO UPDATE SET enabled = $2, enabled_at = NOW()`,
			tenantID, *req.Storefront,
		); err != nil {
			return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to update storefront feature")
		}
	}

	if req.Delivery != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE tenants SET delivery_enabled = $2, updated_at = NOW() WHERE id = $1`,
			tenantID, *req.Delivery,
		); err != nil {
			return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to update delivery feature")
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to commit changes")
	}

	var resp tenantMeResponse
	var fiscalID *string
	var createdAt time.Time

	err = h.pool.QueryRow(ctx,
		`SELECT id, name, fiscal_id, country_code, status, created_at, slug, pos_search_mode, delivery_enabled
		 FROM tenants
		 WHERE id = $1`,
		tenantID,
	).Scan(&resp.ID, &resp.Name, &fiscalID, &resp.CountryCode, &resp.Status, &createdAt, &resp.Slug, &resp.PosSearchMode, &resp.DeliveryEnabled)
	if err != nil {
		if err == pgx.ErrNoRows {
			return WriteProblem(c, http.StatusNotFound, "tenant-not-found", "tenant not found")
		}
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to fetch tenant")
	}

	if fiscalID != nil {
		resp.FiscalID = *fiscalID
	}
	resp.CreatedAt = createdAt.Format(time.RFC3339)
	resp.Features = loadTenantFeatures(ctx, h.pool, tenantID)

	return c.JSON(http.StatusOK, resp)
}
