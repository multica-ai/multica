# --- Build stage ---
FROM golang:1.26-alpine AS builder

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY

RUN apk add --no-cache git

WORKDIR /src

# Cache dependencies
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download

# Copy server source
COPY server/ ./server/

# Build binaries
ARG VERSION=dev
ARG COMMIT=unknown
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/server ./cmd/server
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/multica ./cmd/multica
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/migrate ./cmd/migrate

# --- Runtime stage ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /src/server/bin/server .
COPY --from=builder /src/server/bin/multica .
COPY --from=builder /src/server/bin/migrate .
COPY server/migrations/ ./migrations/
COPY docker/entrypoint.sh .
RUN sed -i 's/\r$//' entrypoint.sh && chmod +x entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["./entrypoint.sh"]
