# Precios en USD con conversión automática a BS (tasa BCV)

## Objective

Permitir cargar el precio de un producto en USD. El precio en BS (`internal_price`, el que ya usan el POS, las facturas internas y los reportes) se calcula a partir de ese USD y se actualiza automáticamente cada vez que se registra una tasa nueva — sin cron, sin job en background, sin que el usuario tenga que volver a tocar el precio.

## Why

El cliente pidió: cargar un producto con su precio en dólares, y que el equivalente en bolívares se ajuste solo cuando el dólar (BCV) se actualice. La carga de cantidad por lote ("20 harinas de una, no una por una") ya está cubierta por `POST /api/v1/inventory/adjustments` (campo `delta`) — no hace falta nada nuevo para eso.

## Scope

- `products.price_usd` nuevo (nullable). Si está seteado, es la fuente de verdad del precio; `internal_price` pasa a ser un valor derivado, recalculado.
- Reutilizar la infraestructura de tasa ya existente (`exchange_rates`, `BCVFetcher` de la feature pos-caja-supermercado) — no se crea una segunda fuente de tasa.
- Cada vez que se registra una tasa nueva para un tenant (auto o manual, al abrir caja), recalcular en la misma operación `internal_price = price_usd * tasa` para todos los productos de ese tenant con `price_usd` seteado.
- Al crear/editar un producto con `price_usd`, calcular `internal_price` de inmediato usando la tasa más reciente conocida del tenant (sin esperar a la próxima apertura de caja); si no existe ninguna tasa registrada todavía, usar el fetcher de BCV para obtener una.
- Frontend: campo "Precio en USD" (opcional) en el formulario de producto. Si está seteado, `internal_price` se muestra como valor calculado (no editable a mano) — para editarlo a mano hay que vaciar `price_usd`.

## Explicitly out of scope

- Carga masiva de múltiples productos distintos vía Excel/CSV — no fue lo que pidió el usuario (aclarado: la carga "por lote" es cantidad, no importación masiva).
- Lotes de inventario con número de lote/fecha de vencimiento (FEFO) — no es lo que se pidió.
- Tocar `fiscal_price` — el usuario solo mencionó precios de venta normales; el precio fiscal queda igual, sin USD por ahora.

## Tasks

- [ ] T1 — Migration: `products.price_usd NUMERIC(12,2) NULL`
- [ ] T2 — Servicio compartido para resolver la tasa más reciente de un tenant (reusa `exchange_rates` + `BCVFetcher`, sin duplicar la lógica ya construida en `CashSessionService`)
- [ ] T3 — `ProductService.Create/Update`: aceptar `price_usd`, calcular `internal_price` al guardar
- [ ] T4 — Recalcular `internal_price` de todos los productos con `price_usd` cada vez que se registra una tasa nueva (hook en el mismo punto donde `CashSessionService` graba en `exchange_rates`)
- [ ] T5 — Frontend: campo "Precio en USD" en `ProductFormModal.vue`, `internal_price` read-only/calculado cuando `price_usd` está seteado
- [ ] T6 — Tests (Go contra Postgres real, siguiendo el mismo patrón `TEST_DATABASE_URL` que el resto del repo)

## Constraints

- No duplicar la lógica de fetch/upsert de tasa que ya existe en `CashSessionService` — extraerla a un helper compartido.
- No tocar el flujo de venta/POS/reportes — todos siguen leyendo `internal_price` tal cual, sin cambios en esos call sites.
- No background job ni cron — el recálculo ocurre en la misma transacción que graba la tasa nueva.

## Acceptance criteria

- Cargar un producto con `price_usd = 1` y tasa del día 45 → `internal_price` queda en 45.
- Al abrir una nueva caja con tasa distinta (ej. 46), todos los productos con `price_usd` seteado quedan con su `internal_price` recalculado a la nueva tasa, sin tocar nada a mano.
- Un producto sin `price_usd` sigue funcionando exactamente igual que antes (precio manual en BS).

## Branch

`feat/usd-pricing-bcv`, basada en `feat/pos-caja-supermercado` (PR #168, sin mergear) porque depende de `exchange_rates`/`BCVFetcher` de esa feature.

## Progress

- 2026-11-14: Documento creado. Confirmado con el usuario que la carga "por lote" ya está cubierta por `/inventory/adjustments` (campo `delta`); el trabajo real es la conversión USD→BS automática.
