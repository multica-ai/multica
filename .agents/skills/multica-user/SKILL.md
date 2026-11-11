---
name: multica-user
description: Operate a Multica workspace from outside the platform via the multica CLI — list/search issues, read issue detail and agent task runs, post comments with @agent/@squad mentions to delegate work, create/update issues, and supervise or steer running agent tasks (cancel, rerun, supplement). Use when the user mentions Multica, a Multica issue key (e.g. MUL-123), asks to check their Multica tasks/agents, or wants work delegated to or judged from within Multica while working in a local checkout.
---

# Multica (as the user)

Drive a Multica workspace through the `multica` CLI from any local checkout or
terminal — the same CLI the platform's agents use, but logged in as a user.

## Setup (once)

```bash
multica login --token mul_...        # a personal access token
multica workspace list
multica workspace switch <id|slug>   # set the default workspace
```

- Get the CLI from a Multica release, or extract it from the backend image.
- The PAT is created in the Multica web UI (or by an admin) for the account
  the assistant should act as. A dedicated "assistant" member account is
  recommended: comments are attributable, the token is independently
  revocable, and it never conflates the assistant's actions with the user's.
- If the machine also runs a daemon or other identities, isolate this login
  with `--profile assistant` (used in all examples below).

## Workflow

1. **Discover** — `multica issue list` / `multica issue search <text>`.
2. **Understand** — `multica issue get <id>`, `comment list`, `timeline`,
   `children`, and for agent work: `issue runs` + `issue run-messages <runId>`
   (see [references/agent-supervision.md](references/agent-supervision.md)).
   When the issue references a repo, read the actual code locally before
   judging.
3. **Judge & propose** — combine issue context and code into a recommendation;
   present it to the user.
4. **Write back (after user confirmation)** — comments with mentions (see
   [references/mentions.md](references/mentions.md)), issue updates, run
   supervision. Never post, update, cancel, or create anything without
   explicit user confirmation; pure reads are always fine.

## Reference files

- [references/agent-supervision.md](references/agent-supervision.md) — runs,
  run-messages, cancel/rerun/steer/supplement.
- [references/mentions.md](references/mentions.md) — the `mention://` link
  contract: how to @ a person, agent, squad, or issue correctly.
- [references/inbox-wakeups.md](references/inbox-wakeups.md) — inbox (raw API),
  scheduled wakeups, PR linking.
- [references/api.md](references/api.md) — raw REST forms for the few things
  the CLI does not cover; also ground truth for request payloads.

## Conventions

- Prefer `--output json` when parsing; table output is for human eyes. List
  outputs are typically a top-level object (e.g. `{"issues": [...]}`), not a
  bare array — inspect keys with `jq 'keys'` before indexing.
- Comments are Markdown; mention links are inert inside code spans/blocks —
  write mention links in prose.
- Mentions must use real UUIDs (resolved via the list commands); never guess
  from display names.
- When acting as an assistant account, state conclusions as recommendations
  and attribute user decisions as the user's.
