//go:build integration

// Integration tests for MeHandler.UpdateFeatures. They spin up a real
// PostgreSQL instance via testcontainers and exercise the handler against it.
// Build with: go test -tags integration ./internal/adapter/http/handler/...
package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/danzt/daas/api/internal/adapter/http/handler"
	"github.com/danzt/daas/api/internal/adapter/http/middleware"
)

// doUpdateFeatures builds and runs a PATCH /api/v1/tenants/me/features
// request against the given handler for tenantID, with body as the JSON
// request payload.
func doUpdateFeatures(t *testing.T, h *handler.MeHandler, tenantID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/me/features", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(middleware.ContextKeyTenantID), tenantID)
	if err := h.UpdateFeatures(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return rec
}

func TestMeHandler_UpdateFeatures_StorefrontOnly(t *testing.T) {
	pool := setupTestDB(t)
	tenantID, _ := seedTenant(t, pool, "Storefront Only Tenant")
	activateRLS(t, pool, tenantID)

	h := handler.NewMeHandler(pool)
	rec := doUpdateFeatures(t, h, tenantID.String(), map[string]any{"storefront": true})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Features        map[string]bool `json:"features"`
		DeliveryEnabled bool            `json:"delivery_enabled"`
	}
	decodeBody(t, rec, &resp)

	if !resp.Features["storefront"] {
		t.Errorf("expected features.storefront = true, got %v", resp.Features)
	}
	if resp.DeliveryEnabled {
		t.Errorf("expected delivery_enabled = false (untouched), got true")
	}
}

func TestMeHandler_UpdateFeatures_DeliveryOnly(t *testing.T) {
	pool := setupTestDB(t)
	tenantID, _ := seedTenant(t, pool, "Delivery Only Tenant")
	activateRLS(t, pool, tenantID)

	h := handler.NewMeHandler(pool)
	rec := doUpdateFeatures(t, h, tenantID.String(), map[string]any{"delivery": true})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Features        map[string]bool `json:"features"`
		DeliveryEnabled bool            `json:"delivery_enabled"`
	}
	decodeBody(t, rec, &resp)

	if !resp.DeliveryEnabled {
		t.Errorf("expected delivery_enabled = true, got false")
	}
	if resp.Features["storefront"] {
		t.Errorf("expected features.storefront = false (untouched), got true")
	}
}

func TestMeHandler_UpdateFeatures_Both(t *testing.T) {
	pool := setupTestDB(t)
	tenantID, _ := seedTenant(t, pool, "Both Features Tenant")
	activateRLS(t, pool, tenantID)

	h := handler.NewMeHandler(pool)
	rec := doUpdateFeatures(t, h, tenantID.String(), map[string]any{"storefront": true, "delivery": true})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Features        map[string]bool `json:"features"`
		DeliveryEnabled bool            `json:"delivery_enabled"`
	}
	decodeBody(t, rec, &resp)

	if !resp.Features["storefront"] {
		t.Errorf("expected features.storefront = true, got %v", resp.Features)
	}
	if !resp.DeliveryEnabled {
		t.Errorf("expected delivery_enabled = true, got false")
	}

	// Toggle both back off in a second call to confirm the UPSERT path
	// (not just the first INSERT) works correctly.
	rec = doUpdateFeatures(t, h, tenantID.String(), map[string]any{"storefront": false, "delivery": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	decodeBody(t, rec, &resp)
	if resp.Features["storefront"] {
		t.Errorf("expected features.storefront = false after toggle-off, got true")
	}
	if resp.DeliveryEnabled {
		t.Errorf("expected delivery_enabled = false after toggle-off, got true")
	}
}

func TestMeHandler_UpdateFeatures_NeitherFieldPresent_Returns422(t *testing.T) {
	pool := setupTestDB(t)
	tenantID, _ := seedTenant(t, pool, "Validation Error Tenant")
	activateRLS(t, pool, tenantID)

	h := handler.NewMeHandler(pool)
	rec := doUpdateFeatures(t, h, tenantID.String(), map[string]any{})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
