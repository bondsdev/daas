// Package product contains the domain entities and business rules for product
// management. This package has ZERO imports from Echo, pgx, or any external
// framework — it is pure Go.
package product

import (
	"time"

	"github.com/google/uuid"
)

// Product is the root aggregate for a product in the catalog.
// Each product belongs to exactly one tenant and carries fiscal classification
// that determines how it is priced and invoiced.
type Product struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Name          string
	SKU           string
	Barcode       string
	Unit          string
	Description   string
	CategoryID    *uuid.UUID
	IsFiscal      bool
	FiscalPrice   *float64
	InternalPrice *float64
	// PriceUSD is the optional USD source-of-truth price. When set,
	// InternalPrice is a derived value, recomputed automatically whenever a
	// new BS/USD exchange rate is recorded (see ProductService and
	// CashSessionService.Open). When nil, InternalPrice behaves exactly as a
	// manually-entered BS price always has.
	PriceUSD  *float64
	TaxRate   *float64
	Active    bool
	ImageURL  string // optional — publicly resolvable URL; empty when no image uploaded
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Validate enforces the fiscal/internal invariants of a product.
//
// Rules:
//   - Fiscal products MUST have a positive fiscal_price AND a tax_rate.
//   - Internal products MUST have a positive internal_price.
//   - Internal products MUST NOT have a tax_rate (only fiscal products are taxed).
func (p *Product) Validate() error {
	if p.IsFiscal {
		if p.FiscalPrice == nil || *p.FiscalPrice <= 0 {
			return ErrFiscalPriceMissing
		}
		if p.TaxRate == nil {
			return ErrTaxRateMissing
		}
	} else {
		if p.InternalPrice == nil || *p.InternalPrice <= 0 {
			return ErrInternalPriceMissing
		}
		if p.TaxRate != nil {
			return ErrTaxRateOnInternal
		}
	}
	return nil
}

// Category is a grouping entity for products. Categories may be nested via
// parent_id to support a two-level hierarchy (e.g., Bebidas > Refrescos).
type Category struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Name      string
	ParentID  *uuid.UUID
	CreatedAt time.Time
}
