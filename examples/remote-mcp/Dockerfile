# The `multica` CLI is copied from the backend image. Pin MULTICA_BACKEND_IMAGE to the same tag as your server
# (for example ghcr.io/multica-ai/multica-backend:v0.7.1) so the CLI and the API stay in step.
ARG MULTICA_BACKEND_IMAGE=ghcr.io/multica-ai/multica-backend:latest
FROM ${MULTICA_BACKEND_IMAGE} AS multica-cli

FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    MULTICA_BIN=/usr/local/bin/multica \
    MULTICA_MCP_STATE_DIR=/state \
    MULTICA_MCP_HOST=0.0.0.0

RUN groupadd --gid 10001 mcp \
    && useradd --uid 10001 --gid mcp --no-create-home --shell /usr/sbin/nologin mcp \
    && install -d -o mcp -g mcp -m 0700 /state
COPY --from=multica-cli /app/multica /usr/local/bin/multica
WORKDIR /app
COPY multica_mcp/ multica_mcp/
USER 10001:10001
EXPOSE 8101
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
    CMD ["python3", "-I", "-c", "import os, urllib.request; urllib.request.urlopen('http://127.0.0.1:%s/healthz' % os.environ.get('MULTICA_MCP_PORT', '8101'), timeout=3)"]
ENTRYPOINT ["python3", "-m", "multica_mcp"]
