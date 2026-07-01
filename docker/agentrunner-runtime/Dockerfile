# syntax=docker/dockerfile:1.7
#
# agentrunner-runtime — headless agent-runner pod for the agentfarm platform.
#
# Base:  agent-runtime-base (multica + claude + codex + opencode + pi + hermes
#        + gh + acli + ripgrep + tini + non-root `agent` user)
# Adds:  yq + openssh-client; agentfarm-bootstrap, agent-templates, entrypoint.

ARG BASE_IMAGE=ghcr.io/g2crowd/agent-runtime-base
ARG BASE_TAG=latest

FROM ${BASE_IMAGE}:${BASE_TAG}

USER root

# ── System packages the agentfarm bootstrap needs ───────────────────────────
#   yq            — agentfarm-bootstrap.sh parses agent-templates/*.yaml
#   openssh-client — entrypoint.sh generates an ed25519 keypair via ssh-keygen
#   openssl       — git-credential-platform-bot signs the GitHub App JWT
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      yq \
      openssh-client \
      openssl \
 && rm -rf /var/lib/apt/lists/*

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
RUN multica --version | head -1

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
