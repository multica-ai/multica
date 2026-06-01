# packaging/docker/repocache/Dockerfile
#
# Long-lived in-cluster server that mirrors workspace repos on a single RWX
# volume. Needs `git` + `openssh-client` available at runtime because Cache
# forks `git clone --bare` / `git fetch` and reads the SSH deploy key from the
# multica-git-ssh Secret.

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
      -o /out/multica-repocache ./cmd/multica-repocache

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      git \
      openssh-client \
      ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 1001 multica \
    && useradd -m -u 1001 -g 1001 -s /bin/bash multica \
    && mkdir -p /home/multica/.ssh \
    && ssh-keyscan github.com > /home/multica/.ssh/known_hosts 2>/dev/null \
    && chown -R multica:multica /home/multica/.ssh \
    && chmod 700 /home/multica/.ssh \
    && chmod 644 /home/multica/.ssh/known_hosts

COPY --from=build /out/multica-repocache /usr/local/bin/multica-repocache
USER multica
WORKDIR /home/multica
ENTRYPOINT ["/usr/local/bin/multica-repocache"]
