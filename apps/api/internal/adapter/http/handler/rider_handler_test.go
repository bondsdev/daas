package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/adapter/http/handler"
	"github.com/danzt/daas/api/internal/adapter/http/middleware"
)

// newRiderCtx builds an Echo context with an optional role and tenant_id.
// No DB pool is used — these tests only exercise the role-gating and
// param-validation paths that return before touching the database.
func newRiderCtx(method, path, role, tenantID string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if role != "" {
		c.Set(string(middleware.ContextKeyRole), role)
	}
	if tenantID != "" {
		c.Set(string(middleware.ContextKeyTenantID), tenantID)
	}
	return c, rec
}

func TestRiderHandler_RoleGating(t *testing.T) {
	h := handler.NewRiderHandler(nil)
	tenantID := uuid.New().String()

	cases := []struct {
		name string
		call func(echo.Context) error
		role string
	}{
		{"CreateRider rejects rider role", h.CreateRider, "rider"},
		{"ListRiders rejects rider role", h.ListRiders, "rider"},
		{"UpdateRider rejects rider role", h.UpdateRider, "rider"},
		{"MyDeliveries rejects owner role", h.MyDeliveries, "owner"},
		{"MyDeliveries rejects employee role", h.MyDeliveries, "employee"},
		{"CreateRider rejects empty role", h.CreateRider, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newRiderCtx(http.MethodPost, "/", tc.role, tenantID)
			if err := tc.call(c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
			}
		})
	}
}

func TestRiderHandler_MissingTenantContext(t *testing.T) {
	h := handler.NewRiderHandler(nil)
	c, rec := newRiderCtx(http.MethodGet, "/", "owner", "")
	if err := h.ListRiders(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRiderHandler_InvalidRiderID(t *testing.T) {
	h := handler.NewRiderHandler(nil)
	c, rec := newRiderCtx(http.MethodPatch, "/", "owner", uuid.New().String())
	c.SetParamNames("id")
	c.SetParamValues("not-a-uuid")
	if err := h.UpdateRider(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRiderHandler_MarkDelivered_RejectsUnknownRole(t *testing.T) {
	h := handler.NewRiderHandler(nil)
	c, rec := newRiderCtx(http.MethodPost, "/", "viewer", uuid.New().String())
	c.SetParamNames("orderId")
	c.SetParamValues(uuid.New().String())
	if err := h.MarkDelivered(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
