# --- Dependencies ---
FROM node:22-alpine AS deps

RUN corepack enable && corepack prepare pnpm@10.28.2 --activate

WORKDIR /app

# Copy workspace config and all package.json files for dependency resolution
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json turbo.json .npmrc ./
COPY apps/web/package.json apps/web/
COPY packages/core/package.json packages/core/
COPY packages/ui/package.json packages/ui/
COPY packages/views/package.json packages/views/
COPY packages/tsconfig/package.json packages/tsconfig/
COPY packages/eslint-config/package.json packages/eslint-config/

RUN pnpm install --frozen-lockfile

# --- Build ---
FROM node:22-alpine AS builder

RUN corepack enable && corepack prepare pnpm@10.28.2 --activate

WORKDIR /app

# Copy installed dependencies (preserves pnpm symlink structure)
COPY --from=deps /app ./

# Copy source
COPY package.json turbo.json pnpm-workspace.yaml ./
COPY apps/web/ apps/web/
COPY packages/ packages/

# Re-link after source overlay (fixes any symlinks overwritten by COPY)
RUN pnpm install --frozen-lockfile --offline

# Set build-time env: tells Next.js rewrites to proxy API calls to the backend service
ARG REMOTE_API_URL=http://backend:8080
ARG NEXT_PUBLIC_WS_URL
ARG NEXT_PUBLIC_APP_VERSION=dev
ENV REMOTE_API_URL=$REMOTE_API_URL
ENV NEXT_PUBLIC_WS_URL=$NEXT_PUBLIC_WS_URL
ENV NEXT_PUBLIC_APP_VERSION=$NEXT_PUBLIC_APP_VERSION
ENV STANDALONE=true

# Build the web app (standalone output for minimal runtime)
RUN pnpm --filter @multica/web build

# --- Runtime ---
FROM node:22-alpine AS runner

WORKDIR /app

ENV NODE_ENV=production

RUN addgroup --system --gid 1001 nodejs && \
    adduser --system --uid 1001 nextjs

# Copy standalone output (includes traced node_modules)
COPY --from=builder --chown=nextjs:nodejs /app/apps/web/.next/standalone ./
# Copy static files (not included in standalone)
COPY --from=builder --chown=nextjs:nodejs /app/apps/web/.next/static ./apps/web/.next/static
# Copy public assets
COPY --from=builder --chown=nextjs:nodejs /app/apps/web/public ./apps/web/public

USER nextjs

EXPOSE 3000
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

CMD ["node", "apps/web/server.js"]
