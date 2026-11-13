package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/domain/cashsession"
	"github.com/danzt/daas/api/internal/domain/report"
)

// ReportService generates read-only aggregates from existing data.
type ReportService struct {
	pool *pgxpool.Pool
}

func NewReportService(pool *pgxpool.Pool) *ReportService {
	return &ReportService{pool: pool}
}

// SalesReport returns sales aggregates for the given period.
func (s *ReportService) SalesReport(ctx context.Context, tenantID uuid.UUID, dr report.DateRange) (*report.SalesSummary, error) {
	sum := &report.SalesSummary{
		From:        dr.From,
		To:          dr.To,
		ByDay:       []report.DayRevenue{},
		TopProducts: []report.ProductRevenue{},
	}

	// Total revenue + counts from internal invoices
	var internalRevenue float64
	var internalCount int
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(total),0), COUNT(*)
		 FROM internal_invoices
		 WHERE tenant_id=$1 AND status='issued'
		   AND issued_at >= $2 AND issued_at <= $3`,
		tenantID, dr.From, dr.To,
	).Scan(&internalRevenue, &internalCount)
	if err != nil {
		return nil, err
	}

	// Total revenue + counts from fiscal invoices
	var fiscalRevenue float64
	var fiscalCount int
	err = s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(total),0), COUNT(*)
		 FROM fiscal_invoices
		 WHERE tenant_id=$1 AND status='issued'
		   AND issued_at >= $2 AND issued_at <= $3`,
		tenantID, dr.From, dr.To,
	).Scan(&fiscalRevenue, &fiscalCount)
	if err != nil {
		return nil, err
	}

	sum.TotalRevenue = internalRevenue + fiscalRevenue
	sum.InternalCount = internalCount
	sum.FiscalCount = fiscalCount
	sum.TotalInvoices = internalCount + fiscalCount

	// Revenue by day (union of both invoice types)
	rows, err := s.pool.Query(ctx,
		`SELECT DATE(issued_at) AS day, SUM(total) AS revenue, COUNT(*) AS cnt
		 FROM (
		   SELECT issued_at, total FROM internal_invoices
		   WHERE tenant_id=$1 AND status='issued' AND issued_at>=$2 AND issued_at<=$3
		   UNION ALL
		   SELECT issued_at, total FROM fiscal_invoices
		   WHERE tenant_id=$1 AND status='issued' AND issued_at>=$2 AND issued_at<=$3
		 ) sub
		 GROUP BY day
		 ORDER BY day`,
		tenantID, dr.From, dr.To,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d report.DayRevenue
		var day time.Time
		if err := rows.Scan(&day, &d.Revenue, &d.Count); err != nil {
			return nil, err
		}
		d.Date = day
		sum.ByDay = append(sum.ByDay, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Top 10 products by revenue (internal invoices only — fiscal lines reference same products)
	prows, err := s.pool.Query(ctx,
		`SELECT l.product_id::text, COALESCE(p.name,'Unknown'), SUM(l.quantity), SUM(l.subtotal)
		 FROM internal_invoice_lines l
		 JOIN internal_invoices i ON i.id = l.invoice_id
		 LEFT JOIN products p ON p.id = l.product_id
		 WHERE i.tenant_id=$1 AND i.status='issued'
		   AND i.issued_at>=$2 AND i.issued_at<=$3
		 GROUP BY l.product_id, p.name
		 ORDER BY SUM(l.subtotal) DESC
		 LIMIT 10`,
		tenantID, dr.From, dr.To,
	)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var pr report.ProductRevenue
		if err := prows.Scan(&pr.ProductID, &pr.ProductName, &pr.TotalSold, &pr.Revenue); err != nil {
			return nil, err
		}
		sum.TopProducts = append(sum.TopProducts, pr)
	}
	if err := prows.Err(); err != nil {
		return nil, err
	}

	return sum, nil
}

// InventorySnapshot returns the current stock state for all tenant products.
func (s *ReportService) InventorySnapshot(ctx context.Context, tenantID uuid.UUID) (*report.InventorySnapshot, error) {
	snap := &report.InventorySnapshot{
		GeneratedAt: time.Now().UTC(),
		Items:       []report.StockItem{},
	}

	rows, err := s.pool.Query(ctx,
		`SELECT p.id::text, p.name, COALESCE(p.sku,''),
		        COALESCE(c.name,''), COALESCE(ps.quantity_on_hand,0),
		        p.is_fiscal, p.active
		 FROM products p
		 LEFT JOIN product_stock ps ON ps.product_id = p.id
		 LEFT JOIN product_categories c ON c.id = p.category_id
		 WHERE p.tenant_id = $1
		 ORDER BY p.name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item report.StockItem
		if err := rows.Scan(
			&item.ProductID, &item.ProductName, &item.SKU,
			&item.CategoryName, &item.QuantityOnHand,
			&item.IsFiscal, &item.Active,
		); err != nil {
			return nil, err
		}
		snap.Items = append(snap.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	snap.TotalProducts = len(snap.Items)
	for _, it := range snap.Items {
		if it.QuantityOnHand == 0 {
			snap.OutOfStockCount++
		} else if it.QuantityOnHand < 5 {
			snap.LowStockCount++
		}
	}

	return snap, nil
}

// CashSessionSummary returns the close-out summary for one cash session,
// scoped by tenant so a session from another tenant resolves as not found.
func (s *ReportService) CashSessionSummary(ctx context.Context, tenantID, sessionID uuid.UUID) (*report.CashSessionSummary, error) {
	var rate float64
	err := s.pool.QueryRow(ctx,
		`SELECT exchange_rate FROM cash_sessions WHERE id=$1 AND tenant_id=$2`,
		sessionID, tenantID,
	).Scan(&rate)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cashsession.ErrSessionNotFound
		}
		return nil, fmt.Errorf("load cash session: %w", err)
	}

	sum := &report.CashSessionSummary{
		ByPaymentMethod: []report.PaymentMethodTotal{},
		ProductsSold:    []report.ProductRevenue{},
	}

	// total is stored in the invoice's own currency (internal_invoices.currency,
	// added in migration 000019); convert line-by-line using the session's
	// frozen rate instead of assuming every invoice shares one currency.
	err = s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(CASE WHEN currency='USD' THEN total*$2 ELSE total END),0),
		        COALESCE(SUM(CASE WHEN currency='USD' THEN total ELSE total/$2 END),0),
		        COUNT(*)
		 FROM internal_invoices
		 WHERE cash_session_id=$1 AND status='issued'`,
		sessionID, rate,
	).Scan(&sum.TotalBS, &sum.TotalUSD, &sum.SaleCount)
	if err != nil {
		return nil, err
	}
	// POS counter sales have no customer identity (no customer table for
	// anonymous walk-in buyers), so each sale counts as one customer.
	sum.CustomerCount = sum.SaleCount

	pmRows, err := s.pool.Query(ctx,
		`SELECT COALESCE(payment_method::text,'unknown'), COUNT(*),
		        COALESCE(SUM(CASE WHEN currency='USD' THEN total*$2 ELSE total END),0)
		 FROM internal_invoices
		 WHERE cash_session_id=$1 AND status='issued'
		 GROUP BY payment_method
		 ORDER BY payment_method`,
		sessionID, rate,
	)
	if err != nil {
		return nil, err
	}
	defer pmRows.Close()
	for pmRows.Next() {
		var pm report.PaymentMethodTotal
		if err := pmRows.Scan(&pm.Method, &pm.Count, &pm.Total); err != nil {
			return nil, err
		}
		sum.ByPaymentMethod = append(sum.ByPaymentMethod, pm)
	}
	if err := pmRows.Err(); err != nil {
		return nil, err
	}

	prRows, err := s.pool.Query(ctx,
		`SELECT l.product_id::text, COALESCE(p.name,'Unknown'), SUM(l.quantity), SUM(l.subtotal)
		 FROM internal_invoice_lines l
		 JOIN internal_invoices i ON i.id = l.invoice_id
		 LEFT JOIN products p ON p.id = l.product_id
		 WHERE i.cash_session_id=$1 AND i.status='issued'
		 GROUP BY l.product_id, p.name
		 ORDER BY SUM(l.subtotal) DESC`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer prRows.Close()
	for prRows.Next() {
		var pr report.ProductRevenue
		if err := prRows.Scan(&pr.ProductID, &pr.ProductName, &pr.TotalSold, &pr.Revenue); err != nil {
			return nil, err
		}
		sum.ProductsSold = append(sum.ProductsSold, pr)
	}
	if err := prRows.Err(); err != nil {
		return nil, err
	}

	inv, err := s.InventorySnapshot(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	sum.InventoryRemaining = inv

	return sum, nil
}

// SalesBook returns the libro de ventas: every issued sales document in the
// period, from internal (non-fiscal) and fiscal invoices combined, since the
// accountant needs everything in one place regardless of document type.
func (s *ReportService) SalesBook(ctx context.Context, tenantID uuid.UUID, dr report.DateRange) (*report.SalesBookSummary, error) {
	sum := &report.SalesBookSummary{
		From:    dr.From,
		To:      dr.To,
		Entries: []report.SalesBookEntry{},
	}

	// internal_invoices has no explicit tax_base/tax_amount columns — it's a
	// non-fiscal flat-total "comprobante interno" (VES dropped mandatory
	// electronic invoicing), so tax_base = total and tax_amount = 0 for those.
	// fiscal_invoices carries the real IVA breakdown, used as-is.
	rows, err := s.pool.Query(ctx,
		`SELECT issued_at, 'internal' AS doc_type, correlative,
		        customer_name, customer_id_number, total AS tax_base, 0 AS tax_amount, total
		 FROM internal_invoices
		 WHERE tenant_id=$1 AND status='issued' AND issued_at>=$2 AND issued_at<=$3
		 UNION ALL
		 SELECT issued_at, 'fiscal' AS doc_type, COALESCE(fiscal_number,''),
		        customer_name, customer_id_number, subtotal_base, tax_amount, total
		 FROM fiscal_invoices
		 WHERE tenant_id=$1 AND status='issued' AND issued_at>=$2 AND issued_at<=$3
		 ORDER BY issued_at DESC`,
		tenantID, dr.From, dr.To,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e report.SalesBookEntry
		var d time.Time
		if err := rows.Scan(&d, &e.DocumentType, &e.DocumentNumber,
			&e.CustomerName, &e.CustomerIDNumber, &e.TaxBase, &e.TaxAmount, &e.Total); err != nil {
			return nil, err
		}
		e.Date = d
		sum.TotalTaxBase += e.TaxBase
		sum.TotalTaxDebit += e.TaxAmount
		sum.TotalRevenue += e.Total
		sum.Entries = append(sum.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sum.DocumentCount = len(sum.Entries)

	return sum, nil
}

// PurchaseReport returns purchase order aggregates for the given period.
func (s *ReportService) PurchaseReport(ctx context.Context, tenantID uuid.UUID, dr report.DateRange) (*report.PurchaseSummary, error) {
	sum := &report.PurchaseSummary{
		From:       dr.From,
		To:         dr.To,
		BySupplier: []report.SupplierSpend{},
	}

	// Totals
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(total),0), COUNT(*),
		        COUNT(*) FILTER (WHERE status='received')
		 FROM purchase_orders
		 WHERE tenant_id=$1
		   AND created_at>=$2 AND created_at<=$3
		   AND status != 'cancelled'`,
		tenantID, dr.From, dr.To,
	).Scan(&sum.TotalSpend, &sum.TotalOrders, &sum.ReceivedOrders)
	if err != nil {
		return nil, err
	}

	// By supplier
	rows, err := s.pool.Query(ctx,
		`SELECT po.supplier_id::text, COALESCE(s.name,'Unknown'),
		        SUM(po.total), COUNT(*)
		 FROM purchase_orders po
		 LEFT JOIN suppliers s ON s.id = po.supplier_id
		 WHERE po.tenant_id=$1
		   AND po.created_at>=$2 AND po.created_at<=$3
		   AND po.status != 'cancelled'
		 GROUP BY po.supplier_id, s.name
		 ORDER BY SUM(po.total) DESC`,
		tenantID, dr.From, dr.To,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sp report.SupplierSpend
		if err := rows.Scan(&sp.SupplierID, &sp.SupplierName, &sp.TotalSpend, &sp.OrderCount); err != nil {
			return nil, err
		}
		sum.BySupplier = append(sum.BySupplier, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Libro de compras — facturas de proveedor registradas en el período, con su
	// crédito fiscal (IVA). Se filtra por la fecha de la factura cuando existe.
	sum.Invoices = []report.PurchaseInvoiceEntry{}
	invRows, err := s.pool.Query(ctx,
		`SELECT COALESCE(po.supplier_invoice_date, po.created_at::date) AS d,
		        po.supplier_invoice_number,
		        COALESCE(s.name,'—'), COALESCE(s.rif,''),
		        COALESCE(po.supplier_invoice_tax_base,0),
		        COALESCE(po.supplier_invoice_tax_amount,0)
		 FROM purchase_orders po
		 LEFT JOIN suppliers s ON s.id = po.supplier_id
		 WHERE po.tenant_id=$1
		   AND po.supplier_invoice_number IS NOT NULL
		   AND po.status != 'cancelled'
		   AND COALESCE(po.supplier_invoice_date, po.created_at::date) >= $2::date
		   AND COALESCE(po.supplier_invoice_date, po.created_at::date) <= $3::date
		 ORDER BY d DESC`,
		tenantID, dr.From, dr.To,
	)
	if err != nil {
		return nil, err
	}
	defer invRows.Close()
	for invRows.Next() {
		var e report.PurchaseInvoiceEntry
		var d time.Time
		if err := invRows.Scan(&d, &e.Number, &e.SupplierName, &e.SupplierRIF, &e.TaxBase, &e.TaxAmount); err != nil {
			return nil, err
		}
		e.Date = d
		e.Total = e.TaxBase + e.TaxAmount
		sum.TotalTaxBase += e.TaxBase
		sum.TotalTaxCredit += e.TaxAmount
		sum.Invoices = append(sum.Invoices, e)
	}
	if err := invRows.Err(); err != nil {
		return nil, err
	}

	return sum, nil
}
