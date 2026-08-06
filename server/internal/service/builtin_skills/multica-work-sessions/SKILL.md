---
name: multica-work-sessions
description: How to log agent work to a Multica work session through the multica MCP server — attach_session, report_activity, complete_work, and the work_session_id routing rule that keeps parallel subagents from overwriting each other. Use when the multica MCP server is connected and you are doing multi-step work on an issue, or when you are unsure which session an activity call will land on.
---

# Multica work sessions

A **work session** is the record of one agent's work on one issue, shown in the
Multica UI. It is not the same thing as a comment session (a comment thread) —
see `multica-working-on-issues` for those.

When the `multica` MCP server is connected, log activity to YOUR work session at
natural milestones. It costs few tokens and is the only thing that makes the
work visible while it is still running.

## Routing — every activity tool needs an explicit `work_session_id`

The MCP server does NOT pick the target session for you. `get_me.active_session`
is an *ambient* pointer shared across the MCP process, so parallel subagents
overwrite it. Always pass the `work_session_id` you got from `attach_session`
(or `resume_session` / `fork_session`) to `report_activity` and `complete_work`.

## Session lifecycle

1. `attach_session(issue_id)` when work starts — returns a `work_session_id`.
   **Capture and remember it.** Git context is reported automatically.
2. `report_activity(work_session_id=..., type=..., summary=...)` at milestones.
3. `complete_work(work_session_id=..., summary=...)` when done. The git diff is
   captured automatically.
4. If a run is interrupted before `complete_work`, the next MCP startup
   re-exposes the ambient session through `get_me`, so a single agent can
   resume.

## Parallel subagents (one parent, N children, one MCP process)

- Children call `attach_session(issue_id, set_active=false)` so the parent's
  `get_me.active_session` pointer survives.
- Each child uses the `work_session_id` it received — **never** read it back
  from `get_me`.
- A child completing its session does not affect the parent's session.

## When to call `report_activity`

- `decision` — after choosing between two or more approaches
- `verification` — after tests, typecheck, or build (include pass/fail)
- `blocker` — when stuck or needing user input
- `dependency` — after adding a package, creating a migration, changing infra
- `error` — after hitting and fixing an error (what failed, what fixed it)

Do NOT call it after every file edit (the diff is captured at `complete_work`),
for routine reads and searches, or for formatting-only changes.
