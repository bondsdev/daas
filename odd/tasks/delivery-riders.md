# Delivery Riders (Motorizados Marketplace)

## Objective

Let a tenant (minimarket/small shop) run its own fleet of delivery riders on top
of the existing `shop_orders` B2C flow: assign a rider to an order, track a
per-order rider fee, settle payouts, and let riders see/fulfill their own
deliveries from a mobile login.

## Why

Market context: Venezuela dropped mandatory e-invoicing, making DaaS more
relevant to minimarkets. The storefront (`shop_orders`, S6-S9) already charges
a flat shipping fee to the customer but has no concept of paying a rider.
This is new domain on top of it — see Engram `daas/minimarket-marketplace-delivery-idea`
(obs 764) and `architecture/storefront-b2c-plan` (obs 192).

## Decisions (resolved in planning, 2026-11-14)

1. Riders are **per-tenant**, not a shared platform pool.
2. Delivery is an **opt-in feature per tenant**, gated by `tenants.delivery_enabled`,
   flipped manually by the platform admin (no self-service UI, no plans table).
3. Riders get a **real login** (Supabase Auth), reusing the existing
   `tenant_users` role/JWT/RLS infra — new role `rider`, scoped to see only
   their own assigned orders.
4. Assignment is **manual or auto-queue**: auto picks the `available` rider
   with capacity left (`active assigned orders < max_concurrent_deliveries`)
   and the oldest `last_assigned_at` (round-robin via one timestamp column,
   no separate queue table).
5. A rider can carry **multiple concurrent orders** (a route) —
   `riders.max_concurrent_deliveries`, independent of assignment mode.
6. One order per route can be marked **urgent** — `is_urgent` +
   snapshotted `urgent_fee` (default from `tenants.urgent_delivery_fee`,
   owner-editable). Rider's list sorts `is_urgent DESC, rider_assigned_at ASC`.
   Only that order jumps the rider's own queue — no global reordering.
7. Liquidación: tenant marks an order's rider fee as paid
   (`shop_orders.rider_paid_at`) — no ledger table for MVP.

## Scope (MVP)

In scope:

- Migration: `riders` table, `shop_orders` delivery columns, `tenants` flags,
  `rider` role.
- Go domain + service: rider CRUD, assignment (manual + auto), mark
  delivered, mark urgent, mark rider-paid.
- Rider-scoped auth (JWT role `rider`, RLS restricting to own orders).
- Tenant-facing endpoints + UI: manage riders, assign/auto-assign, mark
  urgent, mark rider paid.
- Rider-facing endpoints + mobile UI (reuses `components/mobile/*`): login,
  "my deliveries" list (urgent first), mark delivered.

Out of scope (later):

- Platform-wide rider pool / self-service feature activation.
- Push notifications to riders (in-app list is enough for MVP).
- Payout ledger/reconciliation beyond one `rider_paid_at` timestamp.
- Manual drag-reorder of a rider's route beyond the urgent flag.

## Untested / needs real-environment verification

- PWA install prompt (`feat/pwa-install-prompt`, merged separately) — browsers
  only offer "Add to Home Screen" over HTTPS; not verifiable on localhost.
- Everything in this feature, once built, needs a real mobile browser pass
  for the rider screens (list, assign, mark delivered) — add findings here.
- Migration `000022_delivery_riders` was never run up/down against a real DB
  (no Docker daemon running locally, no migrate-up script in the repo) —
  needs a real run before merge.

## Tasks

- [x] T1 — Migration: `riders` table, `shop_orders` + `tenants` columns, `rider` role
      — Spec: this doc
- [ ] T2 — Go domain: `Rider` entity, `ShopOrder` delivery fields/transitions
- [ ] T3 — Go app service: rider CRUD, assign (manual/auto), mark delivered/urgent/paid
- [ ] T4 — Go handlers + router: tenant-facing + rider-facing endpoints
- [ ] T5 — Auth: `rider` role in JWT/middleware, RLS scoping riders to own orders
- [ ] T6 — Frontend: tenant panel — riders CRUD, assignment, urgent, mark paid
- [ ] T7 — Frontend: rider mobile PWA — login, my deliveries, mark delivered

## Acceptance Criteria

- A tenant with `delivery_enabled=false` sees no rider UI/endpoints (403/hidden).
- Auto-assign never exceeds a rider's `max_concurrent_deliveries`.
- Urgent order sorts first in that rider's own list only.
- Rider JWT cannot read another rider's or another tenant's orders (RLS test).
- `go test ./...` and `pnpm --filter web typecheck` pass.

## Progress

- 2026-11-14: Branch `feat/delivery-riders` created off `main`. Task doc created.
- 2026-11-14: T1 done — `apps/api/migrations/000022_delivery_riders.{up,down}.sql`
  (tenant_users role CHECK widened to add `rider`, `tenants.delivery_enabled`/`urgent_delivery_fee`,
  new `riders` table + RLS, `shop_orders` delivery columns). Committed as `8c4d7ff`.
  Could NOT run up/down against a real DB — Docker daemon was not running locally
  (no other migrate-up script/Makefile target found); `go build ./...` passes clean.
