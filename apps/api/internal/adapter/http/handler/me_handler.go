package handler

import (
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
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	FiscalID      string          `json:"fiscal_id"`
	CountryCode   string          `json:"country_code"`
	Status        string          `json:"status"`
	CreatedAt     string          `json:"created_at"`
	Slug          string          `json:"slug"`
	Features      map[string]bool `json:"features"`
	PosSearchMode string          `json:"pos_search_mode"`
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
		`SELECT id, name, fiscal_id, country_code, status, created_at, slug, pos_search_mode
		 FROM tenants
		 WHERE id = $1`,
		tenantID,
	).Scan(&resp.ID, &resp.Name, &fiscalID, &resp.CountryCode, &resp.Status, &createdAt, &resp.Slug, &resp.PosSearchMode)
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

	features := map[string]bool{
		"storefront":       false,
		"fiscal_invoicing": false,
	}
	rows, err := h.pool.Query(c.Request().Context(),
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
	resp.Features = features

	return c.JSON(http.StatusOK, resp)
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

	features := map[string]bool{
		"storefront":       false,
		"fiscal_invoicing": false,
	}
	rows, err := h.pool.Query(c.Request().Context(),
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
	resp.Features = features

	return c.JSON(http.StatusOK, resp)
}
