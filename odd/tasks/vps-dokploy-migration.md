# Migración de DaaS a VPS propio vía Dokploy (reemplaza Render + Vercel)

## Objective

Move DaaS compute (Go API + Nuxt web) off Render + Vercel onto the shared
VPS (`root@77.237.235.159`), deployed through Dokploy — the same mechanism
already proven for Jetro/BonDev/Fundación on that box. Database/Auth/Storage
stay on the existing Supabase cloud project (decision: keep as-is, see
Engram `daas/vps-dokploy-migration`).

## Why

User wants to stop paying for / depending on Render + Vercel and consolidate
onto infra they already run.

## Constraints (from recon)

- VPS is shared production infra for 3 other projects + a self-hosted
  Supabase (SionERP's, NOT to be touched or reused for DaaS).
- Ports 80/443 owned by `dokploy-traefik` — no standalone Traefik.
- RAM is tight (2.3Gi free of 7.8Gi at recon time) — watch after deploy.
- Proven deploy pattern (copied from jetro-backend's `deploy-vps.yml`):
  GitHub Actions → `POST {DOKPLOY_URL}/api/application.deploy` with
  `x-api-key` header, one call per Dokploy application ID.
- Migrations: NO CHANGE. DB stays on Supabase cloud, migrations keep
  applying the same way they always have (golang-migrate / Supabase MCP
  against the cloud project). The jetro pattern's backup+migrate steps do
  NOT apply here.

## Scope / Tasks

- [x] T1 — Domain: `sslip.io` quick-start, no DNS purchase needed.
      Web → `daas.77-237-235-159.sslip.io`, API →
      `daas-api.77-237-235-159.sslip.io`.
- [x] T2 — Fixed `apps/web/Dockerfile`: was running `pnpm dev` (dev
      server). Rewritten to multi-stage build from the MONOREPO ROOT
      (pnpm workspace — web depends on `packages/config`), producing
      `.output/server/index.mjs` and running it with `node`. Verified with
      a real `docker build` + `docker run` on the VPS (HTTP 200 on
      `/auth/login`). Found and fixed 2 real build bugs along the way:
      missing `git` in the alpine image (lefthook's `prepare` script needs
      it) and no `.git` repo in the build context (lefthook needs
      `git rev-parse` to succeed) — fixed with `apk add git` + `git init -q`
      as a throwaway repo in the builder stage.
- [x] T3 — Verified `apps/api/Dockerfile` builds + boots correctly
      (verified via `docker build` + `docker run` on the VPS, HTTP 401 on
      `/api/v1/auth/login` with no DB = server alive and routing). Found the
      AUTHORITATIVE full env var list by reading `internal/config/config.go`
      (missed earlier — it uses a `getEnv` helper, not raw `os.Getenv`, so a
      naive grep misses it): `PORT, SUPABASE_URL, SUPABASE_ANON_KEY,
    SUPABASE_SERVICE_ROLE_KEY, JWT_SECRET, DATABASE_URL,
    INTEGRATION_ENCRYPTION_KEY` (via config.Load()) PLUS
      `SUPABASE_PROJECT_URL, SUPABASE_STORAGE_BUCKET, RESEND_API_KEY,
    RESEND_FROM_EMAIL, WHATSAPP_PHONE_NUMBER_ID, WHATSAPP_TOKEN,
    PUBLIC_STOREFRONT_URL, STORAGE_ROOT_DIR, STORAGE_PUBLIC_BASE,
    PUBLIC_RATE_LIMIT_RPM` (direct `os.Getenv` in main.go/middleware).
      `SUPABASE_URL` and `SUPABASE_PROJECT_URL` are two different env var
      names for the SAME value (project URL) — set both to it.
- [ ] T4 — **BLOCKED on user** — Create 2 Dokploy applications (daas-api,
      daas-web). Requires the Dokploy dashboard (no Chrome extension
      connected to drive it, no in-container CLI found) — instructions
      given to user with exact field values.
- [ ] T5 — **BLOCKED on user** — Secrets: user generates a Dokploy API key
      via the dashboard and sets `DOKPLOY_URL` + `DOKPLOY_API_KEY` as
      GitHub repo secrets themselves (never passed through chat).
- [x] T6 — Removed old scaffold (`docker-compose.prod.yml`,
      `deploy-staging.yml`, `deploy-production.yml` — wrong shape for
      Dokploy, had real bugs, confirmed unreferenced elsewhere before
      deleting). Added `.github/workflows/deploy-vps.yml`: verify (same
      gate as `ci.yml`) + deploy via Dokploy API, no backup/migrate steps
      (DB stays cloud). Has placeholder `DAAS_API_APP_ID`/`DAAS_WEB_APP_ID`
      — fill in once T4 creates the real apps.
- [x] T7 — Updated hardcoded `daas-l36f.onrender.com` → new VPS API domain
      in `apps/web/package.json`'s `cap:sync`/`cap:ios`/`cap:android`
      scripts (the only 3 occurrences in the repo, confirmed via repo-wide
      grep). No `daas-inventory.vercel.app` occurrences found.
- [ ] T8 — Smoke test end-to-end against the new VPS deploy (login, a
      few core API calls) before cutting over for real. Blocked on T4/T5.
- [ ] T9 — Once verified, pause/decommission Render + Vercel (user's call
      on timing — keep as fallback for N days or cut immediately).

## Acceptance criteria

- `https://<daas-domain>` (web) and `https://api.<daas-domain>` (api) serve
  traffic from the VPS via Dokploy, TLS via Dokploy's Traefik.
- Login + a core flow (e.g. list products) works end-to-end against the
  VPS-hosted API, using the existing cloud Supabase DB — no data migration.
- Push to the chosen deploy branch (or manual dispatch) redeploys both apps
  without SSHing in by hand.

## Evidence / decisions log

- Engram `daas/vps-dokploy-migration` (decision + full VPS topology recon).
- VPS SSH: `ssh -i ~/.ssh/id_ed25519 root@77.237.235.159`.
