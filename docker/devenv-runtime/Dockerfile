# syntax=docker/dockerfile:1.7
#
# devenv-runtime — opencode server running on the shared agent-runtime base.
#
# Base: agent-runtime-base (multica, claude, codex, opencode, hermes, pi, gh, acli)
# Adds: kubectl, G2 devenv tooling (bun, vim, uv, arb, oh-my-openagent)

ARG BASE_IMAGE=ghcr.io/g2crowd/agent-runtime-base
ARG BASE_TAG=latest

FROM ${BASE_IMAGE}:${BASE_TAG}

ARG KUBECTL_MINOR=v1.31

USER root

# ── kubectl + system packages for G2 devenv ──────────────────────────────────
RUN curl -fsSL "https://pkgs.k8s.io/core:/stable:/${KUBECTL_MINOR}/deb/Release.key" \
      | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg \
 && chmod go+r /etc/apt/keyrings/kubernetes-apt-keyring.gpg \
 && echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/${KUBECTL_MINOR}/deb/ /" \
      > /etc/apt/sources.list.d/kubernetes.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      kubectl yq \
      vim nano tmux fzf unzip \
      python3 wget netcat-openbsd tree less dnsutils procps openssh-client \
 && rm -rf /var/lib/apt/lists/*

# ── Symlink /home/developer → /home/agent for k8s volume mount compat ────────
RUN ln -sf /home/agent /home/developer

# ── Scripts ──────────────────────────────────────────────────────────────────
COPY --chmod=755 entrypoint.sh /usr/local/bin/entrypoint.sh
COPY --chmod=755 agentfarm-bootstrap.sh /usr/local/bin/agentfarm-bootstrap.sh
COPY --chmod=755 git-sudo /usr/local/bin/git-sudo
COPY --chmod=755 setup /usr/local/bin/setup
COPY --chmod=755 restart /usr/local/bin/restart

COPY agent-templates/ /etc/multica/agent-templates/
RUN chmod -R a+r /etc/multica/agent-templates/

# ── arb — installed at runtime via entrypoint (needs user's GH_TOKEN) ────────

USER agent
ENV HOME=/home/agent

# ── Install Bun ──────────────────────────────────────────────────────────────
RUN curl -fsSL https://bun.sh/install | bash
ENV PATH="/home/agent/.bun/bin:${PATH}"

# ── Install uv (Python package manager — needed for uvx MCP servers) ─────────
RUN curl -fsSL https://astral.sh/uv/install.sh | sh
ENV PATH="/home/agent/.local/bin:${PATH}"

# ── Bake in opencode config files ────────────────────────────────────────────
RUN mkdir -p "${HOME}/.config/opencode"
COPY --chown=agent:agent config/opencode.json "${HOME}/.config/opencode/opencode.json"
COPY --chown=agent:agent config/oh-my-openagent.json "${HOME}/.config/opencode/oh-my-openagent.json"
COPY --chown=agent:agent config/skills/ "${HOME}/.config/opencode/skills/"
COPY --chown=agent:agent config/package.json "${HOME}/.config/opencode/package.json"
COPY --chown=agent:agent config/tui.json "${HOME}/.config/opencode/tui.json"
COPY --chown=agent:agent config/vimrc "${HOME}/.vimrc"
COPY --chown=agent:agent config/AGENTS.md /opt/AGENTS.md

# ── Install oh-my-openagent plugin ───────────────────────────────────────────
RUN cd "${HOME}/.config/opencode" && bun install

# ── Verify ───────────────────────────────────────────────────────────────────
RUN opencode --version && bun --version && gh --version | head -1

ENV OPENCODE_HOST=0.0.0.0 \
    OPENCODE_PORT=4096

EXPOSE 4096

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
