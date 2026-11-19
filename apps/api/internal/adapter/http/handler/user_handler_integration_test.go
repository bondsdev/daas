//go:build integration

// Integration tests for UserHandler.Invite. They spin up a real PostgreSQL
// instance via testcontainers and a fake Supabase Admin API via httptest,
// exercising the full invite-acceptance wiring: metadata PATCH + tenant_users
// insert. Build with: go test -tags integration ./internal/adapter/http/handler/...
package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/adapter/http/handler"
	"github.com/danzt/daas/api/internal/adapter/http/middleware"
	"github.com/danzt/daas/api/internal/app"
)

// fakeSupabaseAdmin is a minimal stand-in for the Supabase Admin API used by
// Invite: POST /auth/v1/invite creates the user, PUT /auth/v1/admin/users/{id}
// sets app_metadata. patchFails lets tests force the partial-failure path.
type fakeSupabaseAdmin struct {
	userID     string
	patchFails bool

	lastPatchBody map[string]any
}

func (f *fakeSupabaseAdmin) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/auth/v1/invite":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": f.userID})
		case r.Method == http.MethodPut:
			if f.patchFails {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			var body struct {
				AppMetadata map[string]any `json:"app_metadata"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.lastPatchBody = body.AppMetadata
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func newInviteCtx(method, path, tenantID string, body []byte) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(middleware.ContextKeyTenantID), tenantID)
	return c, rec
}

func TestUserHandler_Invite_CreatesTenantUserAndMetadata(t *testing.T) {
	pool := setupTestDB(t)
	tenantID, _ := seedTenant(t, pool, "Invite Co")

	fake := &fakeSupabaseAdmin{userID: uuid.New().String()}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	tenantSvc := app.NewTenantService(pool, srv.URL, "service-role-key")
	h := handler.NewUserHandler(pool, tenantSvc, srv.URL, "service-role-key")

	c, rec := newInviteCtx(http.MethodPost, "/api/v1/users/invite", tenantID.String(),
		[]byte(`{"email":"newuser@example.com"}`))

	if err := h.Invite(c); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	if fake.lastPatchBody["role"] != "employee" {
		t.Errorf("expected app_metadata role=employee, got %v", fake.lastPatchBody["role"])
	}
	if fake.lastPatchBody["tenant_id"] != tenantID.String() {
		t.Errorf("expected app_metadata tenant_id=%s, got %v", tenantID, fake.lastPatchBody["tenant_id"])
	}

	var role, email string
	err := pool.QueryRow(c.Request().Context(),
		`SELECT role, email FROM tenant_users WHERE supabase_uid = $1 AND tenant_id = $2`,
		fake.userID, tenantID,
	).Scan(&role, &email)
	if err != nil {
		t.Fatalf("expected tenant_users row to exist: %v", err)
	}
	if role != "employee" {
		t.Errorf("expected role=employee, got %s", role)
	}
	if email != "newuser@example.com" {
		t.Errorf("expected email=newuser@example.com, got %s", email)
	}
}

func TestUserHandler_Invite_RiderRole_CreatesTenantUserWithRiderRole(t *testing.T) {
	pool := setupTestDB(t)
	tenantID, _ := seedTenant(t, pool, "Invite Co")

	fake := &fakeSupabaseAdmin{userID: uuid.New().String()}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	tenantSvc := app.NewTenantService(pool, srv.URL, "service-role-key")
	h := handler.NewUserHandler(pool, tenantSvc, srv.URL, "service-role-key")

	c, rec := newInviteCtx(http.MethodPost, "/api/v1/users/invite", tenantID.String(),
		[]byte(`{"email":"rider@example.com","role":"rider"}`))

	if err := h.Invite(c); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	if fake.lastPatchBody["role"] != "rider" {
		t.Errorf("expected app_metadata role=rider, got %v", fake.lastPatchBody["role"])
	}

	var role string
	err := pool.QueryRow(c.Request().Context(),
		`SELECT role FROM tenant_users WHERE supabase_uid = $1 AND tenant_id = $2`,
		fake.userID, tenantID,
	).Scan(&role)
	if err != nil {
		t.Fatalf("expected tenant_users row to exist: %v", err)
	}
	if role != "rider" {
		t.Errorf("expected role=rider, got %s", role)
	}
}

func TestUserHandler_Invite_MetadataPatchFails_ReturnsPartialFailure(t *testing.T) {
	pool := setupTestDB(t)
	tenantID, _ := seedTenant(t, pool, "Invite Co")

	fake := &fakeSupabaseAdmin{userID: uuid.New().String(), patchFails: true}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	tenantSvc := app.NewTenantService(pool, srv.URL, "service-role-key")
	h := handler.NewUserHandler(pool, tenantSvc, srv.URL, "service-role-key")

	c, rec := newInviteCtx(http.MethodPost, "/api/v1/users/invite", tenantID.String(),
		[]byte(`{"email":"partial@example.com"}`))

	if err := h.Invite(c); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var count int
	err := pool.QueryRow(c.Request().Context(),
		`SELECT count(*) FROM tenant_users WHERE supabase_uid = $1`, fake.userID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count tenant_users: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no tenant_users row after partial failure, got %d", count)
	}
}
