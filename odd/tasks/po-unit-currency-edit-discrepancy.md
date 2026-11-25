# Feature: PO unit of measure, currency, edit-draft, invoice discrepancy trace, date-shift fix

Branch: fix/po-unit-currency-edit-discrepancy

## Objective

Add unit-of-measure and currency to products/purchase orders, allow editing draft POs,
fix a frontend date-shift bug for `supplier_invoice_date`, and surface a non-blocking
discrepancy banner when the recorded invoice total differs from the PO's frozen total.

## Scope (pre-authorized, fully specified by user — see full spec in session)

1. Migration 000026 (products.unit, purchase_orders.currency, purchase_order_lines.unit)
2. Backend domain structs (product.go, supplier.go) + UpdatePORequest
3. Backend service (product_service.go unit passthrough; supplier_service.go CreatePO/UpdatePO,
   ListPOs/GetPO/loadPOLines/scanPO currency+unit selects)
4. Backend handler + router (CreatePO/UpdatePO DTOs, new PUT route)
5. Frontend: date-only formatter, unit fields, currency selector, edit-draft flow,
   discrepancy banner — across index.vue, [id].vue (if exists), PurchaseOrderDetail.vue,
   ProductFormModal.vue

## Tasks

- [x] T1 Backend: migration + domain + service + handler + router — build/vet/gofmt clean, all tests pass
- [x] T2 Frontend: date fix, unit, currency, edit-draft, discrepancy banner — index.vue, [id].vue, PurchaseOrderDetail.vue (mobile), ProductFormModal.vue
- [x] T3 Verification: go build/vet/fmt/test (all green), pnpm typecheck/lint (clean)
- [ ] T4 Commit work units on branch

## Allowed edit surfaces

```
apps/api/migrations/
apps/api/internal/domain/product/product.go
apps/api/internal/domain/supplier/supplier.go
apps/api/internal/app/product_service.go
apps/api/internal/app/supplier_service.go
apps/api/internal/adapter/http/handler/supplier_handler.go
apps/api/internal/adapter/http/handler/product_handler.go
apps/api/internal/adapter/http/router.go
apps/web/pages/suppliers/purchase-orders/index.vue
apps/web/pages/suppliers/purchase-orders/[id].vue
apps/web/components/mobile/screens/PurchaseOrderDetail.vue
apps/web/components/products/ProductFormModal.vue
```

## Notes

- User's prior explicit decision: never overwrite PO.total once set; discrepancy is display-only.
- Reuse existing `ErrPONotDraft`, do not duplicate.
