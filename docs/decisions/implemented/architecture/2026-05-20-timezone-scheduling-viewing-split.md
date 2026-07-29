# Decision: Timezone is either scheduling or viewing, never both

Status: implemented

## Problem

One field, `agent_runtime.timezone`, answered two unrelated questions at once. The daemon wrote the host's detected timezone into it as a statement of physical location, and the usage rollup read the same value to decide which calendar day a token belonged to. The two readings pull in opposite directions: a developer running a daemon in San Francisco cannot also give a colleague in Shanghai correct daily reports, and editing the field to fix the report forced a full re-materialization of that runtime's rollup table.

The workspace usage page had the mirror-image problem. Its timezone picker was removed because the backend aggregated by UTC `bucket_date` while the frontend drew week boundaries in the user's picked timezone, so rows near UTC midnight landed in the wrong calendar week. Removing the picker fixed the inconsistency without addressing the need: a viewer still wants their own "today", and the picker's value was `useState(browserTimezone())`, lost on every refresh.

## Decision

Timezone is modeled as exactly two product concepts, each with one field and one question.

| Concept | Question it answers | Field |
|---|---|---|
| Scheduling | Which 9 o'clock does "run at 9" mean? | `autopilot_trigger.timezone` |
| Viewing | Which calendar day is my "today"? | `"user".timezone` |

Scheduling was already correct and is unchanged. Viewing is carried by `"user".timezone`, nullable, where `NULL` means "use the browser-detected zone at render time" — so a new user gets sensible behavior with zero configuration, and one change in Settings → Preferences moves every chart in every workspace.

The data layer stores one materialization: UTC, hourly grain, in `task_usage_hourly`. Every report cuts day boundaries at read time from the caller's zone. `agent_runtime.timezone` is gone.

### How the viewing zone reaches a query

`Handler.resolveViewingTZ(r)` resolves it per request: the `?tz=` query parameter the browser sends explicitly, then the authenticated user's `"user".timezone`, then `UTC`. An invalid IANA name skips its level without erroring, because a timezone is a display concern. The handler converts `days=N` into the UTC instant of the viewer's local day boundary with `parseSinceParamInTZ` and passes `@tz` into SQL alongside it.

### The rollup table

`task_usage_hourly` keys on `(bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model)` and replaced both `task_usage_daily` (per-runtime, materialized in runtime time) and `task_usage_dashboard_daily` (workspace-level, materialized in UTC). Carrying all three entity dimensions in one key lets every existing view derive from one table.

It stores token counts, not cost. Cost is computed client-side from a per-model price table, so a pricing change never requires a re-materialization. `task_count` double-counts a task that spans hour buckets — user-facing task counts come from `agent_task_queue` instead, and the two time/tasks report queries read that table directly with the same `@tz` so all four dashboard tabs agree on where a day ends.

`task_usage_hourly_dirty` carries invalidations that the `updated_at` watermark cannot see: deletes, cascades, and re-attribution when `issue.project_id` or `agent_task_queue.runtime_id` changes. It has a seven-day TTL pruned by `prune_task_usage_hourly_dirty()`. The TTL is load-bearing — hourly grain multiplies the dirty surface roughly 24× over daily, and without pruning the queue grows without bound under sustained traffic.

Migrations 100–104 carry the change: the user column, the hourly schema, the pipeline, the removal of both legacy daily pipelines, and the drop of `agent_runtime.timezone`. `cmd/backfill_task_usage_hourly` loads history workspace by workspace.

## Alternatives considered

**Keep a third "operational" concept for where the machine physically is.** Rejected on two counts. It is a property of a *machine*, not a runtime — several runtimes on one host share one OS clock, so storing it per runtime copies a machine-level fact onto every row and invites states where two runtimes on the same box disagree. There is no machine entity in the schema, so runtime is simply the wrong level. And no reader actually wants it: the runtime detail charts want a reporting zone so their numbers reconcile with the workspace dashboard, and the hour-of-day heatmap has to follow the viewer too, or switching between the Daily and Heatmap tabs of one card shows two different "yesterdays". Autopilot scheduling reads the trigger's zone, and the daemon reads the OS clock directly. The machine's physical clock remains a fact; it just never needed to reach the server.

**Put the viewing zone on the workspace.** Rejected. Two members of one workspace in San Francisco and Beijing genuinely disagree about which day "today" is, so any workspace-level setting forces one of them to read a misaligned report. If a workspace-level *default* for new members is ever wanted, it can be added later with `"user".timezone` as the override.

**Pre-materialize a rollup per timezone.** Rejected. There are roughly 600 IANA zones, each with its own DST history to maintain. Hourly grain answers every zone from one table: a 90-day window for a busy workspace is about 15k rows and ~15ms, the same order as the daily rollup it replaced.

**Restore a per-page timezone picker on the workspace usage page.** Rejected. The picker existed only because there was no persisted concept of a viewing zone; Preferences now covers it. A page-level control also reads as view state, when the viewing zone is an application-wide property of the reader.

## Consequences

Changing a runtime's timezone no longer triggers any data-layer work, because `bucket_hour` is always UTC. That removed the migration-082 re-materialization path, the race where a query rendered against new-zone boundaries while old buckets were still being rebuilt, and the awkward window after a daemon first reports a non-UTC zone.

Cross-region teams pay for this in one place: the hour-of-day heatmap for a San Francisco runtime shows activity in the *viewer's* zone, so a Shanghai viewer does not see that machine's local 9-to-5. Single-region teams are unaffected. This was accepted deliberately as the cost of every chart on a page agreeing with every other chart.

Cost is no longer computed in SQL, so any query grouped by date also keeps `model` in its grouping for the client to price.

Anything that hardcodes "a day is 24 hours" needs testing against a DST boundary; `DATE(bucket_hour AT TIME ZONE @tz)` itself handles the 23- and 25-hour days correctly.

Issue, comment, and inbox timestamps still render in the browser's zone implicitly. Making them follow `"user".timezone` is a separate change. Whether a new autopilot trigger should default its scheduling zone to the author's viewing zone is an open product question — the two are genuinely different things, so no default was assumed.
