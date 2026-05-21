FROM golang:1.26-alpine AS build
WORKDIR /src

RUN apk add --no-cache git

COPY go.mod ./
COPY cmd/ ./cmd/

# Resolve deps + build. go mod tidy generates go.sum on first build; cached
# in the layer so subsequent rebuilds skip the network unless go.mod changes.
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build \
        -ldflags="-s -w" \
        -o /out/sidecar ./cmd/sidecar

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/sidecar /sidecar
EXPOSE 9000
USER nonroot:nonroot
ENTRYPOINT ["/sidecar"]
