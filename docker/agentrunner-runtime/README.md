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
| `EXTRA_NPX_TOOLS`      | (empty)                         | npm/npx analog of `EXTRA_UV_TOOLS`: space-separated `npm install -g` targets, **always version-pinned** (e.g. `EXTRA_NPX_TOOLS="cowsay@1.6.0"`), installed once at pod boot. See "One-off tool needs" below. Best-effort: a failed install warns and does not block boot. |

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

`entrypoint.sh` also exports `~/.local/bin` (where `uv tool install` links
executables) onto `PATH` before the final `exec` into the daemon, so the
installed binary (e.g. `snow`) resolves by bare name for every agent task —
not just in a login shell.

This is scoped to `uv`-installable Python packages for now. A tool that isn't
on PyPI (or needs OS packages) still needs a base-image change or a
workspace-specific downstream image layer — this hook doesn't attempt to
solve that case.

`EXTRA_NPX_TOOLS` is the same mechanism for npm-distributed CLIs: set it as
an SSM param under the workspace's slug
(`/agentfarm/development/agentrunner/<slug>/EXTRA_NPX_TOOLS`), and
`entrypoint.sh` runs `npm install -g` for each space-separated entry once at
pod boot.

This only works because `agent-runtime-base`'s Dockerfile redirects npm's
global-install prefix to an agent-owned directory
(`NPM_CONFIG_PREFIX=~/.npm-global`, its `bin/` on `PATH`) — see that
Dockerfile's "Redirect npm's global-install prefix" comment and its README's
"Toolchain gotchas" section. Without that redirect, `npm install -g` fails
`EACCES` for the non-root `agent` user, because the npm CLIs baked into this
image (`claude`/`codex`/`opencode`/`pi`) are installed as root before `USER
agent`, leaving npm's default global prefix root-owned.

**Always pin an exact version**, e.g. `EXTRA_NPX_TOOLS="cowsay@1.6.0"` rather
than bare `cowsay` — npm's native pin syntax is `pkg@x.y.z` (no `uv`-style
`==` translation needed). Pinning matters here for the same reason it does
for `EXTRA_UV_TOOLS`: this path is best-effort and untested by CI, so an
unpinned entry can resolve to a different (possibly breaking) release the
next time a pod boots, with no build or review to catch it.

Unlike the `EXTRA_UV_TOOLS` block, `entrypoint.sh` doesn't need its own
`PATH` export for this — `NPM_CONFIG_PREFIX`'s `bin/` dir is already on
`PATH` via the base image's `ENV PATH`, not computed at runtime.

## PEM / private-key secrets

This deployment has no file-secret mechanism — `gitops/base/agent-runtime/external-secret.yaml`
sweeps every SSM param under the workspace's slug (plus `/shared/*`) into the
pod as a plain env var via `envFrom: secretRef`. So a PEM-format secret (a
Snowflake key-pair-auth private key, a GitHub App key, etc.) always arrives as
a raw multi-line env var, never a file, from SSM's perspective.

Most tools that consume a private key accept it via stdin or an fd, and don't
need a file at all — see `git-credential-platform-bot.sh`, which signs the
GitHub App JWT via `openssl ... -sign <(printf '%s' "$GITHUB_APP_PRIVATE_KEY")`
and never touches disk. **Prefer that pattern first** if the tool supports it.

For tools that only accept a real path (`snow --private-key-file`, no
stdin/fd variant), `entrypoint.sh` bridges the gap once per pod boot: any env
var named `<NAME>_PRIVATE_KEY` whose value looks like PEM (`-----BEGIN...`)
is written to `~/.secrets/<NAME>_PRIVATE_KEY` (dir `chmod 700`, file
`chmod 600`) and a sibling `<NAME>_PRIVATE_KEY_FILE` env var is exported
pointing at it. No code change needed to pick up a new key — landing
`SNOWFLAKE_PRIVATE_KEY` (or any other `*_PRIVATE_KEY`) as an SSM param under
the workspace's slug is enough; the file and its `_FILE` var appear
automatically at next pod boot. Example, once `snow` is available (see
`agent-runtime-base`'s README for installing it ad hoc via
`uv tool install snowflake-cli`):

```bash
snow connection add my-conn \
  --account "$SNOWFLAKE_ACCOUNT" \
  --user "$SNOWFLAKE_USER" \
  --private-key-file "$SNOWFLAKE_PRIVATE_KEY_FILE"
```

The materialized file lives under `$HOME/.secrets`, which
`gitops/base/agent-runtime/deployment.yaml` mounts as a tmpfs
(`emptyDir: {medium: Memory}`) volume — the PEM material stays in RAM for the
pod's lifetime, is never written to the node's disk, and doesn't survive a
pod rebuild. It's a bridge for the pod's lifetime, not a persistent secrets
store.

`entrypoint.sh` uses the same `$HOME/.secrets` dir for one non-PEM value:
when `GITHUB_APP_ID` and `GITHUB_APP_PRIVATE_KEY` are both present it writes
`$HOME/.secrets/GITHUB_APP_ID` (plain text — the App ID is a public numeric
identifier, not credential material). Hermes tool-command subprocesses
inherit `GITHUB_APP_PRIVATE_KEY` but not `GITHUB_APP_ID`, so
`gh-platform-bot-wrapper.sh` (`/usr/local/bin/gh`) reads this file as a
fallback whenever its own environment is missing the var, then forwards the
resolved value explicitly to `git-credential-platform-bot.sh` — an env var
already present in the wrapper's own process always wins over the file.

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
