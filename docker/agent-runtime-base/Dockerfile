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
#   * acli       — Atlassian CLI (apt; acli.atlassian.com) @ ACLI_VERSION
#   * git-ai     — AI authorship tracking (git-ai-project/git-ai) @ GIT_AI_VERSION
#   * uv / uvx   — astral-sh/uv (multi-arch image copy)   @ UV_VERSION
#   * npx        — ships with npm (Node.js LTS install above)
#
# Substrate: debian bookworm-slim, node LTS, git, bash, curl, jq, rsync,
# ripgrep, ca-certificates, tini. Non-root user `agent` at UID:GID 1000:1000.
#
# Build context: this Dockerfile is built with the agentfarm repo root as
# context (see docker-bake.hcl) so the multica builder can COPY directly out
# of server/. Bump procedure and per-tool details: README.md alongside.

ARG NODE_VERSION=22
ARG GO_VERSION=1.26.6
ARG UV_VERSION=latest

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
# Concrete semver, not "dev": the daemon reports this as its CLI version, and
# the quick-create gate rejects anything unparsable as ≥ 0.2.21 ("daemon doesn't
# report a CLI version"). Version is set by docker-bake.hcl; no default here
# so a bare docker build without --build-arg fails loudly rather than silently
# stamping an empty version string.
ARG MULTICA_VERSION
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
# Stage 2 — uv / uvx binaries (multi-arch official image).
#
# ghcr.io/astral-sh/uv publishes a minimal multi-arch image whose only
# content is the two static binaries /uv and /uvx. We COPY them into the
# runtime stage rather than running the curl installer, which keeps the
# install hermetic, avoids a curl→sh pipeline, and works identically on
# amd64 and arm64 without any platform-conditional logic.
# ----------------------------------------------------------------------------
FROM ghcr.io/astral-sh/uv:${UV_VERSION} AS uv-dist

# ----------------------------------------------------------------------------
# Stage 3 — runtime substrate + npm-installable agent CLIs.
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
# HERMES_REF is the upstream calver release tag (e.g. v2026.7.20) we install
# against. The companion HERMES_COMMIT is the *commit SHA* that tag resolves
# to (after annotated-tag peeling — `git ls-remote ... refs/tags/<tag>` shows
# the tag-object SHA; `git rev-parse HEAD` after `git clone --branch <tag>`
# returns the underlying commit SHA, and that's what we verify against).
# Bump both together via the README's "Bump procedure".
ARG HERMES_REF=v2026.7.20
ARG HERMES_COMMIT=3ef6bbd201263d354fd83ec55b3c306ded2eb72a
# Pin gh via apt — `latest` resolves to whatever cli.github.com/packages ships
# at build time. Empty means "no version constraint" (apt picks the available
# candidate); the cache-from in the bake file plus the smoke test bound risk.
ARG GH_CLI_VERSION=
# Pin acli via apt. Empty means "no version constraint" (installs the latest
# release from acli.atlassian.com/linux/deb). Pass a specific version at
# build time only when you need to lock to a known-good release.
ARG ACLI_VERSION=
# Pin git-ai via GitHub Releases. Companion SHA args are the sha256sum of
# the platform-specific binaries — build fails loud if they mismatch.
# Bump all three together; use the SHA256SUMS file from the release assets.
ARG GIT_AI_VERSION=v1.6.16
ARG GIT_AI_SHA_AMD64=66b0bcd2765c509467b4ebd737de8fde669911f8fd7b6b5b28ed7ddbb002f2cf
ARG GIT_AI_SHA_ARM64=583e245fc0cf84cfc3f8f87234582251dbcf86e81717c03364d6410c30ce6627

ENV DEBIAN_FRONTEND=noninteractive

# Common substrate. ripgrep is required by hermes for fast file search; tini
# is what downstream images use as PID 1 to reap zombies and forward signals.
# The GitHub CLI (`gh`) is baked in here because devenv pods need it as
# git's credential helper for cloning private github.com repos.
# Installing from the upstream cli.github.com apt repo keeps it on a stable
# release cadence — distro-packaged versions of `gh` lag months behind.
# The Atlassian CLI (`acli`) is baked in here so agents can drive JIRA from
# the shell. Source: Atlassian's official apt repo at acli.atlassian.com —
# the documented headless-install channel. Auth is the user's problem at
# runtime; nothing is configured here.
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
 && curl -fsSL https://acli.atlassian.com/gpg/public-key.asc \
      | gpg --dearmor -o /etc/apt/keyrings/acli-archive-keyring.gpg \
 && chmod go+r /etc/apt/keyrings/acli-archive-keyring.gpg \
 && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/acli-archive-keyring.gpg] https://acli.atlassian.com/linux/deb stable main" \
      > /etc/apt/sources.list.d/acli.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      gh${GH_CLI_VERSION:+=${GH_CLI_VERSION}} \
      acli${ACLI_VERSION:+=${ACLI_VERSION}} \
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

# Disable pi's startup network operations (update check / package updates /
# install telemetry / model-catalog refresh). We pin PI_VERSION at image
# build time and handle pi versioning separately in the build, so runtime
# update-checks add seconds to boot per pod without value. See AIPLAT-162.
ENV PI_OFFLINE=1

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

# git-ai — AI authorship tracking binary (static, no self-update path).
# Maps dpkg arch to the release binary name, downloads, verifies SHA, installs.
# The ~/.git-ai/bin/ layout (git + git-og symlinks, config.json) is created at
# runtime by entrypoint.sh because ~/.git-ai lives on the work PVC.
RUN arch=$(dpkg --print-architecture) \
 && case "$arch" in \
      amd64) ga_arch="x64";   expected_sha="${GIT_AI_SHA_AMD64}" ;; \
      arm64) ga_arch="arm64"; expected_sha="${GIT_AI_SHA_ARM64}" ;; \
      *) echo "Unsupported arch: $arch" >&2; exit 1 ;; \
    esac \
 && curl -fsSL \
      "https://github.com/git-ai-project/git-ai/releases/download/${GIT_AI_VERSION}/git-ai-linux-${ga_arch}" \
      -o /tmp/git-ai \
 && actual_sha=$(sha256sum /tmp/git-ai | awk '{print $1}') \
 && { [ "$actual_sha" = "$expected_sha" ] \
      || { echo "git-ai SHA mismatch for $arch: expected $expected_sha got $actual_sha" >&2; exit 1; }; } \
 && install -m 755 /tmp/git-ai /usr/local/bin/git-ai \
 && rm /tmp/git-ai

# uv / uvx are static binaries with no self-update path baked in here, so
# they live in the shared root-owned toolchain dir (unlike multica, which
# self-updates and gets its own agent-owned /opt/multica/bin below).
COPY --from=uv-dist /uv /uvx /usr/local/bin/

# Non-root runtime user. Matches the description's UID=1000 default; pass
# UID/GID build args to align with hosts that need a different mapping
# (devenv overlays uid=1000 onto the EFS PVC owner).
RUN groupadd -g "${GID}" agent \
 && useradd -m -d /home/agent -s /bin/bash -u "${UID}" -g agent agent \
 && mkdir -p /home/agent/.multica /home/agent/multica_workspaces /home/agent/.npm-global \
 && chown -R agent:agent /home/agent

# multica lives in its own agent-owned directory, not the shared /usr/local/bin
# toolchain dir. The CLI self-updates by writing a temp file into the binary's
# directory and renaming it over the original (internal/cli/update.go
# UpdateViaDownload) — that needs write on the *directory*, not just the file,
# and the daemon runs as the non-root `agent` user. A dedicated /opt/multica/bin
# grants that write without making the rest of the toolchain dir agent-writable.
# /opt (not $HOME) keeps the baked binary clear of the work PVC mounted into the
# agentrunner at runtime, which can shadow paths under /home/agent. Added to
# PATH below so every `multica` invocation (smoke tests, entrypoint, daemon)
# still resolves it. Placed after useradd so --chown can reference the agent user.
RUN install -d -o agent -g agent -m 755 /opt/multica/bin
COPY --chown=agent:agent --from=multica-builder /out/multica /opt/multica/bin/multica
# Codex task shells prepend their own vendor path and keep /usr/local/bin, but
# may not preserve the image-level /opt/multica/bin PATH entry. Keep the real
# self-updatable binary in /opt/multica/bin and expose a stable shim on the
# common tool path so `multica ...` resolves inside agent terminals.
RUN ln -sf /opt/multica/bin/multica /usr/local/bin/multica
ENV PATH="/opt/multica/bin:${PATH}"

# Redirect npm's global-install prefix for the `agent` user. The bundled npm
# CLIs (claude/codex/opencode/pi) are installed as root above, before `USER
# agent`, which leaves npm's default global prefix (wherever the NodeSource
# package put it — /usr, i.e. /usr/lib/node_modules + /usr/bin) root-owned.
# Any later `npm install -g <pkg>` run as `agent` (an ad hoc task install, or
# agentrunner-runtime's EXTRA_NPX_TOOLS boot hook) hits EACCES against that
# dir. NPM_CONFIG_PREFIX points npm's global installs at an agent-owned dir
# instead — created + chowned above alongside the other agent dirs — so
# `npm install -g` works unmodified for the agent user. Doesn't touch the
# CLIs already baked into /usr/bin above; only affects installs the agent
# user performs itself. bin/ goes on PATH so installed executables resolve by
# bare name the same way /opt/multica/bin does.
ENV NPM_CONFIG_PREFIX=/home/agent/.npm-global
ENV PATH="/home/agent/.npm-global/bin:${PATH}"

# git-ai wraps git by symlinking ~/.git-ai/bin/git → git-ai. Prepend this dir
# so `git` calls go through git-ai for AI attribution tracking. The symlinks
# are created at runtime by entrypoint.sh (dir lives on the work PVC).
ENV PATH="/home/agent/.git-ai/bin:${PATH}"

# G2 LLM proxy defaults — route Anthropic + OpenAI through llmproxy.g2.com.
# Per-CLI override surface (most authoritative one is seeded):
#   claude / opencode / hermes — env vars (hermes consults PROVIDER_REGISTRY
#                                .base_url_env_var in hermes_cli/auth.py).
#   codex / pi                 — config file (no env-var override exists).
# URL suffixes: Anthropic at root (SDK appends /v1/messages); OpenAI at /v1
# (OpenAI-compatible clients expect /v1 in the base). Matches
# add-llm-proxy-ssm-params.sh.
ENV ANTHROPIC_BASE_URL=https://llmproxy.g2.com \
    OPENAI_BASE_URL=https://llmproxy.g2.com/v1

# Pre-create per-CLI config dirs with agent ownership. COPY --chown and
# `install -d` only chown the deepest path component — every intermediate
# parent must be listed explicitly or it stays root-owned, and the CLIs
# write siblings into these dirs at runtime (codex caches, pi state).
# Hermes reads ANTHROPIC_BASE_URL directly and creates ~/.hermes itself.
RUN install -d -o agent -g agent -m 755 \
      /home/agent/.codex \
      /home/agent/.pi \
      /home/agent/.pi/agent \
      /home/agent/.pi/agent/extensions

COPY --chown=agent:agent docker/agent-runtime-base/llmproxy/codex/config.toml   /home/agent/.codex/config.toml
COPY --chown=agent:agent docker/agent-runtime-base/llmproxy/pi/llmproxy.ts      /home/agent/.pi/agent/extensions/llmproxy.ts

# Smoke-test every tool answers at build time. hermes uses python-fire which
# doesn't surface a stable --version flag; `hermes --help` is the documented
# substitute. Failing the build fast here is cheaper than a runtime miss.
RUN claude --version \
 && codex --version \
 && opencode --version \
 && pi --version \
 && multica --version \
 && gh --version \
 && acli --version \
 && hermes --help >/dev/null \
 && git-ai --version \
 && uvx --version \
 && npx --version

USER agent
WORKDIR /home/agent

ENV HOME=/home/agent \
    MULTICA_WORKSPACES_ROOT=/home/agent/multica_workspaces

LABEL org.opencontainers.image.source="https://github.com/g2crowd/agentfarm" \
      org.opencontainers.image.description="Shared agent-runtime base (multica + claude + codex + opencode + pi + hermes + acli + git-ai + uv/uvx + npx)" \
      org.opencontainers.image.licenses="UNLICENSED"
