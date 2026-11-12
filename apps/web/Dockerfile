# Build context MUST be the monorepo root (pnpm workspace: apps/web depends
# on packages/config via "workspace:*", and `pnpm install` needs every
# workspace package.json present to match pnpm-lock.yaml).
FROM node:22-alpine AS builder
# git: required by the root "prepare" script (lefthook install) that pnpm
# runs automatically on install — without it, `pnpm install` aborts.
RUN apk add --no-cache git
RUN npm install -g pnpm
WORKDIR /app
# lefthook's "prepare" script needs a git repo to attach hooks to (irrelevant
# in a container, but it hard-fails without one). Throwaway init, never shipped.
RUN git init -q

COPY pnpm-workspace.yaml package.json pnpm-lock.yaml* ./
COPY apps/web/package.json apps/web/package.json
COPY apps/landing/package.json apps/landing/package.json
COPY packages/config/package.json packages/config/package.json
COPY packages/types/package.json packages/types/package.json
COPY packages/ui/package.json packages/ui/package.json
RUN pnpm install --frozen-lockfile

COPY . .
ARG NUXT_PUBLIC_API_BASE
ENV NUXT_PUBLIC_API_BASE=$NUXT_PUBLIC_API_BASE
RUN pnpm --filter @daas/web build

FROM node:22-alpine
WORKDIR /app
COPY --from=builder /app/apps/web/.output ./.output

ENV HOST=0.0.0.0
ENV PORT=3000
EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
