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
)

// newInviteHandler builds a UserHandler pointed at a mock Supabase invite
// endpoint. The pool is nil — role validation returns before any DB access.
func newInviteHandler(t *testing.T) *handler.UserHandler {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return handler.NewUserHandler(nil, srv.URL, "test-service-role-key")
}

func doInvite(t *testing.T, h *handler.UserHandler, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/invite", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(middleware.ContextKeyTenantID), uuid.New().String())
	if err := h.Invite(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return rec
}

func TestUserHandler_Invite_DefaultRoleIsEmployee(t *testing.T) {
	h := newInviteHandler(t)
	rec := doInvite(t, h, map[string]string{"email": "a@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestUserHandler_Invite_RiderRoleAccepted(t *testing.T) {
	h := newInviteHandler(t)
	rec := doInvite(t, h, map[string]string{"email": "a@example.com", "role": "rider"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestUserHandler_Invite_OwnerRoleRejected(t *testing.T) {
	h := newInviteHandler(t)
	rec := doInvite(t, h, map[string]string{"email": "a@example.com", "role": "owner"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestUserHandler_Invite_InvalidRoleRejected(t *testing.T) {
	h := newInviteHandler(t)
	rec := doInvite(t, h, map[string]string{"email": "a@example.com", "role": "superadmin"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
