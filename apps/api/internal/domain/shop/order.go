package shop

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ─── Status ───────────────────────────────────────────────────────────────────

type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusPaid      OrderStatus = "paid"
	OrderStatusFulfilled OrderStatus = "fulfilled"
	OrderStatusDelivered OrderStatus = "delivered"
	OrderStatusCancelled OrderStatus = "cancelled"
)

// ─── Aggregates ───────────────────────────────────────────────────────────────

// ShopOrder is the root aggregate for a public (B2C) storefront order.
// No tenant account is required — customers are identified by email + access token.
type ShopOrder struct {
	ID              uuid.UUID   `json:"id"`
	TenantID        uuid.UUID   `json:"tenant_id"`
	CustomerName    string      `json:"customer_name"`
	CustomerEmail   string      `json:"customer_email"`
	CustomerPhone   string      `json:"customer_phone"`
	ShippingAddress string      `json:"shipping_address"`
	ShippingCity    string      `json:"shipping_city"`
	ShippingNotes   string      `json:"shipping_notes"`
	Subtotal        float64     `json:"subtotal"`
	ShippingCost    float64     `json:"shipping_cost"`
	Total           float64     `json:"total"`
	Status          OrderStatus `json:"status"`
	Notes           string      `json:"notes"`
	// AccessToken is only included on creation response (omitempty on public GET).
	AccessToken string      `json:"access_token,omitempty"`
	PaidAt      *time.Time  `json:"paid_at"`
	FulfilledAt *time.Time  `json:"fulfilled_at"`
	DeliveredAt *time.Time  `json:"delivered_at"`
	CancelledAt *time.Time  `json:"cancelled_at"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	Lines       []OrderLine `json:"lines"`

	// Payment proof — populated once the customer uploads a receipt.
	PaymentProofURL        string     `json:"payment_proof_url,omitempty"`
	PaymentProofFilename   string     `json:"payment_proof_filename,omitempty"`
	PaymentProofUploadedAt *time.Time `json:"payment_proof_uploaded_at,omitempty"`
	PaymentMethodID        *uuid.UUID `json:"payment_method_id,omitempty"`
	PaymentReference       string     `json:"payment_reference,omitempty"`

	// Delivery (motorizados) — populated once a rider is assigned.
	RiderID          *uuid.UUID `json:"rider_id,omitempty"`
	RiderFee         *float64   `json:"rider_fee,omitempty"`
	RiderAssignedAt  *time.Time `json:"rider_assigned_at,omitempty"`
	RiderDeliveredAt *time.Time `json:"rider_delivered_at,omitempty"`
	RiderPaidAt      *time.Time `json:"rider_paid_at,omitempty"`
	IsUrgent         bool       `json:"is_urgent"`
	UrgentFee        *float64   `json:"urgent_fee,omitempty"`
}

// OrderLine is a single product line inside a shop order.
// Product info is snapshotted at checkout time — price/name are never updated retroactively.
type OrderLine struct {
	ID        uuid.UUID `json:"id"`
	OrderID   uuid.UUID `json:"order_id"`
	ProductID uuid.UUID `json:"product_id"`
	Name      string    `json:"name"`
	UnitPrice float64   `json:"unit_price"`
	Quantity  int       `json:"quantity"`
	Subtotal  float64   `json:"subtotal"`
	IsFiscal  bool      `json:"is_fiscal"`
	SortOrder int       `json:"sort_order"`
}

// ─── Request types ────────────────────────────────────────────────────────────

// CreateOrderRequest is the input from the storefront checkout form.
type CreateOrderRequest struct {
	CustomerName    string
	CustomerEmail   string
	CustomerPhone   string
	ShippingAddress string
	ShippingCity    string
	ShippingNotes   string
	Notes           string
	Lines           []CreateOrderLineRequest
}

// CreateOrderLineRequest is a single line in a checkout request.
// Stock and price are resolved by the service — only product + quantity come from the client.
type CreateOrderLineRequest struct {
	ProductID uuid.UUID
	Quantity  int
}

// Validate enforces input invariants. Stock and price come from the service.
func (r *CreateOrderRequest) Validate() error {
	if r.CustomerName == "" {
		return ErrCustomerNameRequired
	}
	if r.CustomerEmail == "" {
		return ErrCustomerEmailRequired
	}
	if r.ShippingAddress == "" {
		return ErrShippingAddressRequired
	}
	if len(r.Lines) == 0 {
		return ErrEmptyOrder
	}
	for _, l := range r.Lines {
		if l.Quantity <= 0 {
			return ErrInvalidQuantity
		}
	}
	return nil
}

// ─── State machine ────────────────────────────────────────────────────────────

// CanTransitionTo returns nil if the transition is allowed, an error otherwise.
// Terminal states (delivered, cancelled) block all further transitions.
func (o *ShopOrder) CanTransitionTo(next OrderStatus) error {
	transitions := map[OrderStatus][]OrderStatus{
		OrderStatusPending:   {OrderStatusPaid, OrderStatusCancelled},
		OrderStatusPaid:      {OrderStatusFulfilled, OrderStatusCancelled},
		OrderStatusFulfilled: {OrderStatusDelivered},
		OrderStatusDelivered: {},
		OrderStatusCancelled: {},
	}
	allowed, ok := transitions[o.Status]
	if !ok {
		return ErrInvalidStatus
	}
	for _, s := range allowed {
		if s == next {
			return nil
		}
	}
	return ErrInvalidTransition
}

// ─── Domain errors ────────────────────────────────────────────────────────────

var (
	ErrCustomerNameRequired    = errors.New("customer name is required")
	ErrCustomerEmailRequired   = errors.New("customer email is required")
	ErrShippingAddressRequired = errors.New("shipping address is required")
	ErrEmptyOrder              = errors.New("order must have at least one line")
	ErrInvalidQuantity         = errors.New("quantity must be positive")
	ErrInvalidStatus           = errors.New("invalid order status")
	ErrInvalidTransition       = errors.New("invalid status transition")
	ErrShopOrderNotFound       = errors.New("shop order not found")
	ErrInsufficientStock       = errors.New("insufficient stock")
	ErrProductNotPurchasable   = errors.New("product is not available for purchase")
	ErrInvalidAccessToken      = errors.New("invalid access token")

	// Payment proof errors
	ErrPaymentProofRequired      = errors.New("payment proof file required")
	ErrPaymentProofAlreadyExists = errors.New("payment proof already submitted")
	ErrPaymentProofInvalidType   = errors.New("unsupported file type")
	ErrPaymentProofTooLarge      = errors.New("file too large")

	// Delivery (motorizados) errors
	ErrRiderAlreadyAssigned        = errors.New("a rider is already assigned to this order")
	ErrNoRiderAssigned             = errors.New("no rider assigned to this order")
	ErrOrderNotEligibleForDelivery = errors.New("order is not eligible for delivery assignment")
)

// ─── Payment proof validation ─────────────────────────────────────────────────

// AllowedPaymentProofMimeTypes is the whitelist for uploaded payment proofs.
var AllowedPaymentProofMimeTypes = []string{
	"image/jpeg",
	"image/png",
	"image/webp",
	"application/pdf",
}

// MaxPaymentProofBytes is 5 MB — the hard upper limit for proof uploads.
const MaxPaymentProofBytes int64 = 5 * 1024 * 1024

// ValidatePaymentProofMeta returns nil when contentType and sizeBytes are acceptable.
// Returns ErrPaymentProofRequired if size is zero, ErrPaymentProofTooLarge if over
// MaxPaymentProofBytes, and ErrPaymentProofInvalidType for any unsupported MIME type.
func ValidatePaymentProofMeta(contentType string, sizeBytes int64) error {
	if sizeBytes <= 0 {
		return ErrPaymentProofRequired
	}
	if sizeBytes > MaxPaymentProofBytes {
		return ErrPaymentProofTooLarge
	}
	for _, allowed := range AllowedPaymentProofMimeTypes {
		if contentType == allowed {
			return nil
		}
	}
	return ErrPaymentProofInvalidType
}
