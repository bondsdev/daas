// Package rider contains the domain entities and business rules for delivery
// riders (motorizados). Zero external imports beyond stdlib + uuid.
package rider

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ─── Status ───────────────────────────────────────────────────────────────────

// Status represents whether a rider can currently be assigned new orders.
type Status string

const (
	StatusAvailable   Status = "available"
	StatusUnavailable Status = "unavailable"
)

// ─── Aggregates ───────────────────────────────────────────────────────────────

// Rider is the aggregate root for a tenant's delivery rider.
type Rider struct {
	ID                      uuid.UUID  `json:"id"`
	TenantID                uuid.UUID  `json:"tenant_id"`
	UserID                  uuid.UUID  `json:"user_id"`
	Name                    string     `json:"name"`
	Phone                   string     `json:"phone"`
	RatePerDelivery         float64    `json:"rate_per_delivery"`
	Status                  Status     `json:"status"`
	MaxConcurrentDeliveries int        `json:"max_concurrent_deliveries"`
	LastAssignedAt          *time.Time `json:"last_assigned_at"`
	Active                  bool       `json:"active"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// ─── Request types ────────────────────────────────────────────────────────────

// CreateRiderRequest is the input to register a new rider for a tenant.
type CreateRiderRequest struct {
	Name                    string
	Phone                   string
	RatePerDelivery         float64
	MaxConcurrentDeliveries int
}

// Validate enforces input invariants on a create request.
func (r *CreateRiderRequest) Validate() error {
	if r.Name == "" {
		return ErrRiderNameRequired
	}
	if r.MaxConcurrentDeliveries <= 0 {
		return ErrInvalidMaxConcurrent
	}
	if r.RatePerDelivery < 0 {
		return ErrInvalidRate
	}
	return nil
}

// ─── Assignment eligibility ───────────────────────────────────────────────────

// HasCapacity returns true when the rider is active, available, and has room
// for one more concurrent delivery given its current active order count.
func (r *Rider) HasCapacity(activeOrderCount int) bool {
	return r.Active && r.Status == StatusAvailable && activeOrderCount < r.MaxConcurrentDeliveries
}

// ─── Domain errors ────────────────────────────────────────────────────────────

var (
	ErrRiderNotFound        = errors.New("rider not found")
	ErrRiderNameRequired    = errors.New("rider name is required")
	ErrInvalidMaxConcurrent = errors.New("max concurrent deliveries must be positive")
	ErrInvalidRate          = errors.New("rate per delivery must not be negative")
	ErrRiderUnavailable     = errors.New("rider is unavailable")
	ErrRiderAtCapacity      = errors.New("rider is at maximum concurrent deliveries")
)
