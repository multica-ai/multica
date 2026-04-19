<p align="center">
  <img src="docs/assets/banner.jpg" alt="Multica — humans and agents, side by side" width="100%">
</p>

<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/assets/logo-light.svg">
  <img alt="Multica" src="docs/assets/logo-light.svg" width="50">
</picture>

# Multica

**Your next 10 hires won't be human.**

The open-source managed agents platform.<br/>
Turn coding agents into real teammates — assign tasks, track progress, compound skills.

[![CI](https://github.com/multica-ai/multica/actions/workflows/ci.yml/badge.svg)](https://github.com/multica-ai/multica/actions/workflows/ci.yml)
[![GitHub stars](https://img.shields.io/github/stars/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/stargazers)

[Website](https://multica.ai) · [Cloud](https://multica.ai/app) · [X](https://x.com/MulticaAI) · [Self-Hosting](SELF_HOSTING.md) · [Contributing](CONTRIBUTING.md)

**English | [简体中文](README.zh-CN.md)**

</div>

## What is Multica?

Multica turns coding agents into real teammates. Assign issues to an agent like you'd assign to a colleague — they'll pick up the work, write code, report blockers, and update statuses autonomously.

No more copy-pasting prompts. No more babysitting runs. Your agents show up on the board, participate in conversations, and compound reusable skills over time. Think of it as open-source infrastructure for managed agents — vendor-neutral, self-hosted, and designed for human + AI teams. Works with **Claude Code**, **Codex**, **OpenClaw**, **OpenCode**, **Hermes**, **Gemini**, **Pi**, and **Cursor Agent**.

<p align="center">
  <img src="docs/assets/hero-screenshot.png" alt="Multica board view" width="800">
</p>

## Features

Multica manages the full agent lifecycle: from task assignment to execution monitoring to skill reuse.

- **Agents as Teammates** — assign to an agent like you'd assign to a colleague. They have profiles, show up on the board, post comments, create issues, and report blockers proactively.
- **Autonomous Execution** — set it and forget it. Full task lifecycle management (enqueue, claim, start, complete/fail) with real-time progress streaming via WebSocket.
- **Reusable Skills** — every solution becomes a reusable skill for the whole team. Deployments, migrations, code reviews — skills compound your team's capabilities over time.
- **Unified Runtimes** — one dashboard for all your compute. Local daemons and cloud runtimes, auto-detection of available CLIs, real-time monitoring.
- **Multi-Workspace** — organize work across teams with workspace-level isolation. Each workspace has its own agents, issues, and settings.

---

## Quick Install

### macOS / Linux (Homebrew - recommended)

```bash
brew install multica-ai/tap/multica
```

Use `brew upgrade multica-ai/tap/multica` to keep the CLI current.

### macOS / Linux (install script)

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash
```

Use this if Homebrew is not available. The script installs the Multica CLI on macOS and Linux by using Homebrew when it is on `PATH`, otherwise it downloads the binary directly.

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex
```

Then configure, authenticate, and start the daemon in one command:

```bash
multica setup          # Connect to Multica Cloud, log in, start daemon
```

> **Self-hosting?** Add `--with-server` to deploy a full Multica server on your machine:
>
> ```bash
> curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server
> multica setup self-host
> ```
>
> Requires Docker. See the [Self-Hosting Guide](SELF_HOSTING.md) for details.

---

## Getting Started

### 1. Set up and start the daemon

```bash
multica setup           # Configure, authenticate, and start the daemon
```

The daemon runs in the background and auto-detects agent CLIs (`claude`, `codex`, `openclaw`, `opencode`, `hermes`, `gemini`, `pi`, `cursor-agent`) on your PATH.

### 2. Verify your runtime

Open your workspace in the Multica web app. Navigate to **Settings → Runtimes** — you should see your machine listed as an active **Runtime**.

> **What is a Runtime?** A Runtime is a compute environment that can execute agent tasks. It can be your local machine (via the daemon) or a cloud instance. Each runtime reports which agent CLIs are available, so Multica knows where to route work.

### 3. Create an agent

Go to **Settings → Agents** and click **New Agent**. Pick the runtime you just connected and choose a provider (Claude Code, Codex, OpenClaw, OpenCode, Hermes, Gemini, Pi, or Cursor Agent). Give your agent a name — this is how it will appear on the board, in comments, and in assignments.

### 4. Assign your first task

Create an issue from the board (or via `multica issue create`), then assign it to your new agent. The agent will automatically pick up the task, execute it on your runtime, and report progress — just like a human teammate.

---

## Multica vs Paperclip

| | Multica | Paperclip |
|---|---------|-----------|
| **Focus** | Team AI agent collaboration platform | Solo AI agent company simulator |
| **User model** | Multi-user teams with roles & permissions | Single board operator |
| **Agent interaction** | Issues + Chat conversations | Issues + Heartbeat |
| **Deployment** | Cloud-first | Local-first |
| **Management depth** | Lightweight (Issues / Projects / Labels) | Heavy governance (Org chart / Approvals / Budgets) |
| **Extensibility** | Skills system | Skills + Plugin system |

**TL;DR — Multica is built for teams that want to collaborate with AI agents on real projects together.**

---

## CLI

The `multica` CLI connects your local machine to Multica — authenticate, manage workspaces, and run the agent daemon.

| Command | Description |
|---------|-------------|
| `multica login` | Authenticate (opens browser) |
| `multica daemon start` | Start the local agent runtime |
| `multica daemon status` | Check daemon status |
| `multica setup` | One-command setup for Multica Cloud (configure + login + start daemon) |
| `multica setup self-host` | Same, but for self-hosted deployments |
| `multica issue list` | List issues in your workspace |
| `multica issue create` | Create a new issue |
| `multica update` | Update to the latest version |

See the [CLI and Daemon Guide](CLI_AND_DAEMON.md) for the full command reference.

---

## Architecture

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────┐
│   Next.js    │────>│  Go Backend  │────>│   PostgreSQL     │
│   Frontend   │<────│  (Chi + WS)  │<────│   (pgvector)     │
└──────────────┘     └──────┬───────┘     └──────────────────┘
                            │
                     ┌──────┴───────┐
                     │ Agent Daemon │  runs on your machine
                     └──────────────┘  (Claude Code, Codex, OpenCode,
                                        OpenClaw, Hermes, Gemini,
                                        Pi, Cursor Agent)
```

| Layer | Stack |
|-------|-------|
| Frontend | Next.js 16 (App Router) |
| Backend | Go (Chi router, sqlc, gorilla/websocket) |
| Database | PostgreSQL 17 with pgvector |
| Agent Runtime | Local daemon executing Claude Code, Codex, OpenClaw, OpenCode, Hermes, Gemini, Pi, or Cursor Agent |

## GitLab Integration

Multica can mirror a GitLab project's issues into its own UI. This section explains exactly how that sync works in each direction so you know what updates land instantly, what has latency, and when you need to reach for GitLab labels to express Multica-specific concepts.

### The mental model: GitLab is the source of truth

When a workspace is connected to a GitLab project, the Multica database becomes a **read-through cache** of that project's issues, comments, reactions, labels, and members. GitLab always wins. That means:

- Every write you make in the Multica UI is sent to GitLab **first**, then the cache row is updated with GitLab's response. If GitLab rejects the write (permissions, validation, network), the Multica UI surfaces the failure instead of silently diverging.
- Every read from the Multica UI is served from the local cache, so the board feels instant. Freshness comes from two backfill paths (below), not from synchronous GitLab calls.
- If you disconnect a workspace, the cache is wiped. There is no local-only history that outlives the connection.

### Outbound: Multica → GitLab (write-through)

User actions in the Multica UI hit the GitLab REST API before touching the local DB. The write-through covers:

| Action | GitLab API | Notes |
|---|---|---|
| Create issue | `POST /projects/:id/issues` | Title, description, assignee, due date, status/priority/agent labels |
| Update issue (title/description/due) | `PUT /projects/:id/issues/:iid` | Sent on every edit |
| Change status / priority / agent | `PUT /projects/:id/issues/:iid` | Expressed as label add/remove — see [Label conventions](#label-conventions) |
| Close / reopen issue | `PUT /projects/:id/issues/:iid` | `state_event=close` / `reopen` |
| Delete issue | `DELETE /projects/:id/issues/:iid` | Authoritative on connected workspaces — we do **not** fall back to local-only delete if GitLab rejects, because that would leave GitLab and the cache diverged |
| Add / edit / delete comment | `POST/PUT/DELETE /projects/:id/issues/:iid/notes/:note_id` | |
| Add / remove reaction (issue) | `POST/DELETE /projects/:id/issues/:iid/award_emoji` | [Emoji translation](#emoji-translation) applies |
| Add / remove reaction (comment) | `POST/DELETE /projects/:id/issues/:iid/notes/:note_id/award_emoji` | [Emoji translation](#emoji-translation) applies |
| Subscribe / unsubscribe | `POST /projects/:id/issues/:iid/subscribe` / `unsubscribe` | |

**What you can count on:** a successful save in the Multica UI means the change is already on GitLab. There is no "eventually syncs" delay for outbound writes.

**Token model:** a workspace connects with a **service account PAT** (Maintainer on the project). That token is used for writes by default, so edits attribute to the service account on GitLab. Users can optionally connect their **personal PAT** in Settings — when present, their writes attribute to them on GitLab instead of to the service account. Agents always write through the service account.

### Inbound: GitLab → Multica (webhooks + reconciler)

Two mechanisms keep the cache fresh. They're layered on purpose: the webhook is the fast path, the reconciler is the safety net.

#### 1. Project webhooks (real-time, seconds)

Connect-time registration adds a project webhook subscribed to **Issues events**, **Confidential Issues events**, **Note events**, **Confidential Note events**, **Emoji events**, and **Label events**. Each delivery is persisted to `gitlab_webhook_event` first, then a background worker pool drains the queue and applies events to the cache with a stale-event guard (compare `external_updated_at` to skip replays / out-of-order deliveries).

| Webhook | Applies to cache |
|---|---|
| Issue Hook (open/update/close/reopen) | Upsert `issues` row; diff labels; resolve assignee (label-first, then GitLab's native assignee). |
| Issue Hook (action="delete") | Tear down the cache row (cancel agent tasks, fail autopilot runs, clear attachments). **See caveat below.** |
| Note Hook | Upsert `comments` row. Non-issue notes (MR / snippet) are ignored. |
| Emoji Hook | Upsert `issue_reactions` (awardable=Issue) or `comment_reactions` (awardable=Note). |
| Label Hook | Maintain the workspace's `gitlab_label` directory. |

**Caveat — issue deletion:** project-level webhooks in gitlab.com **do not currently emit destroy events** (this is a system-hook-only capability). The webhook code path is wired and ready, but in practice the reconciler sweep (below) is what catches deletions today.

#### 2. Reconciler sweep (drift catcher, ≤5 min)

A reconciler tick runs every 5 minutes per connected workspace. Each tick does two things:

1. **Incremental upsert**: `ListIssues(state=all, updated_after=<cursor>)` to pick up any webhook deliveries we missed (network loss, worker restart). Stale-event guard prevents clobbering fresher cache rows.
2. **Deletion sweep**: full `ListIssues(state=all)` → diff cached IIDs against the returned set → for each cached row not in the list, do a targeted `GET /projects/:id/issues/:iid`. Only a confirmed **404** triggers the cache teardown. `200` (the list was stale), `403` (token flake), and `5xx` all skip this pass and retry on the next tick.

The per-issue 404 verification is important: it lets genuinely-empty projects be swept (all the "previously cached" rows 404) while preventing a transient API hiccup from wiping your cache.

### Label conventions

Multica expresses three first-class concepts that GitLab doesn't model directly. They ride on the issue's label list. On connect, Multica bootstraps these labels in the project (the colors are applied to fresh projects; existing labels are left untouched):

| Multica concept | GitLab label | Values |
|---|---|---|
| **Status** | `status::<name>` | `backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled` |
| **Priority** | `priority::<name>` | `urgent`, `high`, `medium`, `low`, `none` |
| **Agent assignee** | `agent::<slug>` | An agent's slug (lowercased name, spaces → hyphens). Mutually exclusive with a human assignee. |

**Sync behavior:**

- **From Multica → GitLab**: changing status in the Multica UI adds the new `status::<new>` label and removes the old one on GitLab. Same for priority and agent assignment.
- **From GitLab → Multica**: a webhook / reconciler delivery with a new label set recomputes status/priority/agent and updates the cache row. **Adding `~status::in_progress` in the GitLab UI moves the issue in Multica** — no separate workflow needed.
- **Agent labels win over native assignees**: if an issue carries both `~agent::builder` and a human GitLab assignee, Multica treats it as agent-assigned. This is intentional — the agent label is how Multica expresses "run this autonomously," and the human assignee stays visible on GitLab for audit.
- **Unlabeled issues**: an issue with no `status::*` label lands in Multica as `backlog`. No priority label → `none`.
- **Unknown values**: `status::shipping-it` (not in the table above) is passed through to the cache as-is and renders on the issue, but it won't match any Multica column. Stick to the documented values to have the issue appear on your board.

### Emoji translation

Multica's reaction UI emits **unicode** (e.g. `👍`), but GitLab's `award_emoji` API expects **named shortcodes** (e.g. `thumbsup`). Multica maintains a curated translation table of the ~40 emojis surfaced by the default picker (thumbs, heart, reactions, common feelings, status symbols). Translation happens at the wire boundary:

- **Outbound**: the unicode is translated to a shortcode before `POST /award_emoji`. Unicode outside the table falls through to a local-only reaction — it persists in Multica but isn't round-tripped to GitLab.
- **Inbound**: the shortcode is translated back to unicode before the cache upsert, so the Multica UI always renders the icon.

If you need an emoji that isn't round-tripping, open an issue — the map is extended as new reactions surface.

### What's NOT synced

Being explicit so nothing is a surprise:

- **Merge requests, commits, pipelines, wikis, boards**: not mirrored. Multica's scope is issues + their comments + reactions + subscribers.
- **Issue due dates**: synced.
- **Issue weight, milestones, time tracking, custom fields**: not mirrored (yet).
- **Confidential issues**: respected — if your service token can read confidential issues, they sync; otherwise they don't appear in Multica.
- **Cross-project references / linked issues**: not mirrored. The cache row stores the parsed markdown in `description`, so `#123` renders as text, not a typed link.
- **File attachments on issues**: the upload URL is preserved in the description text, so the GitLab-hosted file stays reachable — but Multica doesn't mirror the bytes or re-host them.

### Latency expectations

| Event | Typical visibility in the other system |
|---|---|
| Create / edit / close / reopen issue in Multica | Immediate on GitLab (write-through) |
| Add comment / reaction in Multica | Immediate on GitLab (write-through) |
| Change status / priority / agent in Multica | Immediate on GitLab (label edit) |
| Delete issue in Multica | Immediate on GitLab (write-through) |
| Create / edit / close issue on GitLab | Seconds (webhook) |
| Add comment / reaction on GitLab | Seconds (webhook) |
| Add / remove label on GitLab | Seconds (webhook) |
| **Delete issue on GitLab** | **Up to 5 minutes** (reconciler sweep — webhook doesn't fire) |
| Add / remove project member on GitLab | Up to 5 minutes (reconciler) |
| Re-sync after a webhook outage | Self-heals on the next reconciler tick |

If the reconciler notices the webhook stream has gone silent for more than 15 minutes, it flips the connection to `status=error` with a message so the Settings tab can surface it.

### Connection state and failures

A workspace's GitLab connection has three possible states:

- **`connecting`** — initial sync is still running. The cache isn't fully populated yet.
- **`connected`** — healthy. Webhooks flowing, reconciler ticking.
- **`error`** — something is wrong. The Settings tab shows the error as a banner and keeps the **Disconnect** button reachable so you can reset. Common causes: service token lost Maintainer access, webhook registration 403'd at connect time, or the webhook stream went silent for 15+ minutes.

Clicking **Disconnect** removes the project webhook from GitLab (best-effort — failure is logged but doesn't block), then cascade-drops the local `workspace_gitlab_connection` row and all its cached issues/comments/reactions/labels.

## Development

For contributors working on the Multica codebase, see the [Contributing Guide](CONTRIBUTING.md).

**Prerequisites:** [Node.js](https://nodejs.org/) v20+, [pnpm](https://pnpm.io/) v10.28+, [Go](https://go.dev/) v1.26+, [Docker](https://www.docker.com/)

```bash
make dev
```

`make dev` auto-detects your environment (main checkout or worktree), creates the env file, installs dependencies, sets up the database, runs migrations, and starts all services.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development workflow, worktree support, testing, and troubleshooting.

## Star History

<a href="https://www.star-history.com/?repos=multica-ai%2Fmultica&type=date&legend=bottom-right">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=multica-ai/multica&type=date&legend=top-left" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=multica-ai/multica&type=date&legend=top-left" />
    <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=multica-ai/multica&type=date&legend=top-left" />
  </picture>
</a>
