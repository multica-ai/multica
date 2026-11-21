# REA behavior specification: recurring automation

Status: observed behavior, 2026-11-21

This document describes externally observable autopilot behavior for a clean-room NexAI implementation. It is a behavioral contract, not a source-code port. The implementation must be authored independently and must not copy Multica source code.

## Scope and actors

An automation binds a workspace, an agent or squad assignee, an execution mode, and one or more triggers. A squad resolves to its current leader at dispatch time. The system records each attempted dispatch as a run and eventually records its terminal result. Autopilot configuration and run inspection require an authenticated workspace member. The webhook ingress is public and uses an unguessable bearer URL, with optional provider signature verification.

## Trigger contract

| Trigger | Initiation | Admission and payload | Observed outcome |
| --- | --- | --- | --- |
| Schedule | A server scheduler checks every 30 seconds. It atomically claims enabled, due triggers belonging to active automations. | Cron expression and timezone determine the next due instant; missing timezone defaults to UTC. A claimed trigger is advanced after dispatch. A claimed trigger left without a next time is recovered by recomputing the next occurrence. | Dispatch source is `schedule`; no trigger payload is attached. A disabled trigger or inactive automation is not claimed. |
| Webhook | An HTTP request to the trigger's opaque URL. | Apply per-IP then per-token rate limits; reject bodies over 256 KiB; accept only JSON object/array; normalize event and payload; deduplicate provider delivery identifiers; validate configured signature. Invalid/missing required signature is rejected. Disabled trigger or inactive automation is recorded as ignored. | Dispatch source is `webhook`; a normalized envelope (event, event payload, receipt time, content type) is attached. Duplicate delivery returns the existing delivery reference and does not dispatch again. |
| Manual | An authenticated member calls the automation trigger endpoint in its selected workspace. | Automation must exist in that workspace and be active. | Dispatch source is `manual`; no trigger payload is attached. |

## Execution modes

Every admitted trigger enters a common dispatch operation that creates a run before attempting the work. Assignee readiness is checked before queueing. A missing/archived assignee, unresolvable squad, absent runtime, or offline runtime is an admission skip: record `skipped` with a reason and enqueue nothing. A transient readiness lookup error does not silently discard the run; dispatch proceeds to normal error handling.

### `create_issue`

Create one `todo` issue in the automation workspace, set its origin to the automation, assign it to the configured agent/squad, then enqueue issue work. Resolve squad leader at dispatch time. The run starts as `issue_created` and links to the new issue. Issue `done` or `in_review` completes the run; `blocked` or `cancelled` fails it. If the issue task fails terminally before the issue status changes, the linked run fails after retries are exhausted.

### `run_only`

Create a direct queued agent task with no issue. For a squad, execute as the leader resolved at trigger time. The run starts as `running` and links to the task. Task `completed` completes the run and stores its result; task `failed` or `cancelled` fails the run with its error when available. Optional chat delivery is outside this module's required behavior and must remain opt-in if implemented.

## State and failure contract

Run states observed at the boundary are `issue_created`, `running`, `skipped`, `completed`, and `failed` (storage may include an initial pending state). Dispatch errors mark the run failed and are returned to the caller. Successful dispatch updates the automation's last-run time and publishes a run-start event. Terminal transitions publish a run-done event. A skipped run is neither success nor failure for failure-rate calculations. Sustained real failures may pause an automation; skipped runs do not affect that ratio.

Schedule processing must be safe when multiple scheduler workers overlap: only one may claim a due trigger. If dispatch fails after claim, the next schedule occurrence must still be calculated. If the process stops after claim but before advancement, recovery must restore the next occurrence without replaying the claimed occurrence.

Webhook delivery records distinguish queued, dispatched, rejected, ignored, and failed. Invalid JSON is rejected before persistence. Signature failure is persisted as rejected and returns unauthorized. Disabled/inactive targets are persisted as ignored and return success to stop sender retries. Duplicate deliveries are idempotent. Do not log or expose webhook bearer tokens or signing secrets.

## Authorization and isolation

- Configuration, trigger management, manual triggering, and run reads are scoped to the authenticated member's selected workspace.
- Assignees and squads must belong to that workspace. Resolve squad work to its leader and verify leader readiness at execution time.
- Private squad leaders require the automation creator to retain access; deny dispatch if that access is absent.
- Webhook ingress is unauthenticated by session but must resolve its opaque token to an enabled webhook trigger and verify trigger/automation workspace consistency before dispatch.
- A webhook signing secret is write-only; never include it in read responses.

## Observable success and failure paths

1. Schedule/manual/webhook accepted + ready assignee + `create_issue` → run linked to a created issue → issue reaches `done`/`in_review` → completed.
2. Accepted trigger + ready assignee + `run_only` → run linked to queued task → task completes → completed with result.
3. Accepted trigger + unavailable assignee/leader/runtime → skipped with reason; no task or issue is created.
4. Accepted trigger + issue/task dispatch error → failed with reason; caller receives an error where the trigger is synchronous.
5. Webhook with invalid JSON/oversize/missing or invalid required signature → reject without execution (signature rejection has a persisted delivery record).
6. Repeated webhook delivery with the same provider delivery key → duplicate response; no second run.
7. Disabled trigger or paused/archived automation webhook → ignored; no run.
8. Overlapping scheduler workers → one claim and at most one dispatch for that due occurrence.

## Observation sources

Observed in the checked-out repository at commit `b244cac70`:

- `server/cmd/server/autopilot_scheduler.go`: 30-second schedule loop, claim, next-time advancement, and recovery.
- `server/pkg/db/queries/autopilot.sql`: active/enabled due-trigger claim, run creation, and failure-rate accounting.
- `server/internal/service/autopilot.go`: common dispatch, execution-mode behavior, readiness admission, and run terminal synchronization.
- `server/internal/handler/autopilot.go`: authenticated workspace-scoped configuration and manual trigger boundary.
- `server/internal/handler/autopilot_webhook.go`: public webhook validation, normalization, deduplication, signature, and delivery outcomes.
- `server/migrations/042_autopilot.up.sql`: persisted trigger, run, and execution-mode state shape.

The sources above were inspected to derive behavior. This specification intentionally contains no copied implementation code. Any discrepancy discovered during clean-room conformance work must be recorded here with evidence before changing the implementation contract.
