# POS / Caja para supermercado

## Objective

Dar a un supermercado real (primer cliente del vertical) un punto de venta de mostrador: tomar producto → cobrar (tarjeta/efectivo/pago móvil) → cliente se lleva el producto con un comprobante interno, sin factura fiscal impresa. Al cierre de caja, saber cuánto se generó en BS y en USD a la tasa del día, cuánto se consumió de inventario, y cuántas ventas/clientes hubo. Las ventas quedan registradas como base para que, más adelante, el contador las baje a los libros contables.

## Why

Venezuela eliminó la obligatoriedad de facturación electrónica, lo que abre la puerta a atender minimarkets/supermercados sin forzarlos a facturar fiscal desde el día uno. El cliente real no quiere que se le cambie su flujo actual; DaaS debe sistematizarlo tal cual es hoy y permitir, más adelante, escalar hacia facturación fiscal sin romper nada (el módulo `fiscal_invoices` ya existe y queda intacto para ese salto futuro).

## Scope (confirmed with user)

- Apertura y cierre de caja (`cash_sessions`), una por tenant por turno.
- Tasa de cambio BS/USD: auto-fetch desde fuente pública al abrir caja, con override manual. Se congela en la sesión (no se recalcula durante el día).
- Venta rápida de mostrador: crea un `internal_invoice` (ya existente) con `payment_method` y `cash_session_id`, deduce stock.
- Resumen de cierre de caja: total BS, total USD convertido, ventas/clientes del día, consumo de inventario.
- Libro de ventas (espejo de `PurchaseReport`, que ya existe) como insumo para el futuro módulo de contabilidad.
- Búsqueda de producto en el punto de venta configurable por tenant (`pos_search_mode`), no hardcodeada a barcode — para no tener que reabrir esto si más adelante cambia el tipo de negocio.

## Explicitly out of scope (deferred, see [[architecture/pos-caja-supermercado-plan]] in memory)

- Catálogo de "tipos de negocio" (supermercado/autorepuestos/custom) — YAGNI por ahora, sin cliente real de otro rubro.
- Impresión física del comprobante — el cliente no la necesita hoy.
- Facturación fiscal automática desde el POS — queda para una fase posterior, reutilizando `fiscal_invoices` ya existente.
- Diseño detallado del módulo de contabilidad (cómo se "bajan" los libros) — pendiente de que el usuario entienda esa parte con su contador.

## Tasks

- [ ] T1 — Migration: tabla `cash_sessions` (tenant_id, opened_by, opened_at, exchange_rate_bs_usd, status, closed_by, closed_at, closing notes)
- [ ] T2 — Migration: tabla `exchange_rates` (tenant_id, date, rate_bs_per_usd, source, fetched_at) + columnas `cash_session_id`, `payment_method`, `currency` en `internal_invoices`
- [ ] T3 — Domain + `CashSessionService`: abrir caja (con auto-fetch de tasa + fallback manual) y cerrar caja
- [ ] T4 — POS quick-sale: endpoint/servicio que crea `internal_invoice` ligado a la caja abierta, valida stock, lo deduce
- [ ] T5 — `ReportService.CashSessionSummary`: total BS/USD, ventas, clientes, consumo de inventario, filtrado por `cash_session_id`
- [ ] T6 — `ReportService.SalesBook` (libro de ventas), espejo de `PurchaseReport`
- [ ] T7 — HTTP handlers + rutas para T3-T6
- [ ] T8 — Frontend: pantalla de apertura/cierre de caja + resumen de cierre
- [ ] T9 — Frontend: pantalla de venta rápida (búsqueda configurable + carrito + cobro)
- [ ] T10 — Tests (Go: services/handlers; Vitest donde aplique)

## Constraints

- No tocar `sale_orders` ni `fiscal_invoices` — son flujos paralelos que deben seguir intactos.
- No construir el catálogo de tipos de negocio ni un "filter builder" — fuera de alcance.
- Config de búsqueda del POS debe ser un campo simple por tenant, no una tabla nueva.

## Acceptance criteria

- Se puede abrir caja con tasa automática o manual.
- Se puede vender un producto por búsqueda (barcode o texto según config), cobrando con un método de pago, y queda un `internal_invoice` + stock deducido.
- Al cerrar caja, se ve: total BS, total USD, # ventas, # productos vendidos, inventario restante.
- `sale_orders` y `fiscal_invoices` siguen funcionando sin cambios.

## Progress

- 2026-11-12: Documento creado tras sesión de planning. Diseño acordado con el usuario, pendiente de iniciar T1.
