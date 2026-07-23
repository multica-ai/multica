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
- the **Claude Code CLI**, pinned **Cursor Agent CLI**, pinned **Pi Coding
  Agent**, and pinned **Hermes Agent** that the daemon shells out to,
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
| `CURSOR_API_KEY` | for Cursor Agent | API key for headless Cursor Agent runs. Store it as a secret in the runtime environment. |
| `MULTICA_SERVER_URL` | yes | Backend API base URL the daemon connects to. |
| `MULTICA_DAEMON_TOKEN` | one path | Long-lived daemon token (a Multica PAT, `mul_…`). Pair with `MULTICA_WORKSPACE_ID`. |
| `MULTICA_WORKSPACE_ID` | with token | Workspace the runtime serves. |
| `MULTICA_SETUP_TOKEN` | other path | One-time setup token (`mst_…`) from the "Add runtime" dialog; exchanged on first boot. |
| `GITHUB_TOKEN` | for private repos | Lets the daemon clone private Firtal repos into task worktrees. |
| `MULTICA_PI_MODEL` | no | Primary Pi model. Defaults to `openai-codex/gpt-5.3-codex` (ChatGPT Pro). |
| `FIRTAL_REGISTRY_URL` | for backup | Firtal Data Registry base URL used by the managed AI Gateway backup. |
| `FIRTAL_REGISTRY_KEY` | for backup | Infisical-backed service credential for the AI Gateway. It is never written into Pi or Hermes config. |
| `FIRTAL_REGISTRY_MODEL` | no | Backup model. Defaults to the managed `claude-sonnet-5`. |
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

Pi stores settings and OAuth refresh credentials under
`/home/multica/.multica/pi` through `PI_CODING_AGENT_DIR`. Hermes stores the
same classes of state under `/home/multica/.multica/hermes` through
`HERMES_HOME`. Both therefore survive image rebuilds on the existing config
volume. Never print, copy into git, or attach either `auth.json` file to an
issue.

## Deploy on Sliplane (outline)

1. Point Sliplane at this repo, Dockerfile path `Dockerfile.runtime`.
2. **Expose Service = OFF** — there is nothing to expose; the runtime is
   outbound-only.
3. Set the env vars above from Infisical.
4. Attach the two persistent volumes.
5. Deploy. Confirm it comes online in the workspace's **Runtimes** list and that
   `multica daemon status` (via Sliplane shell) shows `running` with `claude`,
   `cursor`, and `pi` detected.

## ChatGPT Pro primary, Firtal AI Gateway backup

Both Pi and Hermes support the `openai-codex` provider through a ChatGPT Plus or
Pro subscription. After the first image deployment, open a shell for the
private runtime service:

1. Run `pi`, enter `/login`, select **OpenAI Codex**, and complete the URL/code
   flow in the account owner's browser.
2. Run `hermes model`, select **OpenAI Codex**, and complete its device-code
   login with the same ChatGPT Pro account.

Each agent stores its own OAuth refresh credential on the persistent volume and
renews short-lived access automatically. There is no separate permanent key to
copy: the persisted, automatically rotated refresh credential is the
long-lived login mechanism.

At startup, the entrypoint installs the Infisical-backed Firtal AI Gateway as a
backup in both agents. Hermes uses its native fallback chain. Multica retries a
failed Pi call through the gateway only before Pi has emitted text or started a
tool; it never retries after a possible side effect. The gateway credential is
read from `FIRTAL_REGISTRY_KEY` at runtime and is not persisted in agent config.

The Pi provider extension asks the gateway which models the key may call
(`GET /api/ai/proxy/v1/models`) and registers every chat model it returns, so a
grant added on **AI Proxy → Permissions** in the registry shows up as
`firtal-gateway/<model-id>` in Pi after the next task — no image change. Grants
are the only control surface: embeddings are filtered out, and
`FIRTAL_REGISTRY_MODEL` stays registered even when the gateway is unreachable,
because the Pi retry path always targets that model.

Do not replace this flow with `OPENAI_API_KEY`: that is a separate API-billing
identity and does not use the requested ChatGPT subscription.

## Replace a compromised ChatGPT OAuth login

Deleting a local credential is not sufficient revocation: Pi's `/logout` only
removes its local copy, and OpenAI documents that **Log out all** does not manage
[Codex CLI or third-party **Sign in with ChatGPT** sessions](https://help.openai.com/en/articles/20001257-managing-active-sessions-in-chatgpt/).
Use this sequence:

1. In ChatGPT, open **Settings → Connectors** and disconnect the relevant Codex
   CLI / **Sign in with ChatGPT** authorization if it is shown.
2. In the service shell, remove the old local credentials with Pi `/logout`
   for `openai-codex` and `hermes auth logout openai-codex`.
3. If an API key was generated by the authorization and is visible in the
   OpenAI API dashboard, revoke that key separately. OpenAI confirms that
   [disconnecting OAuth does not revoke an auto-generated API key](https://help.openai.com/en/articles/11381614-api-codex-cli-and-sign-in-with-chatgpt).
4. Restart the service, then repeat both ChatGPT Pro login flows above. Never
   paste the old credential into a comment, log, command output, or support
   ticket.

## Release canary

Run the same check after deployment and again after one service restart:

```sh
runtime-agent-canary all
```

`PI PRIMARY PASS` proves the ChatGPT Pro path works. If the primary path is
temporarily unavailable, `PI FALLBACK PASS` proves continuity through Firtal AI
Gateway. `HERMES PASS` proves Hermes completed through its configured primary or
native fallback. Do not move Mette, Jack, or Michael to the cloud runtime until
both post-deploy and post-restart checks exit successfully and the provider
route shown by the service diagnostics has been reviewed.

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
