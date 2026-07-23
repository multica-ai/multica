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
| `AGENTFARM_WORKSPACE_ID` | UUID of the target agentfarm workspace.                                  |
| `ANTHROPIC_API_KEY`    | LiteLLM virtual key for the Anthropic route. Passed to agent templates as the Anthropic key; `agentfarm-bootstrap.sh` injects the base URL. |
| `OPENAI_API_KEY`       | LiteLLM virtual key for the OpenAI route. Passed to agent templates as the OpenAI key; `agentfarm-bootstrap.sh` injects the base URL. |
| `WORKSPACE_SLUG`       | Kubernetes namespace (`metadata.namespace`) — injected via Downward API. Used to name the multica daemon device. |

### Optional

| Var                    | Default                         | Notes                                                                      |
|------------------------|---------------------------------|----------------------------------------------------------------------------|
| `GITHUB_PAT`           | (empty)                         | GitHub PAT (written by gandalf). When present, exported as `GH_TOKEN` and wires `gh` as the git credential helper for HTTPS clones against private repos. |
| `GIT_USER_NAME`        | (empty)                         | Git identity (`user.name`).                                                |
| `GIT_USER_EMAIL`       | (empty)                         | Git identity (`user.email`). Unrelated to Jira/acli auth — see `JIRA_EMAIL` below. |
| `JIRA_EMAIL`           | (empty)                         | Atlassian account email. Together with `JIRA_PAT`, triggers `acli jira auth login` at pod startup AND is forwarded as agent `custom_env` so an invoked agent's isolated runtime — which may resolve a different `$HOME` than the pod-startup shell — can re-authenticate acli itself (AIPLAT-147; see the `acli` skill template). |
| `JIRA_PAT`             | (empty)                         | Atlassian API token. Together with `JIRA_EMAIL`, authenticates acli. Piped via stdin — never on argv. Also forwarded as agent `custom_env`, same reasoning as `JIRA_EMAIL` above. |
| `ATLASSIAN_SITE`       | `https://g2crowd.atlassian.net` | Atlassian Cloud site URL. Override to target a different instance. Also forwarded as agent `custom_env` (only set) alongside `JIRA_EMAIL`/`JIRA_PAT`. |
| `OPENCODE_HOST`        | `0.0.0.0`                       | Bind address for `opencode serve`.                                         |
| `OPENCODE_PORT`        | `4096`                          | Port for `opencode serve`.                                                 |
| `OPENCODE_EXTRA_ARGS`  | (empty)                         | Appended verbatim to `opencode serve`, e.g. `--cors https://...`.          |
| `EXTRA_UV_TOOLS`       | (empty)                         | Space-separated `uv tool install` targets, **always version-pinned** (e.g. `EXTRA_UV_TOOLS="snowflake-cli==3.23.0"`), installed once at pod boot. A one-off/custom-need opt-in for a single workspace without changing the shared image — see "One-off tool needs" below. Best-effort: a failed install warns and does not block boot. |

## One-off tool needs

Most tools belong in `agent-runtime-base` (every workspace gets them, one
build). Some tools are needed by exactly one workspace today (e.g. `snow`,
the Snowflake CLI, for a workspace doing Snowflake-backed reporting) — baking
those into the shared base image by default isn't worth the extra image
weight and attack surface for every other workspace that will never call
them.

`EXTRA_UV_TOOLS` is the current answer for that case: set it as an SSM param
under the workspace's slug
(`/agentfarm/development/agentrunner/<slug>/EXTRA_UV_TOOLS`), and
`entrypoint.sh` runs `uv tool install` for each space-separated entry once at
pod boot — using `uv`, already baked into `agent-runtime-base`, so there's no
system-Python fighting (see `agent-runtime-base`'s README for why `pip`
isn't an option here). No image rebuild, no per-workspace Dockerfile fork.

**Always pin an exact version**, e.g. `EXTRA_UV_TOOLS="snowflake-cli==3.23.0"`
rather than bare `snowflake-cli`. `uv tool install` accepts PEP 508
requirement specifiers, so the operator is `==` (double equals) — a single
`=` is rejected outright:

```
$ uv tool install "snowflake-cli=3.23.0"
error: Failed to parse: `snowflake-cli=3.23.0`
  Caused by: no such comparison operator "=", must be one of ~= == != <= >= < > ===
```

Pinning matters more here than for the base image, precisely because this
path is best-effort and untested by CI: an unpinned entry can resolve to a
different (possibly breaking) release the next time a pod boots, with no
build or review to catch it.

This is scoped to `uv`-installable Python packages for now. A tool that isn't
on PyPI (or needs OS packages) still needs a base-image change or a
workspace-specific downstream image layer — this hook doesn't attempt to
solve that case.

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
  -e AGENTFARM_WORKSPACE_ID=... \
  -e ANTHROPIC_API_KEY=... \
  -e OPENAI_API_KEY=... \
  -e WORKSPACE_SLUG=smoke-test \
  ghcr.io/g2crowd/agentrunner-runtime:dev

curl -fsS http://localhost:4096/doc | jq '.info'

docker rm -f agentrunner-smoke
```
