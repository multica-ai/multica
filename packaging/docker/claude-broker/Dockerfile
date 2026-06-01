# packaging/docker/claude-broker/Dockerfile
#
# In-cluster OAuth token broker for Claude Code (multica-claude-broker).
# Single source of truth for the Anthropic refresh_token; serves the current
# bearer to worker Job pods via HTTP. See the broker plan for design.

ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download
COPY server/ ./server/
# oauth-constants.json lives at server/cmd/multica-claude-broker/oauth-constants.json
# and is committed; the COPY above brings it in. The companion extractor
# (server/cmd/extract-oauth-constants) writes it; the watcher PR refreshes it
# whenever Claude Code publishes a new version.
ARG VERSION=dev
ARG COMMIT=unknown
RUN cd server && CGO_ENABLED=0 go build \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/multica-claude-broker ./cmd/multica-claude-broker

# distroless static:nonroot ships ca-certificates (we need them to TLS to
# platform.claude.com) and a UID 65532 nonroot user that the chart's
# PodSecurity restricted profile expects.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/multica-claude-broker /usr/local/bin/multica-claude-broker
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/multica-claude-broker"]
