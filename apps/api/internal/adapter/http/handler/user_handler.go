package handler

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"

	mw "github.com/danzt/daas/api/internal/adapter/http/middleware"
	"github.com/danzt/daas/api/internal/app"
	"github.com/danzt/daas/api/internal/domain/tenant"
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
// Role is optional: empty defaults to "employee". Only "employee" and
// "rider" are invitable — "owner" is never assignable via invite.
//
// Password is required: accounts are created directly with a password
// (no magic-link email), since Supabase Cloud's default email service
// is rate-limited and no custom SMTP provider is configured yet.
type inviteRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role,omitempty"`
}

// Invite handles POST /api/v1/users/invite.
// Creates a Supabase Auth user directly with the given email/password
// (no email is sent) and links it to the tenant.
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
	if len(req.Password) < 6 {
		return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error", "password must be at least 6 characters")
	}

	role := req.Role
	if role == "" {
		role = "employee"
	}
	if role != "employee" && role != "rider" {
		return WriteProblem(c, http.StatusUnprocessableEntity, "validation-error", "role must be employee or rider")
	}

	tenantUUID, err := uuid.Parse(tenantID)
	if err != nil {
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "invalid tenant context")
	}

	supabaseUID, err := h.tenantSvc.CreateSupabaseUser(c.Request().Context(), req.Email, req.Password)
	if err != nil {
		if err == tenant.ErrDuplicateEmail {
			return WriteProblem(c, http.StatusConflict, "duplicate-email", "email already registered")
		}
		log.Error().Err(err).Str("email", req.Email).
			Msg("invite: failed to create supabase user via /auth/v1/admin/users")
		return WriteProblem(c, http.StatusInternalServerError, "internal-error", "failed to create rider/employee account")
	}

	if err := h.tenantSvc.SetTenantMetadata(c.Request().Context(), supabaseUID, tenantUUID, role); err != nil {
		log.Error().Err(err).Str("email", req.Email).Str("supabase_uid", supabaseUID.String()).
			Msg("invite: supabase user created but app_metadata PATCH failed — manual follow-up needed")
		return WriteProblem(c, http.StatusInternalServerError, "internal-error",
			"invite partially succeeded: user created in auth but local setup failed; contact support")
	}

	if _, err := h.pool.Exec(c.Request().Context(),
		`INSERT INTO tenant_users (tenant_id, supabase_uid, email, role)
         VALUES ($1, $2, $3, $4)`,
		tenantUUID, supabaseUID, req.Email, role,
	); err != nil {
		log.Error().Err(err).Str("email", req.Email).Str("supabase_uid", supabaseUID.String()).
			Msg("invite: supabase user created and metadata set but tenant_users insert failed — manual follow-up needed")
		return WriteProblem(c, http.StatusInternalServerError, "internal-error",
			"invite partially succeeded: user created in auth but local setup failed; contact support")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "account created for " + req.Email,
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
