# Agent runtime map (PUL-102 PR1 discovery)

Discovery sub-task from the [PUL-102 event-driven multi-PR autonomy plan](https://github.com/rabbeet/plans/blob/main/Multica/2026-05-13-pul-102-event-driven-multi-pr-autonomy.md) (rev 3 commit `2adceac`).

Maps the existing multica server entry points that PR2–PR8 will extend, so subsequent PRs do not waste tokens rediscovering them. Conventions captured here also dictate adjustments made in PR1 relative to the plan text (the plan was written against generic Laravel-style idioms; multica is Go + sqlc + PostgreSQL).

## Stack reality

- `server/` — Go 1.26.1, Chi router (`cmd/server/router.go`), sqlc for Postgres types (`server/sqlc.yaml` → generated under `server/pkg/db/generated/`), `pgx/v5` driver, `log/slog` for structured logs.
- `server/internal/service/` — service layer (`TaskService`, `AutopilotService`, etc.).
- `server/internal/handler/` — HTTP handlers (Chi).
- `server/internal/daemon/` — daemon-side runtime (per-task worktree, task claim, skills).
- `server/migrations/` — flat numbered SQL files (`NNN_<slug>.up.sql` / `.down.sql`). Up files wrap DDL in `BEGIN; ... COMMIT;`. CHECK constraints over enum strings, **not** `CREATE TYPE … AS ENUM`.
- Tables are singular: `issue`, `agent`, `agent_task_queue`, `member`, `workspace`. Generated Go types collapse to PascalCase (`db.Issue`, `db.AgentTaskQueue`).

This deviates from the plan text in two places that PR1 reconciles:

1. The plan uses table name `issues` and pseudo-Laravel `cascade_state_enum AS ENUM`. PR1 uses `issue` and a CHECK constraint on a TEXT column, matching `issue.status` / `agent.runtime_mode` patterns already in `001_init.up.sql`.
2. The plan calls the spawn entry point `AgentRunService::spawn(issue_id, trigger_context)`. The existing entry point is **already** `TaskService.EnqueueTaskForIssue(ctx, issue, triggerCommentID...)` (`server/internal/service/task.go:104`) and it is the only path used by every caller. PR4 will add a sibling `EnqueueCascadeWebhookTask(ctx, issue, triggerCtx)` that reuses the same enqueue plumbing — no breaking rename. The plan's C2 requirement (single entry point) is already satisfied.

## Spawn entry points (C2)

`TaskService` is the single spawn surface today. Every caller goes through it:

| File | Caller | Trigger | What it passes |
|---|---|---|---|
| `server/internal/handler/comment.go:392` | comment created | user/agent posted comment that mentions an agent | `issue`, `comment.ID` |
| `server/internal/handler/issue.go:1290` | issue assignment changed to agent | first-time assignment | `issue` |
| `server/internal/handler/issue.go:1504` | issue status flip | hook-driven re-spawn | `issue` |
| `server/internal/handler/issue.go:1522` | issue status flip (alt) | hook-driven re-spawn | `issue` |
| `server/internal/handler/issue.go:1897` | issue update path | re-spawn on reassign | `issue` |
| `server/internal/handler/issue.go:1905` | issue update path | re-spawn on parent change | `issue` |
| `server/internal/handler/onboarding.go:594` | onboarding welcome | system | welcome issue |
| `server/internal/service/autopilot.go:165` | autopilot fired | scheduled trigger | `issue` |

PR4 adds one more caller: the cascade webhook background worker, which reads `cascade_retrigger.processed_at IS NULL` and dispatches to `TaskService.EnqueueCascadeWebhookTask` (new method, same enqueue path). No existing caller changes.

Variadic `triggerCommentID ...pgtype.UUID` (`task.go:104`) is the current way callers attach "why this run was spawned". PR4 promotes that to a `TriggerContext` struct carrying `{ Kind, CommentID, PRNumber, HeadSHA, EventID }` so a webhook-triggered run has full provenance and the agent CLI can read it back via `multica issue runs`.

## Task lifecycle hook surface (A2 drain hook)

`TaskService.CompleteTask` at `server/internal/service/task.go:604` is the terminal transition for a successful run. It runs inside a transaction and is the natural seam for the A2 drain hook: after the `CompleteAgentTask` query succeeds (line 607–611) and before broadcasting the completion event, PR4 inserts a call to `CascadeService.DrainPendingFor(issue_id)` that:

1. Looks up `cascade_pending_event` by `issue_id`.
2. If a row exists, calls `TaskService.EnqueueCascadeWebhookTask` with the pending trigger context.
3. Deletes the pending row.

`TaskService.FailAgentTask` and `CancelAgentTask` (sqlc-generated, `pkg/db/queries/agent.sql:249`, `:302`) need the same hook so a run that crashes mid-cascade does not strand pending events. PR4 wraps all three exit paths.

## Issue table extension (G1 + A3 + P1)

PR1 migration `072_cascade_event_driven.up.sql` adds four columns to `issue`:

- `cascade_state TEXT` with `CHECK (cascade_state IN ('approved','paused','loop_guarded','completed'))` — A3 collapse, replaces the originally-planned ENUM and label-flag mix. NULL means "this issue is not running a cascade".
- `cascade_started_at TIMESTAMPTZ` — set by `/plan-and-implement` skill at first approval.
- `cascade_last_event_at TIMESTAMPTZ` — P1 top-level column for the reconciliation cron (PR8) and dashboard (PR7). Updated every time the background worker (PR4) processes an event.
- `cascade_progress JSONB` — G1, structured per `internal/cascade.Progress` Go type (PR1). Atomic init lives in agent CLI / skill (PR5).

Existing single-PR issues stay at `cascade_state = NULL` — full regression safety.

## New tables (G2 dedup + queue depth 1)

- `cascade_retrigger` — every webhook event is persisted here on receipt (PR2/PR3 will write rows; PR4 reads them). `event_id` is `UUID UNIQUE` so re-deliveries from GitHub idempotent-no-op. `processed_at IS NULL` is the "pending" predicate, indexed by `idx_cascade_retrigger_unprocessed`.
- `cascade_pending_event` — at most one row per `issue_id` (PK on `issue_id`). Holds the trigger context for the most recent event that arrived while a run was already active. Replace-on-conflict semantics (`ON CONFLICT (issue_id) DO UPDATE`). Consumed by the A2 drain hook from `CompleteTask` / `FailAgentTask` / `CancelAgentTask` in PR4.

Both reference `issue(id) ON DELETE CASCADE` so issue deletion is cleanup-safe.

## Indexes (P1 + P3)

- `idx_cascade_active` on `issue (cascade_state, cascade_last_event_at) WHERE cascade_state IS NOT NULL` — dashboard (PR7) and reconciliation cron (PR8). Partial index keeps it small.
- `idx_cascade_started` on `issue (cascade_started_at) WHERE cascade_started_at IS NOT NULL` — chronological listing for dashboard secondary sorting.
- `idx_cascade_retrigger_loop_guard` on `cascade_retrigger (pr_url, head_sha, fired_at DESC, action)` — PR4 loop-guard COUNT query (`>= 3 distinct head_sha in last 6h`).
- `idx_cascade_retrigger_unprocessed` on `cascade_retrigger (processed_at, fired_at) WHERE processed_at IS NULL` — PR4 background worker FIFO pickup; T3 startup re-scan.

## Notification preferences surface (PR6 C5 audit)

`server/migrations/064_notification_preference.up.sql` already adds a per-user notification preference store. PR6 should extend that table rather than create a new one. Specifically the cascade-event toggles (`cascade.loop_guard_tripped`, `cascade.plan_completed`, etc.) slot in as new event types, and the Slack webhook URL / Telegram chat ID columns live on the existing settings record. No new UI infrastructure is needed if the existing notification preferences page accepts new event types — PR6 first step is to confirm that.

## Feature flag

`MULTICA_CASCADE_WEBHOOK_ENABLED` env var, default `false`. Read at `cmd/server/router.go` startup time and wired into PR2 route registration so `POST /webhooks/{source}` simply 404s when disabled. PR1 does **not** add the env wiring (no consumer exists yet) — convention is documented here so PR2 follows the same env-var pattern (`os.Getenv` + parse-bool, see e.g. `internal/handler/skill_test.go:521` for the existing pattern style).

## Out of scope for PR1

PR1 ships the dormant foundation. The following come in later PRs and intentionally do not appear here:

- HTTP route `POST /webhooks/{source}` (PR2).
- GitHub adapter, payload normalization, HMAC verification (PR3).
- Background queue worker, loop guard math, state validation, Redis cache, `TriggerContext` type, A2 drain hook wiring (PR4).
- `CLAUDE.md` cascade-execution conditional injection, atomic init query, auto-rebase (PR5).
- Slack/Telegram notification bridge (PR6).
- `/cascades` dashboard (PR7).
- Daily reconciliation cron, load test, startup re-scan, observability (PR8).

PR1 is a no-op at runtime — the new columns are unread, the new tables are unwritten — but it is the prerequisite for every later PR.

## Related

- Source plan: [`2026-05-13-pul-102-event-driven-multi-pr-autonomy.md`](https://github.com/rabbeet/plans/blob/main/Multica/2026-05-13-pul-102-event-driven-multi-pr-autonomy.md) (rev 3 `2adceac`).
- Source issue: [PUL-102](https://multica.ai/issues/PUL-102).
- Depends on: PUL-94 per-task worktree (merged) — each cascade-spawned run gets an isolated worktree.
- Adjacent: `0070_issue_status_flow_p0.up.sql` uses the same CHECK-constraint-extension pattern PR1 follows.
