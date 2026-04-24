---
title: Create and configure an agent
description: The minimum fields to create an agent, plus every optional setting — system instructions, environment variables, visibility, concurrency limit, and archiving.
---

import { Callout } from "fumadocs-ui/components/callout";

Creating an [agent](/agents) takes only two things: **a name** and **a choice of [AI coding tool](/providers)**. Everything else is optional — system instructions, model, environment variables, CLI arguments, visibility, concurrency limit — the defaults work fine. Get it running first and tune later; every field can be changed at any time.

## Create an agent

Prerequisite: you already have at least one supported [AI coding tool](/providers) installed on your machine (Claude Code, Codex, etc.) and a [daemon](/daemon-runtimes) running. If you're not there yet, start with [Cloud quickstart](/cloud-quickstart) or [Self-host quickstart](/self-host-quickstart).

Once that's in place, go to the **Agents** page in your workspace and click **+ New**, or use the CLI:

```bash
multica agent create
```

The form has only two required fields: **name** (unique within the workspace) and **runtime** (= pick an AI coding tool). Every other field is covered section by section below.

## Pick an AI coding tool

Each runtime is backed by a specific AI coding tool. Multica supports 10 of them. The most common choices:

| Tool | Good for |
|---|---|
| **Claude Code** | Anthropic's official tool, most complete feature set; **best first pick** |
| **Codex** | OpenAI, the mainstream alternative |
| **Cursor** | Users in the Cursor editor ecosystem |
| **Copilot** | Teams leveraging their GitHub account entitlements |
| **Gemini** | Users in the Google ecosystem |

The other five (Hermes, Kimi, OpenCode, Pi, OpenClaw), along with each tool's full capability matrix (session resume, MCP, skill injection path, model selection), are covered in [AI coding tools comparison](/providers).

## Writing system instructions

**System instructions** (`instructions`) are prepended to every task, telling the agent what role it plays and what rules to follow:

```text
You're a frontend code-review agent. When an issue comes in, read the diff first. Focus only on:
- Styling issues (tailwind class names, box model)
- Accessibility (a11y)
Don't change code — leave suggestions in a comment.
```

When left blank (the default), the agent uses the native behavior of its underlying AI coding tool with no extra constraints.

## Picking a model

Most AI coding tools support model selection (for example, Claude Code lets you pick between Sonnet and Opus). Leave it blank and the tool's own default is used; pick one explicitly and that's what runs. Each tool's supported models are listed in [AI coding tools comparison](/providers).

Changing the model **only applies to new tasks**. Already-dispatched tasks continue with the model that was locked in at dispatch time.

## Custom environment variables (custom_env)

**Custom environment variables** (`custom_env`) let you inject extra env vars at task execution time — typical uses are API keys or switching the upstream endpoint:

```
ANTHROPIC_API_KEY = sk-...
ANTHROPIC_BASE_URL = https://my-proxy.example.com
```

System-critical variables cannot be overridden: `PATH`, `HOME`, `USER`, `SHELL`, `TERM`, `CODEX_HOME`, and any key starting with `MULTICA_*` are silently ignored by the daemon (with a warn log — no error).

<Callout type="warning">
**Values in `custom_env` are stored in plaintext in Multica's server database.** Non-creators and non-workspace-admins can't see the values (the API returns `****`), but they're still visible in database backups and DB audits.

**Don't put high-value secrets in `custom_env`** (production database passwords, root-level tokens, etc.). Use **dedicated, limited-scope credentials** for agents (read-only API keys, single-scope PATs), and rotate them regularly.
</Callout>

## Custom CLI arguments (custom_args)

**Custom CLI arguments** (`custom_args`) is a string array appended one-by-one to the AI coding tool's command line:

```json
["--max-turns", "100", "--append-system-prompt", "always respond in Chinese"]
```

The final command comes out as:

```bash
claude --model <model> --max-turns 100 --append-system-prompt "always respond in Chinese" [...]
```

Arguments are passed as-is, not through a shell (no injection risk), but whether a given flag is recognized is up to the AI coding tool itself — tools differ substantially here.

<Callout type="tip">
`custom_env` and `custom_args` have no hard caps, but in practice **keep each under 10 entries**. Too many makes the command line long, slows startup, and gets harder to maintain.
</Callout>

## Visibility

- **Workspace** (`workspace`) — any member of the workspace can assign it
- **Private** (`private`) — only workspace owners, admins, or the agent's creator can assign it

New agents default to `private`.

**Private does not mean hidden** — every member sees a private agent's name and description in the list, they just can't see sensitive config fields (the values in `custom_env` and MCP config are masked). Full meaning in [Agents → Who can assign an agent](/agents#who-can-assign-an-agent).

## Concurrency limit

**Concurrency limit** (`max_concurrent_tasks`) controls how many tasks this agent can run in parallel at once. The default is **6**. New tasks that hit the cap queue up — they aren't rejected.

This is only the "agent layer" of a two-tier limit — the daemon itself enforces a broader cap (default 20), and whichever is tighter wins. Details in [Daemon and runtimes → How many tasks can run in parallel](/daemon-runtimes#how-many-tasks-can-run-in-parallel).

Changing this value **does not cancel tasks already running** — it only applies to the next task about to be picked up.

## Attaching domain expertise: Skills

A created agent can have **Skills** attached — **knowledge packs** (`SKILL.md` + supporting files) automatically delivered to the AI coding tool at task execution time. You can create a new skill, import from GitHub or ClawHub, or scan one from an existing skill directory on your machine. See [Skills](/skills).

## Archive and restore

Agents you no longer use can be **archived** — they disappear from everyday views, but their historical data (tasks run, comments posted) is fully preserved. **Restore** them anytime to put them back to work.

<Callout type="warning">
**Archiving immediately cancels every unfinished task belonging to the agent** — running, dispatched, and queued tasks are all marked `cancelled` and won't continue. If you have an important task in flight, let it finish before archiving.
</Callout>

Archived agents can't be assigned new tasks.

## Next steps

- [Skills](/skills) — attach knowledge packs to an agent
- [AI coding tools comparison](/providers) — full capability matrix across all 10 tools
- [Assigning issues to agents](/assigning-issues) — put your new agent to work
