// Package app contains application services that orchestrate domain entities
// and external adapters. These services coordinate multiple domain operations
// and call external APIs (Supabase Admin API) as needed.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/domain/tenant"
)

// countryCodeRE validates ISO-3166 alpha-2 country codes (2 uppercase letters).
var countryCodeRE = regexp.MustCompile(`^[A-Z]{2}$`)

// RegisterRequest holds the input data for tenant registration.
type RegisterRequest struct {
	Name        string
	Email       string
	Password    string
	CountryCode string
	FiscalID    string // optional
}

// RegisterResult is returned on successful registration.
type RegisterResult struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
	Email    string
}

// TenantService orchestrates tenant registration: creates the DB tenant,
// calls the Supabase Admin API to create the Auth user, and links them.
type TenantService struct {
	pool           *pgxpool.Pool
	supabaseURL    string
	serviceRoleKey string
	httpClient     *http.Client
}

// NewTenantService creates a TenantService.
// supabaseURL and serviceRoleKey are used to call the Supabase Admin API.
func NewTenantService(pool *pgxpool.Pool, supabaseURL, serviceRoleKey string) *TenantService {
	return &TenantService{
		pool:           pool,
		supabaseURL:    supabaseURL,
		serviceRoleKey: serviceRoleKey,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
	}
}

// Register creates a new tenant and owner user in a single atomic operation.
//
// Flow:
//  1. Validate inputs.
//  2. Begin DB transaction.
//  3. Insert tenant row.
//  4. Call Supabase Admin API to create the Auth user.
//  5. PATCH the Auth user to set app_metadata.tenant_id.
//  6. Insert tenant_user row linking the Supabase UID to the tenant.
//  7. Commit. If any step fails, rollback and return the appropriate error.
func (s *TenantService) Register(ctx context.Context, req RegisterRequest) (*RegisterResult, error) {
	if err := validateRegisterRequest(req); err != nil {
		return nil, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Step 1: create tenant row.
	tenantRow, err := s.createTenantDB(ctx, tx, req)
	if err != nil {
		return nil, err
	}

	// Step 2: create Supabase Auth user.
	supabaseUID, err := s.createSupabaseUser(ctx, req.Email, req.Password)
	if err != nil {
		return nil, err
	}

	// Step 3: set app_metadata.tenant_id on the Auth user so the JWT includes it.
	if err := s.setTenantMetadata(ctx, supabaseUID, tenantRow.ID, "owner"); err != nil {
		// If setting metadata fails we rollback DB — no orphan user with wrong state.
		return nil, fmt.Errorf("set tenant metadata: %w", err)
	}

	// Step 4: link Supabase user to tenant in tenant_users table.
	userID, err := s.createTenantUserDB(ctx, tx, tenantRow.ID, supabaseUID, req.Email)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &RegisterResult{
		TenantID: tenantRow.ID,
		UserID:   userID,
		Email:    req.Email,
	}, nil
}

// createTenantDB inserts a tenant row within the transaction.
//
// A slug is generated atomically using the PL/pgSQL function generate_tenant_slug()
// introduced by migration 000009. The CTE pre-allocates the tenant UUID so that
// slug generation can reference the ID for conflict exclusion in the same statement.
//
// STORE-SLUG-6: slug MUST be generated in the same transaction as the tenant INSERT.
func (s *TenantService) createTenantDB(ctx context.Context, tx pgx.Tx, req RegisterRequest) (*tenant.Tenant, error) {
	row := tx.QueryRow(ctx,
		`WITH new_id AS (
             SELECT uuid_generate_v4() AS id
         ),
         inserted AS (
             INSERT INTO tenants (id, name, fiscal_id, country_code, slug)
             SELECT id, $1::varchar, $2, $3, generate_tenant_slug($1::text, id)
             FROM new_id
             RETURNING id, name, fiscal_id, country_code, status, created_at, updated_at
         )
         SELECT * FROM inserted`,
		req.Name, req.FiscalID, req.CountryCode,
	)

	var t tenant.Tenant
	var fiscalID *string
	err := row.Scan(&t.ID, &t.Name, &fiscalID, &t.CountryCode, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}
	if fiscalID != nil {
		t.FiscalID = *fiscalID
	}
	return &t, nil
}

// createTenantUserDB inserts the tenant_user link row.
func (s *TenantService) createTenantUserDB(
	ctx context.Context, tx pgx.Tx,
	tenantID uuid.UUID, supabaseUID uuid.UUID, email string,
) (uuid.UUID, error) {
	var userID uuid.UUID
	err := tx.QueryRow(ctx,
		`INSERT INTO tenant_users (tenant_id, supabase_uid, email, role)
         VALUES ($1, $2, $3, 'owner')
         RETURNING id`,
		tenantID, supabaseUID, email,
	).Scan(&userID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create tenant user: %w", err)
	}
	return userID, nil
}

// supabaseCreateUserRequest is the payload for POST /auth/v1/admin/users.
type supabaseCreateUserRequest struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	EmailConfirm bool   `json:"email_confirm"`
}

// supabaseUserResponse is the response from the Supabase Admin API.
type supabaseUserResponse struct {
	ID string `json:"id"`
}

// createSupabaseUser calls the Supabase Admin API to create an Auth user.
// Returns the Supabase user UUID string.
func (s *TenantService) createSupabaseUser(ctx context.Context, email, password string) (uuid.UUID, error) {
	body := supabaseCreateUserRequest{Email: email, Password: password, EmailConfirm: true}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal create user request: %w", err)
	}

	url := s.supabaseURL + "/auth/v1/admin/users"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return uuid.Nil, fmt.Errorf("create supabase user request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.serviceRoleKey)
	req.Header.Set("apikey", s.serviceRoleKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return uuid.Nil, fmt.Errorf("call supabase admin API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnprocessableEntity || resp.StatusCode == http.StatusConflict {
		return uuid.Nil, tenant.ErrDuplicateEmail
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return uuid.Nil, fmt.Errorf("supabase admin API returned %d", resp.StatusCode)
	}

	var user supabaseUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return uuid.Nil, fmt.Errorf("decode supabase user response: %w", err)
	}

	uid, err := uuid.Parse(user.ID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse supabase user UUID %q: %w", user.ID, err)
	}
	return uid, nil
}

// supabaseUpdateMetadataRequest is the payload for PATCH /auth/v1/admin/users/{id}.
type supabaseUpdateMetadataRequest struct {
	AppMetadata map[string]any `json:"app_metadata"`
}

// setTenantMetadata calls PATCH /auth/v1/admin/users/{uid} to set the
// tenant_id and role in app_metadata. This causes the claim to appear in
// subsequent JWTs issued by Supabase Auth for this user.
func (s *TenantService) setTenantMetadata(ctx context.Context, uid uuid.UUID, tenantID uuid.UUID, role string) error {
	meta := supabaseUpdateMetadataRequest{
		AppMetadata: map[string]any{
			"tenant_id": tenantID.String(),
			"role":      role,
		},
	}
	bodyBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata request: %w", err)
	}

	url := s.supabaseURL + "/auth/v1/admin/users/" + uid.String()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create metadata patch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.serviceRoleKey)
	req.Header.Set("apikey", s.serviceRoleKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call supabase admin patch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("supabase admin PUT returned %d", resp.StatusCode)
	}
	return nil
}

// validateRegisterRequest performs input validation before touching any I/O.
func validateRegisterRequest(req RegisterRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if req.Email == "" {
		return fmt.Errorf("email is required")
	}
	if req.Password == "" {
		return fmt.Errorf("password is required")
	}
	if !countryCodeRE.MatchString(req.CountryCode) {
		return tenant.ErrInvalidCountryCode
	}
	return nil
}
