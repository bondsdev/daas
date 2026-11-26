package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"

	"github.com/danzt/daas/api/internal/domain/supplier"
)

// SupplierService handles supplier CRUD and purchase order lifecycle.
type SupplierService struct {
	pool *pgxpool.Pool
}

func NewSupplierService(pool *pgxpool.Pool) *SupplierService {
	return &SupplierService{pool: pool}
}

// ═══ Suppliers ════════════════════════════════════════════════════════════════

func (s *SupplierService) CreateSupplier(ctx context.Context, tenantID uuid.UUID, req supplier.CreateSupplierRequest) (*supplier.Supplier, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	sup := &supplier.Supplier{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Name:        req.Name,
		RIF:         req.RIF,
		ContactName: req.ContactName,
		Email:       req.Email,
		Phone:       req.Phone,
		Address:     req.Address,
		Notes:       req.Notes,
		Active:      true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO suppliers
		    (id, tenant_id, name, rif, contact_name, email, phone, address, notes)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		sup.ID, sup.TenantID, sup.Name, sup.RIF, sup.ContactName,
		sup.Email, sup.Phone, sup.Address, sup.Notes,
	)
	if err != nil {
		return nil, fmt.Errorf("insert supplier: %w", err)
	}
	return sup, nil
}

func (s *SupplierService) ListSuppliers(ctx context.Context, tenantID uuid.UUID, activeOnly bool) ([]*supplier.Supplier, error) {
	query := `SELECT id, tenant_id, name, rif, contact_name, email, phone, address, COALESCE(notes,''), active, created_at, updated_at, COALESCE(portal_token,'')
	          FROM suppliers WHERE tenant_id=$1`
	if activeOnly {
		query += " AND active=true"
	}
	query += " ORDER BY name"
	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list suppliers: %w", err)
	}
	defer rows.Close()
	var result []*supplier.Supplier
	for rows.Next() {
		sup, err := scanSupplier(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, sup)
	}
	return result, nil
}

func (s *SupplierService) GetSupplier(ctx context.Context, tenantID, supplierID uuid.UUID) (*supplier.Supplier, error) {
	sup, err := scanSupplier(s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, rif, contact_name, email, phone, address, COALESCE(notes,''), active, created_at, updated_at, COALESCE(portal_token,'')
		 FROM suppliers WHERE id=$1 AND tenant_id=$2`,
		supplierID, tenantID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, supplier.ErrSupplierNotFound
		}
		return nil, fmt.Errorf("get supplier: %w", err)
	}
	return sup, nil
}

func (s *SupplierService) UpdateSupplier(ctx context.Context, tenantID, supplierID uuid.UUID, req supplier.UpdateSupplierRequest) (*supplier.Supplier, error) {
	sup, err := s.GetSupplier(ctx, tenantID, supplierID)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		sup.Name = *req.Name
	}
	if req.RIF != nil {
		sup.RIF = *req.RIF
	}
	if req.ContactName != nil {
		sup.ContactName = *req.ContactName
	}
	if req.Email != nil {
		sup.Email = *req.Email
	}
	if req.Phone != nil {
		sup.Phone = *req.Phone
	}
	if req.Address != nil {
		sup.Address = *req.Address
	}
	if req.Notes != nil {
		sup.Notes = *req.Notes
	}
	if req.Active != nil {
		sup.Active = *req.Active
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE suppliers
		 SET name=$1, rif=$2, contact_name=$3, email=$4, phone=$5, address=$6, notes=$7, active=$8, updated_at=NOW()
		 WHERE id=$9 AND tenant_id=$10`,
		sup.Name, sup.RIF, sup.ContactName, sup.Email, sup.Phone,
		sup.Address, sup.Notes, sup.Active, supplierID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("update supplier: %w", err)
	}
	sup.UpdatedAt = time.Now().UTC()
	return sup, nil
}

// GeneratePortalToken crea (o rota) el token del link público del proveedor.
func (s *SupplierService) GeneratePortalToken(ctx context.Context, tenantID, supplierID uuid.UUID) (*supplier.Supplier, error) {
	if _, err := s.GetSupplier(ctx, tenantID, supplierID); err != nil {
		return nil, err
	}
	token, err := randomToken(24)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE suppliers SET portal_token=$1, updated_at=NOW() WHERE id=$2 AND tenant_id=$3`,
		token, supplierID, tenantID,
	); err != nil {
		return nil, fmt.Errorf("set portal token: %w", err)
	}
	return s.GetSupplier(ctx, tenantID, supplierID)
}

// RevokePortalToken desactiva el link público del proveedor.
func (s *SupplierService) RevokePortalToken(ctx context.Context, tenantID, supplierID uuid.UUID) (*supplier.Supplier, error) {
	if _, err := s.GetSupplier(ctx, tenantID, supplierID); err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE suppliers SET portal_token=NULL, updated_at=NOW() WHERE id=$1 AND tenant_id=$2`,
		supplierID, tenantID,
	); err != nil {
		return nil, fmt.Errorf("revoke portal token: %w", err)
	}
	return s.GetSupplier(ctx, tenantID, supplierID)
}

// randomToken devuelve un token hex aleatorio de n bytes (2n caracteres).
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ═══ Portal público del proveedor (Fase 2b) ══════════════════════════════════

// SupplierByToken resuelve el proveedor a partir del token del portal público.
func (s *SupplierService) SupplierByToken(ctx context.Context, token string) (*supplier.Supplier, error) {
	if token == "" {
		return nil, supplier.ErrSupplierNotFound
	}
	sup, err := scanSupplier(s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, rif, contact_name, email, phone, address, COALESCE(notes,''), active, created_at, updated_at, COALESCE(portal_token,'')
		 FROM suppliers WHERE portal_token=$1`, token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, supplier.ErrSupplierNotFound
		}
		return nil, fmt.Errorf("supplier by token: %w", err)
	}
	return sup, nil
}

// StoreName devuelve el nombre visible de la tienda (branding o razón social).
func (s *SupplierService) StoreName(ctx context.Context, tenantID uuid.UUID) string {
	var name string
	var brand *string
	if err := s.pool.QueryRow(ctx,
		`SELECT name, branding_store_name FROM tenants WHERE id=$1`, tenantID,
	).Scan(&name, &brand); err != nil {
		return "la tienda"
	}
	if brand != nil && *brand != "" {
		return *brand
	}
	return name
}

// ListCatalog lista el catálogo cargado por el proveedor.
func (s *SupplierService) ListCatalog(ctx context.Context, supplierID uuid.UUID) ([]supplier.CatalogItem, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, supplier_id, name, COALESCE(sku,''), COALESCE(cost,0), COALESCE(unit,''), COALESCE(barcode,''), COALESCE(description,''), created_at, updated_at
		 FROM supplier_catalog_items WHERE supplier_id=$1 ORDER BY created_at DESC`, supplierID)
	if err != nil {
		return nil, fmt.Errorf("list catalog: %w", err)
	}
	defer rows.Close()
	items := []supplier.CatalogItem{}
	for rows.Next() {
		var it supplier.CatalogItem
		if err := rows.Scan(&it.ID, &it.SupplierID, &it.Name, &it.SKU, &it.Cost,
			&it.Unit, &it.Barcode, &it.Description, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// AddCatalogItem agrega un producto al catálogo del proveedor.
func (s *SupplierService) AddCatalogItem(ctx context.Context, tenantID, supplierID uuid.UUID, req supplier.CreateCatalogItemRequest) (*supplier.CatalogItem, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var it supplier.CatalogItem
	err := s.pool.QueryRow(ctx,
		`INSERT INTO supplier_catalog_items (tenant_id, supplier_id, name, sku, cost, unit, barcode, description)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id, supplier_id, name, COALESCE(sku,''), COALESCE(cost,0), COALESCE(unit,''), COALESCE(barcode,''), COALESCE(description,''), created_at, updated_at`,
		tenantID, supplierID, req.Name, req.SKU, req.Cost, req.Unit, req.Barcode, req.Description,
	).Scan(&it.ID, &it.SupplierID, &it.Name, &it.SKU, &it.Cost, &it.Unit, &it.Barcode, &it.Description, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("add catalog item: %w", err)
	}
	return &it, nil
}

// DeleteCatalogItem elimina un producto del catálogo (scopeado al proveedor).
func (s *SupplierService) DeleteCatalogItem(ctx context.Context, supplierID, itemID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM supplier_catalog_items WHERE id=$1 AND supplier_id=$2`, itemID, supplierID)
	if err != nil {
		return fmt.Errorf("delete catalog item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return supplier.ErrCatalogItemNotFound
	}
	return nil
}

// ImportCatalogExcel parsea un .xlsx y crea ítems de catálogo en masa.
// Columnas esperadas (fila 1 = encabezado, se ignora):
// nombre | sku | costo | unidad | codigo_barras | descripcion
// Devuelve (importados, salteados, error). Filas sin nombre se saltean.
func (s *SupplierService) ImportCatalogExcel(ctx context.Context, tenantID, supplierID uuid.UUID, data []byte) (int, int, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("no se pudo leer el archivo (¿es un .xlsx válido?): %w", err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return 0, 0, errors.New("el archivo no tiene hojas")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return 0, 0, fmt.Errorf("no se pudieron leer las filas: %w", err)
	}

	imported, skipped := 0, 0
	for i, row := range rows {
		if i == 0 {
			continue // encabezado
		}
		name := strings.TrimSpace(cellAt(row, 0))
		if name == "" {
			skipped++
			continue
		}
		_, err := s.pool.Exec(ctx,
			`INSERT INTO supplier_catalog_items (tenant_id, supplier_id, name, sku, cost, unit, barcode, description)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			tenantID, supplierID, name, cellAt(row, 1), parseCost(cellAt(row, 2)),
			cellAt(row, 3), cellAt(row, 4), cellAt(row, 5))
		if err != nil {
			skipped++
			continue
		}
		imported++
	}
	return imported, skipped, nil
}

// cellAt devuelve la celda en el índice, o "" si la fila es más corta.
func cellAt(row []string, idx int) string {
	if idx < len(row) {
		return strings.TrimSpace(row[idx])
	}
	return ""
}

// parseCost interpreta el costo aceptando coma o punto decimal.
func parseCost(s string) float64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// ImportCatalogItem crea un producto del tenant a partir de un ítem del catálogo
// del proveedor. Importa como NO-fiscal con internal_price = costo (el owner
// ajusta el precio de venta / flag fiscal después). Import selectivo.
func (s *SupplierService) ImportCatalogItem(ctx context.Context, tenantID, supplierID, itemID uuid.UUID) (uuid.UUID, string, error) {
	var name, sku, barcode string
	var cost float64
	err := s.pool.QueryRow(ctx,
		`SELECT name, COALESCE(sku,''), COALESCE(barcode,''), COALESCE(cost,0)
		 FROM supplier_catalog_items WHERE id=$1 AND supplier_id=$2 AND tenant_id=$3`,
		itemID, supplierID, tenantID,
	).Scan(&name, &sku, &barcode, &cost)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, "", supplier.ErrCatalogItemNotFound
		}
		return uuid.Nil, "", fmt.Errorf("load catalog item: %w", err)
	}
	if cost <= 0 {
		return uuid.Nil, "", supplier.ErrCatalogItemNoCost
	}

	var pid uuid.UUID
	err = s.pool.QueryRow(ctx,
		`INSERT INTO products (tenant_id, name, sku, barcode, is_fiscal, internal_price, active)
		 VALUES ($1, $2, $3, $4, false, $5, true) RETURNING id`,
		tenantID, nullableString(name), nullableString(sku), nullableString(barcode), cost,
	).Scan(&pid)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uuid.Nil, "", supplier.ErrProductSKUExists
		}
		return uuid.Nil, "", fmt.Errorf("import to products: %w", err)
	}
	return pid, name, nil
}

// ═══ Purchase Orders ══════════════════════════════════════════════════════════

func (s *SupplierService) CreatePO(ctx context.Context, tenantID uuid.UUID, createdBySupabaseUID string, req supplier.CreatePORequest) (*supplier.PurchaseOrder, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	createdBy, err := s.resolveSupplierUserID(ctx, tenantID, createdBySupabaseUID)
	if err != nil {
		return nil, err
	}
	// Verify supplier belongs to tenant
	if _, err := s.GetSupplier(ctx, tenantID, req.SupplierID); err != nil {
		return nil, err
	}
	// Resolve product descriptions/units + compute total
	var total float64
	for i, line := range req.Lines {
		var name, productUnit string
		err := s.pool.QueryRow(ctx,
			`SELECT name, unit FROM products WHERE id=$1 AND tenant_id=$2 AND active=true`,
			line.ProductID, tenantID,
		).Scan(&name, &productUnit)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("product %s not found or inactive", line.ProductID)
			}
			return nil, fmt.Errorf("lookup product: %w", err)
		}
		if req.Lines[i].Description == "" {
			req.Lines[i].Description = name
		}
		if req.Lines[i].Unit == "" {
			req.Lines[i].Unit = productUnit
		}
		total += line.Quantity * line.UnitCost
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	po := &supplier.PurchaseOrder{
		ID:         uuid.New(),
		TenantID:   tenantID,
		SupplierID: req.SupplierID,
		Status:     supplier.POStatusDraft,
		Notes:      req.Notes,
		Currency:   req.Currency,
		Total:      total,
		CreatedBy:  createdBy,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO purchase_orders (id, tenant_id, supplier_id, notes, total, created_by, currency)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		po.ID, po.TenantID, po.SupplierID, po.Notes, po.Total, po.CreatedBy, po.Currency,
	)
	if err != nil {
		return nil, fmt.Errorf("insert po: %w", err)
	}
	for i, line := range req.Lines {
		lineID := uuid.New()
		sub := line.Quantity * line.UnitCost
		_, err = tx.Exec(ctx,
			`INSERT INTO purchase_order_lines (id, po_id, product_id, description, quantity_ordered, unit_cost, subtotal, sort_order, unit)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			lineID, po.ID, line.ProductID, req.Lines[i].Description, line.Quantity, line.UnitCost, sub, i, req.Lines[i].Unit,
		)
		if err != nil {
			return nil, fmt.Errorf("insert po line: %w", err)
		}
		po.Lines = append(po.Lines, supplier.POLine{
			ID: lineID, POID: po.ID, ProductID: line.ProductID,
			Description: req.Lines[i].Description, QuantityOrdered: line.Quantity,
			UnitCost: line.UnitCost, Subtotal: sub, SortOrder: i, Unit: req.Lines[i].Unit,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return po, nil
}

// UpdatePO replaces a draft purchase order's supplier, notes, currency and
// lines in place. Only draft POs may be edited; any other status returns
// ErrPONotDraft.
func (s *SupplierService) UpdatePO(ctx context.Context, tenantID, poID uuid.UUID, req supplier.UpdatePORequest) (*supplier.PurchaseOrder, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	po, err := s.GetPO(ctx, tenantID, poID)
	if err != nil {
		return nil, err
	}
	if po.Status != supplier.POStatusDraft {
		return nil, supplier.ErrPONotDraft
	}
	// Verify the (possibly new) supplier belongs to the tenant.
	if _, err := s.GetSupplier(ctx, tenantID, req.SupplierID); err != nil {
		return nil, err
	}

	// Resolve product descriptions/units + compute total.
	var total float64
	for i, line := range req.Lines {
		var name, productUnit string
		err := s.pool.QueryRow(ctx,
			`SELECT name, unit FROM products WHERE id=$1 AND tenant_id=$2 AND active=true`,
			line.ProductID, tenantID,
		).Scan(&name, &productUnit)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("product %s not found or inactive", line.ProductID)
			}
			return nil, fmt.Errorf("lookup product: %w", err)
		}
		if req.Lines[i].Description == "" {
			req.Lines[i].Description = name
		}
		if req.Lines[i].Unit == "" {
			req.Lines[i].Unit = productUnit
		}
		total += line.Quantity * line.UnitCost
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx,
		`UPDATE purchase_orders SET supplier_id=$2, notes=$3, currency=$4, total=$5, updated_at=NOW() WHERE id=$1`,
		poID, req.SupplierID, req.Notes, req.Currency, total,
	)
	if err != nil {
		return nil, fmt.Errorf("update po: %w", err)
	}
	_, err = tx.Exec(ctx, `DELETE FROM purchase_order_lines WHERE po_id=$1`, poID)
	if err != nil {
		return nil, fmt.Errorf("delete po lines: %w", err)
	}
	for i, line := range req.Lines {
		lineID := uuid.New()
		sub := line.Quantity * line.UnitCost
		_, err = tx.Exec(ctx,
			`INSERT INTO purchase_order_lines (id, po_id, product_id, description, quantity_ordered, unit_cost, subtotal, sort_order, unit)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			lineID, poID, line.ProductID, req.Lines[i].Description, line.Quantity, line.UnitCost, sub, i, req.Lines[i].Unit,
		)
		if err != nil {
			return nil, fmt.Errorf("insert po line: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return s.GetPO(ctx, tenantID, poID)
}

func (s *SupplierService) ListPOs(ctx context.Context, tenantID uuid.UUID, supplierID *uuid.UUID, status *supplier.POStatus) ([]*supplier.PurchaseOrder, error) {
	query := `SELECT id, tenant_id, supplier_id, status, COALESCE(notes,''), total, ordered_at, received_at, created_by, created_at, updated_at,
	                 COALESCE(supplier_invoice_number,''), supplier_invoice_date, COALESCE(supplier_invoice_tax_base,0), COALESCE(supplier_invoice_tax_amount,0),
	                 currency
	          FROM purchase_orders WHERE tenant_id=$1`
	args := []any{tenantID}
	idx := 2
	if supplierID != nil {
		query += fmt.Sprintf(" AND supplier_id=$%d", idx)
		args = append(args, *supplierID)
		idx++
	}
	if status != nil {
		query += fmt.Sprintf(" AND status=$%d", idx)
		args = append(args, *status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list pos: %w", err)
	}
	defer rows.Close()
	var result []*supplier.PurchaseOrder
	supplierIDs := make(map[uuid.UUID]struct{})
	for rows.Next() {
		po, err := scanPO(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, po)
		supplierIDs[po.SupplierID] = struct{}{}
	}

	// Batch-load suppliers and attach
	if len(supplierIDs) > 0 {
		ids := make([]uuid.UUID, 0, len(supplierIDs))
		for id := range supplierIDs {
			ids = append(ids, id)
		}
		sRows, err := s.pool.Query(ctx,
			`SELECT id, tenant_id, name, rif, contact_name, email, phone, address, COALESCE(notes,''), active, created_at, updated_at, COALESCE(portal_token,'')
			 FROM suppliers WHERE tenant_id=$1 AND id = ANY($2)`,
			tenantID, ids,
		)
		if err == nil {
			defer sRows.Close()
			supMap := make(map[uuid.UUID]*supplier.Supplier, len(ids))
			for sRows.Next() {
				sup, err := scanSupplier(sRows)
				if err != nil {
					continue
				}
				supMap[sup.ID] = sup
			}
			for _, po := range result {
				if sup, ok := supMap[po.SupplierID]; ok {
					po.Supplier = sup
				}
			}
		}
	}

	return result, nil
}

func (s *SupplierService) GetPO(ctx context.Context, tenantID, poID uuid.UUID) (*supplier.PurchaseOrder, error) {
	po, err := scanPO(s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, supplier_id, status, COALESCE(notes,''), total, ordered_at, received_at, created_by, created_at, updated_at,
		        COALESCE(supplier_invoice_number,''), supplier_invoice_date, COALESCE(supplier_invoice_tax_base,0), COALESCE(supplier_invoice_tax_amount,0),
		        currency
		 FROM purchase_orders WHERE id=$1 AND tenant_id=$2`,
		poID, tenantID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, supplier.ErrPONotFound
		}
		return nil, fmt.Errorf("get po: %w", err)
	}
	lines, err := s.loadPOLines(ctx, poID)
	if err != nil {
		return nil, err
	}
	po.Lines = lines
	// Load supplier name
	sup, err := s.GetSupplier(ctx, tenantID, po.SupplierID)
	if err == nil {
		po.Supplier = sup
	}
	return po, nil
}

// OrderPO transitions a draft PO to ordered status (sent to supplier).
func (s *SupplierService) OrderPO(ctx context.Context, tenantID, poID uuid.UUID, supabaseUID string) (*supplier.PurchaseOrder, error) {
	_, err := s.resolveSupplierUserID(ctx, tenantID, supabaseUID)
	if err != nil {
		return nil, err
	}
	po, err := s.GetPO(ctx, tenantID, poID)
	if err != nil {
		return nil, err
	}
	if po.Status != supplier.POStatusDraft {
		if po.Status == supplier.POStatusOrdered {
			return nil, supplier.ErrPOAlreadyOrdered
		}
		return nil, supplier.ErrPONotDraft
	}
	now := time.Now().UTC()
	_, err = s.pool.Exec(ctx,
		`UPDATE purchase_orders SET status='ordered', ordered_at=$1, updated_at=NOW() WHERE id=$2`,
		now, poID,
	)
	if err != nil {
		return nil, fmt.Errorf("order po: %w", err)
	}
	return s.GetPO(ctx, tenantID, poID)
}

// ReceivePO transitions an ordered PO to received and creates inventory movements.
func (s *SupplierService) ReceivePO(ctx context.Context, tenantID, poID uuid.UUID, supabaseUID string) (*supplier.PurchaseOrder, error) {
	userID, err := s.resolveSupplierUserID(ctx, tenantID, supabaseUID)
	if err != nil {
		return nil, err
	}
	po, err := s.GetPO(ctx, tenantID, poID)
	if err != nil {
		return nil, err
	}
	if po.Status != supplier.POStatusOrdered {
		if po.Status == supplier.POStatusReceived {
			return nil, supplier.ErrPOAlreadyReceived
		}
		return nil, supplier.ErrPONotOrdered
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	now := time.Now().UTC()
	_, err = tx.Exec(ctx,
		`UPDATE purchase_orders SET status='received', received_at=$1, updated_at=NOW() WHERE id=$2`,
		now, poID,
	)
	if err != nil {
		return nil, fmt.Errorf("mark received: %w", err)
	}

	// Create inventory movements for each line (type: purchase → increments stock)
	for _, line := range po.Lines {
		movID := uuid.New()
		_, err = tx.Exec(ctx,
			`INSERT INTO inventory_movements
			    (id, tenant_id, product_id, type, quantity, unit_cost, reference_type, reference_id, notes, created_by)
			 VALUES ($1,$2,$3,'entry',$4,$5,'purchase_order',$6,$7,$8)`,
			movID, tenantID, line.ProductID, line.QuantityOrdered, line.UnitCost, poID,
			fmt.Sprintf("Recepción OC — %s", po.SupplierID), userID,
		)
		if err != nil {
			return nil, fmt.Errorf("insert movement: %w", err)
		}
		// Update stock
		_, err = tx.Exec(ctx,
			`INSERT INTO product_stock (tenant_id, product_id, quantity_on_hand, last_updated_at)
			 VALUES ($1,$2,$3,NOW())
			 ON CONFLICT (product_id)
			 DO UPDATE SET quantity_on_hand = product_stock.quantity_on_hand + $3, last_updated_at=NOW()`,
			tenantID, line.ProductID, line.QuantityOrdered,
		)
		if err != nil {
			return nil, fmt.Errorf("update stock: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return s.GetPO(ctx, tenantID, poID)
}

// CancelPO transitions a draft or ordered PO to cancelled.
func (s *SupplierService) CancelPO(ctx context.Context, tenantID, poID uuid.UUID, supabaseUID string) (*supplier.PurchaseOrder, error) {
	_, err := s.resolveSupplierUserID(ctx, tenantID, supabaseUID)
	if err != nil {
		return nil, err
	}
	po, err := s.GetPO(ctx, tenantID, poID)
	if err != nil {
		return nil, err
	}
	if po.Status == supplier.POStatusCancelled {
		return nil, supplier.ErrPOAlreadyCancelled
	}
	if po.Status == supplier.POStatusReceived {
		return nil, supplier.ErrPOAlreadyReceived
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE purchase_orders SET status='cancelled', updated_at=NOW() WHERE id=$1`,
		poID,
	)
	if err != nil {
		return nil, fmt.Errorf("cancel po: %w", err)
	}
	return s.GetPO(ctx, tenantID, poID)
}

// SetPurchaseInvoice records the supplier's invoice data (número, fecha, base,
// IVA) on a purchase order so the purchase can be declared to the tax authority.
// Allowed on any non-cancelled PO — the invoice usually arrives with the goods.
func (s *SupplierService) SetPurchaseInvoice(ctx context.Context, tenantID, poID uuid.UUID, req supplier.SetPurchaseInvoiceRequest) (*supplier.PurchaseOrder, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	po, err := s.GetPO(ctx, tenantID, poID)
	if err != nil {
		return nil, err
	}
	if po.Status == supplier.POStatusCancelled {
		return nil, supplier.ErrPOAlreadyCancelled
	}

	_, err = s.pool.Exec(ctx,
		`UPDATE purchase_orders
		 SET supplier_invoice_number = $2,
		     supplier_invoice_date = $3,
		     supplier_invoice_tax_base = $4,
		     supplier_invoice_tax_amount = $5,
		     updated_at = NOW()
		 WHERE id = $1 AND tenant_id = $6`,
		poID, req.InvoiceNumber, req.InvoiceDate, req.TaxBase, req.TaxAmount, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("set purchase invoice: %w", err)
	}
	return s.GetPO(ctx, tenantID, poID)
}

// ─── Private helpers ──────────────────────────────────────────────────────────

func (s *SupplierService) resolveSupplierUserID(ctx context.Context, tenantID uuid.UUID, supabaseUID string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM tenant_users WHERE tenant_id=$1 AND supabase_uid=$2`,
		tenantID, supabaseUID,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve user: %w", err)
	}
	return id, nil
}

func (s *SupplierService) loadPOLines(ctx context.Context, poID uuid.UUID) ([]supplier.POLine, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, po_id, product_id, description, quantity_ordered, unit_cost, subtotal, sort_order, unit
		 FROM purchase_order_lines WHERE po_id=$1 ORDER BY sort_order`,
		poID,
	)
	if err != nil {
		return nil, fmt.Errorf("load po lines: %w", err)
	}
	defer rows.Close()
	var lines []supplier.POLine
	for rows.Next() {
		var l supplier.POLine
		if err := rows.Scan(&l.ID, &l.POID, &l.ProductID, &l.Description,
			&l.QuantityOrdered, &l.UnitCost, &l.Subtotal, &l.SortOrder, &l.Unit); err != nil {
			return nil, fmt.Errorf("scan po line: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, nil
}

type supplierScanner interface {
	Scan(dest ...any) error
}

func scanSupplier(row supplierScanner) (*supplier.Supplier, error) {
	var s supplier.Supplier
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.Name, &s.RIF, &s.ContactName,
		&s.Email, &s.Phone, &s.Address, &s.Notes, &s.Active,
		&s.CreatedAt, &s.UpdatedAt, &s.PortalToken,
	); err != nil {
		return nil, err
	}
	return &s, nil
}

func scanPO(row supplierScanner) (*supplier.PurchaseOrder, error) {
	var po supplier.PurchaseOrder
	if err := row.Scan(
		&po.ID, &po.TenantID, &po.SupplierID, &po.Status, &po.Notes, &po.Total,
		&po.OrderedAt, &po.ReceivedAt, &po.CreatedBy, &po.CreatedAt, &po.UpdatedAt,
		&po.SupplierInvoiceNumber, &po.SupplierInvoiceDate,
		&po.SupplierInvoiceTaxBase, &po.SupplierInvoiceTaxAmount,
		&po.Currency,
	); err != nil {
		return nil, err
	}
	return &po, nil
}
