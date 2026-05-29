# packaging/docker/controller/Dockerfile
# Tiny image containing the multica-k8s-controller binary.

ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download
COPY server/ ./server/
ARG VERSION=dev
ARG COMMIT=unknown
RUN cd server && CGO_ENABLED=0 go build \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/multica-k8s-controller ./cmd/multica-k8s-controller

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/multica-k8s-controller /usr/local/bin/multica-k8s-controller
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/multica-k8s-controller"]
