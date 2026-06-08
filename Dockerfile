# --- Build stage ---
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

# Cache dependencies. The cerebro fork's server/go.mod has replace() directives
# pointing at ../packages/cerebro-persona-sdk and ../packages/cerebro-pdf-text,
# so those local modules must exist before `go mod download` can resolve the
# module graph (otherwise: "replacement directory ... does not exist").
COPY server/go.mod server/go.sum ./server/
COPY packages/cerebro-persona-sdk ./packages/cerebro-persona-sdk
COPY packages/cerebro-pdf-text ./packages/cerebro-pdf-text
RUN cd server && go mod download

# Copy server source
COPY server/ ./server/

# Build binaries
ARG VERSION=dev
ARG COMMIT=unknown
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/server ./cmd/server
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/multica ./cmd/multica
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/migrate ./cmd/migrate
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/backfill_task_usage_hourly ./cmd/backfill_task_usage_hourly

# --- Runtime stage ---
FROM alpine:3.21

# git is required by the FIR-2656 skill archive sync worker (no-op unless
# CEREBRO_SKILLS_SYNC_REPO/_TOKEN are set). su-exec lets the entrypoint
# fix volume ownership as root, then drop to the unprivileged app user.
RUN apk add --no-cache ca-certificates tzdata git su-exec

WORKDIR /app

COPY --from=builder /src/server/bin/server .
COPY --from=builder /src/server/bin/multica .
COPY --from=builder /src/server/bin/migrate .
COPY --from=builder /src/server/bin/backfill_task_usage_hourly .
COPY server/migrations/ ./migrations/
COPY docker/entrypoint.sh .
RUN sed -i 's/\r$//' entrypoint.sh && chmod +x entrypoint.sh

# Create the unprivileged app user and pre-create the local-storage upload
# dir so a fresh Docker named volume mounted at /app/data/uploads inherits
# app:app ownership from the image. The entrypoint still chowns at runtime
# to fix volumes created before this change (where the dir is root:root).
RUN addgroup -S app && adduser -S -G app app \
    && mkdir -p /app/data/uploads \
    && chown -R app:app /app

EXPOSE 8080

ENTRYPOINT ["./entrypoint.sh"]
