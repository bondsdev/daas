// Package invoice contains the domain entities and business rules for
// internal (non-fiscal) invoice management. Zero imports from Echo, pgx,
// or any external framework.
package invoice

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Status represents the lifecycle state of an internal invoice.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusIssued    Status = "issued"
	StatusCancelled Status = "cancelled"
)

// CustomerIDType classifies the customer identification document.
type CustomerIDType string

const (
	CustomerIDTypeCedula    CustomerIDType = "cedula"
	CustomerIDTypeRIF       CustomerIDType = "rif"
	CustomerIDTypePassport  CustomerIDType = "passport"
	CustomerIDTypeAnonymous CustomerIDType = "anonymous"
)

// InternalInvoice is the aggregate root for a non-fiscal sale document.
type InternalInvoice struct {
	ID               uuid.UUID      `json:"id"`
	TenantID         uuid.UUID      `json:"tenant_id"`
	Correlative      string         `json:"correlative"` // empty while status=draft
	CustomerName     string         `json:"customer_name"`
	CustomerIDType   CustomerIDType `json:"customer_id_type"`
	CustomerIDNumber string         `json:"customer_id_number"`
	Status           Status         `json:"status"`
	Subtotal         float64        `json:"subtotal"`
	Total            float64        `json:"total"`
	Notes            string         `json:"notes"`
	IssuedAt         *time.Time     `json:"issued_at"`
	CreatedBy        uuid.UUID      `json:"created_by"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	Lines            []InvoiceLine  `json:"lines"`

	// POS counter-sale fields — nil/empty for invoices created outside the POS flow.
	CashSessionID *uuid.UUID    `json:"cash_session_id,omitempty"`
	PaymentMethod PaymentMethod `json:"payment_method,omitempty"`
	Currency      string        `json:"currency,omitempty"`
}

// PaymentMethod is how a POS counter sale was paid, matching the
// payment_method_type Postgres enum.
type PaymentMethod string

const (
	PaymentMethodCash          PaymentMethod = "cash"
	PaymentMethodCard          PaymentMethod = "card"
	PaymentMethodMobilePayment PaymentMethod = "mobile_payment"
)

// PosSaleLine is one product line in a POS quick sale. Unit price is not
// supplied by the caller — it is resolved server-side from the product's
// current internal price, same as a regular internal invoice line.
type PosSaleLine struct {
	ProductID uuid.UUID
	Quantity  float64
}

// PosSaleRequest is the input for a POS counter sale: product + quantity
// lines, a payment method, and the currency the sale was charged in.
type PosSaleRequest struct {
	Lines         []PosSaleLine
	PaymentMethod PaymentMethod
	Currency      string // defaults to "VES" when empty
}

// InvoiceLine is a single product line inside an invoice.
type InvoiceLine struct {
	ID          uuid.UUID `json:"id"`
	InvoiceID   uuid.UUID `json:"invoice_id"`
	ProductID   uuid.UUID `json:"product_id"`
	Description string    `json:"description"`
	Quantity    float64   `json:"quantity"`
	UnitPrice   float64   `json:"unit_price"`
	Subtotal    float64   `json:"subtotal"`
	SortOrder   int       `json:"sort_order"`
}

// CreateLineRequest is the input for one invoice line.
type CreateLineRequest struct {
	ProductID   uuid.UUID
	Description string
	Quantity    float64
	UnitPrice   float64
	IsFiscal    bool // populated from product lookup, validated by service
}

// CreateRequest is the full invoice creation payload.
type CreateRequest struct {
	CustomerName     string
	CustomerIDType   CustomerIDType
	CustomerIDNumber string
	Notes            string
	Lines            []CreateLineRequest
}

// Validate checks the create request for domain-level constraints.
func (r *CreateRequest) Validate() error {
	if len(r.Lines) == 0 {
		return ErrEmptyInvoice
	}
	for _, l := range r.Lines {
		if l.IsFiscal {
			return ErrFiscalProductForbidden
		}
		if l.Quantity <= 0 {
			return fmt.Errorf("quantity must be positive")
		}
		if l.UnitPrice <= 0 {
			return fmt.Errorf("unit_price must be positive")
		}
	}
	return nil
}

// FormatCorrelative builds the display correlative string.
func FormatCorrelative(year, seq int) string {
	return fmt.Sprintf("INT-%d-%05d", year, seq)
}

// Domain errors
var (
	ErrInvoiceNotFound         = errors.New("invoice not found")
	ErrInvoiceAlreadyIssued    = errors.New("invoice is already issued")
	ErrInvoiceAlreadyCancelled = errors.New("invoice is already cancelled")
	ErrFiscalProductForbidden  = errors.New("fiscal products cannot be added to an internal invoice")
	ErrEmptyInvoice            = errors.New("invoice must have at least one line")
	ErrInvoiceNotDraft         = errors.New("invoice is not in draft status")
	ErrInsufficientStock       = errors.New("insufficient stock for one or more products")
)
