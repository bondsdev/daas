package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danzt/daas/api/internal/domain/invoice"
)

// InvoiceListFilters holds optional query parameters for listing invoices.
type InvoiceListFilters struct {
	Status *invoice.Status
	From   *time.Time
	To     *time.Time
	Limit  int
	Offset int
}

// InternalInvoiceService handles all internal invoicing use cases.
type InternalInvoiceService struct {
	pool           *pgxpool.Pool
	cashSessionSvc *CashSessionService // optional — required only by CreatePOSSale
}

// NewInternalInvoiceService creates a service backed by the given pool.
func NewInternalInvoiceService(pool *pgxpool.Pool) *InternalInvoiceService {
	return &InternalInvoiceService{pool: pool}
}

// SetCashSessionService injects the CashSessionService used by CreatePOSSale
// to require an open cash session. Call once at wiring time.
func (s *InternalInvoiceService) SetCashSessionService(svc *CashSessionService) {
	s.cashSessionSvc = svc
}

// Create validates and persists a new draft invoice with its lines.
// All products MUST be non-fiscal; fiscal products are rejected with ErrFiscalProductForbidden.
// createdBySupabaseUID is the Supabase sub claim; resolved internally to tenant_users.id.
func (s *InternalInvoiceService) Create(ctx context.Context, tenantID uuid.UUID, createdBySupabaseUID string, req invoice.CreateRequest) (*invoice.InternalInvoice, error) {
	createdBy, err := s.resolveUserID(ctx, tenantID, createdBySupabaseUID)
	if err != nil {
		return nil, err
	}
	// Resolve product details to validate is_fiscal flag.
	for i, line := range req.Lines {
		var isFiscal bool
		var internalPrice float64
		err := s.pool.QueryRow(ctx,
			`SELECT is_fiscal, COALESCE(internal_price, 0)
			 FROM products
			 WHERE id = $1 AND tenant_id = $2 AND active = true`,
			line.ProductID, tenantID,
		).Scan(&isFiscal, &internalPrice)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("product %s not found or inactive", line.ProductID)
			}
			return nil, fmt.Errorf("lookup product: %w", err)
		}
		if isFiscal {
			return nil, invoice.ErrFiscalProductForbidden
		}
		// Use internal_price as unit_price if caller didn't specify
		if req.Lines[i].UnitPrice == 0 {
			req.Lines[i].UnitPrice = internalPrice
		}
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Insert invoice header.
	inv := &invoice.InternalInvoice{
		ID:               uuid.New(),
		TenantID:         tenantID,
		CustomerName:     req.CustomerName,
		CustomerIDType:   req.CustomerIDType,
		CustomerIDNumber: req.CustomerIDNumber,
		Status:           invoice.StatusDraft,
		Notes:            req.Notes,
		CreatedBy:        createdBy,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if inv.CustomerIDType == "" {
		inv.CustomerIDType = invoice.CustomerIDTypeAnonymous
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO internal_invoices
			(id, tenant_id, customer_name, customer_id_type, customer_id_number,
			 status, subtotal, total, notes, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,'draft',0,0,$6,$7,NOW(),NOW())`,
		inv.ID, inv.TenantID, inv.CustomerName, inv.CustomerIDType,
		inv.CustomerIDNumber, inv.Notes, inv.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("insert invoice: %w", err)
	}

	// Insert lines and accumulate subtotal.
	var subtotal float64
	for i, l := range req.Lines {
		lineSubtotal := l.Quantity * l.UnitPrice
		subtotal += lineSubtotal

		lineID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO internal_invoice_lines
				(id, invoice_id, product_id, description, quantity, unit_price, subtotal, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			lineID, inv.ID, l.ProductID, l.Description, l.Quantity, l.UnitPrice, lineSubtotal, i,
		)
		if err != nil {
			return nil, fmt.Errorf("insert line: %w", err)
		}
		inv.Lines = append(inv.Lines, invoice.InvoiceLine{
			ID:          lineID,
			InvoiceID:   inv.ID,
			ProductID:   l.ProductID,
			Description: l.Description,
			Quantity:    l.Quantity,
			UnitPrice:   l.UnitPrice,
			Subtotal:    lineSubtotal,
			SortOrder:   i,
		})
	}

	// Update totals on the header.
	inv.Subtotal = subtotal
	inv.Total = subtotal
	_, err = tx.Exec(ctx,
		`UPDATE internal_invoices SET subtotal=$1, total=$2 WHERE id=$3`,
		inv.Subtotal, inv.Total, inv.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("update totals: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return inv, nil
}

// Issue confirms a draft invoice: generates the correlative atomically and
// registers inventory exit movements for all lines.
// issuedBySupabaseUID is the Supabase sub claim; resolved internally to tenant_users.id.
func (s *InternalInvoiceService) Issue(ctx context.Context, tenantID, invoiceID uuid.UUID, issuedBySupabaseUID string) (*invoice.InternalInvoice, error) {
	issuedBy, err := s.resolveUserID(ctx, tenantID, issuedBySupabaseUID)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Load invoice and lock it.
	inv, err := s.loadForUpdate(ctx, tx, tenantID, invoiceID)
	if err != nil {
		return nil, err
	}
	if inv.Status == invoice.StatusIssued {
		return nil, invoice.ErrInvoiceAlreadyIssued
	}
	if inv.Status == invoice.StatusCancelled {
		return nil, invoice.ErrInvoiceAlreadyCancelled
	}

	// Generate correlative atomically.
	year := time.Now().Year()
	seq, err := s.nextCorrelative(ctx, tx, tenantID, year)
	if err != nil {
		return nil, fmt.Errorf("generate correlative: %w", err)
	}
	correlative := invoice.FormatCorrelative(year, seq)
	now := time.Now()

	// Update status to issued.
	_, err = tx.Exec(ctx, `
		UPDATE internal_invoices
		SET status='issued', correlative=$1, issued_at=$2, updated_at=NOW()
		WHERE id=$3 AND tenant_id=$4`,
		correlative, now, invoiceID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("update invoice status: %w", err)
	}

	// Register inventory exit for each line.
	for _, line := range inv.Lines {
		err = s.deductStock(ctx, tx, tenantID, line.ProductID, line.Quantity, invoiceID, issuedBy)
		if err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	inv.Status = invoice.StatusIssued
	inv.Correlative = correlative
	inv.IssuedAt = &now
	return inv, nil
}

// Cancel transitions an issued (or draft) invoice to cancelled and reverses
// any inventory movements that were created at issue time.
// cancelledBySupabaseUID is the Supabase sub claim; resolved internally to tenant_users.id.
func (s *InternalInvoiceService) Cancel(ctx context.Context, tenantID, invoiceID uuid.UUID, cancelledBySupabaseUID string, notes string) (*invoice.InternalInvoice, error) {
	cancelledBy, err := s.resolveUserID(ctx, tenantID, cancelledBySupabaseUID)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	inv, err := s.loadForUpdate(ctx, tx, tenantID, invoiceID)
	if err != nil {
		return nil, err
	}
	if inv.Status == invoice.StatusCancelled {
		return nil, invoice.ErrInvoiceAlreadyCancelled
	}

	// Reverse inventory movements only if the invoice was issued.
	if inv.Status == invoice.StatusIssued {
		for _, line := range inv.Lines {
			err = s.restoreStock(ctx, tx, tenantID, line.ProductID, line.Quantity, invoiceID, cancelledBy, notes)
			if err != nil {
				return nil, err
			}
		}
	}

	cancelNote := fmt.Sprintf("Cancelled invoice %s", inv.Correlative)
	if notes != "" {
		cancelNote = notes
	}
	_, err = tx.Exec(ctx, `
		UPDATE internal_invoices
		SET status='cancelled', notes=$1, updated_at=NOW()
		WHERE id=$2 AND tenant_id=$3`,
		cancelNote, invoiceID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("cancel invoice: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	inv.Status = invoice.StatusCancelled
	return inv, nil
}

// GetByID returns a single invoice with its lines.
func (s *InternalInvoiceService) GetByID(ctx context.Context, tenantID, invoiceID uuid.UUID) (*invoice.InternalInvoice, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, COALESCE(correlative,''), COALESCE(customer_name,''),
		       customer_id_type, COALESCE(customer_id_number,''), status,
		       subtotal, total, COALESCE(notes,''), issued_at, created_by, created_at, updated_at
		FROM internal_invoices
		WHERE id=$1 AND tenant_id=$2`,
		invoiceID, tenantID,
	)
	inv, err := scanInvoice(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, invoice.ErrInvoiceNotFound
		}
		return nil, fmt.Errorf("get invoice: %w", err)
	}

	lines, err := s.loadLines(ctx, inv.ID)
	if err != nil {
		return nil, err
	}
	inv.Lines = lines
	return inv, nil
}

// List returns invoices for the tenant with optional filters.
func (s *InternalInvoiceService) List(ctx context.Context, tenantID uuid.UUID, f InvoiceListFilters) ([]*invoice.InternalInvoice, error) {
	if f.Limit == 0 {
		f.Limit = 50
	}

	query := `
		SELECT id, tenant_id, COALESCE(correlative,''), COALESCE(customer_name,''),
		       customer_id_type, COALESCE(customer_id_number,''), status,
		       subtotal, total, COALESCE(notes,''), issued_at, created_by, created_at, updated_at
		FROM internal_invoices
		WHERE tenant_id=$1`
	args := []any{tenantID}
	n := 2

	if f.Status != nil {
		query += fmt.Sprintf(" AND status=$%d", n)
		args = append(args, *f.Status)
		n++
	}
	if f.From != nil {
		query += fmt.Sprintf(" AND created_at>=$%d", n)
		args = append(args, *f.From)
		n++
	}
	if f.To != nil {
		query += fmt.Sprintf(" AND created_at<=$%d", n)
		args = append(args, *f.To)
		n++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", n, n+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()

	var result []*invoice.InternalInvoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan invoice: %w", err)
		}
		result = append(result, inv)
	}
	return result, rows.Err()
}

// ─── private helpers ─────────────────────────────────────────────────────────

// loadForUpdate loads an invoice with its lines inside a transaction, locking
// the row to prevent concurrent modifications.
func (s *InternalInvoiceService) loadForUpdate(ctx context.Context, tx pgx.Tx, tenantID, invoiceID uuid.UUID) (*invoice.InternalInvoice, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, tenant_id, COALESCE(correlative,''), COALESCE(customer_name,''),
		       customer_id_type, COALESCE(customer_id_number,''), status,
		       subtotal, total, COALESCE(notes,''), issued_at, created_by, created_at, updated_at
		FROM internal_invoices
		WHERE id=$1 AND tenant_id=$2
		FOR UPDATE`,
		invoiceID, tenantID,
	)
	inv, err := scanInvoice(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, invoice.ErrInvoiceNotFound
		}
		return nil, fmt.Errorf("load invoice: %w", err)
	}

	// Load lines within the same transaction.
	lRows, err := tx.Query(ctx, `
		SELECT id, invoice_id, product_id, description, quantity, unit_price, subtotal, sort_order
		FROM internal_invoice_lines
		WHERE invoice_id=$1
		ORDER BY sort_order`,
		inv.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("load lines: %w", err)
	}
	defer lRows.Close()
	for lRows.Next() {
		var l invoice.InvoiceLine
		if err := lRows.Scan(&l.ID, &l.InvoiceID, &l.ProductID, &l.Description,
			&l.Quantity, &l.UnitPrice, &l.Subtotal, &l.SortOrder); err != nil {
			return nil, fmt.Errorf("scan line: %w", err)
		}
		inv.Lines = append(inv.Lines, l)
	}
	return inv, lRows.Err()
}

// loadLines fetches lines for a given invoice (read-only, no transaction).
func (s *InternalInvoiceService) loadLines(ctx context.Context, invoiceID uuid.UUID) ([]invoice.InvoiceLine, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, invoice_id, product_id, description, quantity, unit_price, subtotal, sort_order
		FROM internal_invoice_lines
		WHERE invoice_id=$1
		ORDER BY sort_order`,
		invoiceID,
	)
	if err != nil {
		return nil, fmt.Errorf("load lines: %w", err)
	}
	defer rows.Close()
	var lines []invoice.InvoiceLine
	for rows.Next() {
		var l invoice.InvoiceLine
		if err := rows.Scan(&l.ID, &l.InvoiceID, &l.ProductID, &l.Description,
			&l.Quantity, &l.UnitPrice, &l.Subtotal, &l.SortOrder); err != nil {
			return nil, fmt.Errorf("scan line: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

// nextCorrelative atomically increments and returns the next sequence number
// for the given tenant and year, using SELECT ... FOR UPDATE.
func (s *InternalInvoiceService) nextCorrelative(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, year int) (int, error) {
	// Upsert the counter row, then lock and increment.
	_, err := tx.Exec(ctx, `
		INSERT INTO invoice_correlative_seq (tenant_id, year, last_seq)
		VALUES ($1, $2, 0)
		ON CONFLICT (tenant_id, year) DO NOTHING`,
		tenantID, year,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert correlative row: %w", err)
	}

	var next int
	err = tx.QueryRow(ctx, `
		UPDATE invoice_correlative_seq
		SET last_seq = last_seq + 1
		WHERE tenant_id=$1 AND year=$2
		RETURNING last_seq`,
		tenantID, year,
	).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("increment correlative: %w", err)
	}
	return next, nil
}

// deductStock registers an inventory exit movement and decrements product_stock.
func (s *InternalInvoiceService) deductStock(ctx context.Context, tx pgx.Tx, tenantID, productID uuid.UUID, qty float64, invoiceID, createdBy uuid.UUID) error {
	// Lock stock row.
	var onHand float64
	err := tx.QueryRow(ctx,
		`SELECT quantity_on_hand FROM product_stock WHERE product_id=$1 AND tenant_id=$2 FOR UPDATE`,
		productID, tenantID,
	).Scan(&onHand)
	if err != nil {
		return fmt.Errorf("lock stock: %w", err)
	}
	if onHand < qty {
		return invoice.ErrInsufficientStock
	}

	_, err = tx.Exec(ctx,
		`UPDATE product_stock SET quantity_on_hand=quantity_on_hand-$1, last_updated_at=NOW()
		 WHERE product_id=$2 AND tenant_id=$3`,
		qty, productID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("deduct stock: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO inventory_movements
			(id, tenant_id, product_id, type, quantity, reference_type, reference_id, notes, created_by, created_at)
		VALUES ($1,$2,$3,'exit',$4,'sale',$5,'Factura interna',$6,NOW())`,
		uuid.New(), tenantID, productID, qty, invoiceID, createdBy,
	)
	return err
}

// restoreStock reverses an inventory exit by creating an adjustment entry.
func (s *InternalInvoiceService) restoreStock(ctx context.Context, tx pgx.Tx, tenantID, productID uuid.UUID, qty float64, invoiceID, cancelledBy uuid.UUID, reason string) error {
	if reason == "" {
		reason = "Reversa por cancelación de factura interna"
	}
	_, err := tx.Exec(ctx,
		`UPDATE product_stock SET quantity_on_hand=quantity_on_hand+$1, last_updated_at=NOW()
		 WHERE product_id=$2 AND tenant_id=$3`,
		qty, productID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("restore stock: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO inventory_movements
			(id, tenant_id, product_id, type, quantity, reference_type, reference_id, notes, created_by, created_at)
		VALUES ($1,$2,$3,'adjustment',$4,'sale',$5,$6,$7,NOW())`,
		uuid.New(), tenantID, productID, qty, invoiceID, reason, cancelledBy,
	)
	return err
}

// CreateAndIssueFromShopOrder generates and immediately issues an internal
// invoice for all non-fiscal lines of a shop order. Designed for the
// auto-invoice flow triggered on MarkPaid — it does NOT require a Supabase
// UID and instead looks up the tenant owner user directly.
//
// If the shop order has ONLY fiscal products, it skips silently and returns nil.
// The caller (ShopOrderService.MarkPaid) is responsible for calling this
// asynchronously / best-effort — a failure here must NOT block the state transition.
func (s *InternalInvoiceService) CreateAndIssueFromShopOrder(
	ctx context.Context,
	tenantID uuid.UUID,
	orderID uuid.UUID,
	customerName string,
	customerNote string,
) (*invoice.InternalInvoice, error) {
	// Resolve owner user ID for audit columns.
	ownerID, err := s.ownerUserID(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("auto-invoice: resolve owner: %w", err)
	}

	// Load shop_order_lines for this order (only non-fiscal products).
	type shopLine struct {
		productID   uuid.UUID
		description string
		quantity    float64
		unitPrice   float64
	}
	rows, err := s.pool.Query(ctx, `
		SELECT sol.product_id, sol.name, sol.quantity, sol.unit_price
		FROM shop_order_lines sol
		JOIN products p ON p.id = sol.product_id
		WHERE sol.order_id = $1 AND p.is_fiscal = false`,
		orderID,
	)
	if err != nil {
		return nil, fmt.Errorf("auto-invoice: load lines: %w", err)
	}
	defer rows.Close()

	var lines []shopLine
	for rows.Next() {
		var l shopLine
		if err := rows.Scan(&l.productID, &l.description, &l.quantity, &l.unitPrice); err != nil {
			return nil, fmt.Errorf("auto-invoice: scan line: %w", err)
		}
		lines = append(lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auto-invoice: rows error: %w", err)
	}

	// Nothing to invoice — all products were fiscal.
	if len(lines) == 0 {
		return nil, nil
	}

	// Build CreateRequest.
	req := invoice.CreateRequest{
		CustomerName:     customerName,
		CustomerIDType:   invoice.CustomerIDTypeAnonymous,
		CustomerIDNumber: "",
		Notes:            customerNote,
	}
	for _, l := range lines {
		req.Lines = append(req.Lines, invoice.CreateLineRequest{
			ProductID:   l.productID,
			Description: l.description,
			Quantity:    l.quantity,
			UnitPrice:   l.unitPrice,
			IsFiscal:    false,
		})
	}

	// Create draft directly using internal user ID (bypass supabase UID resolution).
	inv, err := s.createWithUserID(ctx, tenantID, ownerID, req)
	if err != nil {
		return nil, fmt.Errorf("auto-invoice: create draft: %w", err)
	}

	// Issue immediately.
	// skipStockDeduction=true because checkout already decremented stock atomically.
	issued, err := s.issueWithUserID(ctx, tenantID, inv.ID, ownerID, true)
	if err != nil {
		return nil, fmt.Errorf("auto-invoice: issue: %w", err)
	}
	return issued, nil
}

// ownerUserID returns the tenant_users.id of the tenant owner (role='owner').
func (s *InternalInvoiceService) ownerUserID(ctx context.Context, tenantID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM tenant_users WHERE tenant_id = $1 AND role = 'owner' LIMIT 1`,
		tenantID,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("owner user: %w", err)
	}
	return id, nil
}

// createWithUserID is the same as Create but accepts an already-resolved tenant_users.id.
func (s *InternalInvoiceService) createWithUserID(ctx context.Context, tenantID, createdBy uuid.UUID, req invoice.CreateRequest) (*invoice.InternalInvoice, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	inv := &invoice.InternalInvoice{
		ID:               uuid.New(),
		TenantID:         tenantID,
		CustomerName:     req.CustomerName,
		CustomerIDType:   req.CustomerIDType,
		CustomerIDNumber: req.CustomerIDNumber,
		Status:           invoice.StatusDraft,
		Notes:            req.Notes,
		CreatedBy:        createdBy,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if inv.CustomerIDType == "" {
		inv.CustomerIDType = invoice.CustomerIDTypeAnonymous
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO internal_invoices
			(id, tenant_id, customer_name, customer_id_type, customer_id_number,
			 status, subtotal, total, notes, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,'draft',0,0,$6,$7,NOW(),NOW())`,
		inv.ID, inv.TenantID, inv.CustomerName, inv.CustomerIDType,
		inv.CustomerIDNumber, inv.Notes, inv.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("insert invoice: %w", err)
	}

	var subtotal float64
	for i, l := range req.Lines {
		lineSubtotal := l.Quantity * l.UnitPrice
		subtotal += lineSubtotal
		lineID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO internal_invoice_lines
				(id, invoice_id, product_id, description, quantity, unit_price, subtotal, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			lineID, inv.ID, l.ProductID, l.Description, l.Quantity, l.UnitPrice, lineSubtotal, i,
		)
		if err != nil {
			return nil, fmt.Errorf("insert line: %w", err)
		}
		inv.Lines = append(inv.Lines, invoice.InvoiceLine{
			ID:        lineID,
			InvoiceID: inv.ID,
			ProductID: l.ProductID,
			Quantity:  l.Quantity,
			UnitPrice: l.UnitPrice,
			Subtotal:  lineSubtotal,
			SortOrder: i,
		})
	}

	inv.Subtotal = subtotal
	inv.Total = subtotal
	_, err = tx.Exec(ctx, `UPDATE internal_invoices SET subtotal=$1, total=$2 WHERE id=$3`, inv.Subtotal, inv.Total, inv.ID)
	if err != nil {
		return nil, fmt.Errorf("update totals: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return inv, nil
}

// issueWithUserID is the same as Issue but accepts an already-resolved tenant_users.id.
// When skipStockDeduction is true, inventory exit movements are NOT created — use this
// for shop orders where stock was already decremented atomically at checkout time.
func (s *InternalInvoiceService) issueWithUserID(ctx context.Context, tenantID, invoiceID, issuedBy uuid.UUID, skipStockDeduction bool) (*invoice.InternalInvoice, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	inv, err := s.loadForUpdate(ctx, tx, tenantID, invoiceID)
	if err != nil {
		return nil, err
	}
	if inv.Status == invoice.StatusIssued {
		return nil, invoice.ErrInvoiceAlreadyIssued
	}

	year := time.Now().Year()
	seq, err := s.nextCorrelative(ctx, tx, tenantID, year)
	if err != nil {
		return nil, fmt.Errorf("generate correlative: %w", err)
	}
	correlative := invoice.FormatCorrelative(year, seq)
	now := time.Now()

	_, err = tx.Exec(ctx, `
		UPDATE internal_invoices
		SET status='issued', correlative=$1, issued_at=$2, updated_at=NOW()
		WHERE id=$3 AND tenant_id=$4`,
		correlative, now, invoiceID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("update invoice status: %w", err)
	}

	if !skipStockDeduction {
		for _, line := range inv.Lines {
			if err := s.deductStock(ctx, tx, tenantID, line.ProductID, line.Quantity, invoiceID, issuedBy); err != nil {
				return nil, err
			}
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	inv.Status = invoice.StatusIssued
	inv.Correlative = correlative
	inv.IssuedAt = &now
	return inv, nil
}

// resolveUserID translates a Supabase sub (UUID string) to the internal
// tenant_users.id used in audit fields. Matches the pattern used by InventoryService.
func (s *InternalInvoiceService) resolveUserID(ctx context.Context, tenantID uuid.UUID, supabaseUID string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM tenant_users WHERE tenant_id = $1 AND supabase_uid = $2`,
		tenantID, supabaseUID,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve user: %w", err)
	}
	return id, nil
}

// CreatePOSSale creates and immediately issues an internal invoice for a POS
// counter sale: requires an open cash session, resolves unit prices from the
// product catalog (same rule as Create — non-fiscal only, internal_price),
// and validates + deducts stock atomically in the same transaction as the
// invoice insert, so a failed sale never partially deducts stock.
func (s *InternalInvoiceService) CreatePOSSale(ctx context.Context, tenantID uuid.UUID, createdBySupabaseUID string, req invoice.PosSaleRequest) (*invoice.InternalInvoice, error) {
	if s.cashSessionSvc == nil {
		return nil, fmt.Errorf("pos sale: cash session service not configured")
	}
	if len(req.Lines) == 0 {
		return nil, invoice.ErrEmptyInvoice
	}

	createdBy, err := s.resolveUserID(ctx, tenantID, createdBySupabaseUID)
	if err != nil {
		return nil, err
	}

	sess, err := s.cashSessionSvc.GetOpen(ctx, tenantID)
	if err != nil {
		return nil, err // propagates cashsession.ErrNoOpenSession
	}

	currency := req.Currency
	if currency == "" {
		currency = "VES"
	}

	type resolvedLine struct {
		productID   uuid.UUID
		description string
		quantity    float64
		unitPrice   float64
	}
	lines := make([]resolvedLine, 0, len(req.Lines))
	for _, l := range req.Lines {
		if l.Quantity <= 0 {
			return nil, fmt.Errorf("quantity must be positive")
		}
		var isFiscal bool
		var name string
		var internalPrice float64
		err := s.pool.QueryRow(ctx,
			`SELECT is_fiscal, name, COALESCE(internal_price, 0)
			 FROM products
			 WHERE id = $1 AND tenant_id = $2 AND active = true`,
			l.ProductID, tenantID,
		).Scan(&isFiscal, &name, &internalPrice)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("product %s not found or inactive", l.ProductID)
			}
			return nil, fmt.Errorf("lookup product: %w", err)
		}
		if isFiscal {
			return nil, invoice.ErrFiscalProductForbidden
		}
		lines = append(lines, resolvedLine{productID: l.ProductID, description: name, quantity: l.Quantity, unitPrice: internalPrice})
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	year := time.Now().Year()
	seq, err := s.nextCorrelative(ctx, tx, tenantID, year)
	if err != nil {
		return nil, fmt.Errorf("generate correlative: %w", err)
	}
	correlative := invoice.FormatCorrelative(year, seq)
	now := time.Now()

	inv := &invoice.InternalInvoice{
		ID:             uuid.New(),
		TenantID:       tenantID,
		Correlative:    correlative,
		CustomerIDType: invoice.CustomerIDTypeAnonymous,
		Status:         invoice.StatusIssued,
		CashSessionID:  &sess.ID,
		PaymentMethod:  req.PaymentMethod,
		Currency:       currency,
		IssuedAt:       &now,
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO internal_invoices
			(id, tenant_id, correlative, customer_id_type, status, subtotal, total,
			 created_by, created_at, updated_at, issued_at, cash_session_id, payment_method, currency)
		VALUES ($1,$2,$3,$4,'issued',0,0,$5,$6,$6,$6,$7,$8,$9)`,
		inv.ID, inv.TenantID, inv.Correlative, inv.CustomerIDType,
		inv.CreatedBy, now, inv.CashSessionID, inv.PaymentMethod, inv.Currency,
	)
	if err != nil {
		return nil, fmt.Errorf("insert invoice: %w", err)
	}

	var subtotal float64
	for i, l := range lines {
		lineSubtotal := l.quantity * l.unitPrice
		subtotal += lineSubtotal

		lineID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO internal_invoice_lines
				(id, invoice_id, product_id, description, quantity, unit_price, subtotal, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			lineID, inv.ID, l.productID, l.description, l.quantity, l.unitPrice, lineSubtotal, i,
		)
		if err != nil {
			return nil, fmt.Errorf("insert line: %w", err)
		}
		inv.Lines = append(inv.Lines, invoice.InvoiceLine{
			ID:          lineID,
			InvoiceID:   inv.ID,
			ProductID:   l.productID,
			Description: l.description,
			Quantity:    l.quantity,
			UnitPrice:   l.unitPrice,
			Subtotal:    lineSubtotal,
			SortOrder:   i,
		})

		if err := s.deductStock(ctx, tx, tenantID, l.productID, l.quantity, inv.ID, createdBy); err != nil {
			return nil, err
		}
	}

	inv.Subtotal = subtotal
	inv.Total = subtotal
	_, err = tx.Exec(ctx,
		`UPDATE internal_invoices SET subtotal=$1, total=$2 WHERE id=$3`,
		inv.Subtotal, inv.Total, inv.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("update totals: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return inv, nil
}

// scanInvoice reads one invoice row (without lines).
func scanInvoice(row interface {
	Scan(dest ...any) error
}) (*invoice.InternalInvoice, error) {
	var inv invoice.InternalInvoice
	return &inv, row.Scan(
		&inv.ID, &inv.TenantID, &inv.Correlative, &inv.CustomerName,
		&inv.CustomerIDType, &inv.CustomerIDNumber, &inv.Status,
		&inv.Subtotal, &inv.Total, &inv.Notes, &inv.IssuedAt,
		&inv.CreatedBy, &inv.CreatedAt, &inv.UpdatedAt,
	)
}
