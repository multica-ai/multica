# --- Build ---
FROM node:22-alpine AS builder
RUN corepack enable && corepack prepare pnpm@latest --activate
WORKDIR /app

COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY apps/web/package.json ./apps/web/

# Use hoisted node_modules to avoid symlink issues in Docker
RUN echo "node-linker=hoisted" > .npmrc && pnpm install --frozen-lockfile

COPY apps/web/ ./apps/web/

# NEXT_PUBLIC_* vars are baked in at build time.
# Leave empty so the frontend uses relative paths (works behind any reverse proxy).
ENV NEXT_PUBLIC_API_URL=""
ENV NEXT_PUBLIC_WS_URL=""

RUN pnpm --filter @multica/web build

# --- Runtime ---
FROM node:22-alpine
WORKDIR /app

COPY --from=builder /app/apps/web/.next/standalone ./
COPY --from=builder /app/apps/web/.next/static ./apps/web/.next/static
COPY --from=builder /app/apps/web/public ./apps/web/public

ENV PORT=3000
EXPOSE 3000

CMD ["node", "apps/web/server.js"]
