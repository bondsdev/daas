// Package supplier contains the domain entities and business rules for
// supplier management and purchase orders. Zero external imports.
package supplier

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ─── Supplier ────────────────────────────────────────────────────────────────

// Supplier is an external vendor from which the tenant purchases products.
type Supplier struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	Name        string    `json:"name"`
	RIF         string    `json:"rif"`
	ContactName string    `json:"contact_name"`
	Email       string    `json:"email"`
	Phone       string    `json:"phone"`
	Address     string    `json:"address"`
	Notes       string    `json:"notes"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// PortalToken es el token del link público del proveedor. Vacío = sin link.
	PortalToken string `json:"portal_token"`
}

// CatalogItem is one product in a supplier's own catalog (loaded from the
// supplier portal). Separate from the tenant's products — se importa selectivo.
type CatalogItem struct {
	ID          uuid.UUID `json:"id"`
	SupplierID  uuid.UUID `json:"supplier_id"`
	Name        string    `json:"name"`
	SKU         string    `json:"sku"`
	Cost        float64   `json:"cost"`
	Unit        string    `json:"unit"`
	Barcode     string    `json:"barcode"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateCatalogItemRequest is the input for adding a catalog item from the portal.
type CreateCatalogItemRequest struct {
	Name        string
	SKU         string
	Cost        float64
	Unit        string
	Barcode     string
	Description string
}

func (r *CreateCatalogItemRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return ErrCatalogNameRequired
	}
	if r.Cost < 0 {
		return fmt.Errorf("cost must be non-negative")
	}
	return nil
}

// CreateSupplierRequest is the input for creating a supplier.
type CreateSupplierRequest struct {
	Name        string
	RIF         string
	ContactName string
	Email       string
	Phone       string
	Address     string
	Notes       string
}

// UpdateSupplierRequest carries the fields that can be changed.
type UpdateSupplierRequest struct {
	Name        *string
	RIF         *string
	ContactName *string
	Email       *string
	Phone       *string
	Address     *string
	Notes       *string
	Active      *bool
}

func (r *CreateSupplierRequest) Validate() error {
	if r.Name == "" {
		return ErrSupplierNameRequired
	}
	return nil
}

// ─── Purchase Order ───────────────────────────────────────────────────────────

// POStatus represents the lifecycle of a purchase order.
type POStatus string

const (
	POStatusDraft     POStatus = "draft"
	POStatusOrdered   POStatus = "ordered"
	POStatusReceived  POStatus = "received"
	POStatusCancelled POStatus = "cancelled"
)

// PurchaseOrder is an aggregate root representing a purchase from a supplier.
type PurchaseOrder struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	SupplierID uuid.UUID  `json:"supplier_id"`
	Status     POStatus   `json:"status"`
	Notes      string     `json:"notes"`
	Currency   string     `json:"currency"`
	Total      float64    `json:"total"`
	OrderedAt  *time.Time `json:"ordered_at"`
	ReceivedAt *time.Time `json:"received_at"`
	CreatedBy  uuid.UUID  `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	Lines      []POLine   `json:"lines"`
	Supplier   *Supplier  `json:"supplier,omitempty"` // optional, populated on demand

	// Factura de compra del proveedor (Fase 1). Se registra al recibir la
	// mercadería, para poder declarar la compra (crédito fiscal IVA).
	SupplierInvoiceNumber    string     `json:"supplier_invoice_number"`
	SupplierInvoiceDate      *time.Time `json:"supplier_invoice_date"`
	SupplierInvoiceTaxBase   float64    `json:"supplier_invoice_tax_base"`
	SupplierInvoiceTaxAmount float64    `json:"supplier_invoice_tax_amount"`
}

// SetPurchaseInvoiceRequest registers the supplier's invoice on a purchase order.
type SetPurchaseInvoiceRequest struct {
	InvoiceNumber string
	InvoiceDate   *time.Time
	TaxBase       float64
	TaxAmount     float64
}

func (r *SetPurchaseInvoiceRequest) Validate() error {
	if strings.TrimSpace(r.InvoiceNumber) == "" {
		return ErrInvoiceNumberRequired
	}
	if r.TaxBase < 0 || r.TaxAmount < 0 {
		return fmt.Errorf("tax_base and tax_amount must be non-negative")
	}
	return nil
}

// POLine is a single product line in a purchase order.
type POLine struct {
	ID              uuid.UUID `json:"id"`
	POID            uuid.UUID `json:"po_id"`
	ProductID       uuid.UUID `json:"product_id"`
	Description     string    `json:"description"`
	QuantityOrdered float64   `json:"quantity_ordered"`
	UnitCost        float64   `json:"unit_cost"`
	Subtotal        float64   `json:"subtotal"`
	SortOrder       int       `json:"sort_order"`
	Unit            string    `json:"unit"`
}

// CreatePOLineRequest is the input for one purchase order line.
type CreatePOLineRequest struct {
	ProductID   uuid.UUID
	Description string
	Quantity    float64
	UnitCost    float64
	// Unit is optional — empty means "use the product's own unit".
	Unit string
}

// CreatePORequest is the full purchase order creation payload.
type CreatePORequest struct {
	SupplierID uuid.UUID
	Notes      string
	Currency   string
	Lines      []CreatePOLineRequest
}

func (r *CreatePORequest) Validate() error {
	if r.SupplierID == uuid.Nil {
		return fmt.Errorf("supplier_id is required")
	}
	if len(r.Lines) == 0 {
		return ErrEmptyPO
	}
	for _, l := range r.Lines {
		if l.Quantity <= 0 {
			return fmt.Errorf("quantity must be positive")
		}
		if l.UnitCost <= 0 {
			return fmt.Errorf("unit_cost must be positive")
		}
	}
	if r.Currency == "" {
		r.Currency = "VES"
	} else if r.Currency != "VES" && r.Currency != "USD" {
		return fmt.Errorf("currency must be VES or USD")
	}
	return nil
}

// UpdatePORequest is the full purchase order update payload. It mirrors
// CreatePORequest and follows the same validation rules.
type UpdatePORequest struct {
	SupplierID uuid.UUID
	Notes      string
	Currency   string
	Lines      []CreatePOLineRequest
}

func (r *UpdatePORequest) Validate() error {
	if r.SupplierID == uuid.Nil {
		return fmt.Errorf("supplier_id is required")
	}
	if len(r.Lines) == 0 {
		return ErrEmptyPO
	}
	for _, l := range r.Lines {
		if l.Quantity <= 0 {
			return fmt.Errorf("quantity must be positive")
		}
		if l.UnitCost <= 0 {
			return fmt.Errorf("unit_cost must be positive")
		}
	}
	if r.Currency == "" {
		r.Currency = "VES"
	} else if r.Currency != "VES" && r.Currency != "USD" {
		return fmt.Errorf("currency must be VES or USD")
	}
	return nil
}

// ─── Domain errors ────────────────────────────────────────────────────────────

var (
	ErrSupplierNotFound      = errors.New("supplier not found")
	ErrSupplierNameRequired  = errors.New("supplier name is required")
	ErrPONotFound            = errors.New("purchase order not found")
	ErrEmptyPO               = errors.New("purchase order must have at least one line")
	ErrPOAlreadyOrdered      = errors.New("purchase order is already ordered")
	ErrPOAlreadyReceived     = errors.New("purchase order is already received")
	ErrPOAlreadyCancelled    = errors.New("purchase order is already cancelled")
	ErrPONotOrdered          = errors.New("purchase order is not in ordered status")
	ErrPONotDraft            = errors.New("purchase order is not in draft status")
	ErrInvoiceNumberRequired = errors.New("supplier invoice number is required")
	ErrCatalogNameRequired   = errors.New("catalog item name is required")
	ErrCatalogItemNotFound   = errors.New("catalog item not found")
	ErrCatalogItemNoCost     = errors.New("el producto del catálogo no tiene costo; asignalo antes de importar")
	ErrProductSKUExists      = errors.New("ya existe un producto con ese SKU")
)
