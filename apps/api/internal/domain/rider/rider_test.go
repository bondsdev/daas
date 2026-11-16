package rider_test

import (
	"testing"
	"time"

	"github.com/danzt/daas/api/internal/domain/rider"
)

// ─── CreateRiderRequest.Validate ─────────────────────────────────────────────

func TestCreateRiderRequest_Validate_HappyPath(t *testing.T) {
	req := rider.CreateRiderRequest{
		Name:                    "Juan Pérez",
		Phone:                   "0414-1234567",
		RatePerDelivery:         2.5,
		MaxConcurrentDeliveries: 3,
	}
	if err := req.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

func TestCreateRiderRequest_Validate_MissingName(t *testing.T) {
	req := rider.CreateRiderRequest{
		Name:                    "",
		MaxConcurrentDeliveries: 1,
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate() expected error for missing name, got nil")
	}
}

func TestCreateRiderRequest_Validate_ZeroMaxConcurrent(t *testing.T) {
	req := rider.CreateRiderRequest{
		Name:                    "Juan Pérez",
		MaxConcurrentDeliveries: 0,
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate() expected error for zero max concurrent deliveries, got nil")
	}
}

func TestCreateRiderRequest_Validate_NegativeMaxConcurrent(t *testing.T) {
	req := rider.CreateRiderRequest{
		Name:                    "Juan Pérez",
		MaxConcurrentDeliveries: -1,
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate() expected error for negative max concurrent deliveries, got nil")
	}
}

func TestCreateRiderRequest_Validate_NegativeRate(t *testing.T) {
	req := rider.CreateRiderRequest{
		Name:                    "Juan Pérez",
		RatePerDelivery:         -1,
		MaxConcurrentDeliveries: 1,
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate() expected error for negative rate, got nil")
	}
}

// ─── Rider.HasCapacity ────────────────────────────────────────────────────────

func TestRider_HasCapacity_UnderCapacity(t *testing.T) {
	r := &rider.Rider{
		Active:                  true,
		Status:                  rider.StatusAvailable,
		MaxConcurrentDeliveries: 3,
	}
	if !r.HasCapacity(1) {
		t.Error("HasCapacity(1) expected true, got false")
	}
}

func TestRider_HasCapacity_ZeroActiveOrders(t *testing.T) {
	r := &rider.Rider{
		Active:                  true,
		Status:                  rider.StatusAvailable,
		MaxConcurrentDeliveries: 1,
	}
	if !r.HasCapacity(0) {
		t.Error("HasCapacity(0) expected true, got false")
	}
}

func TestRider_HasCapacity_AtCapacity(t *testing.T) {
	r := &rider.Rider{
		Active:                  true,
		Status:                  rider.StatusAvailable,
		MaxConcurrentDeliveries: 2,
	}
	if r.HasCapacity(2) {
		t.Error("HasCapacity(2) expected false when activeOrderCount == max, got true")
	}
}

func TestRider_HasCapacity_Unavailable(t *testing.T) {
	r := &rider.Rider{
		Active:                  true,
		Status:                  rider.StatusUnavailable,
		MaxConcurrentDeliveries: 3,
	}
	if r.HasCapacity(0) {
		t.Error("HasCapacity() expected false for unavailable rider, got true")
	}
}

func TestRider_HasCapacity_Inactive(t *testing.T) {
	r := &rider.Rider{
		Active:                  false,
		Status:                  rider.StatusAvailable,
		MaxConcurrentDeliveries: 3,
	}
	if r.HasCapacity(0) {
		t.Error("HasCapacity() expected false for inactive rider, got true")
	}
}

func TestRider_HasCapacity_WithLastAssignedAt(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	r := &rider.Rider{
		Active:                  true,
		Status:                  rider.StatusAvailable,
		MaxConcurrentDeliveries: 1,
		LastAssignedAt:          &past,
	}
	if !r.HasCapacity(0) {
		t.Error("HasCapacity(0) expected true regardless of LastAssignedAt, got false")
	}
}
