FROM node:20-slim

# Base tooling + Python + fonts + canvas runtime/build deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    git \
    ca-certificates \
    jq \
    unzip \
    python3 \
    python3-pip \
    python3-venv \
    fontconfig \
    fonts-dejavu-core \
    fonts-liberation \
    libcairo2 \
    libpango-1.0-0 \
    libpangocairo-1.0-0 \
    libjpeg62-turbo \
    libgif7 \
    librsvg2-2 \
    build-essential \
    pkg-config \
    libcairo2-dev \
    libpango1.0-dev \
    libjpeg-dev \
    libgif-dev \
    librsvg2-dev \
    && rm -rf /var/lib/apt/lists/*

# Install Claude Code
RUN npm install -g @anthropic-ai/claude-code

# Pre-install commonly used global npm tooling so agent skills don't cold-start install on every task
RUN npm install -g \
    @aws-sdk/client-s3 \
    @aws-sdk/s3-request-presigner \
    canvas

# Install Bun system-wide (required by skills with `#!/usr/bin/env bun` shebang, e.g. r2-upload)
RUN curl -fsSL https://bun.sh/install | BUN_INSTALL=/usr/local bash \
    && ln -sf /usr/local/bin/bun /usr/local/bin/bunx

# Install multica (auto-detects arm64/amd64)
ARG TARGETARCH=amd64
RUN MULTICA_VERSION=0.2.15 && \
    curl -fsSL "https://github.com/multica-ai/multica/releases/download/v${MULTICA_VERSION}/multica-cli-${MULTICA_VERSION}-linux-${TARGETARCH}.tar.gz" \
    | tar -xz -C /usr/local/bin multica && \
    chmod +x /usr/local/bin/multica

# Create non-root user (claude refuses to run as root with bypassPermissions)
RUN useradd -m -s /bin/bash agent
RUN mkdir -p /home/agent/.multica /home/agent/.claude /home/agent/multica_workspaces && \
    chown -R agent:agent /home/agent

# Allow agent scripts to resolve globally-installed npm packages from any CWD
ENV NODE_PATH=/usr/local/lib/node_modules

COPY claude-settings.json /claude-settings.json
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

USER agent
WORKDIR /home/agent

ENTRYPOINT ["/entrypoint.sh"]
