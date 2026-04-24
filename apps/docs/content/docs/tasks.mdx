---
title: Tasks
description: The unit of work for every agent run, with a clear state machine, timeouts, and retry rules.
---

import { Callout } from "fumadocs-ui/components/callout";
import { Mermaid } from "@/components/mermaid";

A **task** is the unit of every [agent](/agents) run — [assigning an issue to an agent](/assigning-issues), [@-mentioning an agent in a comment](/mentioning-agents), sending a message in [chat](/chat), or an [Autopilot](/autopilots) firing on schedule all produce a task. Multica puts it in a queue; a [daemon](/daemon-runtimes) picks it up and hands it off to the corresponding [AI coding tool](/providers), then writes the result back to the server when it finishes.

Tasks and [issues](/issues) are two different objects. A single issue can be assigned, @-mentioned, and manually rerun many times — each produces a **new** task.

## The states a task goes through

<Mermaid chart={`
graph LR
    Q["Queued<br/>queued"] -->|daemon picks up| D["Dispatched<br/>dispatched"]
    D -->|agent starts| R["Running<br/>running"]
    R -->|success| C["Completed<br/>completed"]
    R -->|error or timeout| F["Failed<br/>failed"]
    Q -->|user cancels| X["Cancelled<br/>cancelled"]
    D -->|user cancels| X
    R -->|user cancels| X
    F -.retryable reason.-> Q
`} />

- **Queued** — the task was just created and is waiting for a daemon to pick it up
- **Dispatched** — a daemon has claimed it and is starting the AI coding tool
- **Running** — the AI coding tool is actually doing the work
- **Completed** — finished successfully; the output (comments, code commits, status changes) is written back to the server
- **Failed** — aborted with an error or timeout; if the failure reason is retryable, the task automatically returns to `queued` for another attempt
- **Cancelled** — the user cancelled it

## What happens when a task times out

The Multica server scans every 30 seconds. Two kinds of timeout trigger a failure:

| Situation | Timeout |
|---|---|
| Dispatched but never started (daemon picked it up but didn't launch the AI tool) | **5 minutes** |
| Running too long | **2.5 hours** |

Both timeouts use failure reason `timeout` and **retry automatically** (next section). For the related runtime-missing check, see [Daemon and runtimes → When a runtime is marked offline](/daemon-runtimes#when-a-runtime-is-marked-offline).

## Which failures retry automatically, which don't

Failures fall into two categories: **retryable** and **non-retryable**.

**Retryable** (Multica automatically requeues):

- `runtime_offline` — the daemon went missing after the task was dispatched
- `runtime_recovery` — the daemon crashed and restarted, reclaiming tasks it didn't finish
- `timeout` — runtime or dispatch timeout

**Non-retryable** (the task stays in failed):

- `agent_error` — the AI coding tool itself reported an error (API error, quota exceeded, internal bug). Underlying problems aren't retried — that would loop forever.

Automatic retry also has two extra conditions:

1. **At most 2 attempts** — 1 original + 1 retry. If the retry also fails, no further retries, even if the reason is retryable.
2. **Only for issue- and chat-triggered tasks** — Autopilot-triggered tasks do **not** retry automatically.

<Callout type="warning">
**Autopilot tasks don't retry automatically** by design. An Autopilot has its own firing cadence (e.g. daily); automatic retries on failure would overlap with the next scheduled run. If you need an immediate re-run after failure, use a manual rerun (next section).
</Callout>

## Manual rerun vs. automatic retry

A **manual rerun** is one you trigger from the UI or CLI:

```bash
multica issue rerun <issue-id>
```

Behavior:

- **Cancels** the currently running task (if any)
- Creates a **brand-new** task — attempt count resets to 1, even if the original task hit the attempt ceiling
- Inherits the previous session ID; if the corresponding AI coding tool supports session resumption, the new task continues from the previous context

Comparison:

| Dimension | Automatic retry | Manual rerun |
|---|---|---|
| Trigger | System, based on failure reason | You, manually |
| Ceiling | 2 attempts | No limit |
| Applicable sources | Issues, chat | All sources |
| Session inheritance | Yes | Yes |

## How a failed task affects issue status

If an issue-triggered task fails (and no automatic retry succeeds) because the issue was assigned to an agent, **the issue's status automatically rolls back from `in_progress` to `todo`** — so when you open the board you immediately see "this one needs another look." See [Issues and projects](/issues).

## Can a task continue from the previous context

Yes — as long as the AI coding tool supports session resumption.

Multica pins the session ID **twice** during a task: once at the start (when the AI tool returns its first system message), and once at the end (on completion or failure). The first lets the daemon recover if it crashes mid-run; the second is reserved for future reruns. On the next rerun or automatic retry, that ID is passed back so the agent can pick up the previous conversation and file state.

But **which AI coding tools actually support this** varies a lot:

- ✅ **Real support** — Claude Code, Copilot, Hermes, Kimi, OpenCode, OpenClaw, Pi
- ⚠️ **Code exists but unusable** — Codex, Cursor
- ❌ **No support** — Gemini

See [Providers Matrix → Session resumption](/providers#session-resumption-who-really-supports-it).

## Next

- [Providers Matrix](/providers) — capability differences across the 10 AI coding tools (including the exact session-resumption status)
- [Assigning issues to agents](/assigning-issues) / [@-mentioning agents in comments](/mentioning-agents) / [Chat](/chat) / [Autopilots](/autopilots) — the four ways to trigger a task
