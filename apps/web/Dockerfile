# --- Builder stage ---
FROM node:22-alpine AS builder

RUN corepack enable && corepack prepare pnpm@10.28.2 --activate

WORKDIR /app

# Copy workspace root config (including .npmrc for hoisting)
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml .npmrc ./

# Copy web package.json first for better cache
COPY apps/web/package.json ./apps/web/package.json

# Install all dependencies
RUN pnpm install --frozen-lockfile

# Copy web source
COPY apps/web/ ./apps/web/

# Build-time environment variables
ARG NEXT_PUBLIC_API_URL=http://localhost:8080
ARG NEXT_PUBLIC_WS_URL=ws://localhost:8080/ws

ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_WS_URL=$NEXT_PUBLIC_WS_URL

WORKDIR /app/apps/web

RUN pnpm build

# --- Runner stage ---
FROM node:22-alpine AS runner

WORKDIR /app

ENV NODE_ENV=production

# Next.js standalone copies a minimal server
COPY --from=builder /app/apps/web/.next/standalone ./
COPY --from=builder /app/apps/web/.next/static ./apps/web/.next/static
COPY --from=builder /app/apps/web/public ./apps/web/public

EXPOSE 3000

ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

CMD ["node", "apps/web/server.js"]
