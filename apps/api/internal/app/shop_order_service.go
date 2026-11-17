package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/danzt/daas/api/internal/adapter/storage"
	"github.com/danzt/daas/api/internal/domain/shop"
)

// ShopOrderService handles the public storefront order lifecycle.
// Stock is decremented atomically at checkout via Serializable transactions.
type ShopOrderService struct {
	pool             *pgxpool.Pool
	notifier         *ShopOrderNotifier      // optional — when nil, lifecycle emails are skipped
	storage          storage.Storage         // optional — when nil, proof uploads fail gracefully
	invoiceSvc       *InternalInvoiceService // optional — when nil, auto-invoice on MarkPaid is skipped
	fiscalInvoiceSvc *FiscalInvoiceService   // optional — when nil, fiscal auto-invoice is skipped
}

// NewShopOrderService creates a ShopOrderService backed by the given pool.
func NewShopOrderService(pool *pgxpool.Pool) *ShopOrderService {
	return &ShopOrderService{pool: pool}
}

// SetNotifier wires the lifecycle email notifier. Safe to call once at startup.
// When nil (or never called), all order state transitions skip the email send.
func (s *ShopOrderService) SetNotifier(n *ShopOrderNotifier) {
	s.notifier = n
}

// SetStorage injects the storage adapter for proof uploads.
// Must be called before UploadPaymentProof is used.
func (s *ShopOrderService) SetStorage(st storage.Storage) {
	s.storage = st
}

// SetInvoiceService wires the internal invoice service for auto-invoice on MarkPaid.
// Optional — when nil, no internal invoice is generated automatically.
func (s *ShopOrderService) SetInvoiceService(svc *InternalInvoiceService) {
	s.invoiceSvc = svc
}

// SetFiscalInvoiceService wires the fiscal invoice service for auto-fiscal-invoice on MarkPaid.
// Optional — when nil, no fiscal invoice is generated for fiscal product lines.
func (s *ShopOrderService) SetFiscalInvoiceService(svc *FiscalInvoiceService) {
	s.fiscalInvoiceSvc = svc
}

// ═══ Public checkout ═════════════════════════════════════════════════════════

// Checkout creates a shop order from the storefront, decrements stock atomically,
// and returns the order with its one-time access token.
//
// The transaction uses Serializable isolation to protect against concurrent checkouts
// of the last available unit.
func (s *ShopOrderService) Checkout(ctx context.Context, tenantID uuid.UUID, req shop.CreateOrderRequest) (*shop.ShopOrder, error) {
	// 1. Validate request invariants.
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// 2. Open Serializable transaction for atomic stock check + decrement.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, fmt.Errorf("begin checkout tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 3. For each line: lock product row, check active + stock, snapshot price/name.
	lines := make([]shop.OrderLine, 0, len(req.Lines))
	var subtotal float64

	for i, l := range req.Lines {
		var name string
		var internalPrice, fiscalPrice *float64
		var stockOnHand float64
		var isActive, isFiscal bool

		err := tx.QueryRow(ctx, `
			SELECT p.name,
			       p.internal_price,
			       p.fiscal_price,
			       COALESCE(ps.quantity_on_hand, 0),
			       p.active,
			       p.is_fiscal
			FROM products p
			LEFT JOIN product_stock ps ON ps.product_id = p.id
			WHERE p.id = $1 AND p.tenant_id = $2
			FOR UPDATE OF p`,
			l.ProductID, tenantID,
		).Scan(&name, &internalPrice, &fiscalPrice, &stockOnHand, &isActive, &isFiscal)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, shop.ErrProductNotPurchasable
			}
			return nil, fmt.Errorf("lock product %s: %w", l.ProductID, err)
		}

		if !isActive {
			return nil, shop.ErrProductNotPurchasable
		}
		if int(stockOnHand) < l.Quantity {
			return nil, shop.ErrInsufficientStock
		}

		// Resolve price: fiscal products use fiscal_price; non-fiscal use internal_price.
		var unitPrice float64
		if isFiscal && fiscalPrice != nil {
			unitPrice = *fiscalPrice
		} else if internalPrice != nil {
			unitPrice = *internalPrice
		}

		lineSubtotal := unitPrice * float64(l.Quantity)
		subtotal += lineSubtotal

		lines = append(lines, shop.OrderLine{
			ProductID: l.ProductID,
			Name:      name,
			UnitPrice: unitPrice,
			Quantity:  l.Quantity,
			Subtotal:  lineSubtotal,
			IsFiscal:  isFiscal,
			SortOrder: i,
		})

		// Decrement stock.
		if _, err := tx.Exec(ctx,
			`UPDATE product_stock
			 SET quantity_on_hand = quantity_on_hand - $1, last_updated_at = NOW()
			 WHERE product_id = $2`,
			l.Quantity, l.ProductID,
		); err != nil {
			return nil, fmt.Errorf("decrement stock for product %s: %w", l.ProductID, err)
		}
	}

	// 4. Compute shipping: flat $5 if subtotal < $100, else free.
	shippingCost := 0.0
	if subtotal < 100 {
		shippingCost = 5.0
	}
	total := subtotal + shippingCost

	// 5. Generate access token (crypto/rand hex 32 bytes → 64 hex chars).
	accessToken := generateAccessToken()

	// 6. Insert order.
	orderID := uuid.New()
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO shop_orders (
			id, tenant_id,
			customer_name, customer_email, customer_phone,
			shipping_address, shipping_city, shipping_notes,
			delivery_lat, delivery_lng,
			subtotal, shipping_cost, total,
			status, notes, access_token,
			created_at, updated_at
		) VALUES (
			$1, $2,
			$3, $4, $5,
			$6, $7, $8,
			$9, $10,
			$11, $12, $13,
			'pending', $14, $15,
			$16, $16
		)`,
		orderID, tenantID,
		req.CustomerName, req.CustomerEmail, req.CustomerPhone,
		req.ShippingAddress, req.ShippingCity, req.ShippingNotes,
		req.DeliveryLat, req.DeliveryLng,
		subtotal, shippingCost, total,
		req.Notes, accessToken,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert shop order: %w", err)
	}

	// 7. Insert order lines.
	for i := range lines {
		lines[i].ID = uuid.New()
		lines[i].OrderID = orderID
		_, err = tx.Exec(ctx, `
			INSERT INTO shop_order_lines (id, order_id, product_id, name, unit_price, quantity, subtotal, is_fiscal, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			lines[i].ID, orderID, lines[i].ProductID,
			lines[i].Name, lines[i].UnitPrice, lines[i].Quantity,
			lines[i].Subtotal, lines[i].IsFiscal, lines[i].SortOrder,
		)
		if err != nil {
			return nil, fmt.Errorf("insert shop order line: %w", err)
		}
	}

	// 8. Commit.
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit checkout: %w", err)
	}

	return &shop.ShopOrder{
		ID:              orderID,
		TenantID:        tenantID,
		CustomerName:    req.CustomerName,
		CustomerEmail:   req.CustomerEmail,
		CustomerPhone:   req.CustomerPhone,
		ShippingAddress: req.ShippingAddress,
		DeliveryLat:     req.DeliveryLat,
		DeliveryLng:     req.DeliveryLng,
		ShippingCity:    req.ShippingCity,
		ShippingNotes:   req.ShippingNotes,
		Subtotal:        subtotal,
		ShippingCost:    shippingCost,
		Total:           total,
		Status:          shop.OrderStatusPending,
		Notes:           req.Notes,
		AccessToken:     accessToken,
		CreatedAt:       now,
		UpdatedAt:       now,
		Lines:           lines,
	}, nil
}

// ═══ Public order tracking ════════════════════════════════════════════════════

// GetPublicOrder returns an order for the customer, validating the access token.
// Returns ErrShopOrderNotFound if the order does not exist or the token does not match.
// The returned ShopOrder has AccessToken cleared (not exposed on tracking view).
func (s *ShopOrderService) GetPublicOrder(ctx context.Context, tenantID, orderID uuid.UUID, accessToken string) (*shop.ShopOrder, error) {
	order, err := s.getOrderByIDWithToken(ctx, tenantID, orderID, accessToken)
	if err != nil {
		return nil, err
	}
	// Never expose the token after creation.
	order.AccessToken = ""
	return order, nil
}

// ═══ Tenant admin ═════════════════════════════════════════════════════════════

// GetAdminOrder returns an order for the tenant admin (no token required).
func (s *ShopOrderService) GetAdminOrder(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	return s.getOrderByID(ctx, tenantID, orderID)
}

// ListTenantOrders returns a paginated list of orders for the tenant admin.
func (s *ShopOrderService) ListTenantOrders(ctx context.Context, tenantID uuid.UUID, status *shop.OrderStatus, limit, offset int) ([]*shop.ShopOrder, int, error) {
	// Count.
	var total int
	countQ := `SELECT COUNT(*) FROM shop_orders WHERE tenant_id = $1`
	countArgs := []any{tenantID}
	if status != nil {
		countQ += ` AND status = $2`
		countArgs = append(countArgs, *status)
	}
	if err := s.pool.QueryRow(ctx, countQ, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count shop orders: %w", err)
	}

	// Data.
	dataQ := `
		SELECT id, tenant_id,
		       customer_name, customer_email, COALESCE(customer_phone, ''),
		       shipping_address, COALESCE(shipping_city, ''), COALESCE(shipping_notes, ''),
		       delivery_lat, delivery_lng,
		       subtotal, shipping_cost, total,
		       status, COALESCE(notes, ''),
		       paid_at, fulfilled_at, delivered_at, cancelled_at,
		       created_at, updated_at,
		       payment_proof_url, payment_proof_filename, payment_proof_uploaded_at,
		       payment_method_id, payment_reference
		FROM shop_orders
		WHERE tenant_id = $1`
	dataArgs := []any{tenantID}
	if status != nil {
		dataQ += ` AND status = $2`
		dataArgs = append(dataArgs, *status)
		dataQ += ` ORDER BY created_at DESC LIMIT $3 OFFSET $4`
		dataArgs = append(dataArgs, limit, offset)
	} else {
		dataQ += ` ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		dataArgs = append(dataArgs, limit, offset)
	}

	rows, err := s.pool.Query(ctx, dataQ, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list shop orders: %w", err)
	}
	defer rows.Close()

	var orders []*shop.ShopOrder
	for rows.Next() {
		o, err := scanShopOrder(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan shop order: %w", err)
		}
		// Don't expose access token in list view.
		o.AccessToken = ""
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("shop orders rows: %w", err)
	}
	if orders == nil {
		orders = []*shop.ShopOrder{}
	}
	return orders, total, nil
}

// ListCustomerOrders returns up to 50 orders for a given customer email (case-insensitive)
// across the tenant, sorted newest first. The access_token is included in the returned
// orders so the customer can navigate to the order detail page.
func (s *ShopOrderService) ListCustomerOrders(ctx context.Context, tenantID uuid.UUID, email string) ([]*shop.ShopOrder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT so.id, so.tenant_id,
		       so.customer_name, so.customer_email, COALESCE(so.customer_phone, ''),
		       so.shipping_address, COALESCE(so.shipping_city, ''), COALESCE(so.shipping_notes, ''),
		       so.delivery_lat, so.delivery_lng,
		       so.subtotal, so.shipping_cost, so.total,
		       so.status, COALESCE(so.notes, ''),
		       so.paid_at, so.fulfilled_at, so.delivered_at, so.cancelled_at,
		       so.created_at, so.updated_at,
		       so.payment_proof_url, so.payment_proof_filename, so.payment_proof_uploaded_at,
		       so.payment_method_id, so.payment_reference,
		       so.access_token,
		       COALESCE(
		           json_agg(
		               json_build_object(
		                   'id',         sol.id,
		                   'order_id',   sol.order_id,
		                   'product_id', sol.product_id,
		                   'name',       sol.name,
		                   'unit_price', sol.unit_price,
		                   'quantity',   sol.quantity,
		                   'subtotal',   sol.subtotal,
		                   'is_fiscal',  sol.is_fiscal,
		                   'sort_order', sol.sort_order
		               ) ORDER BY sol.sort_order
		           ) FILTER (WHERE sol.id IS NOT NULL),
		           '[]'::json
		       ) AS lines
		FROM shop_orders so
		LEFT JOIN shop_order_lines sol ON sol.order_id = so.id
		WHERE so.tenant_id = $1 AND so.customer_email ILIKE $2
		GROUP BY so.id
		ORDER BY so.created_at DESC
		LIMIT 50`,
		tenantID, email,
	)
	if err != nil {
		return nil, fmt.Errorf("list customer orders: %w", err)
	}
	defer rows.Close()

	var orders []*shop.ShopOrder
	for rows.Next() {
		o, err := scanCustomerOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan customer order: %w", err)
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("customer orders rows: %w", err)
	}
	if orders == nil {
		orders = []*shop.ShopOrder{}
	}
	return orders, nil
}

// scanCustomerOrder scans one row from the ListCustomerOrders query.
// The query selects 25 base columns + 1 json_agg(lines) column, so all 26
// must be consumed in a single Scan call.
func scanCustomerOrder(rows pgx.Rows) (*shop.ShopOrder, error) {
	type lineDTO struct {
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

	var o shop.ShopOrder
	var proofURL, proofFilename, proofRef *string
	var linesJSON json.RawMessage

	if err := rows.Scan(
		&o.ID, &o.TenantID,
		&o.CustomerName, &o.CustomerEmail, &o.CustomerPhone,
		&o.ShippingAddress, &o.ShippingCity, &o.ShippingNotes,
		&o.DeliveryLat, &o.DeliveryLng,
		&o.Subtotal, &o.ShippingCost, &o.Total,
		&o.Status, &o.Notes,
		&o.PaidAt, &o.FulfilledAt, &o.DeliveredAt, &o.CancelledAt,
		&o.CreatedAt, &o.UpdatedAt,
		&proofURL, &proofFilename, &o.PaymentProofUploadedAt,
		&o.PaymentMethodID, &proofRef,
		&o.AccessToken,
		&linesJSON,
	); err != nil {
		return nil, err
	}
	if proofURL != nil {
		o.PaymentProofURL = *proofURL
	}
	if proofFilename != nil {
		o.PaymentProofFilename = *proofFilename
	}
	if proofRef != nil {
		o.PaymentReference = *proofRef
	}

	var dtos []lineDTO
	if err := json.Unmarshal(linesJSON, &dtos); err != nil {
		return nil, fmt.Errorf("unmarshal order lines: %w", err)
	}
	o.Lines = make([]shop.OrderLine, len(dtos))
	for i, d := range dtos {
		o.Lines[i] = shop.OrderLine{
			ID:        d.ID,
			OrderID:   d.OrderID,
			ProductID: d.ProductID,
			Name:      d.Name,
			UnitPrice: d.UnitPrice,
			Quantity:  d.Quantity,
			Subtotal:  d.Subtotal,
			IsFiscal:  d.IsFiscal,
			SortOrder: d.SortOrder,
		}
	}
	return &o, nil
}

// MarkPaid transitions pending → paid.
// On success it asynchronously:
//   - Sends the lifecycle email notification (existing behaviour)
//   - Creates + issues an internal invoice for all non-fiscal lines (auto-invoice)
func (s *ShopOrderService) MarkPaid(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	o, err := s.transition(ctx, tenantID, orderID, shop.OrderStatusPaid, "paid_at")
	if err != nil {
		return nil, err
	}
	// Best-effort async side effects — failures must never block the HTTP response.
	s.notifier.Notify(ctx, tenantID, o)
	if s.invoiceSvc != nil {
		go s.generateAutoInvoice(tenantID, orderID, o.CustomerName, o.Notes)
	}
	if s.fiscalInvoiceSvc != nil {
		go s.generateAutoFiscalInvoice(tenantID, orderID, o.CustomerName, o.Notes)
	}
	return o, nil
}

// generateAutoInvoice runs the auto-invoice flow for a just-paid shop order.
// Designed to be called as a goroutine — failures are logged, not propagated.
func (s *ShopOrderService) generateAutoInvoice(tenantID, orderID uuid.UUID, customerName, notes string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inv, err := s.invoiceSvc.CreateAndIssueFromShopOrder(ctx, tenantID, orderID, customerName, notes)
	if err != nil {
		log.Error().
			Err(err).
			Str("order_id", orderID.String()).
			Str("tenant_id", tenantID.String()).
			Msg("auto-invoice: failed to create invoice for paid shop order")
		return
	}
	if inv == nil {
		// All products were fiscal — skip is expected.
		return
	}
	log.Info().
		Str("order_id", orderID.String()).
		Str("invoice_id", inv.ID.String()).
		Str("correlative", inv.Correlative).
		Msg("auto-invoice: internal invoice created and issued")
}

// generateAutoFiscalInvoice runs the auto fiscal-invoice flow for a just-paid shop order.
// Designed to be called as a goroutine — failures are logged, not propagated.
func (s *ShopOrderService) generateAutoFiscalInvoice(tenantID, orderID uuid.UUID, customerName, notes string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inv, err := s.fiscalInvoiceSvc.CreateAndIssueFromShopOrder(ctx, tenantID, orderID, customerName, notes)
	if err != nil {
		log.Error().
			Err(err).
			Str("order_id", orderID.String()).
			Str("tenant_id", tenantID.String()).
			Msg("auto-fiscal-invoice: failed to create fiscal invoice for paid shop order")
		return
	}
	if inv == nil {
		// No fiscal products in this order — skip is expected.
		return
	}
	log.Info().
		Str("order_id", orderID.String()).
		Str("invoice_id", inv.ID.String()).
		Str("status", string(inv.Status)).
		Msg("auto-fiscal-invoice: fiscal invoice created and issued")
}

// MarkFulfilled transitions paid → fulfilled.
func (s *ShopOrderService) MarkFulfilled(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	o, err := s.transition(ctx, tenantID, orderID, shop.OrderStatusFulfilled, "fulfilled_at")
	if err == nil {
		s.notifier.Notify(ctx, tenantID, o)
	}
	return o, err
}

// MarkDelivered transitions fulfilled → delivered.
func (s *ShopOrderService) MarkDelivered(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	o, err := s.transition(ctx, tenantID, orderID, shop.OrderStatusDelivered, "delivered_at")
	if err == nil {
		s.notifier.Notify(ctx, tenantID, o)
	}
	return o, err
}

// Cancel transitions pending|paid → cancelled (admin version, no token required).
func (s *ShopOrderService) Cancel(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	o, err := s.transition(ctx, tenantID, orderID, shop.OrderStatusCancelled, "cancelled_at")
	if err == nil {
		s.notifier.Notify(ctx, tenantID, o)
	}
	return o, err
}

// CustomerCancel transitions pending → cancelled using the access token.
func (s *ShopOrderService) CustomerCancel(ctx context.Context, tenantID, orderID uuid.UUID, accessToken string) (*shop.ShopOrder, error) {
	order, err := s.getOrderByIDWithToken(ctx, tenantID, orderID, accessToken)
	if err != nil {
		return nil, err
	}
	if err := order.CanTransitionTo(shop.OrderStatusCancelled); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	_, err = s.pool.Exec(ctx, `
		UPDATE shop_orders
		SET status = 'cancelled', cancelled_at = $1, updated_at = $1
		WHERE id = $2 AND tenant_id = $3`,
		now, orderID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("cancel shop order: %w", err)
	}
	o, err := s.getOrderByID(ctx, tenantID, orderID)
	if err == nil {
		s.notifier.Notify(ctx, tenantID, o)
	}
	return o, err
}

// ─── Private helpers ──────────────────────────────────────────────────────────

// transition runs a status transition for admin operations (no token check).
func (s *ShopOrderService) transition(ctx context.Context, tenantID, orderID uuid.UUID, next shop.OrderStatus, tsField string) (*shop.ShopOrder, error) {
	order, err := s.getOrderByID(ctx, tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if err := order.CanTransitionTo(next); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	q := fmt.Sprintf(`
		UPDATE shop_orders
		SET status = $1, %s = $2, updated_at = $2
		WHERE id = $3 AND tenant_id = $4`, tsField)
	if _, err := s.pool.Exec(ctx, q, next, now, orderID, tenantID); err != nil {
		return nil, fmt.Errorf("update shop order status: %w", err)
	}
	return s.getOrderByID(ctx, tenantID, orderID)
}

// getOrderByID fetches a full order (lines included) by tenant + order ID.
func (s *ShopOrderService) getOrderByID(ctx context.Context, tenantID, orderID uuid.UUID) (*shop.ShopOrder, error) {
	o, err := scanShopOrder(s.pool.QueryRow(ctx, `
		SELECT id, tenant_id,
		       customer_name, customer_email, COALESCE(customer_phone, ''),
		       shipping_address, COALESCE(shipping_city, ''), COALESCE(shipping_notes, ''),
		       delivery_lat, delivery_lng,
		       subtotal, shipping_cost, total,
		       status, COALESCE(notes, ''),
		       paid_at, fulfilled_at, delivered_at, cancelled_at,
		       created_at, updated_at,
		       payment_proof_url, payment_proof_filename, payment_proof_uploaded_at,
		       payment_method_id, payment_reference
		FROM shop_orders
		WHERE id = $1 AND tenant_id = $2`,
		orderID, tenantID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shop.ErrShopOrderNotFound
		}
		return nil, fmt.Errorf("get shop order: %w", err)
	}
	lines, err := s.loadLines(ctx, orderID)
	if err != nil {
		return nil, err
	}
	o.Lines = lines
	return o, nil
}

// getOrderByIDWithToken fetches an order and validates the access token.
func (s *ShopOrderService) getOrderByIDWithToken(ctx context.Context, tenantID, orderID uuid.UUID, accessToken string) (*shop.ShopOrder, error) {
	o, err := scanShopOrder(s.pool.QueryRow(ctx, `
		SELECT id, tenant_id,
		       customer_name, customer_email, COALESCE(customer_phone, ''),
		       shipping_address, COALESCE(shipping_city, ''), COALESCE(shipping_notes, ''),
		       delivery_lat, delivery_lng,
		       subtotal, shipping_cost, total,
		       status, COALESCE(notes, ''),
		       paid_at, fulfilled_at, delivered_at, cancelled_at,
		       created_at, updated_at,
		       payment_proof_url, payment_proof_filename, payment_proof_uploaded_at,
		       payment_method_id, payment_reference
		FROM shop_orders
		WHERE id = $1 AND tenant_id = $2 AND access_token = $3`,
		orderID, tenantID, accessToken,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Intentionally opaque: whether the order doesn't exist or the token is wrong,
			// we return the same error to prevent token enumeration.
			return nil, shop.ErrShopOrderNotFound
		}
		return nil, fmt.Errorf("get shop order (public): %w", err)
	}
	lines, err := s.loadLines(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	o.Lines = lines
	return o, nil
}

// loadLines returns all lines for a given order sorted by sort_order.
func (s *ShopOrderService) loadLines(ctx context.Context, orderID uuid.UUID) ([]shop.OrderLine, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, order_id, product_id, name, unit_price, quantity, subtotal, is_fiscal, sort_order
		FROM shop_order_lines
		WHERE order_id = $1
		ORDER BY sort_order`,
		orderID,
	)
	if err != nil {
		return nil, fmt.Errorf("load shop order lines: %w", err)
	}
	defer rows.Close()

	var lines []shop.OrderLine
	for rows.Next() {
		var l shop.OrderLine
		if err := rows.Scan(
			&l.ID, &l.OrderID, &l.ProductID,
			&l.Name, &l.UnitPrice, &l.Quantity,
			&l.Subtotal, &l.IsFiscal, &l.SortOrder,
		); err != nil {
			return nil, fmt.Errorf("scan order line: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

type shopOrderScanner interface {
	Scan(dest ...any) error
}

func scanShopOrder(row shopOrderScanner) (*shop.ShopOrder, error) {
	var o shop.ShopOrder
	var proofURL, proofFilename *string
	var proofRef *string
	if err := row.Scan(
		&o.ID, &o.TenantID,
		&o.CustomerName, &o.CustomerEmail, &o.CustomerPhone,
		&o.ShippingAddress, &o.ShippingCity, &o.ShippingNotes,
		&o.DeliveryLat, &o.DeliveryLng,
		&o.Subtotal, &o.ShippingCost, &o.Total,
		&o.Status, &o.Notes,
		&o.PaidAt, &o.FulfilledAt, &o.DeliveredAt, &o.CancelledAt,
		&o.CreatedAt, &o.UpdatedAt,
		&proofURL, &proofFilename, &o.PaymentProofUploadedAt,
		&o.PaymentMethodID, &proofRef,
	); err != nil {
		return nil, err
	}
	if proofURL != nil {
		o.PaymentProofURL = *proofURL
	}
	if proofFilename != nil {
		o.PaymentProofFilename = *proofFilename
	}
	if proofRef != nil {
		o.PaymentReference = *proofRef
	}
	return &o, nil
}

// ═══ Payment proof upload ═════════════════════════════════════════════════════

// UploadPaymentProof saves the customer's payment screenshot / PDF and records
// the URL on the order. Validates:
//   - order belongs to tenant, access token is valid, status is pending
//   - file meta is acceptable (delegates to shop.ValidatePaymentProofMeta)
//   - no proof uploaded yet (idempotency guard — returns ErrPaymentProofAlreadyExists)
//
// Returns the updated order with payment_proof_url populated.
func (s *ShopOrderService) UploadPaymentProof(
	ctx context.Context,
	tenantID, orderID uuid.UUID,
	accessToken string,
	filename string,
	contentType string,
	sizeBytes int64,
	content io.Reader,
	paymentMethodID *uuid.UUID,
	reference string,
) (*shop.ShopOrder, error) {
	// 1. Validate file metadata before touching the DB.
	if err := shop.ValidatePaymentProofMeta(contentType, sizeBytes); err != nil {
		return nil, err
	}

	// 2. Load and validate the order.
	order, err := s.getOrderByIDWithToken(ctx, tenantID, orderID, accessToken)
	if err != nil {
		return nil, err
	}
	if order.Status != shop.OrderStatusPending {
		return nil, shop.ErrInvalidTransition
	}
	if order.PaymentProofURL != "" {
		return nil, shop.ErrPaymentProofAlreadyExists
	}

	// 3. Build a sanitized storage key.
	sanitized := sanitizeFilename(filename)
	ts := time.Now().UTC()
	key := fmt.Sprintf("payment-proofs/%s/%s/%d-%s",
		tenantID.String(),
		orderID.String(),
		ts.UnixMilli(),
		sanitized,
	)

	// 4. Persist to storage.
	publicURL, err := s.storage.Save(ctx, key, content, contentType)
	if err != nil {
		return nil, fmt.Errorf("save payment proof: %w", err)
	}

	// 5. Update the order row.
	_, err = s.pool.Exec(ctx, `
		UPDATE shop_orders
		SET payment_proof_url        = $1,
		    payment_proof_filename   = $2,
		    payment_proof_uploaded_at = $3,
		    payment_method_id        = $4,
		    payment_reference        = $5,
		    updated_at               = $3
		WHERE id = $6 AND tenant_id = $7`,
		publicURL, sanitized, ts,
		paymentMethodID, reference,
		orderID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("update shop order proof: %w", err)
	}

	updated, err := s.getOrderByID(ctx, tenantID, orderID)
	if err != nil {
		return nil, err
	}

	// Notify the tenant owner that a proof was uploaded (async, best-effort).
	s.notifier.NotifyProofUploaded(ctx, tenantID, updated)

	updated.AccessToken = ""
	return updated, nil
}

// ─── Private helpers ──────────────────────────────────────────────────────────

// sanitizeFilename strips path separators, replaces spaces with hyphens,
// removes characters outside [a-zA-Z0-9._-], and caps at 80 characters.
var reUnsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._\-]`)

func sanitizeFilename(name string) string {
	// Strip any directory component.
	base := name
	for _, sep := range []string{"/", "\\"} {
		if idx := strings.LastIndex(base, sep); idx >= 0 {
			base = base[idx+1:]
		}
	}
	// Replace spaces.
	base = strings.ReplaceAll(base, " ", "-")
	// Remove unsafe characters.
	base = reUnsafeChars.ReplaceAllString(base, "")
	// Cap length.
	if len(base) > 80 {
		base = base[:80]
	}
	if base == "" {
		base = "proof"
	}
	return base
}

// generateAccessToken returns a 64-char hex string from crypto/rand.
func generateAccessToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to timestamp-based to avoid panic in handlers.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
