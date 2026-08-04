# syntax=docker/dockerfile:1.7
#
# agentrunner-runtime — headless agent-runner pod for the agentfarm platform.
#
# Base:  agent-runtime-base (multica + claude + codex + opencode + pi + hermes
#        + gh + acli + ripgrep + tini + non-root `agent` user)
# Adds:  yq + openssh-client + AWS CLI; agentfarm-bootstrap, agent-templates,
#        entrypoint.

ARG BASE_IMAGE=ghcr.io/g2crowd/agent-runtime-base
ARG BASE_TAG=latest

FROM ${BASE_IMAGE}:${BASE_TAG}

USER root

# ── System packages the agentfarm bootstrap needs ───────────────────────────
#   yq            — agentfarm-bootstrap.sh parses agent-templates/*.yaml
#   openssh-client — entrypoint.sh generates an ed25519 keypair via ssh-keygen
#   openssl       — git-credential-platform-bot signs the GitHub App JWT
#   unzip         — unpacks the AWS CLI installer below
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      yq \
      openssh-client \
      openssl \
      unzip \
 && rm -rf /var/lib/apt/lists/*

# ── AWS CLI v2 ───────────────────────────────────────────────────────────────
# How agents read and write S3 from a session (AIPLAT-169). The pod resolves an
# IRSA role via AWS_ROLE_ARN / AWS_WEB_IDENTITY_TOKEN_FILE, so the CLI needs no
# credential configuration of its own.
#
# Installed from the official bundle rather than apt: Debian ships awscli v1,
# which predates several of the credential-chain behaviours IRSA relies on.
# TARGETARCH is supplied by BuildKit and differs from the name AWS uses for the
# same architecture, hence the mapping.
ARG TARGETARCH
RUN set -eux; \
    case "${TARGETARCH:-amd64}" in \
      amd64) awscli_arch=x86_64 ;; \
      arm64) awscli_arch=aarch64 ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL --retry 3 "https://awscli.amazonaws.com/awscli-exe-linux-${awscli_arch}.zip" -o /tmp/awscliv2.zip; \
    unzip -q /tmp/awscliv2.zip -d /tmp; \
    /tmp/aws/install; \
    rm -rf /tmp/awscliv2.zip /tmp/aws

# ── Bundled scripts and templates ────────────────────────────────────────────
COPY --chmod=755 docker/agentrunner-runtime/entrypoint.sh                   /usr/local/bin/entrypoint.sh
COPY --chmod=755 docker/agentrunner-runtime/agentfarm-bootstrap.sh          /usr/local/bin/agentfarm-bootstrap.sh
COPY --chmod=755 docker/agentrunner-runtime/git-credential-platform-bot.sh  /usr/local/bin/git-credential-platform-bot
# Shadows the real gh (/usr/bin/gh) on PATH to inject a fresh token per call.
COPY --chmod=755 docker/agentrunner-runtime/gh-platform-bot-wrapper.sh      /usr/local/bin/gh

COPY docker/agentrunner-runtime/agent-templates/  /etc/multica/agent-templates/
RUN chmod -R a+r /etc/multica/agent-templates/

COPY docker/agentrunner-runtime/skill-templates/  /etc/multica/skill-templates/
RUN chmod -R a+r /etc/multica/skill-templates/

# ── Smoke test script (lives in scripts/ alongside other agentfarm scripts) ──
COPY --chmod=755 scripts/smoke-test-agentrunner.sh /opt/agentfarm/scripts/smoke-test-agentrunner.sh

USER agent
ENV HOME=/home/agent

# ── Build-time smoke test ────────────────────────────────────────────────────
RUN multica --version | head -1 \
 && aws --version

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
