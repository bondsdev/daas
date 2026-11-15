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

// TestUserHandler_Invite_OwnerRoleRejected and TestUserHandler_Invite_InvalidRoleRejected
// validate role BEFORE Invite ever calls Supabase or the DB, so they run as
// fast unit tests with no pool/Supabase server needed.

func TestUserHandler_Invite_OwnerRoleRejected(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, "http://unused", "unused-key")
	rec := doInvite(t, h, map[string]string{"email": "a@example.com", "role": "owner"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestUserHandler_Invite_InvalidRoleRejected(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, "http://unused", "unused-key")
	rec := doInvite(t, h, map[string]string{"email": "a@example.com", "role": "superadmin"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
