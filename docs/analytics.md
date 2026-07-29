# Product Analytics

Multica's product analytics have two homes: the **operational database**, which is
the source of truth for what happened, and **Prometheus / Grafana**, which carries
the counters. This document is the catalogue of that instrumentation.

PostHog carries error and crash monitoring only — see the
[decision record](decisions/implemented/simplification/2026-07-22-retire-posthog-for-product-analytics.md)
for why product analytics do not live there.

## Configuration

All analytics shipping is toggled by environment variables (see `.env.example`):

| Variable | Meaning | Default |
|---|---|---|
| `POSTHOG_API_KEY` | PostHog project API key. Empty = no events are shipped. | `""` |
| `POSTHOG_HOST` | PostHog host (US or EU cloud, or self-hosted URL). | `https://us.i.posthog.com` |
| `ANALYTICS_ENVIRONMENT` | Optional override for the standard `environment` event property. Normalized to `production`, `staging`, or `dev`; defaults from `APP_ENV`. | `APP_ENV` / `dev` |
| `ANALYTICS_DISABLED` | Set to `true`/`1` to force the no-op client even when `POSTHOG_API_KEY` is set. | `""` |

Local dev and self-hosted instances run with `POSTHOG_API_KEY=""`, so **no
events leave the process unless the operator explicitly opts in**.

### Self-hosted instances

Self-hosters should **never inherit a Multica-issued `POSTHOG_API_KEY`** —
that would route their users' behavior to our analytics project. The
defaults guarantee this:

- `.env.example` ships `POSTHOG_API_KEY=` empty. The Docker self-host
  compose does not set a default either.
- With the key unset, `NewFromEnv` returns `NoopClient` and logs
  `analytics: POSTHOG_API_KEY not set, using noop client` at startup — a
  visible confirmation that nothing is shipped.
- Operators who want their own analytics can set `POSTHOG_API_KEY` and
  `POSTHOG_HOST` to point at their own PostHog project (Cloud or
  self-hosted PostHog).
- The frontend receives the key via `/api/config`, so
  self-hosts' blank server config also disables frontend event shipping
  automatically — no separate frontend opt-out plumbing required.

## Architecture

```
handler → analytics.Client.Capture(Event)   ← non-blocking, returns immediately
                    │
                    ▼
           bounded queue (1024 events)
                    │
                    ▼
     background worker: batch + POST /batch/
                    │
                    ▼
                PostHog
```

- `analytics.Capture` is **never allowed to block a request handler**. A
  broken backend must not degrade the product — when the queue is full,
  events are dropped and counted (visible via `slog` + the `dropped` counter
  on shutdown).
- Batches flush either when `BatchSize` is reached or every `FlushEvery`
  (default 10 s), whichever comes first.
- `Close()` drains remaining events during graceful shutdown. Called from
  `server/cmd/server/main.go` via `defer`.

## Identity model

- **`distinct_id`** — always the user's UUID for logged-in events. The
  frontend's `posthog.identify(user.id)` merges any prior anonymous events
  under the same identity, so acquisition attribution (UTM / referrer) stays
  intact across signup.
- **`workspace_id`** — added to every event as a property when present. v1
  uses event property filtering (free tier) rather than PostHog Groups
  Analytics (paid) to compute workspace-level metrics.
- **PII** — events carry `email_domain` (e.g. `gmail.com`), not the full
  email. Full email is stored once in person properties via `$set_once` so
  it's available for individual debugging but not broadcast with every
  event.
- **Person properties (`$set`)** — use for mutable cohort signals
  (role, use_case, team_size, platform_preference) that a user can
  legitimately change during onboarding. `Event.Set` on the backend
  maps to `$set`; the frontend helper is
  `setPersonProperties()` in `@multica/core/analytics`. Use
  `$set_once` only for values that must never be overwritten (email,
  initial attribution, first-completion timestamp).

## Event catalogue

Where each family of events goes:

| Family | Events | Destination |
|---|---|---|
| `core_loop` | `workspace_created`, `agent_created`, `issue_created`, `chat_message_sent`, `issue_executed`, `autopilot_created`, `squad_created` | Prometheus |
| `onboarding_support` | `onboarding_started`, `onboarding_questionnaire_submitted`, `onboarding_completed` | Prometheus |
| `acquisition` | `signup`, `cloud_waitlist_joined`, `contact_sales_submitted` | Prometheus |
| `ops_feedback` | `feedback_submitted` | Prometheus |
| `operational` | `runtime_registered` / `ready` / `failed` / `offline`, `agent_task_*`, `autopilot_run_started` / `completed` / `failed` | Prometheus |
| Error and crash monitoring | `$exception`, `client_crash`, `client_unresponsive`, and the `$identify` / `$set` calls that attach them | PostHog (frontend only) |

Every row but the last is a Grafana counter backed by `multica_*` business metrics
in `server/internal/metrics`; the database rows those events describe remain the
source of truth for any product question. The core dashboard uses `core_loop` plus
the `onboarding_support` steps in the activation funnel; acquisition and feedback
belong on separate dashboards.

## Standard core properties

Canonical core events should carry these properties whenever the entity exists:

| Property | Type | Notes |
|---|---|---|
| `environment` | string | `production` / `staging` / `dev`; stamped by backend and frontend analytics clients. |
| `event_schema_version` | int | Current version: `2`. |
| `user_id` | string UUID | Human user ID when known. Agent/system events may omit it. |
| `workspace_id` | string UUID | Required for workspace-scoped events. |
| `agent_id` | string UUID | Required for agent/task events. |
| `task_id` | string UUID | Required for `agent_task_*` events. |
| `issue_id` / `chat_session_id` / `autopilot_run_id` | string UUID | Relevant source entity for the task/entry event. |
| `source` | string | Canonical values: `onboarding`, `manual`, `chat`, `autopilot`, `api`. UI surface details use `surface` or `trigger_source`. |
| `runtime_mode` | string | `cloud` / `local` when a runtime/agent task is involved. |
| `provider` | string | `claude`, `codex`, `cursor`, etc. when a runtime/agent task is involved. |
| `is_demo` | bool | Always `false`; the label exists so demo and test workspaces can be filtered out without a schema change. |

Task terminal events additionally carry `duration_ms`; failures carry
`failure_reason`, `error_type`, and `will_retry`. Runtime failure events carry
`recoverable`; runtime ready events carry `runtime_id`, `ready_duration_ms`
only when it is actually measured, and `daemon_id` for local runtimes.

Schema v2 is the canonical core-metrics schema: `failure_reason` is not mirrored
into `error_type`, `recoverable` applies to runtime failures rather than task or
autopilot ones, and `ready_duration_ms` is emitted only when it was actually
measured.

## Event contract

### `signup`

Fires when a new user is created. Covers both verification-code and Google
OAuth entry points (`findOrCreateUser` is the single emission site).

| Property | Type | Description |
|---|---|---|
| `email_domain` | string | Lower-cased domain portion of the user's email. |
| `signup_source` | string | Opaque attribution bundle from the frontend cookie `multica_signup_source` (UTM + referrer). Empty when the cookie is absent. |
| `auth_method` | string | Optional. `"google"` for Google OAuth signups. Absent for verification-code signups. |

`signup_source` also survives in bucketed form as the
`multica_signup_total{signup_source}` Prometheus label — see
`NormalizeSignupSource`.

### `workspace_created`

Fires after a `CreateWorkspace` transaction commits successfully.

| Property | Type | Description |
|---|---|---|
| `workspace_id` | string (UUID) | Added globally; present here for clarity. |

**Note on "first workspace" segmentation** — we deliberately do *not* stamp
an `is_first_workspace` boolean at emit time. Computing it correctly would
require an extra column or transaction-scoped logic that still races under
concurrent creates. Instead, PostHog answers the same question exactly by
looking at whether the user has a prior `workspace_created` event (use a
funnel with "first time user does X" or a cohort on
`person_properties.$initial_event`). No information is lost.

### `runtime_registered`

> **Prometheus-only — not shipped to PostHog** (see the note at the top of this
> doc). The `analytics.Event` is still constructed so `metrics.IncForEvent` can
> derive the Prometheus counter; the fields below are that **event** shape, not
> a PostHog contract. Only the low-cardinality fields (`runtime_mode`,
> `provider`) become Prometheus labels — ids like `runtime_id` / `daemon_id`
> are not labels.

Fires the first time a `(workspace_id, daemon_id, provider)` tuple is
upserted. Heartbeats and repeat registrations never re-emit. First-time
detection uses Postgres `xmax = 0` on the upsert RETURNING clause — no
extra query, no race.

| Property | Type | Description |
|---|---|---|
| `runtime_id` | string (UUID) | The newly created agent_runtime row id. |
| `daemon_id` | string | Local daemon identity when available. |
| `runtime_mode` | string | Currently `local`; reserved for cloud runtimes. |
| `provider` | string | e.g. `"codex"`, `"claude"`. |
| `runtime_version` | string | Version of the agent runtime binary. |
| `cli_version` | string | Version of the `multica` CLI that registered it. |

`distinct_id` is the authenticated owner's user id when the daemon was
registered via a member's JWT/PAT; daemon-token registrations fall back to
`workspace:<workspace_id>` so PostHog doesn't bucket unrelated daemons
under a single "anonymous" person.

### `runtime_ready`

> **Prometheus-only — not shipped to PostHog.**

Fires when a runtime is first registered in an online/ready state. This is the
activation-funnel step that should replace treating `runtime_registered` as
proof of readiness. The backend emits this only on the INSERT path for a new
`agent_runtime` row; ordinary daemon reconnects update the existing row and do
not emit another `runtime_ready`. Dashboard funnels should still count
distinct `runtime_id`.

| Property | Type | Description |
|---|---|---|
| `runtime_id` | string (UUID) | The `agent_runtime` row id. |
| `daemon_id` | string | Local daemon identity when available. |
| `ready_duration_ms` | int64 | Optional. Time from registration start to ready; omitted until the registration path can measure it. |
| `runtime_mode` | string | `local` / `cloud`. |
| `provider` | string | Runtime provider. |

### `runtime_failed`

> **Prometheus-only — not shipped to PostHog.**

Fires when runtime setup/registration fails before a ready runtime can be
recorded. Today this is scoped to backend registration persistence failures;
future setup flows should reuse it for provider detection or daemon boot
failures.

| Property | Type | Description |
|---|---|---|
| `daemon_id` | string | Local daemon identity when available. |
| `provider` | string | Runtime provider attempted. |
| `failure_reason` | string | Stable coarse reason. |
| `error_type` | string | Stable error classifier. |
| `recoverable` | bool | Whether retrying setup may succeed. |

### `runtime_offline`

> **Prometheus-only — not shipped to PostHog.**

Fires when a runtime is explicitly deregistered or the backend sweeper marks it
offline after missed heartbeats. This is not an activation step; it supports
local runtime retention and drop-off diagnosis.

### `issue_created`

Fires after an issue row is created, including manual UI/API issue creation,
quick-create issue creation by an agent, and autopilot `create_issue` runs.

| Property | Type | Description |
|---|---|---|
| `issue_id` | string (UUID) | Created issue. |
| `agent_id` | string (UUID) | Agent assignee or creating agent when applicable. |
| `task_id` | string (UUID) | Present for quick-create issue creation. |
| `autopilot_run_id` | string (UUID) | Present for autopilot-created issues. |
| `source` | string | `manual`, `api`, or `autopilot`. |

### `chat_message_sent`

Fires after a user chat message is persisted and the corresponding agent task
is queued.

| Property | Type | Description |
|---|---|---|
| `chat_session_id` | string (UUID) | Chat session. |
| `task_id` | string (UUID) | Queued agent task. |
| `agent_id` | string (UUID) | Chat agent. |
| `source` | string | Always `chat`. |

### agent task lifecycle (Prometheus-only)

> **Recorded directly to Prometheus, with no `analytics.Event`.** The agent task
> lifecycle is recorded by the typed
> `BusinessMetrics.RecordTask*` methods in `server/internal/service/task.go`.
> Names of the form (`agent_task_queued` / `dispatched` / `started` /
> `completed` / `failed` / `cancelled`) and their properties (`task_id`,
> `agent_id`, `issue_id`, `chat_session_id`, `autopilot_run_id`, `duration_ms`,
> `error_type`, `will_retry`) do not exist: those high-cardinality
> ids were never Prometheus labels and must not be used in dashboards or
> reconciliation.

The actual metrics (defined in `server/internal/metrics/business.go`; label
sets in `server/internal/metrics/labels.go`):

| Metric | Type | Labels |
|---|---|---|
| `multica_agent_task_enqueued_total` | counter | `source`, `runtime_mode` |
| `multica_agent_task_dispatched_total` | counter | `source`, `runtime_mode` |
| `multica_agent_task_started_total` | counter | `source`, `runtime_mode`, `provider` |
| `multica_agent_task_terminal_total` | counter | `source`, `runtime_mode`, `terminal_status` |
| `multica_agent_task_failed_total` | counter | `source`, `runtime_mode`, `failure_reason` |
| `multica_agent_task_queue_wait_seconds` | histogram | `source`, `runtime_mode` |
| `multica_agent_task_run_seconds` | histogram | `source`, `runtime_mode`, `terminal_status` |
| `multica_agent_task_total_seconds` | histogram | `source`, `runtime_mode`, `terminal_status` |

- `terminal_status` is the task's final `agent_task_queue.status` —
  `completed` / `failed` / `cancelled`. There is **no** separate
  completed/cancelled metric: all three land on
  `multica_agent_task_terminal_total{terminal_status=…}`. Failures
  additionally increment `multica_agent_task_failed_total` carrying the coarse
  `failure_reason` (`agent_task_queue.failure_reason`, default `agent_error`).
- Task wall-clock lives in the `*_seconds` histograms (queue wait / run /
  total), replacing the old `duration_ms` event property.
- `source` / `runtime_mode` / `provider` are the normalized label values
  (`NormalizeTaskSource` / `NormalizeRuntimeMode` / `NormalizeRuntimeProvider`).

### `autopilot_run_started` / `autopilot_run_completed` / `autopilot_run_failed`

> **Prometheus-only — not shipped to PostHog.** The `analytics.*` constructors
> are retained only so `metrics.IncForEvent` can derive the Prometheus counter;
> `analytics.IsMetricsOnly` keeps them out of PostHog. Only `cadence`,
> `trigger_kind`, and `terminal_status` become Prometheus labels — the
> `autopilot_id` / `autopilot_run_id` / `agent_id` fields below are event shape,
> not labels.

Fires from `autopilot_run` lifecycle changes. `source` is always
`autopilot`; the trigger origin is carried in `trigger_source` (`manual`,
`schedule`, `webhook`, or `api`).

| Property | Type | Description |
|---|---|---|
| `autopilot_id` | string (UUID) | Autopilot definition. |
| `autopilot_run_id` | string (UUID) | Run row. |
| `agent_id` | string (UUID) | Assigned agent. |
| `trigger_source` | string | `manual`, `schedule`, `webhook`, or `api`. |
| `duration_ms` | int64 | Terminal events only. |
| `failure_reason` | string | Failed events only. |
| `error_type` | string | Failed events only; stable coarse classifier such as `configuration`, `issue_terminal`, `dispatch_error`, `task_error`, or `autopilot_error`. |
| `will_retry` | bool | Failed events only; currently `false` because autopilot retry cadence is owned by triggers/schedules. |

### `issue_executed`

Fires **at most once per issue** — when the first task on that issue
reaches terminal `done` state. Backed by an atomic
`UPDATE issue SET first_executed_at = now() WHERE id = $1 AND first_executed_at IS NULL RETURNING *`;
retries, re-assignments, and comment-triggered follow-up tasks all hit the
WHERE clause and no-op, so the `≥1 / ≥2 / ≥5 / ≥10` funnel buckets count
distinct issues, not tasks.

| Property | Type | Description |
|---|---|---|
| `issue_id` | string (UUID) | |
| `task_id` | string (UUID) | Completing task. |
| `agent_id` | string (UUID) | Completing agent. |
| `source` | string | `manual`, `chat`, or `autopilot`. |
| `runtime_mode` | string | `local` / `cloud`. |
| `provider` | string | Runtime provider. |
| `task_duration_ms` | int64 | Wall-clock time between `task.started_at` and `task.completed_at`. Zero when the task was created in a completed state (rare). |

`distinct_id` prefers the issue's human creator so agent-executed events
flow into the issue-author's person profile (same place `signup` and
`workspace_created` land). Agent-created issues prefix with `agent:` to
keep PostHog from merging the agent into a user record.

**Note on workspace-Nth ordinals** — we deliberately do *not* stamp
`nth_issue_for_workspace` at emit time. Computing it correctly would
require either a serialised transaction or an advisory lock per workspace;
two concurrent first-completions could otherwise both read `count=1` and
emit `n=1`. PostHog answers the same question at query time via
`row_number() OVER (PARTITION BY properties.workspace_id ORDER BY timestamp)`,
and funnel steps of the form "workspace has had ≥2 `issue_executed`
events" are expressible without the property. No information is lost.

`issue_executed` is the canonical core-loop success signal, recorded like every
server event as
`multica_issue_executed_total{source}` and backed in the database by
`issue.first_executed_at`. Per-task completion counts live in Grafana via
`BusinessMetrics.RecordTaskTerminal`; use `multica_issue_executed_total` for the
activation funnel and break down by `source` as needed.

### `team_invite_sent`

Fires from `CreateInvitation` after the DB row is written.

| Property | Type | Description |
|---|---|---|
| `invited_email_domain` | string | Lower-cased domain; full email lives in the invitation row, not the event. |
| `invite_method` | string | Currently always `"email"`. Future non-email invite flows (share link, SCIM) should pass their own value. |

`distinct_id` is the inviter's user id.

### `team_invite_accepted`

Fires from `AcceptInvitation` after both the invitation row is marked
accepted and the member row is inserted in the same transaction.

| Property | Type | Description |
|---|---|---|
| `days_since_invite` | int64 | Whole days from invitation creation to acceptance. Lets us segment "accepted same day" (warm) from "dug out of email weeks later" (cold). |

`distinct_id` is the invitee's user id — this is the event that closes the
expansion funnel.

### `onboarding_started`

Fires once when the onboarding shell mounts and the initial workspace list has
resolved. Existing-workspace users carry `workspace_id`; brand-new users do
not have a workspace yet.

| Property | Type | Description |
|---|---|---|
| `workspace_id` | string (UUID) | Present only when the user already has a workspace. |
| `source` | string | Always `onboarding`. |

### `onboarding_questionnaire_submitted`

Fires on the first PatchOnboarding that transitions the user's
questionnaire JSONB from "at least one slot empty" to "all three
filled" (team_size, role, use_case). Revisions past that point don't
re-emit — the funnel counts users, not edits.

| Property | Type | Description |
|---|---|---|
| `team_size` | string | `solo` / `team` / `other`. |
| `role` | string | `developer` / `product_lead` / `writer` / `founder` / `other`. |
| `use_case` | string | `coding` / `planning` / `writing_research` / `explore` / `other`. |
| `team_size_has_other` | bool | `true` when the user filled the Q1 free-text escape. |
| `role_has_other` | bool | Ditto Q2. |
| `use_case_has_other` | bool | Ditto Q3. |

Person properties set with `$set` (not once — users can go back and
change answers before submitting again):

| Property | Type | Description |
|---|---|---|
| `team_size` | string | Mirrors the event property for cohort queries. |
| `role` | string | Same. |
| `use_case` | string | Same. |

`distinct_id` is the user's id. No workspace_id — the questionnaire is
per-user, not per-workspace.

### `agent_created`

Fires on every successful `POST /api/workspaces/:id/agents`. Not
onboarding-specific — the `is_first_agent_in_workspace` property
isolates the Step 4 signal from later agent additions.

| Property | Type | Description |
|---|---|---|
| `agent_id` | string (UUID) | |
| `provider` | string | Runtime provider the agent is bound to (`claude`, `codex`, etc). |
| `runtime_mode` | string | Runtime mode copied from the bound runtime. |
| `template` | string | Template slug used to seed the agent (`coding` / `planning` / `writing` / `assistant`). Empty when the caller didn't come from a template picker. |
| `is_first_agent_in_workspace` | bool | `true` when the workspace had zero agents before this insert. |

`distinct_id` is the authenticated owner's user id.

### `onboarding_completed`

Fires from CompleteOnboarding on the first call that actually flips
`user.onboarded_at` from NULL. Retries are idempotent server-side but
deliberately do NOT re-emit, so the funnel counts first-completions
only. The client sends `completion_path` in the POST body to label
which exit the user took.

| Property | Type | Description |
|---|---|---|
| `workspace_id` | string (UUID) | Present for workspace-linked onboarding completions. |
| `completion_path` | string | One of `full` / `runtime_skipped` / `cloud_waitlist` / `skip_existing` / `invite_accept` / `unknown`. See below. |
| `joined_cloud_waitlist` | bool | Derived from `user.cloud_waitlist_email`. Orthogonal to `completion_path` — a user may submit the waitlist form and still pick CLI. |

Person properties set with `$set_once`:

| Property | Type | Description |
|---|---|---|
| `onboarded_at` | string (RFC3339) | Timestamp the first completion landed. Enables cohort queries like "users onboarded before X" directly from person_properties. |

`completion_path` values:

- `full` — Reached Step 5 (first_issue) with a runtime connected.
- `runtime_skipped` — Completed without connecting a runtime (user hit Skip in Step 3).
- `cloud_waitlist` — Submitted the cloud waitlist form and skipped Step 3.
- `skip_existing` — "I've done this before" from Welcome. The user already had a workspace.
- `invite_accept` — Accepted at least one workspace invitation.
- `unknown` — Legacy fallback when the client didn't send a path. Should stay near zero after rollout.

### `cloud_waitlist_joined`

Fires from JoinCloudWaitlist whenever a user submits the Step 3 cloud
waitlist form. Not a completion signal — it's orthogonal to the main
funnel and used to size hosted-runtime interest.

| Property | Type | Description |
|---|---|---|
| `has_reason` | bool | Presence flag for the free-text reason field. The free text stays in the DB; we don't broadcast it. |

`distinct_id` is the user's id.

### `contact_sales_submitted`

Fires from `CreateContactSales` after the `contact_sales_inquiry` row is
inserted. The endpoint is public and unauthenticated, so the
`distinct_id` is the inquiry id (no user identity to attach to). The
free-text `goals` field stays in the DB and is never broadcast.

| Property | Type | Description |
|---|---|---|
| `inquiry_id` | string | Stable inquiry id; same as `distinct_id`. Useful for joining to operational data. |
| `company_size` | string | Closed enum from the form dropdown (`1-10`, `11-50`, `51-200`, `201-500`, `501-1000`, `1000+`). |
| `country_region` | string | Country / region label submitted from the dropdown. |
| `use_case` | string | Closed enum (`evaluate` / `adopt_team` / `self_host` / `integrate` / `partner` / `other`). |
| `has_goals` | bool | Presence flag for the free-text goals field. |

### `feedback_submitted`

Fires from `CreateFeedback` after the `feedback` row is inserted and the
hourly per-user rate-limit check has passed. Retries within the same hour
that were rate-limited (429) don't emit. The free-text message is stored
in the DB and never broadcast.

| Property | Type | Description |
|---|---|---|
| `message_length_bucket` | string | `0-100` / `100-500` / `500-2000` / `2000+` — coarse bucket of `len(message)` so we can tell "quick note" from "bug report with repro steps" without leaking content. |
| `has_images` | bool | `true` when the markdown contains at least one `![...](url)` image reference — signals bug reports with visual evidence. |
| `platform` | string | Client platform from `X-Client-Platform` header (`web` / `desktop`). Omitted when the header is absent. |
| `app_version` | string | Client version from `X-Client-Version` header. Omitted when absent. |

`distinct_id` is the submitter's user id; `workspace_id` is attached from
the modal's current-workspace context and may be empty when feedback is
sent from a pre-workspace surface.

### Frontend events

The frontend ships two things to PostHog, both of which have no database
equivalent:

- `$exception` — posthog-js autocapture, with redaction and de-duplication in
  `before_send`.
- `client_crash` / `client_unresponsive` — desktop stability events, documented
  in `packages/core/diagnostics`.

`$identify` / `$set` are retained only to attach a user identity to those.

Attribution is not an event. UTM parameters and the referrer origin are written to
the `multica_signup_source` cookie on a visitor's first page load and read by the
backend's `signup` emission, where they become the `signup_source` Prometheus
label. The cookie carries a JSON payload URL-encoded at write time
(`encodeURIComponent`) and decoded at read time (`url.QueryUnescape`). Individual
values are capped at 96 characters before serialization and the whole payload is
dropped if it still exceeds 512, so a reader sees intact JSON or nothing — never a
mid-truncated value.


## Reconciliation

Task success reconciles **database against Prometheus**: the

`BusinessMetrics.RecordTaskTerminal` counter (exported as a `multica_*` task
metric) should track the operational source of truth:

```sql
SELECT date_trunc('day', completed_at AT TIME ZONE 'UTC') AS day,
       count(*) AS db_completed_tasks
FROM agent_task_queue
WHERE status = 'completed'
  AND completed_at >= now() - interval '30 days'
GROUP BY 1
ORDER BY 1;
```

Compare against the equivalent Prometheus counter in Grafana. The expected
difference should be near zero; sustained drift means either an emission site
is missing or the metrics pipeline is unhealthy.

`issue_executed` remains the product-level success signal (at most one per
issue). It is a Prometheus counter, so reconcile
`multica_issue_executed_total` against `issue.first_executed_at` rather than a
PostHog event.

## Daily client usage and local runtime state

`client_usage_daily` is the operational source of truth for Web/Desktop usage
and Desktop built-in-provider conversion. Its primary key is
`(user_id, client_type, install_id, activity_date)`, where `activity_date` is
derived by the server in UTC. `install_id` is a random UUID stored in the Web
origin or Electron app profile and reused across restarts, upgrades, logout,
and login. Clearing/resetting that profile intentionally creates a new
installation. Web and Desktop never share an installation ID.

Clients report after authentication when that installation has no successful
report for the current UTC day, and re-check on focus/resume. Desktop updates
the same daily row after its first local-runtime probe and whenever the
same-day runtime signature changes. Reports contain only client kind/version,
a coarse OS bucket, optional current workspace context, and aggregate runtime
provider/online/offline counts. The server supplies user, date, and timestamps.
Device names, hostnames, local usernames, filesystem paths, raw user agents,
IP addresses, and raw probe errors are not stored here.

`first_active_at` and `last_active_at` are the first and latest successful
reports **within that UTC day**, not lifetime installation timestamps. Compute
historical first use with `min(first_active_at)` across the installation's daily
rows. The client's UTC day is only a best-effort request throttle; the server's
UTC date is authoritative, so clock skew near midnight can delay the next row
until a later focus/resume without assigning activity to the wrong server day.

Desktop runtime availability is deliberately a daemon-level approximation in
this MVP, not a connection test for each provider. The probe counts locally
detected **built-in provider CLIs**; workspace custom runtime profiles are not
included. Consequently, treat `runtime_count = 0` as "no built-in provider CLI
detected", not proof that the user has no custom profile. If the managed daemon
is running, all detected built-in providers are reported online, and otherwise
they are reported offline. Use `probe_result = 'error'` as unknown rather than
treating a failed probe as zero runtimes. Covering custom profiles requires a
separate inventory contract that remains available after the daemon stops; do
not infer it from this snapshot.

Use this query for a 30-day client split and user-level Desktop built-in-provider
state. It first selects the latest non-null probe for each installation, then
rolls installations up to the user so multi-device users are not double counted
and missing probes remain `unknown` rather than being classified as having no
built-in runtime:

```sql
WITH window_rows AS (
    SELECT *
    FROM client_usage_daily
    WHERE activity_date >= (CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date - 29
),
active_clients AS (
    SELECT DISTINCT user_id, client_type, install_id FROM window_rows
),
latest_desktop_probe AS (
    SELECT DISTINCT ON (user_id, install_id)
        user_id, install_id, probe_result, runtime_count, online_count
    FROM window_rows
    WHERE client_type = 'desktop' AND probe_result IS NOT NULL
    ORDER BY user_id, install_id, activity_date DESC, runtime_probed_at DESC
),
desktop_by_user AS (
    SELECT
        a.user_id,
        count(*) AS installation_count,
        count(*) FILTER (WHERE p.probe_result = 'success') AS successful_probe_count,
        coalesce(sum(p.runtime_count) FILTER (WHERE p.probe_result = 'success'), 0) AS runtime_count,
        coalesce(sum(p.online_count) FILTER (WHERE p.probe_result = 'success'), 0) AS online_count
    FROM active_clients a
    LEFT JOIN latest_desktop_probe p USING (user_id, install_id)
    WHERE a.client_type = 'desktop'
    GROUP BY a.user_id
),
desktop_state AS (
    SELECT user_id,
        CASE
            WHEN successful_probe_count < installation_count THEN 'unknown'
            WHEN runtime_count = 0 THEN 'no_builtin_runtime'
            WHEN online_count = 0 THEN 'all_builtin_runtimes_offline'
            ELSE 'builtin_runtime_available'
        END AS runtime_state
    FROM desktop_by_user
)
SELECT
    (SELECT count(DISTINCT user_id) FROM active_clients WHERE client_type = 'web') AS active_web_users,
    (SELECT count(DISTINCT user_id) FROM active_clients WHERE client_type = 'desktop') AS active_desktop_users,
    count(*) FILTER (WHERE runtime_state = 'builtin_runtime_available')
        AS desktop_users_with_builtin_runtime,
    count(*) FILTER (WHERE runtime_state = 'no_builtin_runtime')
        AS desktop_users_without_builtin_runtime,
    round(100.0 * count(*) FILTER (WHERE runtime_state = 'no_builtin_runtime')
        / nullif((SELECT count(DISTINCT user_id) FROM active_clients WHERE client_type = 'desktop'), 0), 2)
        AS desktop_users_without_builtin_runtime_pct,
    count(*) FILTER (WHERE runtime_state = 'all_builtin_runtimes_offline')
        AS desktop_users_all_builtin_runtimes_offline,
    round(100.0 * count(*) FILTER (WHERE runtime_state = 'all_builtin_runtimes_offline')
        / nullif((SELECT count(DISTINCT user_id) FROM active_clients WHERE client_type = 'desktop'), 0), 2)
        AS desktop_users_all_builtin_runtimes_offline_pct,
    count(*) FILTER (WHERE runtime_state = 'unknown') AS desktop_users_unknown
FROM desktop_state;
```

The initial retention policy is 180 UTC days. This MVP deliberately does not
add another in-process background job; operators should run
`DELETE FROM client_usage_daily WHERE activity_date < (CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date - 179`
through the existing database-maintenance schedule until a shared retention
worker exists. Deleting a workspace nulls its optional context, while deleting
a user must delete that user's daily rows in the same application transaction
because this table has no foreign keys by repository policy. There is currently
no production account hard-delete path; any future one must add that explicit
cleanup before it deletes the user row.

## Governance

Before adding, renaming, or removing any event:

1. Update this document first.
2. Update `server/internal/analytics/events.go` constants and helpers to
   match.
3. PR description must state which existing funnel / insight is affected.
