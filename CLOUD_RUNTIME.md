# Cloud runtime (headless daemon in a container)

Run a Multica **runtime** (the daemon that picks up issues and runs agents) in a
container in the cloud instead of on a developer Mac — so issues keep getting
worked even when laptops are asleep.

This is the same daemon the local "Add runtime" installer (`/install-runtime.sh`)
sets up; the only difference is it runs in a Linux container with no autostart
ceremony. The worker is **private with outbound-only traffic** — it dials the
Multica backend, so it needs no inbound port, DNS, domain, or tunnel.

## What's in the image

`Dockerfile.runtime` builds:

- the cerebro `multica` binary (from `server/`, so it carries our patches — not
  the upstream GitHub release),
- the **Claude Code CLI** (`@anthropic-ai/claude-code`) that the daemon shells
  out to,
- `git` (per-task repo checkouts), `ripgrep`, `curl`, `jq`,
- a non-root `multica` user.

Entrypoint: `docker/runtime-entrypoint.sh` — provisions config, wires git auth,
then runs `multica daemon start --profile local --foreground`.

## Configuration (environment variables)

Provide credentials one of two ways. **A long-lived daemon token is recommended**
for a server because it survives container restarts; a setup token is single-use
with a 30-minute TTL and will fail on the next restart.

| Variable | Required | Notes |
|---|---|---|
| `CLAUDE_AUTH_MODE` | no | `api_key` (use `ANTHROPIC_API_KEY`) or `max` (use a persisted Claude Max/OAuth login). Defaults to `api_key` when a key is set, else `max`. See the auth note below. |
| `ANTHROPIC_API_KEY` | for api_key mode | Auth for Claude Code in `api_key` mode. |
| `MULTICA_SERVER_URL` | yes | Backend API base URL the daemon connects to. |
| `MULTICA_DAEMON_TOKEN` | one path | Long-lived daemon token (a Multica PAT, `mul_…`). Pair with `MULTICA_WORKSPACE_ID`. |
| `MULTICA_WORKSPACE_ID` | with token | Workspace the runtime serves. |
| `MULTICA_SETUP_TOKEN` | other path | One-time setup token (`mst_…`) from the "Add runtime" dialog; exchanged on first boot. |
| `GITHUB_TOKEN` | for private repos | Lets the daemon clone private Firtal repos into task worktrees. |
| `MULTICA_DEVICE_LABEL` | no | Name shown in the runtimes list (default `cloud-runtime`). |
| `GIT_AUTHOR_NAME` / `GIT_AUTHOR_EMAIL` | no | Committer identity for agent commits. |

**Secrets come from Infisical → the platform's env, never committed** (see the
lesson in FIR-2298). On Sliplane, set these as service environment variables
sourced from Infisical; do not bake them into the image or this repo.

## Volumes

| Mount | Why |
|---|---|
| `/home/multica/.multica` | Config + daemon token. Persist it so restarts skip re-provisioning. |
| `/home/multica/workspaces` | Per-task git worktrees. Persist to reuse checkouts and resume sessions; safe to treat as ephemeral cache otherwise. |

## Deploy on Sliplane (outline)

1. Point Sliplane at this repo, Dockerfile path `Dockerfile.runtime`.
2. **Expose Service = OFF** — there is nothing to expose; the runtime is
   outbound-only.
3. Set the env vars above from Infisical.
4. Attach the two persistent volumes.
5. Deploy. Confirm it comes online in the workspace's **Runtimes** list and that
   `multica daemon status` (via Sliplane shell) shows `running` with `claude`
   detected.

## Honest note on Claude auth

The runtime supports **both** auth paths; choose per runtime with `CLAUDE_AUTH_MODE`:

- **`api_key`** (recommended) — set `ANTHROPIC_API_KEY`. The clean path: no manual
  login step, no terms-of-service gray zone. The entrypoint sets
  `MULTICA_RUNTIME_ALLOW_API_KEY=1`, which opts this runtime out of the
  OAuth-only default (the cerebro `claude-oauth-strips-api-key` patch) so the key
  reaches Claude Code. Only affects the runtime where it is set — other runtimes
  and prod are unchanged.
- **`max`** — log in once with a personal **Claude Max** subscription (run `claude`
  interactively in the container, persist `~/.claude` on the config volume).
  Running a personal plan on an always-on server is a gray zone under Anthropic's
  terms. No API key is used in this mode.

## Note on autonomy

The daemon runs Claude Code with `--permission-mode bypassPermissions` (the same
as a local daemon). A cloud runtime therefore executes agent actions
autonomously, 24/7, under the identity of the token's user. Scope the token's
user and workspace deliberately.
