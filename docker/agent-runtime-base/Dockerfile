# syntax=docker/dockerfile:1.7
#
# agent-runtime-base — shared base image for G2's AI-coding pipelines.
#
# Shared base image for G2's AI-coding pipelines. DevEnv per-developer cloud
# IDE pods install the toolchain from this image. Downstream images (devenv-runtime)
# layer on top with just an entrypoint.
#
# Tools baked in:
#   * multica    — Go binary built from this repo's server/cmd/multica
#   * claude     — @anthropic-ai/claude-code (npm)         @ CLAUDE_CODE_VERSION
#   * codex      — @openai/codex (npm)                     @ CODEX_VERSION
#   * opencode   — opencode-ai (npm)                       @ OPENCODE_VERSION
#   * pi         — @earendil-works/pi-coding-agent (npm)   @ PI_VERSION
#   * hermes     — NousResearch/hermes-agent (installer)   @ HERMES_REF (+ HERMES_COMMIT verify)
#   * gh         — GitHub CLI (apt; cli.github.com repo)   @ GH_CLI_VERSION
#
# Substrate: debian bookworm-slim, node LTS, git, bash, curl, jq, rsync,
# ripgrep, ca-certificates, tini. Non-root user `agent` at UID:GID 1000:1000.
#
# Build context: this Dockerfile is built with the agentfarm repo root as
# context (see docker-bake.hcl) so the multica builder can COPY directly out
# of server/. Bump procedure and per-tool details: README.md alongside.

ARG NODE_VERSION=22
ARG GO_VERSION=1.26.1

# ----------------------------------------------------------------------------
# Stage 1 — build the multica CLI from this repo's server/cmd/multica.
#
# Builds from the agentfarm fork (server/) rather than upstream multica-ai —
# the daemon shipped here is calibrated against the fork's source, and going
# through GitHub Releases on a separate repo would couple base-image cadence
# to a release cadence we don't yet maintain. When/if we move to unforked
# multica, this stage flips to a git-clone of multica-ai/multica with a
# pinned MULTICA_REF; until then, "what's in this repo's server/" is the
# binary that ships.
# ----------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS multica-builder
ARG TARGETOS
ARG TARGETARCH
ARG MULTICA_VERSION=dev
ARG MULTICA_COMMIT=unknown

WORKDIR /src
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download

COPY server/ ./server/

RUN cd server \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
      -ldflags "-s -w -X main.version=${MULTICA_VERSION} -X main.commit=${MULTICA_COMMIT}" \
      -o /out/multica ./cmd/multica

# ----------------------------------------------------------------------------
# Stage 2 — runtime substrate + npm-installable agent CLIs.
#
# We install hermes here too (root + Linux path of the installer drops the
# binary at /usr/local/bin/hermes — same FHS layout as claude-code / codex).
# ----------------------------------------------------------------------------
FROM debian:bookworm-slim AS runtime

ARG NODE_VERSION
ARG UID=1000
ARG GID=1000
ARG CLAUDE_CODE_VERSION=latest
ARG CODEX_VERSION=latest
ARG OPENCODE_VERSION=latest
ARG PI_VERSION=latest
# HERMES_REF is the upstream calver release tag (e.g. v2026.5.7) we install
# against. The companion HERMES_COMMIT is the *commit SHA* that tag resolves
# to (after annotated-tag peeling — `git ls-remote ... refs/tags/<tag>` shows
# the tag-object SHA; `git rev-parse HEAD` after `git clone --branch <tag>`
# returns the underlying commit SHA, and that's what we verify against).
# Bump both together via the README's "Bump procedure".
ARG HERMES_REF=v2026.5.7
ARG HERMES_COMMIT=498bfc7bc12a937621b4215312049b1000726df3
# Pin gh via apt — `latest` resolves to whatever cli.github.com/packages ships
# at build time. Empty means "no version constraint" (apt picks the available
# candidate); the cache-from in the bake file plus the smoke test bound risk.
ARG GH_CLI_VERSION=

ENV DEBIAN_FRONTEND=noninteractive

# Common substrate. ripgrep is required by hermes for fast file search; tini
# is what downstream images use as PID 1 to reap zombies and forward signals.
# The GitHub CLI (`gh`) is baked in here because devenv pods need it as
# git's credential helper for cloning private github.com repos.
# Installing from the upstream cli.github.com apt repo keeps it on a stable
# release cadence — distro-packaged versions of `gh` lag months behind.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates \
      tzdata \
      bash \
      git \
      curl \
      jq \
      rsync \
      tini \
      ripgrep \
      gnupg \
 && curl -fsSL https://deb.nodesource.com/setup_${NODE_VERSION}.x | bash - \
 && apt-get install -y --no-install-recommends nodejs \
 && mkdir -p -m 755 /etc/apt/keyrings \
 && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
      -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
 && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
 && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
      > /etc/apt/sources.list.d/github-cli.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      gh${GH_CLI_VERSION:+=${GH_CLI_VERSION}} \
 && rm -rf /var/lib/apt/lists/*

# Pin each npm-distributed agent CLI. `latest` is acceptable as a build-arg
# default (the bake invocation should pass an explicit version for prod
# rebuilds) — there's no signed-image fingerprint check yet, so semver isn't
# load-bearing for reproducibility, just for human readability.
RUN npm install -g \
      @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION} \
      @openai/codex@${CODEX_VERSION} \
      opencode-ai@${OPENCODE_VERSION} \
      @earendil-works/pi-coding-agent@${PI_VERSION} \
 && npm cache clean --force

# Hermes — when invoked as root on Linux, the official installer lays out
# code under /usr/local/lib/hermes-agent and links the entry point into
# /usr/local/bin/hermes (FHS, same as claude-code / codex). Runtime data
# still lives in $HOME/.hermes per-user. --skip-setup avoids the interactive
# wizard.
#
# Pin strategy: --branch pins to HERMES_REF, which is an upstream calver
# release tag (vYYYY.M.D). The installer does `git clone --branch <ref>`
# which accepts tags as well as branches. We then verify the resulting
# HEAD commit matches HERMES_COMMIT — if upstream ever force-moves the
# tag, the build fails loudly here instead of silently producing an
# unverified image. The tag and the commit SHA must be bumped together.
# See the README's "Bump procedure".
#
# build-essential is required for source builds of any Python wheel hermes'
# uv-driven install pulls in without a manylinux wheel (cryptography, lxml,
# etc. on aarch64). We install it, run the hermes installer, then purge —
# everything happens in a single RUN so the toolchain never makes it into a
# committed layer. apt cleanup is at the end of the same chain.
RUN apt-get update \
 && apt-get install -y --no-install-recommends build-essential \
 && curl -fsSL "https://raw.githubusercontent.com/NousResearch/hermes-agent/${HERMES_REF}/scripts/install.sh" \
      | bash -s -- --branch "${HERMES_REF}" --skip-setup \
 && actual_sha=$(git -C /usr/local/lib/hermes-agent rev-parse HEAD) \
 && { [ "$actual_sha" = "${HERMES_COMMIT}" ] || { echo "HERMES_COMMIT mismatch: expected ${HERMES_COMMIT}, got $actual_sha (tag ${HERMES_REF} may have been moved)" >&2; exit 1; }; } \
 && apt-get purge -y --auto-remove build-essential \
 && rm -rf /var/lib/apt/lists/*

COPY --from=multica-builder /out/multica /usr/local/bin/multica

# Non-root runtime user. Matches the description's UID=1000 default; pass
# UID/GID build args to align with hosts that need a different mapping
# (devenv overlays uid=1000 onto the EFS PVC owner).
RUN groupadd -g "${GID}" agent \
 && useradd -m -d /home/agent -s /bin/bash -u "${UID}" -g agent agent \
 && mkdir -p /home/agent/.multica /home/agent/multica_workspaces \
 && chown -R agent:agent /home/agent

# Smoke-test every tool answers at build time. hermes uses python-fire which
# doesn't surface a stable --version flag; `hermes --help` is the documented
# substitute. Failing the build fast here is cheaper than a runtime miss.
RUN claude --version \
 && codex --version \
 && opencode --version \
 && pi --version \
 && multica --version \
 && gh --version \
 && hermes --help >/dev/null

USER agent
WORKDIR /home/agent

ENV HOME=/home/agent \
    MULTICA_WORKSPACES_ROOT=/home/agent/multica_workspaces

LABEL org.opencontainers.image.source="https://github.com/g2crowd/agentfarm" \
      org.opencontainers.image.description="Shared agent-runtime base (multica + claude + codex + opencode + pi + hermes)" \
      org.opencontainers.image.licenses="UNLICENSED"
