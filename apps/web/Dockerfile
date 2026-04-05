FROM node:22-alpine AS base

ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable && corepack prepare pnpm@10.28.2 --activate

# --- Build ---
FROM base AS builder

WORKDIR /app

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/ ./apps/web/

RUN pnpm install --frozen-lockfile --shamefully-hoist

# Inject standalone output mode and create stub .env for dotenv
RUN sed -i 's/const nextConfig: NextConfig = {/const nextConfig: NextConfig = {\n  output: "standalone",/' apps/web/next.config.ts \
    && touch .env

ARG REMOTE_API_URL=http://localhost:8080
ENV REMOTE_API_URL=${REMOTE_API_URL}

RUN pnpm --filter @multica/web build

# --- Run ---
FROM node:22-alpine

WORKDIR /app

RUN addgroup --system --gid 1001 nodejs && \
    adduser --system --uid 1001 nextjs

COPY --from=builder /app/apps/web/public ./apps/web/public
COPY --from=builder --chown=nextjs:nodejs /app/apps/web/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/apps/web/.next/static ./apps/web/.next/static

USER nextjs

ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
EXPOSE 3000

CMD ["node", "apps/web/server.js"]
