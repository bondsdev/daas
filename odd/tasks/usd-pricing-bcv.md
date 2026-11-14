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

- [x] T1 — Migration: `products.price_usd NUMERIC(12,2) NULL`
- [x] T2 — Servicio compartido para resolver la tasa más reciente de un tenant (reusa `exchange_rates` + `BCVFetcher`, sin duplicar la lógica ya construida en `CashSessionService`)
- [x] T3 — `ProductService.Create/Update`: aceptar `price_usd`, calcular `internal_price` al guardar
- [x] T4 — Recalcular `internal_price` de todos los productos con `price_usd` cada vez que se registra una tasa nueva (hook en el mismo punto donde `CashSessionService` graba en `exchange_rates`)
- [ ] T5 — Frontend: campo "Precio en USD" en `ProductFormModal.vue`, `internal_price` read-only/calculado cuando `price_usd` está seteado
- [x] T6 — Tests (Go contra Postgres real, siguiendo el mismo patrón `TEST_DATABASE_URL` que el resto del repo)

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
- 2026-11-14: T1, T2, T3, T4, T6 implementados en el backend Go.
  - **T1**: `apps/api/migrations/000021_products_price_usd.{up,down}.sql` — agrega `products.price_usd NUMERIC(12,2)` nullable (siguiente número tras `000020`).
  - **T2**: `apps/api/internal/app/exchange_rate_resolver.go` — nuevo archivo con `fetchRate` (único call site de `exchangerate.Fetcher.FetchRate` en todo el código) y `currentExchangeRate` (busca la tasa más reciente en `exchange_rates`; si no existe ninguna, cae al fetcher sin persistir). `CashSessionService.CurrentRate` y `CashSessionService.resolveRate` fueron refactorizados para enrutar por ahí; `ProductService` también lo usa.
  - **T3**: `CreateProductRequest`/`UpdateProductRequest` (`apps/api/internal/app/product_service.go`) ganaron `PriceUSD *float64`; `ProductService.Create/Update` calculan `internal_price = price_usd * tasa_actual` antes de insertar/actualizar cuando `PriceUSD != nil` (vía `resolveUSDPrice`), ignorando cualquier `InternalPrice` pasado manualmente en ese caso. `ProductService` ahora recibe un `exchangerate.Fetcher` en su constructor (`NewProductService(pool, fetcher)`); `product.Product` ganó el campo `PriceUSD`; `product_handler.go` (DTOs de request/response) y `router.go` (`handler.NewProductHandler(cfg.Pool, exchangerate.NewBCVFetcher(""))`) actualizados en consecuencia.
  - **T4**: `CashSessionService.upsertRateAndRecomputeUSDPrices` (reemplaza el upsert inline que antes vivía en `Open`) envuelve el upsert de `exchange_rates` y el `UPDATE products SET internal_price = price_usd * $rate ... WHERE price_usd IS NOT NULL` en una única transacción pgx (`pool.Begin`/`Commit`) — sin cron ni job en background, igual en la rama auto y en la manual.
  - **T6**: `apps/api/internal/app/product_service_test.go` (nuevo) — cubre: crear con `price_usd=1` y tasa 45 → `internal_price=45`; crear sin `price_usd` se comporta exactamente igual que antes (usa un fetcher que falla si se invoca, para probar que ni se toca); actualizar con `price_usd` recalcula `internal_price`; abrir caja con una tasa nueva recalcula solo los productos con `price_usd` seteado y deja intacto el de precio manual. Todos gated con `t.Skip` si `TEST_DATABASE_URL` no está seteada, igual que `cash_session_service_test.go`.
  - **Verificación ejecutada** (Docker no disponible en esta máquina — `docker info` falló con "Cannot connect to the Docker daemon"; por lo tanto las aserciones contra DB real en T6 no se ejecutaron, solo compilaron y el `t.Skip` se activó correctamente):
    - `cd apps/api && go build ./...` → **PASS** (sin output)
    - `cd apps/api && go vet ./...` → **PASS** (sin output)
    - `cd apps/api && gofmt -l .` → **PASS** (sin output)
    - `cd apps/api && go test ./internal/app/... ./internal/domain/product/... ./internal/adapter/http/handler/...` → **PASS**:
      ```
      ok  	github.com/danzt/daas/api/internal/app	0.474s
      ok  	github.com/danzt/daas/api/internal/domain/product	0.404s
      ok  	github.com/danzt/daas/api/internal/adapter/http/handler	0.430s
      ```
    - `go test ./...` (módulo completo) → **PASS**, sin romper ningún paquete existente.
