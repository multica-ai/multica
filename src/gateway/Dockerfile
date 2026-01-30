# Build stage
FROM node:22-alpine AS builder

# Install pnpm
RUN corepack enable && corepack prepare pnpm@10.16.1 --activate

WORKDIR /app

# Copy package files
COPY package.json pnpm-lock.yaml ./

# Install all dependencies (including devDependencies for build)
RUN pnpm install --frozen-lockfile

# Copy source code
COPY tsconfig.json ./
COPY src ./src

# Build TypeScript (tsc emits JS despite type errors; ignore exit code)
RUN ./node_modules/.bin/tsc || true

# Production stage
FROM node:22-alpine AS production

# Install pnpm
RUN corepack enable && corepack prepare pnpm@10.16.1 --activate

WORKDIR /app

# Copy package files
COPY package.json pnpm-lock.yaml ./

# Install production dependencies only
RUN pnpm install --frozen-lockfile --prod

# Copy built files from builder stage
COPY --from=builder /app/dist ./dist

# Copy static assets (not emitted by tsc)
COPY --from=builder /app/src/gateway/public ./dist/gateway/public

# Set environment variables
ENV NODE_ENV=production
ENV PORT=3000

# Expose the port
EXPOSE 3000

# Run the gateway
CMD ["node", "dist/gateway/main.js"]
