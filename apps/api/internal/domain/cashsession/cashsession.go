// Package cashsession contains the domain entities and business rules for
// cash register (caja) sessions and their frozen exchange rate. Zero external imports.
package cashsession

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status represents the lifecycle of a cash session.
type Status string

const (
	StatusOpen   Status = "open"
	StatusClosed Status = "closed"
)

// RateSource records whether the exchange rate was auto-fetched or entered manually.
type RateSource string

const (
	RateSourceAuto   RateSource = "auto"
	RateSourceManual RateSource = "manual"
)

// CashSession is the aggregate root for a cash register shift.
type CashSession struct {
	ID                 uuid.UUID  `json:"id"`
	TenantID           uuid.UUID  `json:"tenant_id"`
	Status             Status     `json:"status"`
	ExchangeRate       float64    `json:"exchange_rate"`
	ExchangeRateSource RateSource `json:"exchange_rate_source"`
	OpeningNotes       string     `json:"opening_notes"`
	ClosingNotes       string     `json:"closing_notes"`
	OpenedBy           uuid.UUID  `json:"opened_by"`
	OpenedAt           time.Time  `json:"opened_at"`
	ClosedBy           *uuid.UUID `json:"closed_by"`
	ClosedAt           *time.Time `json:"closed_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// OpenRequest is the input to open a new cash session.
type OpenRequest struct {
	ManualRate *float64
	Notes      string
}

// Validate checks domain-level constraints on an open request.
// A manual rate, when provided, must be positive.
func (r *OpenRequest) Validate() error {
	if r.ManualRate != nil && *r.ManualRate <= 0 {
		return ErrInvalidRate
	}
	return nil
}

// CloseRequest is the input to close the currently open cash session.
type CloseRequest struct {
	Notes string
}

// Domain errors
var (
	ErrSessionAlreadyOpen = errors.New("a cash session is already open for this tenant")
	ErrNoOpenSession      = errors.New("no open cash session for this tenant")
	ErrSessionNotFound    = errors.New("cash session not found")
	ErrInvalidRate        = errors.New("exchange rate must be positive")
	ErrRateRequired       = errors.New("manual exchange rate is required when auto-fetch fails")
)
