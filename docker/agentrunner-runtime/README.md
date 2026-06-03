# agentrunner-runtime

Headless agent-runner image for the agentfarm platform. Layers on top of
`ghcr.io/g2crowd/agent-runtime-base` — no devenv tooling included.

## Image

`ghcr.io/g2crowd/agentrunner-runtime:<tag>` — multi-arch (`linux/amd64` +
`linux/arm64`). Inherits every tool from the base; layers on bun, uv, yq,
netcat-openbsd, the agentfarm bootstrap, agent templates, and opencode config.

## Runtime env

### Mandatory (agentrunner-runtime always runs in agentfarm mode)

| Var                    | Notes                                                                      |
|------------------------|----------------------------------------------------------------------------|
| `MULTICA_PAT`          | Agentfarm service account PAT. Used by `agentfarm-bootstrap.sh` to log in the multica daemon and register the claude runtime. |
| `MULTICA_WORKSPACE_ID` | UUID of the target agentfarm workspace.                                    |
| `LITELLM_API_KEY`      | LiteLLM virtual key. Written to `auth.json` for opencode and passed to agent templates as the Anthropic/OpenAI key. |
| `WORKSPACE_SLUG`       | Kubernetes namespace (`metadata.namespace`) — injected via Downward API. Used to name the multica daemon device. |

### Optional

| Var                    | Default                         | Notes                                                                      |
|------------------------|---------------------------------|----------------------------------------------------------------------------|
| `GH_TOKEN`             | (empty)                         | GitHub PAT. When present, wires `gh` as the git credential helper for HTTPS clones against private repos. |
| `GIT_USER_NAME`        | (empty)                         | Git identity (`user.name`).                                                |
| `GIT_USER_EMAIL`       | (empty)                         | Git identity (`user.email`). Together with `JIRA_PAT`, triggers `acli jira auth login`. |
| `JIRA_PAT`             | (empty)                         | Atlassian API token. Together with `GIT_USER_EMAIL`, authenticates acli. Piped via stdin — never on argv. |
| `ATLASSIAN_SITE`       | `https://g2crowd.atlassian.net` | Atlassian Cloud site URL. Override to target a different instance.         |
| `OPENCODE_HOST`        | `0.0.0.0`                       | Bind address for `opencode serve`.                                         |
| `OPENCODE_PORT`        | `4096`                          | Port for `opencode serve`.                                                 |
| `OPENCODE_EXTRA_ARGS`  | (empty)                         | Appended verbatim to `opencode serve`, e.g. `--cors https://...`.          |

## Smoke test

```bash
# Build locally
docker buildx bake -f docker/agentrunner-runtime/docker-bake.hcl default --load

# Verify tools are present (no agentfarm envs needed for this check)
docker run --rm --entrypoint sh ghcr.io/g2crowd/agentrunner-runtime:dev \
  -c 'opencode --version && bun --version && multica --version'

# Verify the HTTP API answers (requires agentfarm envs for full bootstrap)
docker run --rm -d --name agentrunner-smoke \
  -p 4096:4096 \
  -e MULTICA_PAT=... \
  -e MULTICA_WORKSPACE_ID=... \
  -e LITELLM_API_KEY=... \
  -e WORKSPACE_SLUG=smoke-test \
  ghcr.io/g2crowd/agentrunner-runtime:dev

curl -fsS http://localhost:4096/doc | jq '.info'

docker rm -f agentrunner-smoke
```
