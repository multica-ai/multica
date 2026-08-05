# Multi-user Feishu / Lark OAuth Sidecar for Multica

> A reference integration that lets **each Multica user act with their own Feishu/Lark
> identity** when an agent performs Feishu operations (send messages, read docs, manage
> calendar/wiki) — without storing any user OAuth token in the Multica server database.

This is a community contribution / reference pattern. It is **not** part of Multica core;
it runs as a small companion sidecar next to your Multica daemon.

## Problem

Multica agents often need to act on a user's behalf in an external SaaS (here: Feishu/Lark).
The naive approach — one shared bot token for the whole workspace — means every agent speaks
as the same identity, which breaks per-user attribution, permissions, and audit.

What we want: when user *A* creates an issue and an agent handles it, the Feishu action runs
as *A*'s own Feishu identity; when user *B* does the same, it runs as *B*.

## Approach: HOME-isolation sidecar

The [`lark-cli`](https://www.npmjs.com/package/@larksuite/lark-cli) tool stores OAuth
credentials under `$HOME`. We exploit that: each Multica user gets an isolated `HOME`
directory holding only their own encrypted Feishu token. A small Go sidecar:

1. runs the OAuth **device flow** (RFC 8628) per user and writes the token into that user's
   isolated HOME (`mc-user-<multica_user_id>/`)
2. exposes a tiny HTTP API (`/start`, `/status`, `/complete`, `/force-restart`) + a single-page
   `oauth-ui.html` for scanning the QR code
3. ships a wrapper script (`05_agent_spawn.sh`) the agent calls to run any `lark-cli` command
   **as a specific user**, by pointing `HOME` at that user's isolated dir

No user token ever touches the Multica server DB. Tokens live only on the daemon host,
encrypted, one HOME per user.

```
Multica issue (creator = user A)
   │  agent picks it up, needs to do a Feishu action
   ▼
agent loads the "feishu-as-user" skill (skill/SKILL.md)
   │  1. resolve A's multica_user_id from the issue (fallback chain, see skill)
   │  2. GET sidecar /status?multica_user_id=A   → bound? token valid? lark_open_id?
   │  3. bash scripts/05_agent_spawn.sh A im +messages-send --user-id <open_id> ...
   ▼
05_agent_spawn.sh  →  env HOME=<homes>/mc-user-A  lark-cli <cmd>
   ▼
Feishu API responds as user A's identity
```

## Components

| Path | What |
|------|------|
| `sidecar/` | Go HTTP sidecar (device-flow OAuth + status + force-restart + token-expiry watcher). Self-contained, ~700 LOC, unit-tested. |
| `sidecar/oauth-ui.html` | Single-page QR scan UI (no framework, no external CDN). Auto-detects the current Multica user via `/api/me`. |
| `scripts/01..05_*.sh` | Provisioning + OAuth lifecycle: init → provision per-user HOME → device-flow start → complete → agent spawn wrapper. |
| `skill/SKILL.md` | The agent skill that teaches the LLM the full flow: identity resolution, token check, error classification, anti-hallucination rules. |
| `deploy/*.plist.example` | launchd template (macOS) to run the sidecar as a service. |

## Quick start

> Prerequisites: Go 1.23+, Node (for `lark-cli`), a Feishu/Lark custom app with the scopes
> you need, and a Multica daemon already running on the same host.

```bash
# 1. configure (env vars, all have sane defaults — see scripts headers)
export MULTICA_USER_HOMES_DIR="$HOME/multica-user-homes"
export LARK_BIN="$(command -v lark-cli)"

# 2. one-time host init (copies app config + master key into the homes base dir)
bash scripts/01_mini_init.sh

# 3. build + run the sidecar
cd sidecar && go build -o feishu-oauth-sidecar . && \
  PORT=18090 MAPPING_FILE="$MULTICA_USER_HOMES_DIR/user_mapping.json" ./feishu-oauth-sidecar

# 4. open the OAuth UI, scan, done
#    http://localhost:18090/oauth-ui?multica_user_id=<your-multica-user-id>
```

Wire the skill into any agent that should be able to act as a user in Feishu (assign the
skill in Multica, or drop `skill/SKILL.md` into the agent's skill set). Then the agent can
run Feishu operations on behalf of whoever created the issue.

> **Registering the skill in Multica — set the body via `content`, never as a `SKILL.md` file.**
> When creating the skill, pass the SKILL.md body to the skill's **content** field:
> ```bash
> multica skill create --name feishu-as-user --content "$(cat skill/SKILL.md)"
> ```
> Do **not** add the body as a supporting file named `SKILL.md` (e.g. via
> `multica skill files upsert --path SKILL.md`). The daemon's execenv writes the skill's
> `content` to `<workdir>/.../skills/<slug>/SKILL.md` and then writes each supporting file;
> a supporting file also named `SKILL.md` produces a second write to the same path, which the
> sidecar-manifest guard rejects (`refuse to overwrite pre-existing path`), breaking execenv
> setup for **every** agent assigned the skill. Keep `files` for real bundled assets
> (scripts/templates) only. (Learned the hard way in a self-hosted deployment.)

## The agent skill (identity resolution + safety)

`skill/SKILL.md` is the heart of the integration. Key design points learned in production:

- **Identity fallback chain** — resolve the acting user from, in order: explicit
  `issue.metadata.feishu_user_id` → `creator_id` (if creator is a member) → parent issue
  (if creator is an agent) → `agent.owner_id`. Covers human-created and agent/autopilot-created
  issues alike. Private issues (`metadata.private_to`) require an explicit id and skip all fallback.
- **Authoritative open_id only** — the `--user-id` for a message must come from the sidecar
  `/status` response field `lark_user_open_id`. The skill forbids hand-typing any UUID/`ou_`/`oc_`
  string, because LLMs mis-copy long hex ids (`c↔f`, `0↔o`, `1↔l`) and the Feishu API returns
  `200 OK` + a fake message_id for a non-existent user — silently delivering to the wrong person.
- **Error classification** — distinguishes real OAuth expiry (re-auth) from transient errors
  (retry ≤2) from business errors (report) — so a flaky network never gets misattributed as
  "token expired".

## Security model

- User tokens never leave the daemon host; never stored in the Multica server DB.
- One isolated `HOME` per user; the spawn wrapper scrubs the environment (`env -i` + allowlist)
  so the host's own agent-runtime context can't leak into a user's `lark-cli` invocation.
- Workspace issues are visible to all members by default (Multica's model); the skill enforces
  a `metadata.private_to` convention as an additional guard for sensitive issues.

## Placeholders in this package

All host/identity specifics are placeholders — replace before use:
`multica.example.com`, `relay.example.com`, `RELAY_PUBLIC_IP`, `SERVER_LAN_IP`, `LAN_IP`,
`00000000-0000-0000-0000-000000000000` (a Multica user id), `ou_EXAMPLE_OPEN_ID`,
`oc_EXAMPLE_CHAT_ID`, `your-org`.

## Status

Battle-tested in a small (≤10 user) self-hosted Multica deployment. Contributed as a reference
pattern; adapt the scripts/sidecar to your host and Feishu app before relying on it.
