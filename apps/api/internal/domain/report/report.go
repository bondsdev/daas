// Package report contains read-only aggregate types used by the reporting
// service. No mutations, no external imports.
package report

import "time"

// DateRange represents an inclusive date interval for filtering reports.
type DateRange struct {
	From time.Time
	To   time.Time
}

// ─── Sales Report ─────────────────────────────────────────────────────────────

// SalesSummary is the top-level response for the sales report.
type SalesSummary struct {
	From          time.Time        `json:"from"`
	To            time.Time        `json:"to"`
	TotalRevenue  float64          `json:"total_revenue"`
	TotalInvoices int              `json:"total_invoices"`
	InternalCount int              `json:"internal_count"`
	FiscalCount   int              `json:"fiscal_count"`
	ByDay         []DayRevenue     `json:"by_day"`
	TopProducts   []ProductRevenue `json:"top_products"`
}

// DayRevenue is revenue aggregated by calendar day.
type DayRevenue struct {
	Date    time.Time `json:"date"`
	Revenue float64   `json:"revenue"`
	Count   int       `json:"count"`
}

// ProductRevenue is revenue aggregated by product.
type ProductRevenue struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	TotalSold   float64 `json:"total_sold"`
	Revenue     float64 `json:"revenue"`
}

// ─── Inventory Report ─────────────────────────────────────────────────────────

// InventorySnapshot is the current stock state for all products.
type InventorySnapshot struct {
	GeneratedAt     time.Time   `json:"generated_at"`
	TotalProducts   int         `json:"total_products"`
	LowStockCount   int         `json:"low_stock_count"`
	OutOfStockCount int         `json:"out_of_stock_count"`
	Items           []StockItem `json:"items"`
}

// StockItem is one product's stock summary.
type StockItem struct {
	ProductID      string  `json:"product_id"`
	ProductName    string  `json:"product_name"`
	SKU            string  `json:"sku"`
	CategoryName   string  `json:"category_name"`
	QuantityOnHand float64 `json:"quantity_on_hand"`
	IsFiscal       bool    `json:"is_fiscal"`
	Active         bool    `json:"active"`
}

// ─── Cash Session Summary ─────────────────────────────────────────────────────

// CashSessionSummary is the cash-register close summary for one session:
// totals in both currencies, breakdown by payment method, products sold, and
// the remaining inventory.
type CashSessionSummary struct {
	TotalBS            float64              `json:"total_bs"`
	TotalUSD           float64              `json:"total_usd"`
	SaleCount          int                  `json:"sale_count"`
	CustomerCount      int                  `json:"customer_count"`
	ByPaymentMethod    []PaymentMethodTotal `json:"by_payment_method"`
	ProductsSold       []ProductRevenue     `json:"products_sold"`
	InventoryRemaining *InventorySnapshot   `json:"inventory_remaining"`
}

// PaymentMethodTotal is revenue aggregated by payment method within a cash session.
type PaymentMethodTotal struct {
	Method string  `json:"method"`
	Total  float64 `json:"total"`
	Count  int     `json:"count"`
}

// ─── Purchases Report ─────────────────────────────────────────────────────────

// PurchaseSummary aggregates purchase order data over a period.
type PurchaseSummary struct {
	From           time.Time       `json:"from"`
	To             time.Time       `json:"to"`
	TotalSpend     float64         `json:"total_spend"`
	TotalOrders    int             `json:"total_orders"`
	ReceivedOrders int             `json:"received_orders"`
	BySupplier     []SupplierSpend `json:"by_supplier"`

	// Libro de compras: facturas de proveedor registradas en el período, con
	// su crédito fiscal (IVA). Base para declarar las compras ante el fisco.
	TotalTaxBase   float64                `json:"total_tax_base"`
	TotalTaxCredit float64                `json:"total_tax_credit"`
	Invoices       []PurchaseInvoiceEntry `json:"invoices"`
}

// SupplierSpend is spending aggregated per supplier.
type SupplierSpend struct {
	SupplierID   string  `json:"supplier_id"`
	SupplierName string  `json:"supplier_name"`
	TotalSpend   float64 `json:"total_spend"`
	OrderCount   int     `json:"order_count"`
}

// PurchaseInvoiceEntry is one line of the libro de compras — a supplier invoice
// registered on a purchase order.
type PurchaseInvoiceEntry struct {
	Date         time.Time `json:"date"`
	Number       string    `json:"number"`
	SupplierName string    `json:"supplier_name"`
	SupplierRIF  string    `json:"supplier_rif"`
	TaxBase      float64   `json:"tax_base"`
	TaxAmount    float64   `json:"tax_amount"`
	Total        float64   `json:"total"`
}

// ─── Sales Book ────────────────────────────────────────────────────────────

// SalesBookSummary is the libro de ventas: every issued sales document
// (internal + fiscal) in a period, as the accountant needs to see all of it.
type SalesBookSummary struct {
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	TotalTaxBase float64   `json:"total_tax_base"`
	// Named TaxDebit, not TaxCredit like the purchase side: IVA collected on a
	// sale is owed to the fisco (a debit), while IVA paid on a purchase is a
	// credit against that debit — they are opposite sides of the same ledger.
	TotalTaxDebit float64          `json:"total_tax_debit"`
	TotalRevenue  float64          `json:"total_revenue"`
	DocumentCount int              `json:"document_count"`
	Entries       []SalesBookEntry `json:"entries"`
}

// SalesBookEntry is one line of the libro de ventas, from either an internal
// (non-fiscal) invoice or a fiscal invoice.
type SalesBookEntry struct {
	Date             time.Time `json:"date"`
	DocumentType     string    `json:"document_type"` // "internal" | "fiscal"
	DocumentNumber   string    `json:"document_number"`
	CustomerName     string    `json:"customer_name"`
	CustomerIDNumber string    `json:"customer_id_number"`
	TaxBase          float64   `json:"tax_base"`
	TaxAmount        float64   `json:"tax_amount"`
	Total            float64   `json:"total"`
}
