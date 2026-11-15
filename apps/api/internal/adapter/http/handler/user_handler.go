package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"

	mw "github.com/danzt/daas/api/internal/adapter/http/middleware"
	"github.com/danzt/daas/api/internal/app"
)

// UserHandler handles user management endpoints.
// All endpoints require JWT auth and operate within the tenant context.
type UserHandler struct {
	pool           *pgxpool.Pool
	tenantSvc      *app.TenantService
	supabaseURL    string
	serviceRoleKey string
	httpClient     *http.Client
}

// NewUserHandler creates a UserHandler. tenantSvc is reused to set
// app_metadata on invited users via the Supabase Admin API.
func NewUserHandler(pool *pgxpool.Pool, tenantSvc *app.TenantService, supabaseURL, serviceRoleKey string) *UserHandler {
	return &UserHandler{
		pool:           pool,
		tenantSvc:      tenantSvc,
		supabaseURL:    supabaseURL,
		serviceRoleKey: serviceRoleKey,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
	}
}

// userResponse represents a tenant user in API responses.
type userResponse struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	SupabaseUID string `json:"supabase_uid"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Active      bool   `json:"active"`
	CreatedAt   string `json:"created_at"`
}

// List handles GET /api/v1/users.
// Returns all users within the authenticated tenant. RLS ensures tenant isolation.
func (h *UserHandler) List(c echo.Context) error {
	tenantID, _ := c.Get(string(mw.ContextKeyTenantID)).(string)
	if tenantID == "" {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	rows, err := h.pool.Query(c.Request().Context(),
		`SELECT id, tenant_id, supabase_uid, email, role, active, created_at
         FROM tenant_users
         WHERE tenant_id = $1
         ORDER BY created_at`,
		tenantID,
	)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to list users")
	}
	defer rows.Close()

	var users []userResponse
	for rows.Next() {
		var u userResponse
		var createdAt time.Time
		if err := rows.Scan(&u.ID, &u.TenantID, &u.SupabaseUID, &u.Email, &u.Role, &u.Active, &createdAt); err != nil {
			return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to scan user row")
		}
		u.CreatedAt = createdAt.Format(time.RFC3339)
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "error iterating user rows")
	}

	if users == nil {
		users = []userResponse{}
	}
	return c.JSON(http.StatusOK, users)
}

// inviteRequest is the JSON body for POST /api/v1/users/invite.
type inviteRequest struct {
	Email string `json:"email"`
}

// Invite handles POST /api/v1/users/invite.
// Sends a Supabase magic-link invite to the given email.
// Requires owner role (enforced by OwnerGuard middleware).
func (h *UserHandler) Invite(c echo.Context) error {
	tenantID, _ := c.Get(string(mw.ContextKeyTenantID)).(string)
	if tenantID == "" {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	var req inviteRequest
	if err := c.Bind(&req); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid JSON body")
	}
	if req.Email == "" {
		return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error", "email is required")
	}

	// Call Supabase Admin API to send invite.
	payload := map[string]interface{}{
		"email": req.Email,
		"data":  map[string]interface{}{"tenant_id": tenantID, "role": "employee"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to build invite request")
	}

	url := h.supabaseURL + "/auth/v1/admin/invite"
	httpReq, err := http.NewRequestWithContext(c.Request().Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to create invite request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+h.serviceRoleKey)
	httpReq.Header.Set("apikey", h.serviceRoleKey)

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return WriteProblem(c, http.StatusBadGateway, "auth-unavailable", "authentication service unavailable")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnprocessableEntity || resp.StatusCode == http.StatusConflict {
		return WriteProblem(c, http.StatusConflict, "duplicate-email", "email already registered")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error",
			"failed to send invite via auth service")
	}

	var invited struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&invited); err != nil {
		log.Error().Err(err).Str("email", req.Email).
			Msg("invite: supabase user created but failed to decode response — manual follow-up needed")
		return WriteProblem(c, http.StatusInternalServerError, "internal-error",
			"invite partially succeeded: user created in auth but local setup failed; contact support")
	}
	supabaseUID, err := uuid.Parse(invited.ID)
	if err != nil {
		log.Error().Err(err).Str("email", req.Email).Str("raw_id", invited.ID).
			Msg("invite: supabase user created but returned invalid UID — manual follow-up needed")
		return WriteProblem(c, http.StatusInternalServerError, "internal-error",
			"invite partially succeeded: user created in auth but local setup failed; contact support")
	}
	tenantUUID, err := uuid.Parse(tenantID)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "invalid tenant context")
	}

	const inviteRole = "employee"

	if err := h.tenantSvc.SetTenantMetadata(c.Request().Context(), supabaseUID, tenantUUID, inviteRole); err != nil {
		log.Error().Err(err).Str("email", req.Email).Str("supabase_uid", invited.ID).
			Msg("invite: supabase user created but app_metadata PATCH failed — manual follow-up needed")
		return WriteProblem(c, http.StatusInternalServerError, "internal-error",
			"invite partially succeeded: user created in auth but local setup failed; contact support")
	}

	if _, err := h.pool.Exec(c.Request().Context(),
		`INSERT INTO tenant_users (tenant_id, supabase_uid, email, role)
         VALUES ($1, $2, $3, $4)`,
		tenantUUID, supabaseUID, req.Email, inviteRole,
	); err != nil {
		log.Error().Err(err).Str("email", req.Email).Str("supabase_uid", invited.ID).
			Msg("invite: supabase user created and metadata set but tenant_users insert failed — manual follow-up needed")
		return WriteProblem(c, http.StatusInternalServerError, "internal-error",
			"invite partially succeeded: user created in auth but local setup failed; contact support")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "invite sent to " + req.Email,
	})
}

// updateActiveRequest is the JSON body for PATCH /api/v1/users/:id.
type updateActiveRequest struct {
	Active bool `json:"active"`
}

// UpdateActive handles PATCH /api/v1/users/:id.
// Toggles the active field for a tenant user. Requires owner role.
func (h *UserHandler) UpdateActive(c echo.Context) error {
	tenantID, _ := c.Get(string(mw.ContextKeyTenantID)).(string)
	if tenantID == "" {
		return WriteProblem(c, http.StatusForbidden, "forbidden", "tenant context missing")
	}

	userID := c.Param("id")
	if userID == "" {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "user ID is required")
	}

	var req updateActiveRequest
	if err := c.Bind(&req); err != nil {
		return WriteProblem(c, http.StatusBadRequest, "bad-request", "invalid JSON body")
	}

	var id string
	err := h.pool.QueryRow(c.Request().Context(),
		`UPDATE tenant_users SET active = $2
         WHERE id = $1 AND tenant_id = $3
         RETURNING id`,
		userID, req.Active, tenantID,
	).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return WriteProblem(c, http.StatusNotFound, "user-not-found", "user not found in this tenant")
		}
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to update user")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"id":     id,
		"active": req.Active,
	})
}
