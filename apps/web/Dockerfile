# syntax=docker/dockerfile:1

FROM node:22-alpine AS base
ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable

FROM base AS deps
WORKDIR /app

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json ./apps/web/package.json
COPY packages/core/package.json ./packages/core/package.json
COPY packages/ui/package.json ./packages/ui/package.json
COPY packages/tsconfig/package.json ./packages/tsconfig/package.json
COPY packages/views/package.json ./packages/views/package.json
RUN pnpm install --frozen-lockfile --filter @multica/web...

FROM deps AS build
WORKDIR /app
COPY apps/web ./apps/web
COPY packages ./packages

ARG REMOTE_API_URL=http://backend:8080
ARG NEXT_PUBLIC_API_URL=
ARG NEXT_PUBLIC_WS_URL=
ARG NEXT_PUBLIC_LOCAL_MODE=false
ENV REMOTE_API_URL=${REMOTE_API_URL}
ENV NEXT_PUBLIC_API_URL=${NEXT_PUBLIC_API_URL}
ENV NEXT_PUBLIC_WS_URL=${NEXT_PUBLIC_WS_URL}
ENV NEXT_PUBLIC_LOCAL_MODE=${NEXT_PUBLIC_LOCAL_MODE}

RUN pnpm --filter @multica/web build

FROM base AS runtime
WORKDIR /app

RUN addgroup -S multica && adduser -S -G multica -h /app multica

COPY --from=deps /app/node_modules /app/node_modules
COPY --from=deps /app/package.json /app/package.json
COPY --from=deps /app/pnpm-lock.yaml /app/pnpm-lock.yaml
COPY --from=deps /app/pnpm-workspace.yaml /app/pnpm-workspace.yaml
COPY --from=build /app/apps/web /app/apps/web

RUN chown -R multica:multica /app

ENV NODE_ENV=production
ENV PORT=3000

USER multica
WORKDIR /app/apps/web

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:3000').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"

CMD ["pnpm", "start"]
