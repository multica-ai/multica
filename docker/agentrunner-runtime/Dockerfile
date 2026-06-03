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
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      yq \
      openssh-client \
 && rm -rf /var/lib/apt/lists/*

# ── Bundled scripts and templates ────────────────────────────────────────────
COPY --chmod=755 entrypoint.sh            /usr/local/bin/entrypoint.sh
COPY --chmod=755 agentfarm-bootstrap.sh   /usr/local/bin/agentfarm-bootstrap.sh

COPY agent-templates/                     /etc/multica/agent-templates/
RUN chmod -R a+r /etc/multica/agent-templates/

USER agent
ENV HOME=/home/agent

# ── Build-time smoke test ────────────────────────────────────────────────────
RUN multica --version | head -1

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
